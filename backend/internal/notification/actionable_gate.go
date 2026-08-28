package notification

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/store"
)

// actionable_gate.go 决定一条可回调事件（审批 / 提问）在派发时是否还值得推送。
//
// 审批推送是 ForcePush（用户在线也推），所以必须在推送前自己把关：
//   - 主人已经在 App 里裁决过的审批 / 已回答的提问不再推；
//   - 事件在 NATS 里积压（push 服务重启、重投）太久后不再推。
// ws 侧在收到主人裁决的那一刻调用 MarkActionableResolved 打标记，push 侧
// 派发前用 ActionableSkipReason 判定。

const (
	// actionableResolvedTTL 已裁决标记的保留时间，覆盖 NATS 重投和积压窗口即可。
	actionableResolvedTTL = 24 * time.Hour
	// actionableMaxAge 超过此年龄的审批 / 提问事件不再推送：卡片早已在 App 里
	// 处理或过期，迟到的推送只会打扰人。
	actionableMaxAge = 10 * time.Minute
)

func actionableResolvedKey(eventKey, sessionID, refID string) string {
	return fmt.Sprintf("notify:resolved:%s:%s:%s", eventKey, strings.TrimSpace(sessionID), strings.TrimSpace(refID))
}

// MarkActionableResolved 记录主人已对某条审批（approval_command_id）或提问
// （question_id）作出裁决。幂等，重复调用只刷新 TTL。
func MarkActionableResolved(ctx context.Context, eventKey, sessionID, refID string) {
	if store.RDB == nil || eventKey == "" || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(refID) == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	_ = store.RDB.Set(ctx, actionableResolvedKey(eventKey, sessionID, refID), "1", actionableResolvedTTL).Err()
}

// MarkApprovalResolved / MarkQuestionResolved 是 ws 侧的便捷入口。
func MarkApprovalResolved(ctx context.Context, sessionID, approvalCommandID string) {
	MarkActionableResolved(ctx, EventApprovalRequested, sessionID, approvalCommandID)
}

func MarkQuestionResolved(ctx context.Context, sessionID, questionID string) {
	MarkActionableResolved(ctx, EventAgentQuestion, sessionID, questionID)
}

func isActionableResolved(ctx context.Context, eventKey, sessionID, refID string) bool {
	if store.RDB == nil || strings.TrimSpace(refID) == "" {
		return false
	}
	n, err := store.RDB.Exists(ctx, actionableResolvedKey(eventKey, sessionID, refID)).Result()
	return err == nil && n > 0
}

// actionableRefID 返回事件对应的裁决引用（审批命令 ID / 提问 ID）。
func actionableRefID(evt *AgentNotificationEvent) string {
	if evt == nil || evt.ActionMeta == nil {
		return ""
	}
	switch evt.EventKey {
	case EventApprovalRequested:
		return evt.ActionMeta.ApprovalCommandID
	case EventAgentQuestion:
		return evt.ActionMeta.QuestionID
	}
	return ""
}

// ActionableSkipReason 返回非空原因表示该事件不应再推送。只对审批 / 提问事件
// 生效，其他事件一律放行。now 由调用方传入便于测试。
func ActionableSkipReason(ctx context.Context, evt *AgentNotificationEvent, now time.Time) string {
	if evt == nil || (evt.EventKey != EventApprovalRequested && evt.EventKey != EventAgentQuestion) {
		return ""
	}
	if evt.CreatedAtMs > 0 {
		if age := now.Sub(time.UnixMilli(evt.CreatedAtMs)); age > actionableMaxAge {
			return "stale age=" + age.Truncate(time.Second).String()
		}
	}
	if isActionableResolved(ctx, evt.EventKey, evt.SessionID, actionableRefID(evt)) {
		return "already resolved"
	}
	return ""
}
