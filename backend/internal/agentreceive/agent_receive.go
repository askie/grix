package agentreceive

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

const (
	ModeNormal int16 = 1
	// ModeAll 表示群聊内也始终接收（有问必答），用于私聊转群后保持原 agent 的应答行为，
	// 并据此放行群聊对自有(proprietary) agent 的 @-only 限制。
	ModeAll         int16 = 2
	ModeMentionOnly int16 = 3

	DefaultMode         int16 = ModeNormal
	DefaultBacklogCount       = 8
	MaxBacklogCount           = 20
	bufferKeepCount           = 50
)

const bufferTTL = 48 * time.Hour

type Policy struct {
	SessionID    string
	MemberID     int64
	MemberType   int16
	Mode         int16
	BacklogCount int
}

type MessageTrigger struct {
	SessionID       string
	SessionType     int16
	MsgID           int64
	SenderID        int64
	SenderType      int16
	MsgType         int16
	Content         string
	ExtraRaw        json.RawMessage
	QuotedMessageID int64
	MentionUserIDs  []int64
	CreatedAt       time.Time
}

type Decision struct {
	Dispatch            bool
	Buffered            bool
	ClearBufferOnAccept bool
	ContextMessages     []protocol.ContextMessagePayload
}

func Normalize(mode int16, backlogCount int) (int16, int) {
	switch mode {
	case ModeNormal, ModeAll, ModeMentionOnly:
	default:
		mode = DefaultMode
	}
	if backlogCount <= 0 {
		backlogCount = DefaultBacklogCount
	}
	if backlogCount > MaxBacklogCount {
		backlogCount = MaxBacklogCount
	}
	return mode, backlogCount
}

func Evaluate(
	ctx context.Context,
	policy Policy,
	trigger MessageTrigger,
	publicTriggered bool,
	explicitlyMentioned bool,
) (Decision, error) {
	mode, _ := Normalize(policy.Mode, policy.BacklogCount)
	if trigger.SessionType != 2 {
		return Decision{Dispatch: true}, nil
	}

	if trigger.MsgID == 0 {
		return Decision{}, nil
	}

	switch mode {
	case ModeAll:
		// 群内始终响应，与私聊一致的有问必答。
		return Decision{Dispatch: true}, nil
	case ModeNormal:
		if !publicTriggered {
			return Decision{}, nil
		}
		return Decision{Dispatch: true, ClearBufferOnAccept: true}, nil
	case ModeMentionOnly:
		if !explicitlyMentioned {
			return Decision{}, nil
		}
		return Decision{Dispatch: true, ClearBufferOnAccept: true}, nil
	default:
		return Decision{Dispatch: true}, nil
	}
}

func ClearBuffer(ctx context.Context, sessionID string, memberType int16, memberID int64) error {
	if store.RDB == nil || sessionID == "" || memberID <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return store.RDB.Del(ctx, bufferKey(sessionID, memberType, memberID)).Err()
}

func bufferKey(sessionID string, memberType int16, memberID int64) string {
	return fmt.Sprintf("im:agent_receive:buffer:%s:%d:%d", sessionID, memberType, memberID)
}

func appendBufferedMsgID(ctx context.Context, sessionID string, memberType int16, memberID int64, msgID int64) error {
	if store.RDB == nil || sessionID == "" || memberID <= 0 || msgID <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	key := bufferKey(sessionID, memberType, memberID)
	pipe := store.RDB.TxPipeline()
	pipe.RPush(ctx, key, msgID)
	pipe.LTrim(ctx, key, -bufferKeepCount, -1)
	pipe.Expire(ctx, key, bufferTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func peekBufferedMsgIDs(ctx context.Context, sessionID string, memberType int16, memberID int64) ([]int64, error) {
	if store.RDB == nil || sessionID == "" || memberID <= 0 {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	key := bufferKey(sessionID, memberType, memberID)
	values, err := store.RDB.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, nil
	}
	ids := make([]int64, 0, len(values))
	for _, value := range values {
		var msgID int64
		if _, scanErr := fmt.Sscanf(value, "%d", &msgID); scanErr != nil || msgID <= 0 {
			continue
		}
		ids = append(ids, msgID)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return ids, nil
}
