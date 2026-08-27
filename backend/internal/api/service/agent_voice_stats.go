package service

import (
	"context"
	"fmt"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/store"
)

const (
	// VoiceMaxConcurrentCallsDefault 语音托管默认同时接待人数。
	VoiceMaxConcurrentCallsDefault = 2
	// VoiceMaxConcurrentCallsMax 用户可配置的同时接待人数上限。
	VoiceMaxConcurrentCallsMax = 10
)

// clampVoiceMaxConcurrentCalls 把用户输入钳制到 1..VoiceMaxConcurrentCallsMax；
// 未填或 <=0 取默认值，不再允许 0=不限。
func clampVoiceMaxConcurrentCalls(v int) int {
	if v <= 0 {
		return VoiceMaxConcurrentCallsDefault
	}
	if v > VoiceMaxConcurrentCallsMax {
		return VoiceMaxConcurrentCallsMax
	}
	return v
}

// AgentVoiceStatsResp 语音托管实时状态：通话中人数 / 排队人数 / 配置上限。
type AgentVoiceStatsResp struct {
	Active        int64 `json:"active"`
	Queued        int64 `json:"queued"`
	MaxConcurrent int   `json:"max_concurrent"`
}

// AgentVoiceStats 读取 ws 层维护的 Redis 活跃集合与排队集合（key 契约见 ws/handler/call_widget.go、call_queue.go）。
func AgentVoiceStats(userID, agentID int64) (*AgentVoiceStatsResp, *errcode.ErrCode) {
	var agent model.Agent
	if err := store.DB.Select("id, owner_id, status, voice_max_concurrent_calls").First(&agent, agentID).Error; err != nil {
		return nil, &errcode.ErrAgentNotFound
	}
	if agent.OwnerID != userID {
		return nil, &errcode.ErrAgentForbidden
	}
	if agent.Status == 3 {
		return nil, &errcode.ErrAgentNotFound
	}
	resp := &AgentVoiceStatsResp{MaxConcurrent: agent.VoiceMaxConcurrentCalls}
	if store.RDB == nil {
		return resp, nil
	}
	ctx := context.Background()
	resp.Active, _ = store.RDB.SCard(ctx, fmt.Sprintf("im:voice:concurrent:%d", agentID)).Result()
	resp.Queued, _ = store.RDB.ZCard(ctx, fmt.Sprintf("im:voice:queue:%d", agentID)).Result()
	return resp, nil
}
