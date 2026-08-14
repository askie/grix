package agentapi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	tooli18n "github.com/askie/grix/backend/internal/agenttoolbar/i18n"
)

// TestCardTextKeysExist 扫描本包所有非测试源码里对 tooli18n.T / tooli18n.Tf 的
// 调用，字面量 key 参数必须能在 card_text.go 的模板表里查到。T()/Tf() 对不存在
// 的 key 是静默返回空串，编译和运行都不会报错，卡片会悄悄变成空白——这类 bug
// 只能靠这种"引用即校验"的守卫测试兜住。
func TestCardTextKeysExist(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}

	fset := token.NewFileSet()
	var missing []string
	seen := map[string]bool{}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" {
			continue
		}
		if len(name) > len("_test.go") && name[len(name)-len("_test.go"):] == "_test.go" {
			continue
		}

		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkgIdent, ok := sel.X.(*ast.Ident)
			if !ok || pkgIdent.Name != "tooli18n" {
				return true
			}
			if sel.Sel.Name != "T" && sel.Sel.Name != "Tf" {
				return true
			}
			if len(call.Args) < 2 {
				return true
			}
			lit, ok := call.Args[1].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				// 非字面量 key（变量拼出来的），静态扫描覆盖不到，跳过。
				return true
			}
			key, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if seen[name+"|"+key] {
				return true
			}
			seen[name+"|"+key] = true
			if !tooli18n.HasKey(key) {
				missing = append(missing, name+": "+key)
			}
			return true
		})
	}

	if len(missing) > 0 {
		t.Fatalf("以下 tooli18n.T/Tf 调用引用了不存在的 key（会静默渲染成空白文案）：\n%v", missing)
	}
}
