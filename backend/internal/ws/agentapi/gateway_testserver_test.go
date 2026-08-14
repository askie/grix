package agentapi

import (
	"net/http"
	"net/http/httptest"
)

// newAgentWSTestServer 起一条 agent WS 网关的测试服务，并返回收尾函数。
//
// 收尾顺序是关键：必须先 mgr.Shutdown()（断开在服务的连接、停超时定时器、
// 等后台协程退出）再关 HTTP 服务。httptest 的 Close 不会等被劫持的 WS 连接，
// 少了这一步，ServeWS 及其派生协程会活到测试之后，去读已经被关掉的测试 DB
// （报 "sql: database is closed"）、并与下一个测试重置全局 store.DB/logger 抢跑。
//
// 调用方必须用 defer 调用返回的收尾函数（而不是 t.Cleanup）：
// t.Cleanup 晚于测试函数里的 defer testDB.Close() 执行，那就来不及了。
func newAgentWSTestServer(mgr *Manager) (*httptest.Server, func()) {
	srv := httptest.NewServer(http.HandlerFunc(mgr.ServeWS))
	return srv, func() {
		mgr.Shutdown()
		srv.Close()
	}
}
