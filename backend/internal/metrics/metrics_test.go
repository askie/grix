package metrics

import (
	"database/sql"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandlerExposesMetrics 验证 /metrics 端点真实输出我们关心的负载指标，
// 涵盖：WS 连接数、HTTP 请求计数/延迟、DB 连接池、进程级 Go runtime。
func TestHandlerExposesMetrics(t *testing.T) {
	WSActiveConnections.WithLabelValues("human").Set(42)
	WSActiveConnections.WithLabelValues("agent").Set(7)
	HTTPRequestsTotal.WithLabelValues("api", "GET", "/v1/ping", "200").Inc()
	HTTPRequestDuration.WithLabelValues("api", "GET", "/v1/ping").Observe(0.012)
	RegisterDBPool("primary", func() sql.DBStats {
		return sql.DBStats{OpenConnections: 5, InUse: 3, Idle: 2, MaxOpenConnections: 20}
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	Handler().ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	out := string(body)

	wants := []string{
		`grix_ws_active_connections{type="human"} 42`,
		`grix_ws_active_connections{type="agent"} 7`,
		`grix_http_requests_total{method="GET",route="/v1/ping",service="api",status="200"} 1`,
		`grix_http_request_duration_seconds_bucket`,
		`grix_db_pool_in_use_connections{db="primary"} 3`,
		`grix_db_pool_max_open_connections{db="primary"} 20`,
		`go_goroutines`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("metrics output missing %q", w)
		}
	}
}
