package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/agentreceive"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
)

// 托管态下访客消息必须真正驱动主人的 agent（record_and_process），不得被访问门禁降级成
// record_only 并回一张"仅限主人私聊"的提示卡：托管由主人在本会话内主动开启，被驱动的必然是
// 主人自己的 agent，访客是在跟主人说话而非私聊 agent。门禁只管 direct_session_route 那条路径。
func TestCheckDelegatesVisitorNotBlockedByAccessGate(t *testing.T) {
	cases := []struct {
		name          string
		sessionType   int16
		sessionID     string
		receiveMode   int16
		wantEventType string
	}{
		{
			name:          "private_visitor_drives_delegated_agent",
			sessionType:   1,
			sessionID:     "session-delegate-gate-private",
			receiveMode:   agentreceive.ModeAll,
			wantEventType: "user_chat",
		},
		{
			name:          "group_visitor_mentioning_owner_drives_delegated_agent",
			sessionType:   2,
			sessionID:     "session-delegate-gate-group",
			receiveMode:   agentreceive.ModeMentionOnly,
			wantEventType: "group_mention",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupSendMsgTest(t)
			defer cleanup()

			const (
				ownerID   = int64(8801)
				visitorID = int64(8802)
				agentID   = int64(9971)
			)
			prevManager := wsagentapi.GetGlobal()
			wsagentapi.SetGlobal(nil)
			defer wsagentapi.SetGlobal(prevManager)

			// Claude client_type 让完整门禁（含群聊配对审批）无条件启用——覆盖最严格的情况。
			if err := store.DB.Create(&model.Agent{
				ID:              agentID,
				AgentName:       "delegate-gate-agent",
				OwnerID:         ownerID,
				ProviderType:    model.AgentProviderAPI,
				AgentClientType: model.AgentClientTypeClaude,
				Status:          model.AgentStatusActive,
			}).Error; err != nil {
				t.Fatalf("create agent error: %v", err)
			}
			if err := store.DB.Create(&model.Session{
				SessionID:   tc.sessionID,
				OwnerID:     ownerID,
				SessionType: tc.sessionType,
			}).Error; err != nil {
				t.Fatalf("create session error: %v", err)
			}
			now := time.Now().UTC()
			for _, m := range []model.SessionMember{
				{
					SessionID:                tc.sessionID,
					MemberID:                 ownerID,
					MemberType:               1,
					AgentReceiveMode:         tc.receiveMode,
					AgentReceiveBacklogCount: 2,
					JoinedAt:                 now,
					LastActiveAt:             now,
				},
				{
					SessionID:                tc.sessionID,
					MemberID:                 visitorID,
					MemberType:               1,
					AgentReceiveMode:         tc.receiveMode,
					AgentReceiveBacklogCount: 2,
					JoinedAt:                 now,
					LastActiveAt:             now,
				},
			} {
				if err := store.DB.Create(&m).Error; err != nil {
					t.Fatalf("create session member error: %v", err)
				}
			}

			ctx := context.Background()
			if err := store.RDB.HSet(ctx, fmt.Sprintf("im:delegate:%s:%d", tc.sessionID, ownerID),
				"agent_id", fmt.Sprintf("%d", agentID),
				"max_consecutive_replies", "3",
			).Err(); err != nil {
				t.Fatalf("seed delegate key error: %v", err)
			}

			content := "帮我看看这个"
			if tc.sessionType == 2 {
				content = fmt.Sprintf("@%d 帮我看看这个", ownerID)
			}

			hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{}}
			checkDelegates(
				hub,
				ctx,
				tc.sessionID,
				visitorID,
				1,
				tc.sessionType,
				int64(776001),
				0,
				1,
				content,
				nil,
				delegateTriggerMeta{},
				nil,
				nil,
				false,
			)

			queuedRaw, err := store.RDB.LRange(ctx, fmt.Sprintf("im:agent_api:queued_events:%d", agentID), 0, -1).Result()
			if err != nil {
				t.Fatalf("load queued delegate events error: %v", err)
			}
			if len(queuedRaw) != 1 {
				t.Fatalf("queued delegate events count=%d want=1", len(queuedRaw))
			}
			var queuedEvent wsagentapi.DelegateEventPayload
			if err := json.Unmarshal([]byte(queuedRaw[0]), &queuedEvent); err != nil {
				t.Fatalf("unmarshal queued delegate event error: %v", err)
			}
			// record_only 说明门禁把访客消息降级成了"只记录不驱动"——正是本次修复的 bug。
			if queuedEvent.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
				t.Fatalf("queued mirror_mode=%q want=%q", queuedEvent.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
			}
			if queuedEvent.EventType != tc.wantEventType {
				t.Fatalf("queued event_type=%q want=%q", queuedEvent.EventType, tc.wantEventType)
			}
			if queuedEvent.OwnerID != ownerID {
				t.Fatalf("queued owner_id=%d want=%d", queuedEvent.OwnerID, ownerID)
			}
			if queuedEvent.SenderID != visitorID {
				t.Fatalf("queued sender_id=%d want=%d", queuedEvent.SenderID, visitorID)
			}

			// 托管路径不得留下任何门禁副作用：不发"仅限主人私聊"提示卡、不落群聊配对待批记录。
			var noticeCount int64
			if err := store.DB.Model(&model.Message{}).
				Where("session_id = ? AND content LIKE ?", tc.sessionID, "%agent_status%").
				Count(&noticeCount).Error; err != nil {
				t.Fatalf("count access notice messages error: %v", err)
			}
			if noticeCount != 0 {
				t.Fatalf("access gate notice messages=%d want=0", noticeCount)
			}
			pendingKeys, err := store.RDB.Keys(ctx, fmt.Sprintf("*%d*pending*", agentID)).Result()
			if err != nil {
				t.Fatalf("scan pending keys error: %v", err)
			}
			if len(pendingKeys) != 0 {
				t.Fatalf("access gate pending records=%v want none", pendingKeys)
			}
		})
	}
}
