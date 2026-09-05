package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/push/provider"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/nats-io/nats.go"
	"gorm.io/gorm"
)

var mdRegex = regexp.MustCompile(`[*_~\x60#\[\]()>!|]`)

const (
	pushOfflineQueueGroup         = "push-offline-v1"
	pushOfflineDurable            = "push-offline-v1"
	pushOfflineAckWait            = 2 * time.Minute
	pushOfflineInProgressInterval = 20 * time.Second
	defaultPushSenderTitle        = "Grix"

	// idEpochMillis 与雪花发号器 epoch 一致，用于从消息 ID 还原其生成时刻。
	idEpochMillis int64 = 1700000000000
	// offlinePushStaleAge：用户仍在线时，超过此年龄的普通离线消息不再弹横幅，
	// 避免重新上线时积压的旧推送集中冒出来打扰用户。审批 / 呼叫类不受此限。
	offlinePushStaleAge = 60 * time.Second

	// senderTypeHuman 标识消息由真人用户发出（SenderType==2 为 AI agent）。
	senderTypeHuman int16 = 1

	// webPushFailureDeactivateThreshold：Web Push 端点连续网络失败（超时 / 连不上）
	// 达到此次数即停用。这类端点不会返回"令牌无效"，靠 isTokenInvalid 永远清不掉，
	// 每条推送都要先耗掉一个完整超时才轮到别的设备。
	webPushFailureDeactivateThreshold = 5
	// webPushFailureCounterTTL：连续失败计数的存活时间，成功一次即清零。
	webPushFailureCounterTTL = 7 * 24 * time.Hour
)

type Worker struct {
	apnsSandbox    *provider.APNsProvider
	apnsProduction *provider.APNsProvider
	fcm            *provider.FCMProvider
	jpush          *provider.JPushProvider
	webpush        *provider.WebPushProvider
	// vendors 按设备平台索引国产厂商推送通道（华为 / 小米 等）。
	// 未配置凭据的厂商不会出现在表中，其设备的推送被跳过。
	vendors map[string]provider.VendorSender
}

func NewWorker(
	apnsSandbox *provider.APNsProvider,
	apnsProduction *provider.APNsProvider,
	fcm *provider.FCMProvider,
	jpush *provider.JPushProvider,
	webpush *provider.WebPushProvider,
	vendors map[string]provider.VendorSender,
) *Worker {
	w := &Worker{
		apnsSandbox:    apnsSandbox,
		apnsProduction: apnsProduction,
		fcm:            fcm,
		jpush:          jpush,
		webpush:        webpush,
		vendors:        vendors,
	}
	// 未配置的厂商通道是预期状态（凭据分批开通），不作为异常告警；
	// 真正漏配导致跳过投递时，pushToUserDevices 会按设备打 warn。
	if len(w.vendors) > 0 {
		enabled := make([]string, 0, len(w.vendors))
		for platform := range w.vendors {
			enabled = append(enabled, platform)
		}
		sort.Strings(enabled)
		logger.L.Infof("vendor push providers enabled: %s", strings.Join(enabled, ", "))
	}
	if w.fcm == nil {
		logger.L.Warn("fcm provider not configured: android_fcm devices will not receive pushes")
	}
	if w.jpush == nil {
		logger.L.Warn("jpush provider not configured: android_jpush devices will not receive pushes")
	}
	if w.webpush == nil {
		logger.L.Warn("webpush provider not configured: web_push devices will not receive pushes")
	}
	return w
}

type pushTask struct {
	UserID  int64           `json:"user_id"`
	Cmd     string          `json:"cmd"`
	Payload json.RawMessage `json:"payload"`
}

type pushMsgPayload struct {
	MsgID         int64           `json:"msg_id,string"`
	SessionID     string          `json:"session_id"`
	SenderID      int64           `json:"sender_id,string"`
	SenderType    int16           `json:"sender_type"`
	Content       string          `json:"content"`
	MsgType       int16           `json:"msg_type"`
	Extra         json.RawMessage `json:"extra,omitempty"`
	ForcePush     bool            `json:"force_push,omitempty"`
	TimeSensitive bool            `json:"time_sensitive,omitempty"`
}

