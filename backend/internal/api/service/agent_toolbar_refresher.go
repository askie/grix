package service

import (
	"context"
	"sync"
)

// AgentToolbarRefresher 让 service 层能重建某个 agent 的工具栏快照并推给在线客户端，
// 同时避免 service -> agenttoolbar -> agenttoolbar/resolver -> service 的导入环。
// 实现由 ws 层在启动时注入（见 ws.Server.initAgentToolbarService）。
type AgentToolbarRefresher interface {
	RefreshByAgent(ctx context.Context, ownerID, agentID int64, reason string) error
}

var agentToolbarRefresherState struct {
	mu        sync.RWMutex
	refresher AgentToolbarRefresher
}

// SetAgentToolbarRefresher 安装（或传 nil 卸载）工具栏刷新入口。
func SetAgentToolbarRefresher(refresher AgentToolbarRefresher) {
	agentToolbarRefresherState.mu.Lock()
	defer agentToolbarRefresherState.mu.Unlock()
	agentToolbarRefresherState.refresher = refresher
}

func getAgentToolbarRefresher() AgentToolbarRefresher {
	agentToolbarRefresherState.mu.RLock()
	defer agentToolbarRefresherState.mu.RUnlock()
	return agentToolbarRefresherState.refresher
}

// refreshAgentToolbar 尽力刷新；未注入或刷新失败都不影响主流程（下一次拉快照会自愈）。
func refreshAgentToolbar(ownerID, agentID int64, reason string) {
	refresher := getAgentToolbarRefresher()
	if refresher == nil || ownerID <= 0 || agentID <= 0 {
		return
	}
	_ = refresher.RefreshByAgent(context.Background(), ownerID, agentID, reason)
}
