package push

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

// VoIPCallPayload 来电推送的数据载荷。
type VoIPCallPayload struct {
	CallID     string `json:"call_id"`
	CallerID   string `json:"caller_id"`
	CallerName string `json:"caller_name"`
	CallMode   int    `json:"call_mode"` // 1=voice
}

// processCallInviteTask 处理 call:invite 离线推送任务。
func (w *Worker) processCallInviteTask(ctx context.Context, task *pushTask) error {
	var p VoIPCallPayload
	if err := json.Unmarshal(task.Payload, &p); err != nil {
		logger.L.Warnf("call invite push payload unmarshal error user=%d err=%v", task.UserID, err)
		return nil
	}
	w.SendVoIPPush(ctx, task.UserID, p)
	return nil
}

// SendVoIPPush 向指定用户的所有离线设备发送来电推送。
// iOS 使用 PushKit VoIP push，Android 使用 FCM 高优先级 data 消息。
func (w *Worker) SendVoIPPush(ctx context.Context, calleeID int64, payload VoIPCallPayload) {
	if store.DB == nil {
		logger.L.Warnf("voip push skipped: db not initialized callee=%d", calleeID)
		return
	}
	var devices []model.Device
	store.DB.WithContext(ctx).Where("user_id = ? AND is_active = true", calleeID).Find(&devices)

	if len(devices) == 0 {
		logger.L.Debugf("voip push: no devices for user=%d", calleeID)
		return
	}

	for _, device := range devices {
		switch device.Platform {
		case model.DevicePlatformIOS:
			apnsProvider := w.apnsProvider(device.PushEnv)
			if apnsProvider == nil {
				logger.L.Warnf("voip push: apns provider unavailable device=%s push_env=%s", device.DeviceID, device.PushEnv)
				continue
			}
			if _, err := apnsProvider.SendVoIP(ctx, device.DeviceToken, buildVoIPAPNsPayload(calleeID, payload)); err != nil {
				logger.L.Warnf("voip push apns error device=%s err=%v", device.DeviceID, err)
			}

		case model.DevicePlatformAndroidFCM:
			if w.fcm == nil {
				continue
			}
			if _, err := w.fcm.SendCallNotification(ctx, device.DeviceToken, buildVoIPFCMData(calleeID, payload)); err != nil {
				logger.L.Warnf("voip push fcm error device=%s err=%v", device.DeviceID, err)
			}
		}
	}
}

// buildVoIPAPNsPayload 构造 iOS PushKit 来电 payload，附带 recipient_id（被叫账号），
// 供客户端来电入口比对当前登录账号，丢弃换号后投达的他人来电。
func buildVoIPAPNsPayload(calleeID int64, payload VoIPCallPayload) map[string]any {
	return map[string]any{
		"call_id":      payload.CallID,
		"caller_id":    payload.CallerID,
		"caller_name":  payload.CallerName,
		"call_mode":    payload.CallMode,
		"recipient_id": strconv.FormatInt(calleeID, 10),
	}
}

// buildVoIPFCMData 构造 Android 来电 data 消息，附带 recipient_id（被叫账号）。
func buildVoIPFCMData(calleeID int64, payload VoIPCallPayload) map[string]string {
	return map[string]string{
		"type":         "call_invite",
		"call_id":      payload.CallID,
		"caller_id":    payload.CallerID,
		"caller_name":  payload.CallerName,
		"call_mode":    fmt.Sprintf("%d", payload.CallMode),
		"recipient_id": strconv.FormatInt(calleeID, 10),
	}
}