func (w *Worker) Start(ctx context.Context) {
	handler := func(msg *nats.Msg) {
		done := make(chan struct{})
		go func() {
			ticker := time.NewTicker(pushOfflineInProgressInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if err := msg.InProgress(); err != nil {
						logger.L.Warnf("push task in-progress ack error: %v", err)
					}
				case <-done:
					return
				}
			}
		}()
		defer close(done)

		var task pushTask
		if err := json.Unmarshal(msg.Data, &task); err != nil {
			logger.L.Errorf("push task unmarshal error: %v", err)
			if err := msg.Ack(); err != nil {
				logger.L.Warnf("push task ack error (unmarshal): %v", err)
			}
			return
		}

		if err := w.processTask(ctx, &task); err != nil {
			logger.L.Warnf("push task processing failed, wait redelivery user=%d cmd=%s err=%v", task.UserID, task.Cmd, err)
			return
		}
		if err := msg.Ack(); err != nil {
			logger.L.Warnf("push task ack error: %v", err)
		}
	}

	sub, err := store.JS.QueueSubscribe("im.push.offline.*", pushOfflineQueueGroup, handler,
		nats.ManualAck(),
		nats.Durable(pushOfflineDurable),
		nats.DeliverNew(),
		nats.AckWait(pushOfflineAckWait),
		nats.MaxDeliver(3),
	)

	if err != nil {
		logger.L.Fatalf("failed to subscribe push worker: %v", err)
	}
	if info, infoErr := sub.ConsumerInfo(); infoErr != nil {
		logger.L.Warnf("load push consumer info error: %v", infoErr)
	} else {
		logger.L.Infof(
			"push consumer ready stream=%s durable=%s deliver_policy=%s ack_wait=%s pending=%d ack_pending=%d redelivered=%d",
			info.Stream,
			info.Name,
			info.Config.DeliverPolicy,
			info.Config.AckWait,
			info.NumPending,
			info.NumAckPending,
			info.NumRedelivered,
		)
	}

	<-ctx.Done()
	if err := sub.Unsubscribe(); err != nil {
		logger.L.Warnf("push consumer unsubscribe error: %v", err)
	}
}

func (w *Worker) processTask(ctx context.Context, task *pushTask) error {
	if task == nil || task.UserID <= 0 {
		return nil
	}

	switch task.Cmd {
	case "", protocol.CmdPushMsg:
		return w.processPushMsgTask(ctx, task)
	case protocol.CmdSessionMemberChanged:
		return w.processSessionMemberChangedTask(ctx, task)
	case protocol.CmdCallInvite:
		return w.processCallInviteTask(ctx, task)
	default:
		logger.L.Warnf("unsupported offline push cmd=%s user=%d", task.Cmd, task.UserID)
		return nil
	}
}

