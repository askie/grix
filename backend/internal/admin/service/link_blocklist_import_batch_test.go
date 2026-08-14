package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

// TestImportCSVBatchPerf 验证批量导入：
//  1. 17000 行规模秒级完成（防 HTTP 超时）。
//  2. 第二次导入相同内容 → created=0, skipped=N。
//  3. failures 列表准确报错。
//  4. 批内重复正确合并到 skipped。
func TestImportCSVBatchPerf(t *testing.T) {
	if err := snowflake.Init(2); err != nil {
		t.Fatalf("snowflake init: %v", err)
	}
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	store.DB = tdb.DB

	// 构造 10000 条合法 domain + 100 条批内重复 + 5 条格式错（kind 不存在）。
	var b strings.Builder
	b.WriteString("# header comment line\n")
	const total = 10000
	for i := 0; i < total; i++ {
		b.WriteString("domain,bad")
		b.WriteString(fmtInt(i))
		b.WriteString(".example.com,malicious,test,true,note\n")
	}
	// 批内重复（与上面前 100 条同 value）
	for i := 0; i < 100; i++ {
		b.WriteString("domain,bad")
		b.WriteString(fmtInt(i))
		b.WriteString(".example.com,malicious,test,true,dupe\n")
	}
	// 5 行格式错（kind 非法）
	for i := 0; i < 5; i++ {
		b.WriteString("badkind,xxx")
		b.WriteString(fmtInt(i))
		b.WriteString(".com,malicious,test,true,\n")
	}

	start := time.Now()
	res, err := ImportLinkBlocklistRulesCSV(context.Background(), 1, b.String(), "127.0.0.1", "test")
	dur := time.Since(start)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	t.Logf("imported %d rules in %s (created=%d skipped=%d failures=%d)",
		total, dur, res.Created, res.Skipped, len(res.Failures))

	if res.Created != total {
		t.Errorf("created=%d want=%d", res.Created, total)
	}
	if res.Skipped != 100 {
		t.Errorf("skipped=%d want=100 (batch-internal duplicates)", res.Skipped)
	}
	if len(res.Failures) != 5 {
		t.Errorf("failures=%d want=5", len(res.Failures))
	}
	if dur > 10*time.Second {
		t.Errorf("import too slow: %s — should be < 10s for 10k rows", dur)
	}

	// 二次导入相同内容 → 全部 skipped
	res2, err := ImportLinkBlocklistRulesCSV(context.Background(), 1, b.String(), "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("import 2nd: %v", err)
	}
	t.Logf("2nd import: created=%d skipped=%d failures=%d", res2.Created, res2.Skipped, len(res2.Failures))
	if res2.Created != 0 {
		t.Errorf("2nd run created=%d want=0", res2.Created)
	}
	if res2.Skipped != total+100 {
		t.Errorf("2nd run skipped=%d want=%d", res2.Skipped, total+100)
	}
	if len(res2.Failures) != 5 {
		t.Errorf("2nd run failures=%d want=5", len(res2.Failures))
	}
}

func fmtInt(n int) string {
	if n == 0 {
		return "0"
	}
	digits := make([]byte, 0, 10)
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
