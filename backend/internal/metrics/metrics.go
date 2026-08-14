// Package metrics 提供全平台共享的 Prometheus 指标注册表与暴露端点。
//
// 设计目标：在容器数量/CPU 之外，补齐真正反映服务负载天花板的指标——
// WS 并发连接数、HTTP QPS 与延迟、数据库连接池占用、进程级 CPU/内存/fd/goroutine。
// 各服务通过 Handler() 暴露 /metrics，由各集群的 VictoriaMetrics 抓取。
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry 是全局指标注册表。各服务通过 Handler() 暴露 /metrics。
var Registry = prometheus.NewRegistry()

var (
	// HTTPRequestsTotal 按服务/方法/路由/状态码累计 HTTP 请求数（API QPS 来源）。
	HTTPRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "grix",
		Subsystem: "http",
		Name:      "requests_total",
		Help:      "Total HTTP requests handled, by service/method/route/status.",
	}, []string{"service", "method", "route", "status"})

	// HTTPRequestDuration HTTP 请求耗时分布（p99 延迟来源）。
	HTTPRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "grix",
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "HTTP request latency distribution in seconds.",
		Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"service", "method", "route"})

	// WSActiveConnections 本节点当前活跃 WS 连接数，按类型区分（human/agent）。
	// WS 真实负载天花板（并发连接数）的核心指标——CPU 低不代表连接没满。
	WSActiveConnections = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "grix",
		Subsystem: "ws",
		Name:      "active_connections",
		Help:      "Current active WS connections on this node, by type (human/agent).",
	}, []string{"type"})
)

func init() {
	Registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		HTTPRequestsTotal,
		HTTPRequestDuration,
		WSActiveConnections,
		poolCollector,
	)
}

// Handler 返回 /metrics 处理器，输出 Prometheus 文本格式。
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{})
}