func (w *Worker) processPushMsgTask(ctx context.Context, task *pushTask) error {
	var payload pushMsgPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		logger.L.Errorf("push payload unmarshal error: %v", err)
		return nil
	}

	// Never push a message back to its own sender.
	if task.UserID == payload.SenderID {
		logger.L.Debugf("skip self-push user=%d msg=%d", task.UserID, payload.MsgID)
		return nil
	}

	if w.isSessionMutedForUser(ctx, task.UserID, payload.SessionID) {
		logger.L.Debugf(
			"skip muted-session push user=%d session=%s msg=%d",
			task.UserID,
			payload.SessionID,
			payload.MsgID,
		)
		return nil
	}

	// 过程噪音（工具调用卡片 / 思考过程 / 通话转写 / 空流式占位）不弹离线通知。
	// 在线端用 composing 会话活动状态体现"正在处理"；离线只留给终态消息（文本回复、审批/呼叫卡片）。
	if shouldSuppressOfflinePush(payload) {
		logger.L.Debugf("suppress process-noise offline push user=%d session=%s msg=%d type=%d",
			task.UserID, payload.SessionID, payload.MsgID, payload.MsgType)
		return nil
	}

	// 陈旧门：用户当前在线时，超龄的普通消息不再弹离线横幅——重新上线时积压的旧推送
	// 集中投递只会打扰人。审批 / 呼叫卡片、ForcePush、TimeSensitive 属必达终态不受限；
	// 用户全设备离线时也照常弹（旧消息此时是唯一触达途径）。
	if shouldSuppressStaleOfflinePush(ctx, task.UserID, payload) {
		logger.L.Debugf("suppress stale offline push user=%d session=%s msg=%d age=%s",
			task.UserID, payload.SessionID, payload.MsgID, messageAgeFromID(payload.MsgID))
		return nil
	}

	// Check if session is AI type
	var session model.Session
	if err := store.DB.Select("session_type").Where("session_id = ?", payload.SessionID).Take(&session).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		logger.L.Warnf("load push session failed session=%s err=%v", payload.SessionID, err)
	}
	isAI := session.SessionType == 2 // simplified check

	// 节流仅用于人际会话防刷屏；AI 会话的终态消息（文本回复 / 审批卡片）必达，推原文不降频。
	// ForcePush / TimeSensitive（语音通话振铃等实时信号）必须即时弹出，绝不进节流窗口被降级成角标。
	if !isAI && !payload.ForcePush && !payload.TimeSensitive && ShouldThrottle(ctx, task.UserID, payload.SessionID, false) {
		count := GetThrottledCount(ctx, task.UserID, payload.SessionID)
		logger.L.Debugf("throttled push for user %d session %s (count=%d)", task.UserID, payload.SessionID, count)
		// Still send a badge-only update so the app icon badge stays current
		return w.pushToUserDevices(ctx, task.UserID, &provider.PushPayload{BadgeOnly: true})
	}

	// Resolve sender display info (name, avatar) via cache
	senderInfo := resolveSenderDisplay(ctx, task.UserID, payload.SenderID, payload.SenderType)

	// Sanitize content for push notification
	body := sanitizeContent(payload.Content, payload.MsgType)

	// 人际会话节流窗口内累计多条时合并提示；AI 会话不进此路径（count 恒为 0），始终推原文。
	if !isAI {
		if count := GetThrottledCount(ctx, task.UserID, payload.SessionID); count > 1 {
			body = fmt.Sprintf("%s发来了 %d 条新消息", senderInfo.Name, count)
			ResetThrottledCount(ctx, task.UserID, payload.SessionID)
		}
	}

	pushPayload := &provider.PushPayload{
		Title:           senderInfo.Name,
		Body:            body,
		SessionID:       payload.SessionID,
		SenderAvatarURL: senderInfo.AvatarURL,
		SenderInitial:   senderInfo.Initial,
		ForcePush:       payload.ForcePush,
		TimeSensitive:   payload.TimeSensitive,
		// 分级投递：审批 / 呼叫卡片、语音拨号、来电、以及任何真人发出的消息属"重要"，
		// 立即投递；仅 AI 的普通过程消息降级投递。
		HighPriority: isImportantPush(payload),
	}
	if payload.MsgType == model.MsgTypeImage {
		pushPayload.ImageURL = resolvePushImageURL(payload.Extra, payload.Content)
	}
	return w.pushToUserDevices(ctx, task.UserID, pushPayload)
}

func (w *Worker) isSessionMutedForUser(ctx context.Context, userID int64, sessionID string) bool {
	if userID <= 0 || store.DB == nil {
		return false
	}
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return false
	}

	var member model.SessionMember
	if err := store.DB.WithContext(ctx).
		Select("is_muted").
		Where(
			"session_id = ? AND member_id = ? AND member_type = 1",
			sid,
			userID,
		).
		Take(&member).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.L.Warnf(
				"load session mute flag failed user=%d session=%s err=%v",
				userID,
				sid,
				err,
			)
		}
		return false
	}
	return member.IsMuted || w.isPeerMutedForUser(ctx, userID, sid)
}

