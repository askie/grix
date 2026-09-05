package push

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/liveactivity"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/push/provider"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// liveActivityAttributesType 必须与 iOS 端 ActivityAttributes 的类型名逐字一致，
// 否则系统认不出这条 push-to-start 该起哪种活动，静默丢弃。
const liveActivityAttributesType = "GrixRunAttributes"

// liveActivityTarget 是一次实时活动推送的一个投递目标。
type liveActivityTarget struct {
	deviceID string
	pushEnv  string
	token    string
}

// processLiveActivityTask 处理 cmd="live_activity" 的推送任务。
//
// 收件人解析按事件分两路：start 用设备的 push-to-start token（devices 表一列），
// update / end 用那张活动自己的 token（Redis，iOS 端开卡后才报得上来）。
func (w *Worker) processLiveActivityTask(ctx context.Context, task *pushTask) error {
	var payload protocol.LiveActivityPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		logger.L.Errorf("live activity payload unmarshal error: %v", err)
		return nil
	}
	sessionID := strings.TrimSpace(payload.SessionID)
	if sessionID == "" {
		return nil
	}

	// iOS 通道被塘主关掉时一起跳过：卡片本来就只有 APNs 一条路。
	channels, err := systemsetting.GetPushChannelSettings()
	if err != nil {
		channels = systemsetting.DefaultPushChannelSettings()
	}
	if !channels.EnabledFor(model.DevicePlatformIOS) {
		return nil
	}

	devices := w.loadActiveIOSDevices(ctx, task.UserID)
	var targets []liveActivityTarget
	switch payload.Event {
	case protocol.LiveActivityEventStart:
		targets = startTargets(devices)
	case protocol.LiveActivityEventUpdate, protocol.LiveActivityEventEnd:
		targets = w.activityTargets(ctx, task.UserID, sessionID, devices)
	default:
		logger.L.Warnf("live activity: unknown event=%s user=%d session=%s", payload.Event, task.UserID, sessionID)
		return nil
	}
	if len(targets) == 0 {
		logger.L.Debugf("live activity: no target device user=%d session=%s event=%s",
			task.UserID, sessionID, payload.Event)
		return w.finishLiveActivity(ctx, task.UserID, sessionID, payload.Event)
	}

	apnsPayload := buildAPNsLiveActivityPayload(payload)
	delivered, retryable := 0, 0
	for _, target := range targets {
		apnsProvider := w.apnsProvider(target.pushEnv)
		if apnsProvider == nil {
			logger.L.Warnf("live activity: apns provider unavailable device=%s push_env=%s", target.deviceID, target.pushEnv)
			continue
		}
		result, err := apnsProvider.SendLiveActivity(ctx, target.token, apnsPayload)
		if err != nil {
			logger.L.Errorf("live activity: send error user=%d device=%s: %v", task.UserID, target.deviceID, err)
			retryable++
			continue
		}
		if provider.IsLiveActivityTokenInvalid(result) {
			w.dropInvalidLiveActivityToken(ctx, task.UserID, sessionID, target, payload.Event)
			continue
		}
		if !result.Success {
			logger.L.Warnf(
				"live activity: delivery failed user=%d device=%s status=%d reason=%s",
				task.UserID, target.deviceID, result.StatusCode, strings.TrimSpace(result.Reason),
			)
			retryable++
			continue
		}
		delivered++
	}

	if err := w.finishLiveActivity(ctx, task.UserID, sessionID, payload.Event); err != nil {
		return err
	}
	if delivered == 0 && retryable > 0 {
		return fmt.Errorf("live activity push has only retryable failures user=%d session=%s", task.UserID, sessionID)
	}
	return nil
}

// finishLiveActivity 在 end 之后清掉这个会话的活动 token：活动已经结束，
// 这些 token 立刻作废，留着只会让下一次 update 白发一轮。
func (w *Worker) finishLiveActivity(ctx context.Context, userID int64, sessionID, event string) error {
	if event != protocol.LiveActivityEventEnd || store.RDB == nil {
		return nil
	}
	if err := store.RDB.Del(ctx, liveactivity.TokenKey(userID, sessionID)).Err(); err != nil {
		logger.L.Warnf("live activity: clear tokens user=%d session=%s err=%v", userID, sessionID, err)
	}
	return nil
}

// loadActiveIOSDevices 取该用户全部在用的 iOS 设备，按 device_id 索引。
func (w *Worker) loadActiveIOSDevices(ctx context.Context, userID int64) map[string]model.Device {
	out := make(map[string]model.Device)
	if userID <= 0 || store.DB == nil {
		return out
	}
	var rows []model.Device
	if err := store.DB.WithContext(ctx).
		Where("user_id = ? AND platform = ? AND is_active = true", userID, model.DevicePlatformIOS).
		Find(&rows).Error; err != nil {
		logger.L.Warnf("live activity: load devices user=%d err=%v", userID, err)
		return out
	}
	for _, row := range rows {
		if deviceID := strings.TrimSpace(row.DeviceID); deviceID != "" {
			out[deviceID] = row
		}
	}
	return out
}

