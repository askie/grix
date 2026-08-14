package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

const approvalCardMsgIDTTL = 48 * time.Hour

func approvalCardMsgIDKey(agentID int64, sessionID, requestID string) string {
	return fmt.Sprintf("im:agent_api:approval_card:%d:%s:%s", agentID, strings.TrimSpace(sessionID), strings.TrimSpace(requestID))
}

func saveApprovalCardMsgID(ctx context.Context, agentID int64, sessionID, requestID string, msgID int64) {
	saveApprovalCardMsgIDWithType(ctx, agentID, sessionID, requestID, msgID, "")
}

func saveApprovalCardMsgIDWithType(ctx context.Context, agentID int64, sessionID, requestID string, msgID int64, approvalType string) {
	if store.RDB == nil || agentID <= 0 || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(requestID) == "" || msgID <= 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := store.RDB.Set(ctx, approvalCardMsgIDKey(agentID, sessionID, requestID), msgID, approvalCardMsgIDTTL).Err(); err != nil {
		logger.L.Warnf("save approval card msg_id failed agent=%d session=%s request=%s msg_id=%d err=%v", agentID, sessionID, requestID, msgID, err)
	}
	if approvalType != "" {
		typeKey := approvalCardMsgIDKey(agentID, sessionID, requestID) + ":type"
		if err := store.RDB.Set(ctx, typeKey, approvalType, approvalCardMsgIDTTL).Err(); err != nil {
			logger.L.Warnf("save approval card type failed agent=%d session=%s request=%s type=%s err=%v", agentID, sessionID, requestID, approvalType, err)
		}
	}
	// Reverse index so group dispatch can find the issuing agent by (sessionID, requestID)
	// without iterating over all session members.
	SaveApprovalIssuer(ctx, agentID, sessionID, requestID)
}

func approvalIssuerKey(sessionID, requestID string) string {
	return fmt.Sprintf("im:agent_api:approval_issuer:%s:%s", strings.TrimSpace(sessionID), strings.TrimSpace(requestID))
}

// SaveApprovalIssuer 记录「该审批卡由哪个 agent 发出」，供群聊分发时把审批回传直投回发卡方。
func SaveApprovalIssuer(ctx context.Context, agentID int64, sessionID, requestID string) {
	if store.RDB == nil || agentID <= 0 || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(requestID) == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := store.RDB.Set(ctx, approvalIssuerKey(sessionID, requestID), agentID, approvalCardMsgIDTTL).Err(); err != nil {
		logger.L.Warnf("save approval issuer failed agent=%d session=%s request=%s err=%v", agentID, sessionID, requestID, err)
	}
}

// LoadApprovalIssuer returns the agentID that issued the approval card for the given
// (sessionID, requestID), or 0 if not found or expired.
func LoadApprovalIssuer(ctx context.Context, sessionID, requestID string) int64 {
	if store.RDB == nil || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(requestID) == "" {
		return 0
	}
	if ctx == nil {
		ctx = context.Background()
	}
	id, err := store.RDB.Get(ctx, approvalIssuerKey(sessionID, requestID)).Int64()
	if err != nil {
		return 0
	}
	return id
}

func loadApprovalCardType(ctx context.Context, agentID int64, sessionID, requestID string) string {
	if store.RDB == nil || agentID <= 0 || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(requestID) == "" {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := store.RDB.Get(ctx, approvalCardMsgIDKey(agentID, sessionID, requestID)+":type").Result()
	if err != nil {
		return ""
	}
	return result
}

func loadApprovalCardMsgID(ctx context.Context, agentID int64, sessionID, requestID string) int64 {
	if store.RDB == nil || agentID <= 0 || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(requestID) == "" {
		return 0
	}
	if ctx == nil {
		ctx = context.Background()
	}
	msgID, err := store.RDB.Get(ctx, approvalCardMsgIDKey(agentID, sessionID, requestID)).Int64()
	if err != nil {
		return 0
	}
	return msgID
}

func deleteApprovalCardMsgID(ctx context.Context, agentID int64, sessionID, requestID string) {
	if store.RDB == nil || agentID <= 0 || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(requestID) == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := store.RDB.Del(ctx, approvalCardMsgIDKey(agentID, sessionID, requestID)).Err(); err != nil {
		logger.L.Warnf("delete approval card msg_id failed agent=%d session=%s request=%s err=%v", agentID, sessionID, requestID, err)
	}
}

// extractApprovalIDFromCard extracts the approval_id from a send_msg content/extra
// that contains an exec_approval card. Returns empty string if not an approval card.
func extractApprovalIDFromCard(content string, extra json.RawMessage) string {
	if !strings.Contains(content, "exec_approval") && !strings.Contains(string(extra), "exec_approval") {
		return ""
	}

	// Try extracting from grix://card URI in content
	if idx := strings.Index(content, "grix://card/exec_approval"); idx >= 0 {
		uriStart := idx
		uriEnd := strings.IndexByte(content[uriStart:], ')')
		if uriEnd < 0 {
			uriEnd = len(content) - uriStart
		}
		uriStr := content[uriStart : uriStart+uriEnd]
		if parsed, err := url.Parse(uriStr); err == nil {
			if id := parsed.Query().Get("approval_id"); id != "" {
				return id
			}
			if id := parsed.Query().Get("approval_command_id"); id != "" {
				return id
			}
			// Complex payloads encode all data into the "d" JSON parameter.
			if d := parsed.Query().Get("d"); d != "" {
				var data map[string]any
				if json.Unmarshal([]byte(d), &data) == nil {
					if id, _ := data["approval_id"].(string); strings.TrimSpace(id) != "" {
						return strings.TrimSpace(id)
					}
					if id, _ := data["approval_command_id"].(string); strings.TrimSpace(id) != "" {
						return strings.TrimSpace(id)
					}
				}
			}
		}
	}

	// Fallback: extract from biz_card in extra
	var envelope struct {
		BizCard struct {
			Type    string `json:"type"`
			Payload struct {
				ApprovalID        string `json:"approval_id"`
				ApprovalCommandID string `json:"approval_command_id"`
			} `json:"payload"`
		} `json:"biz_card"`
	}
	if err := json.Unmarshal(extra, &envelope); err == nil && envelope.BizCard.Type == "exec_approval" {
		if id := strings.TrimSpace(envelope.BizCard.Payload.ApprovalID); id != "" {
			return id
		}
		if id := strings.TrimSpace(envelope.BizCard.Payload.ApprovalCommandID); id != "" {
			return id
		}
	}

	return ""
}