func (w *Worker) isPeerMutedForUser(ctx context.Context, userID int64, sessionID string) bool {
	if userID <= 0 || store.DB == nil || strings.TrimSpace(sessionID) == "" {
		return false
	}

	var session model.Session
	if err := store.DB.WithContext(ctx).
		Select("session_id", "session_type").
		Where("session_id = ?", sessionID).
		Take(&session).Error; err != nil {
		return false
	}
	if session.SessionType != model.SessionTypeDirect {
		return false
	}

	var peer model.SessionMember
	if err := store.DB.WithContext(ctx).
		Select("member_id").
		Where(
			"session_id = ? AND NOT (member_type = 1 AND member_id = ?)",
			sessionID,
			userID,
		).
		Take(&peer).Error; err != nil {
		return false
	}
	if peer.MemberID <= 0 {
		return false
	}

	var mute model.UserPeerMute
	if err := store.DB.WithContext(ctx).
		Select("id").
		Where(
			"user_id = ? AND peer_user_id = ? AND is_muted = ?",
			userID,
			peer.MemberID,
			true,
		).
		Take(&mute).Error; err != nil {
		return false
	}
	return true
}

func (w *Worker) processSessionMemberChangedTask(ctx context.Context, task *pushTask) error {
	var payload protocol.SessionMemberChangedPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		logger.L.Errorf("session_member_changed push payload unmarshal error: %v", err)
		return nil
	}
	if payload.Action != "add" || strings.TrimSpace(payload.SessionID) == "" {
		return nil
	}

	title, body := w.sessionMemberAddedPushContent(payload)
	return w.pushToUserDevices(ctx, task.UserID, &provider.PushPayload{
		Title:     title,
		Body:      body,
		SessionID: payload.SessionID,
	})
}

// PushNotification sends an already-assembled payload to all of a user's
// devices. It is the entry point the Agent-notification dispatcher uses to reach
// the push pipeline from outside the push package.
func (w *Worker) PushNotification(ctx context.Context, userID int64, payload *provider.PushPayload) error {
	return w.pushToUserDevices(ctx, userID, payload)
}

func (w *Worker) pushToUserDevices(ctx context.Context, userID int64, pushPayload *provider.PushPayload) error {
	if pushPayload == nil {
		return nil
	}
	// 标记目标账号，供客户端比对当前登录账号、拦截切号后的错投推送。
	pushPayload.RecipientID = userID
	pushPayload.Badge = w.loadUserUnreadBadge(ctx, userID)
	retryableFailures := 0
	delivered := 0

	// Collect online device IDs to skip — each online device receives
	// the message via WebSocket, so push would be redundant.
	// ForcePush overrides this check (e.g. voice call notifications).
	onlineDevices := collectOnlineDevices(ctx, userID)

	// 通道总开关（塘主可在国内连不上 Google 时关闭 AndroidFCM / WebPush）。
	// 1 分钟缓存，读失败时回落到默认全开，绝不因配置读取异常而漏推。
	channels, err := systemsetting.GetPushChannelSettings()
	if err != nil {
		logger.L.Warnf("load push channel settings failed, fallback to all-enabled: %v", err)
		channels = systemsetting.DefaultPushChannelSettings()
	}

	// Get all devices for this user
	var devices []model.Device
	store.DB.Where("user_id = ? AND is_active = true", userID).Find(&devices)
	sortDevicesForDelivery(devices)

	for _, device := range devices {
		if !pushPayload.ForcePush && device.Platform != model.DevicePlatformWebPush && onlineDevices[device.DeviceID] {
			continue
		}

		// 该设备所属通道被塘主关闭则跳过（不计入失败，避免无意义重投）。
		if !pushChannelEnabled(device.Platform, channels) {
			logger.L.Debugf("skip push: channel disabled user=%d device=%s platform=%s",
				userID, device.DeviceID, device.Platform)
			continue
		}
		var result *provider.PushResult
		var err error

		// Badge-only pushes only apply to platforms with app icon badges
		// (iOS and web). Skip Android — it has no icon badge concept and
		// would show a blank notification.
		if pushPayload.BadgeOnly {
			switch device.Platform {
			case model.DevicePlatformIOS, model.DevicePlatformWebPush:
				// proceed below
			default:
				continue
			}
		}

		switch {
		case device.Platform == model.DevicePlatformIOS:
			apnsProvider := w.apnsProvider(device.PushEnv)
			if apnsProvider == nil {
				logger.L.Warnf("apns provider unavailable for device %s push_env=%s", device.DeviceID, device.PushEnv)
				continue
			}
			result, err = apnsProvider.Send(ctx, device.DeviceToken, pushPayload)
		case device.Platform == model.DevicePlatformAndroidFCM:
			if w.fcm == nil {
				logSkipUnconfiguredProvider(userID, device)
				continue
			}
			result, err = w.fcm.Send(ctx, device.DeviceToken, pushPayload)
		case device.Platform == model.DevicePlatformAndroidJPush:
			if w.jpush == nil {
				logSkipUnconfiguredProvider(userID, device)
				continue
			}
			result, err = w.jpush.Send(ctx, device.DeviceToken, pushPayload)
		case device.Platform == model.DevicePlatformWebPush:
			if w.webpush == nil {
				logSkipUnconfiguredProvider(userID, device)
				continue
			}
			result, err = w.webpush.Send(ctx, device.DeviceToken, pushPayload)
		case model.IsAndroidVendorPlatform(device.Platform):
			vendor := w.vendors[device.Platform]
			if vendor == nil {
				logSkipUnconfiguredProvider(userID, device)
				continue
			}
			result, err = vendor.Send(ctx, device.DeviceToken, pushPayload)
		}

		if err != nil {
			logger.L.Errorf("push error for device %s: %v", device.DeviceID, err)
			retryableFailures++
			noteWebPushTransportFailure(ctx, userID, device)
			continue
		}
		if result != nil && !result.Success {
			logger.L.Warnf(
				"push delivery failed user=%d device=%s platform=%s status=%d reason=%s",
				userID,
				device.DeviceID,
				device.Platform,
				result.StatusCode,
				strings.TrimSpace(result.Reason),
			)
			if !w.isTokenInvalid(device.Platform, result) {
				retryableFailures++
			}
		} else {
			delivered++
			resetWebPushTransportFailures(ctx, device)
		}

		// Handle invalid token
		if result != nil && w.isTokenInvalid(device.Platform, result) {
			deactivateDevice(ctx, userID, device, "device_token_invalidated")
			logger.L.Infof("deactivated invalid device token: user=%d device=%s", userID, device.DeviceID)
		}
	}
	if delivered == 0 && retryableFailures > 0 {
		return fmt.Errorf("offline push has only retryable failures user=%d failures=%d", userID, retryableFailures)
	}
	return nil
}