// startTargets 挑出能起卡的设备：报过 push-to-start token 的那些。没报过的设备
// 要么还没升级到带扩展的版本，要么用户在系统设置里关了实时活动，都不该收到。
func startTargets(devices map[string]model.Device) []liveActivityTarget {
	targets := make([]liveActivityTarget, 0, len(devices))
	for deviceID, device := range devices {
		token := strings.TrimSpace(device.LiveActivityToken)
		if token == "" {
			continue
		}
		targets = append(targets, liveActivityTarget{deviceID: deviceID, pushEnv: device.PushEnv, token: token})
	}
	return targets
}

// activityTargets 读 Redis 里这个会话已开出的活动 token。设备行已经不在（换机、
// 退出登录）时跳过：没有 push_env 就选不出该走沙盒还是生产。
func (w *Worker) activityTargets(
	ctx context.Context,
	userID int64,
	sessionID string,
	devices map[string]model.Device,
) []liveActivityTarget {
	if store.RDB == nil {
		return nil
	}
	entries, err := store.RDB.HGetAll(ctx, liveactivity.TokenKey(userID, sessionID)).Result()
	if err != nil {
		logger.L.Warnf("live activity: load tokens user=%d session=%s err=%v", userID, sessionID, err)
		return nil
	}
	targets := make([]liveActivityTarget, 0, len(entries))
	for deviceID, raw := range entries {
		var entry liveactivity.TokenEntry
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			logger.L.Warnf("live activity: bad token entry user=%d session=%s device=%s", userID, sessionID, deviceID)
			continue
		}
		token := strings.TrimSpace(entry.Token)
		if token == "" {
			continue
		}
		device, ok := devices[deviceID]
		if !ok {
			continue
		}
		targets = append(targets, liveActivityTarget{deviceID: deviceID, pushEnv: device.PushEnv, token: token})
	}
	return targets
}

// dropInvalidLiveActivityToken 清掉一个已经作废的 token，与普通推送清理失效
// device token 同理：留着只会让之后每一次更新都白跑一趟 APNs。清的是 token 本身，
// 不停用整台设备——设备的普通推送通道仍然好着。
func (w *Worker) dropInvalidLiveActivityToken(
	ctx context.Context,
	userID int64,
	sessionID string,
	target liveActivityTarget,
	event string,
) {
	if event == protocol.LiveActivityEventStart {
		if store.DB != nil {
			if err := store.DB.WithContext(ctx).Model(&model.Device{}).
				Where("user_id = ? AND device_id = ? AND is_active = true", userID, target.deviceID).
				Update("live_activity_token", "").Error; err != nil {
				logger.L.Warnf("live activity: clear start token user=%d device=%s err=%v", userID, target.deviceID, err)
				return
			}
		}
		logger.L.Infof("live activity: dropped invalid start token user=%d device=%s", userID, target.deviceID)
		return
	}
	if store.RDB != nil {
		if err := store.RDB.HDel(ctx, liveactivity.TokenKey(userID, sessionID), target.deviceID).Err(); err != nil {
			logger.L.Warnf("live activity: clear activity token user=%d device=%s err=%v", userID, target.deviceID, err)
			return
		}
	}
	logger.L.Infof("live activity: dropped invalid activity token user=%d session=%s device=%s", userID, sessionID, target.deviceID)
}

func buildAPNsLiveActivityPayload(payload protocol.LiveActivityPayload) *provider.LiveActivityPayload {
	out := &provider.LiveActivityPayload{
		Event:          payload.Event,
		AttributesType: liveActivityAttributesType,
		Attributes: map[string]any{
			"session_id": payload.Attributes.SessionID,
			"agent_id":   strconv.FormatInt(payload.Attributes.AgentID, 10),
			"agent_name": payload.Attributes.AgentName,
		},
		ContentState: map[string]any{
			"phase":         payload.ContentState.Phase,
			"title":         payload.ContentState.Title,
			"detail":        payload.ContentState.Detail,
			"updated_at_ms": payload.ContentState.UpdatedAtMs,
		},
		// 等着主人处理、以及任务收尾，都要立刻送到；过程更新交给系统合并。
		HighPriority: payload.Alert != nil || payload.Event == protocol.LiveActivityEventEnd,
		Timestamp:    time.Now().Unix(),
	}
	if payload.ContentState.UpdatedAtMs > 0 {
		out.Timestamp = payload.ContentState.UpdatedAtMs / 1000
	}
	if payload.Alert != nil {
		out.AlertTitle = payload.Alert.Title
		out.AlertBody = payload.Alert.Body
	}
	if payload.DismissalAtMs > 0 {
		out.DismissalAt = payload.DismissalAtMs / 1000
	}
	return out
}
