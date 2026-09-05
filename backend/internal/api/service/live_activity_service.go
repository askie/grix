package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/liveactivity"
	"github.com/askie/grix/backend/internal/store"
)

// 实时活动的 token 只在一次 run 的几小时内有用，下一次 run 会换一批，因此存 Redis
// 而不建表。key 与 push 服务读取的一致：im:la:tokens:<user_id>:<session_id>，
// hash 的 field 是 device_id（一个主人可能在手机和 iPad 上各开一张同会话的卡）。
const (
	liveActivityTokenTTL       = liveactivity.TokenTTLSeconds * time.Second
	liveActivityTokenMaxLength = 512
)

var (
	// ErrLiveActivityInvalidRequest 表示入参缺失或超长。
	ErrLiveActivityInvalidRequest = errors.New("invalid live activity token request")
	// ErrLiveActivitySessionForbidden 表示会话不属于调用者。
	ErrLiveActivitySessionForbidden = errors.New("not the owner of this session")
	// ErrLiveActivityStoreUnavailable 表示 Redis 不可用。
	ErrLiveActivityStoreUnavailable = errors.New("live activity token store unavailable")
)

// SaveLiveActivityToken 记录某台设备为某个会话开出的实时活动的更新 token。
//
// 归属判定与离线操作回调同款：chat_states 行以 (session_id, owner_id) 为主键，
// 查得到就证明调用者是这次 run 的主人；查不到一律 403，而不是 404——会话存不存在
// 不该由未授权的调用者试探出来。
func SaveLiveActivityToken(ctx context.Context, userID int64, sessionID, activityID, token, deviceID string) error {
	sessionID = strings.TrimSpace(sessionID)
	activityID = strings.TrimSpace(activityID)
	token = strings.TrimSpace(token)
	deviceID = strings.TrimSpace(deviceID)
	if userID <= 0 || sessionID == "" || activityID == "" || token == "" || deviceID == "" {
		return ErrLiveActivityInvalidRequest
	}
	if len(token) > liveActivityTokenMaxLength || len(activityID) > liveActivityTokenMaxLength {
		return ErrLiveActivityInvalidRequest
	}

	state, err := store.GetSessionAgentState(sessionID, userID)
	if err != nil {
		return err
	}
	if state == nil {
		return ErrLiveActivitySessionForbidden
	}

	if store.RDB == nil {
		return ErrLiveActivityStoreUnavailable
	}
	entry, err := json.Marshal(liveactivity.TokenEntry{ActivityID: activityID, Token: token})
	if err != nil {
		return err
	}
	key := liveactivity.TokenKey(userID, sessionID)
	if err := store.RDB.HSet(ctx, key, deviceID, string(entry)).Err(); err != nil {
		return err
	}
	return store.RDB.Expire(ctx, key, liveActivityTokenTTL).Err()
}