// devicePlatformDeliveryRank orders a user's devices for delivery. iOS first so
// APNs is never queued behind a slow channel, web_push last because a dead
// browser endpoint burns a full send timeout before anything else is tried.
func devicePlatformDeliveryRank(platform string) int {
	switch platform {
	case model.DevicePlatformIOS:
		return 0
	case model.DevicePlatformWebPush:
		return 2
	default:
		return 1
	}
}

// sortDevicesForDelivery reorders devices in place, keeping the relative order
// of devices that share a rank so delivery stays deterministic.
func sortDevicesForDelivery(devices []model.Device) {
	sort.SliceStable(devices, func(i, j int) bool {
		return devicePlatformDeliveryRank(devices[i].Platform) < devicePlatformDeliveryRank(devices[j].Platform)
	})
}

func webPushFailureCounterKey(deviceID string) string {
	return fmt.Sprintf("push:webpush:fail:%s", deviceID)
}

// noteWebPushTransportFailure counts consecutive transport failures (timeout,
// DNS, connection refused) for a Web Push endpoint and deactivates it once the
// threshold is reached. Such an endpoint never answers with an invalid-token
// status, so isTokenInvalid can never retire it: it stays active forever,
// fails every retry, and keeps the whole push task in retryable-failure state.
func noteWebPushTransportFailure(ctx context.Context, userID int64, device model.Device) {
	if device.Platform != model.DevicePlatformWebPush || store.RDB == nil {
		return
	}
	key := webPushFailureCounterKey(device.DeviceID)
	failures, err := store.RDB.Incr(ctx, key).Result()
	if err != nil {
		logger.L.Warnf("web push failure counter error user=%d device=%s: %v", userID, device.DeviceID, err)
		return
	}
	store.RDB.Expire(ctx, key, webPushFailureCounterTTL)
	if failures < webPushFailureDeactivateThreshold {
		return
	}

	deactivateDevice(ctx, userID, device, "device_web_push_unreachable")
	store.RDB.Del(ctx, key)
	logger.L.Infof(
		"deactivated unreachable web push endpoint: user=%d device=%s consecutive_failures=%d",
		userID,
		device.DeviceID,
		failures,
	)
}

