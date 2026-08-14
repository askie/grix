package service

import (
	"errors"
	"strings"

	"github.com/askie/grix/backend/internal/agentreceive"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SessionConvertToGroupResp 私聊转群聊的返回。
type SessionConvertToGroupResp struct {
	SessionID   string `json:"session_id"`
	SessionType int16  `json:"session_type"`
}

// SessionConvertToGroup 将一条私聊会话原地转换为群聊。
//
// 关键点：会话 ID 不变、成员不变、消息不变，仅翻转 session_type 并清掉私聊去重键、
// 写入群名。Agent 的连接与上下文均以 session_id 为键，因此对 agent 无感知。
func SessionConvertToGroup(userID int64, sessionID, groupName string) (*SessionConvertToGroupResp, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, ErrSessionNotFound
	}

	// 操作者必须是该会话的人类成员。
	var operator model.SessionMember
	if err := store.DB.Select("role").
		Where("session_id = ? AND member_id = ? AND member_type = 1", sid, userID).
		First(&operator).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionPermissionDenied
		}
		return nil, err
	}

	name := strings.TrimSpace(groupName)

	// 在事务内对会话行加锁后再校验类型并更新，避免并发/双击各自通过校验、
	// 各提交一次、给对端推两条"已转为群聊"系统提示。
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		var session model.Session
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("session_id = ? AND is_deleted = false", sid).
			First(&session).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSessionNotFound
			}
			return err
		}
		if err := ensureLoadedSessionAccessible(session); err != nil {
			return err
		}
		if session.SessionType != model.SessionTypeDirect {
			return ErrSessionInvalidType
		}

		// 用 map 更新以便把 direct_key 置为 NULL（struct 更新会跳过零值）。
		if err := tx.Model(&model.Session{}).
			Where("session_id = ?", sid).
			Updates(map[string]any{
				"session_type": model.SessionTypeGroup,
				"direct_key":   nil,
				"group_name":   name,
			}).Error; err != nil {
			return err
		}
		// 保持原 agent 的有问必答：群内自有 agent 默认仅 @ 触发，这里把会话内的
		// agent 成员置为 ModeAll，让它在转群后继续像私聊一样响应每条消息。
		if err := tx.Model(&model.SessionMember{}).
			Where("session_id = ? AND member_type = 2", sid).
			Update("agent_receive_mode", agentreceive.ModeAll).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// 失效各 ws 节点进程内的 session_type 缓存（私聊曾被缓存为 1，转群后若不失效，
	// 命中旧缓存的节点会继续把它当私聊路由）。全网广播，与在线路由无关。
	publishBroadcastEvent(protocol.InternalCmdSessionTypeInvalidate, protocol.SessionTypeInvalidatePayload{
		SessionID: sid,
	})

	// 通知所有人类成员：会话已转为群聊，各端据此刷新会话类型与系统提示。
	userIDs, err := listSessionHumanMemberIDs(sid)
	if err == nil && len(userIDs) > 0 {
		notifySessionMemberChanged(sid, "convert", userID, userIDs, sessionMemberChangedNotifyMeta{
			Title: name,
		})
	}

	return &SessionConvertToGroupResp{
		SessionID:   sid,
		SessionType: model.SessionTypeGroup,
	}, nil
}
