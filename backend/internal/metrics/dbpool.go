package metrics

import (
	"database/sql"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// dbPoolCollector 在每次抓取时实时读取已注册数据库连接池的 sql.DBStats，
// 暴露连接池占用情况。in_use 接近 max_open 即意味着请求开始排队——API 的隐藏瓶颈。
type dbPoolCollector struct {
	mu    sync.RWMutex
	stats map[string]func() sql.DBStats

	openDesc      *prometheus.Desc
	inUseDesc     *prometheus.Desc
	idleDesc      *prometheus.Desc
	maxOpenDesc   *prometheus.Desc
	waitCountDesc *prometheus.Desc
}

var poolCollector = newDBPoolCollector()

func newDBPoolCollector() *dbPoolCollector {
	label := []string{"db"}
	return &dbPoolCollector{
		stats:         map[string]func() sql.DBStats{},
		openDesc:      prometheus.NewDesc("grix_db_pool_open_connections", "Open connections to the database.", label, nil),
		inUseDesc:     prometheus.NewDesc("grix_db_pool_in_use_connections", "Connections currently in use.", label, nil),
		idleDesc:      prometheus.NewDesc("grix_db_pool_idle_connections", "Idle connections in the pool.", label, nil),
		maxOpenDesc:   prometheus.NewDesc("grix_db_pool_max_open_connections", "Configured max open connections.", label, nil),
		waitCountDesc: prometheus.NewDesc("grix_db_pool_wait_count_total", "Total number of connections waited for.", label, nil),
	}
}

// RegisterDBPool 注册一个数据库连接池的统计来源。name 用于区分 primary/read。
func RegisterDBPool(name string, statsFn func() sql.DBStats) {
	if statsFn == nil {
		return
	}
	poolCollector.mu.Lock()
	defer poolCollector.mu.Unlock()
	poolCollector.stats[name] = statsFn
}

func (c *dbPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.openDesc
	ch <- c.inUseDesc
	ch <- c.idleDesc
	ch <- c.maxOpenDesc
	ch <- c.waitCountDesc
}

func (c *dbPoolCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for name, fn := range c.stats {
		s := fn()
		ch <- prometheus.MustNewConstMetric(c.openDesc, prometheus.GaugeValue, float64(s.OpenConnections), name)
		ch <- prometheus.MustNewConstMetric(c.inUseDesc, prometheus.GaugeValue, float64(s.InUse), name)
		ch <- prometheus.MustNewConstMetric(c.idleDesc, prometheus.GaugeValue, float64(s.Idle), name)
		ch <- prometheus.MustNewConstMetric(c.maxOpenDesc, prometheus.GaugeValue, float64(s.MaxOpenConnections), name)
		ch <- prometheus.MustNewConstMetric(c.waitCountDesc, prometheus.CounterValue, float64(s.WaitCount), name)
	}
}