// resetWebPushTransportFailures clears the consecutive-failure counter after a
// successful delivery, so only an unbroken run of failures retires an endpoint.
func resetWebPushTransportFailures(ctx context.Context, device model.Device) {
	if device.Platform != model.DevicePlatformWebPush || store.RDB == nil {
		return
	}
	store.RDB.Del(ctx, webPushFailureCounterKey(device.DeviceID))
}

// deactivateDevice retires a device from every push path: the persisted row, the
// online-device hash, and an audit trail naming why it was retired.
func deactivateDevice(ctx context.Context, userID int64, device model.Device, auditEventType string) {
	store.DB.Model(&model.Device{}).Where("id = ?", device.ID).Update("is_active", false)
	if store.RDB != nil {
		store.RDB.HDel(ctx, fmt.Sprintf("im:user:devices:%d", userID), device.DeviceID)
	}
	store.DB.Create(&model.AuditLog{
		EventType: auditEventType,
		UserID:    &userID,
		ClientIP:  "system",
	})
}

// logSkipUnconfiguredProvider 记录因凭据未配置而跳过的设备。
// 必须 continue 而不是往下走：留在原地会让 result 和 err 都为 nil，
// 被下游当成投递成功计入 delivered，于是漏配凭据时推送静默"成功"——
// 既不重投也不告警，还会让整批的 delivered==0 失败判定永远不成立。
func logSkipUnconfiguredProvider(userID int64, device model.Device) {
	logger.L.Warnf("skip push: provider unconfigured user=%d device=%s platform=%s",
		userID, device.DeviceID, device.Platform)
}

// pushChannelEnabled 按设备平台返回对应通道的塘主开关。未知平台默认放行。
func pushChannelEnabled(platform string, c systemsetting.PushChannelSettings) bool {
	return c.EnabledFor(platform)
}

func (w *Worker) loadUserUnreadBadge(ctx context.Context, userID int64) int {
	if userID <= 0 || store.DB == nil {
		return 0
	}

	var row struct {
		Total int64 `gorm:"column:total"`
	}
	// 口径必须与 pull_sync 未读快照（HandlePullSync）一致：未删除会话 + 群组活跃 +
	// （从未历史重置 OR 重置点之后仍有可见消息）。清空记录/删除会话不清 unread_count
	// 计数器，裸 SUM 会把 app 内永不可见的残留未读算进图标角标。
	existsSQL, existsArgs := store.VisibleAfterCutoffExistsSQL("me.session_id", "shr.deleted_before", "me.joined_at", "s.session_type", userID)
	if err := store.DB.WithContext(ctx).
		Table("session_members AS me").
		Select("COALESCE(SUM(me.unread_count), 0) AS total").
		Joins("JOIN sessions AS s ON s.session_id = me.session_id").
		Joins("LEFT JOIN session_history_resets AS shr ON shr.session_id = me.session_id AND shr.user_id = ?", userID).
		Where(
			"me.member_id = ? AND me.member_type = 1 AND me.is_muted = ? AND me.unread_count > 0",
			userID,
			false,
		).
		Where(
			// Peer-level mute applies to direct conversations only. A muted
			// contact that also belongs to a group must not suppress that group's
			// unread count.
			`NOT EXISTS (
				SELECT 1
				FROM session_members AS peer
				JOIN user_peer_mutes AS upm
					ON upm.user_id = ?
					AND upm.peer_user_id = peer.member_id
					AND upm.is_muted = ?
				WHERE s.session_type = ?
					AND peer.session_id = me.session_id
					AND NOT (peer.member_type = 1 AND peer.member_id = ?)
			)`,
			userID,
			true,
			model.SessionTypeDirect,
			userID,
		).
		Where(
			"s.is_deleted = false AND (s.session_type <> ? OR s.moderation_status = ?)",
			model.SessionTypeGroup,
			model.SessionModerationStatusActive,
		).
		Where("shr.session_id IS NULL OR "+existsSQL, existsArgs...).
		Scan(&row).Error; err != nil {
		logger.L.Warnf("load push unread badge failed user=%d err=%v", userID, err)
		return 0
	}

	if row.Total <= 0 {
		return 0
	}

	maxInt := int64(^uint(0) >> 1)
	if row.Total > maxInt {
		return int(maxInt)
	}
	return int(row.Total)
}

