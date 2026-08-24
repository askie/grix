package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

// 就绪探针的价值全在"依赖断了要能被发现"。k8s 拿它决定是否把本实例放进 Service
// 端点，探不出故障的探针等于没有探针——这正是它此前返回静态 200 的问题。
func TestReadyz(t *testing.T) {
	gin.SetMode(gin.TestMode)

	call := func(t *testing.T) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/readyz", nil)
		readyz(c)
		return w
	}

	t.Run("数据库可用时就绪", func(t *testing.T) {
		testDB := testutil.NewTestDB()
		t.Cleanup(testDB.Close)

		original := store.DB
		store.DB = testDB.DB
		t.Cleanup(func() { store.DB = original })

		if got := call(t).Code; got != http.StatusOK {
			t.Fatalf("数据库可用时应返回 200，实际 %d", got)
		}
	})

	// 数据库连不上时网关一个请求也服务不了，必须报未就绪让 k8s 摘掉本实例。
	t.Run("数据库不可用时未就绪", func(t *testing.T) {
		original := store.DB
		store.DB = nil
		t.Cleanup(func() { store.DB = original })

		if got := call(t).Code; got != http.StatusServiceUnavailable {
			t.Fatalf("数据库不可用时应返回 503，实际 %d", got)
		}
	})

	// 网关从不初始化 Redis（那份缓存只是加速），store.RDB 恒为 nil。把 Redis 计入就绪
	// 会让它永远 not ready、滚动发布再也起不来，这里钉住"不受 Redis 缺席影响"。
	t.Run("Redis 缺席不影响就绪", func(t *testing.T) {
		testDB := testutil.NewTestDB()
		t.Cleanup(testDB.Close)

		originalDB, originalRDB := store.DB, store.RDB
		store.DB, store.RDB = testDB.DB, nil
		t.Cleanup(func() { store.DB, store.RDB = originalDB, originalRDB })

		if got := call(t).Code; got != http.StatusOK {
			t.Fatalf("Redis 未初始化时仍应就绪，实际 %d", got)
		}
	})
}
