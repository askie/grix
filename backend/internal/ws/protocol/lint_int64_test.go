package protocol

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"
)

// TestProtocolInt64FieldsAreStringTagged 是 Phase 4.3 守护机制：
// packet.go 中所有 int64 字段必须带 `json:",string"` 标签,
// 防止接入方在 JS / Flutter Web 等环境因精度丢失而出错。
//
// 白名单字段（保留 number 形式）：
//   - chunk_seq / last_chunk_seq —— 连续小整数,不存在精度问题。
//   - 时间戳类字段 *_at / *_time / ttl_ms / timeout_ms 等。
//   - unread_count / event_seq 等数值范围小且不作为对外 ID。
//
// 触发：协议新增 int64 字段但忘记打 ,string 时此测试失败,
// 充当编译期之外的协议规约守护。
func TestProtocolInt64FieldsAreStringTagged(t *testing.T) {
	allowNumber := map[string]bool{
		"chunk_seq":           true,
		"last_chunk_seq":      true,
		"created_at":          true,
		"updated_at":          true,
		"deleted_at":          true,
		"received_at":         true,
		"resolved_at":         true,
		"requested_at":        true,
		"expires_at":          true,
		"unread_count":        true,
		"event_seq":           true,
		"server_time":         true,
		"ts":                  true,
		"ttl_ms":              true,
		"timeout_ms":          true,
		"push_ack_timeout_ms": true,
		// 协议层连续递增计数,非 snowflake 业务 ID
		"seq":              true,
		"expires_in":       true,
		"revision":         true,
		"current_revision": true,
		// 分页总数,计数值非 snowflake 业务 ID(同结构 page/page_size 亦为普通数字)
		"total": true,
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "packet.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse packet.go: %v", err)
	}

	var violations []string
	ast.Inspect(file, func(n ast.Node) bool {
		st, ok := n.(*ast.StructType)
		if !ok || st.Fields == nil {
			return true
		}
		for _, f := range st.Fields.List {
			if f.Tag == nil {
				continue
			}
			ident, ok := f.Type.(*ast.Ident)
			if !ok || ident.Name != "int64" {
				continue
			}
			tag := strings.Trim(f.Tag.Value, "`")
			stag := reflect.StructTag(tag).Get("json")
			name := strings.SplitN(stag, ",", 2)[0]
			if name == "" || name == "-" {
				continue
			}
			if allowNumber[name] {
				continue
			}
			if !strings.Contains(stag, ",string") {
				names := []string{}
				for _, nIdent := range f.Names {
					names = append(names, nIdent.Name)
				}
				violations = append(violations, strings.Join(names, "/")+" json="+stag)
			}
		}
		return true
	})

	if len(violations) > 0 {
		t.Errorf("int64 fields missing `,string` tag (add ,string or whitelist by name): %s",
			strings.Join(violations, "; "))
	}
}