func (w *Worker) sessionMemberAddedPushContent(payload protocol.SessionMemberChangedPayload) (string, string) {
	groupName := strings.TrimSpace(payload.Title)
	if groupName == "" {
		var session model.Session
		if err := store.DB.Select("group_name").
			Where("session_id = ?", payload.SessionID).
			Take(&session).Error; err == nil {
			groupName = strings.TrimSpace(session.GroupName)
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.L.Warnf("load session group_name failed session=%s err=%v", payload.SessionID, err)
		}
	}

	operatorName := ""
	if payload.OperatorID > 0 {
		var operator model.User
		if err := store.DB.Select("nickname").
			Where("id = ?", payload.OperatorID).
			Take(&operator).Error; err == nil {
			operatorName = strings.TrimSpace(operator.Nickname)
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.L.Warnf("load operator nickname failed user=%d err=%v", payload.OperatorID, err)
		}
	}

	title := "群聊通知"
	if groupName != "" {
		title = groupName
	}

	body := "你已被加入群聊"
	switch {
	case operatorName != "" && groupName != "":
		body = fmt.Sprintf("%s邀请你加入群聊“%s”", operatorName, groupName)
	case operatorName != "":
		body = fmt.Sprintf("%s邀请你加入群聊", operatorName)
	case groupName != "":
		body = fmt.Sprintf("你已加入群聊“%s”", groupName)
	}
	return title, body
}

// isTokenInvalid 判定设备 token 是否已失效（失效则解绑设备）。
// 厂商通道各有自己的失效码，交由对应 provider 判定。
func (w *Worker) isTokenInvalid(platform string, result *provider.PushResult) bool {
	if vendor := w.vendors[platform]; vendor != nil {
		return vendor.IsTokenInvalid(result)
	}
	return isTokenInvalid(platform, result)
}

func isTokenInvalid(platform string, result *provider.PushResult) bool {
	switch {
	case platform == model.DevicePlatformIOS:
		return result.StatusCode == 410 || result.Reason == "Unregistered"
	case platform == model.DevicePlatformAndroidFCM:
		return result.Reason == "messaging/registration-token-not-registered"
	case platform == model.DevicePlatformAndroidJPush:
		return result.Reason == "1011"
	case platform == model.DevicePlatformWebPush:
		return result.StatusCode == http.StatusGone || result.StatusCode == http.StatusNotFound || result.Reason == "subscription-expired"
	}
	return false
}

func (w *Worker) apnsProvider(pushEnv string) *provider.APNsProvider {
	switch pushEnv {
	case model.DevicePushEnvAPNsSandbox:
		return w.apnsSandbox
	case model.DevicePushEnvAPNsProduction:
		return w.apnsProduction
	default:
		return nil
	}
}

// collectOnlineDevices returns a set of device IDs that currently have a
// live WebSocket connection, so push can be skipped per-device.
func collectOnlineDevices(ctx context.Context, userID int64) map[string]bool {
	online := make(map[string]bool)
	if store.RDB == nil {
		return online
	}
	routeKey := fmt.Sprintf("im:ws:route:%d", userID)
	devices, err := store.RDB.HGetAll(ctx, routeKey).Result()
	if err != nil || len(devices) == 0 {
		return online
	}
	for deviceID := range devices {
		aliveKey := fmt.Sprintf("im:ws:alive:%d:%s", userID, deviceID)
		exists, err := store.RDB.Exists(ctx, aliveKey).Result()
		if err == nil && exists > 0 {
			online[deviceID] = true
		}
	}
	return online
}

// shouldSuppressOfflinePush 判断一条离线消息是否为 agent 执行过程噪音，过程噪音不弹离线通知。
// 与 handler.ShouldInjectVoiceMessage 同源的语义门：只放行给人看的终态消息。
//   - 审批 / 呼叫卡片：必达终态，永不抑制；
//   - 通话转写片段（msg_type=6）：回声 / 自答噪音；
//   - 空内容的 AI 流式占位（msg_type=4 且 content 为空）：无展示价值；
//   - 工具执行卡片 / 思考过程（channel_data.grix.toolExecution / thinking）：过程噪音。
// messageAgeFromID 用雪花消息 ID 还原消息生成时刻并返回其年龄。
// ID 非法或时钟回拨导致年龄为负时返回 0，按"新消息"处理，绝不误压。
func messageAgeFromID(msgID int64) time.Duration {
	if msgID <= 0 {
		return 0
	}
	createdMillis := (msgID >> 22) + idEpochMillis
	age := time.Since(time.UnixMilli(createdMillis))
	if age < 0 {
		return 0
	}
	return age
}

// shouldSuppressStaleOfflinePush 判断一条普通消息是否因"已陈旧且用户在线"而不再弹离线横幅。
// 审批 / 呼叫卡片、ForcePush、TimeSensitive 为必达终态，永不压制。
// isImportantPush 判定一条消息是否属于"重要 / 需即时投递"——三档分级中的最高档与重要档：
//   - 审批 / 呼叫卡片、语音拨号、来电（ForcePush / TimeSensitive / 卡片内容）；
//   - 任何真人用户发出的消息（SenderType==1，文字 / 图片 / 视频 / 文件）。
//
// 只有 AI 发出的普通过程消息（SenderType==2）才是可降级、可被陈旧门压制的"普通消息"。
func isImportantPush(p pushMsgPayload) bool {
	if p.SenderType == senderTypeHuman {
		return true
	}
	return p.ForcePush || p.TimeSensitive || detectCardPushText(p.Content) != ""
}

func shouldSuppressStaleOfflinePush(ctx context.Context, userID int64, p pushMsgPayload) bool {
	// 重要消息（真人消息 / 审批 / 呼叫）永不被陈旧门压制。
	if isImportantPush(p) {
		return false
	}
	if messageAgeFromID(p.MsgID) < offlinePushStaleAge {
		return false
	}
	// 仅当用户仍有在线设备时才压制；全设备离线时旧消息仍需触达。
	return len(collectOnlineDevices(ctx, userID)) > 0
}

func shouldSuppressOfflinePush(p pushMsgPayload) bool {
	if detectCardPushText(p.Content) != "" {
		return false
	}
	if p.MsgType == model.MsgTypeCallSegment {
		return true
	}
	if p.MsgType == 4 && strings.TrimSpace(p.Content) == "" {
		return true
	}
	if len(p.Extra) > 0 {
		var env struct {
			ChannelData struct {
				Grix struct {
					ToolExecution json.RawMessage `json:"toolExecution"`
					Thinking      json.RawMessage `json:"thinking"`
				} `json:"grix"`
			} `json:"channel_data"`
		}
		if json.Unmarshal(p.Extra, &env) == nil {
			if len(env.ChannelData.Grix.ToolExecution) > 0 || len(env.ChannelData.Grix.Thinking) > 0 {
				return true
			}
		}
	}
	return false
}

func sanitizeContent(content string, msgType int16) string {
	switch msgType {
	case 2:
		return "[图片]"
	case 3:
		return "[系统通知]"
	}

	if text := detectCardPushText(content); text != "" {
		return text
	}

	// 过程噪音已在 shouldSuppressOfflinePush 拦截，到此的 msg_type=4 即终态文本回复，按原文渲染。
	// Strip markdown syntax
	clean := mdRegex.ReplaceAllString(content, "")
	clean = strings.TrimSpace(clean)
	if clean == "" {
		return "[AI消息]" // 空内容兜底
	}

	// Truncate to 60 chars
	runes := []rune(clean)
	if len(runes) > 60 {
		clean = string(runes[:60]) + "..."
	}
	return clean
}

func detectCardPushText(content string) string {
	switch {
	case strings.Contains(content, "grix://card/exec_approval"),
		strings.Contains(content, "[Exec Approval]"):
		return "有任务需要审批"
	case strings.Contains(content, "grix://card/exec_status"),
		strings.Contains(content, "[Exec Status]"):
		return "审批状态更新"
	case strings.Contains(content, "grix://card/call_owner"):
		return "请求与你语音通话"
	default:
		return ""
	}
}
