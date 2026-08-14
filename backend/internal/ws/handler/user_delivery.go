package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

var errOfflinePushUnavailable = errors.New("offline push unavailable: jetstream not initialized")

// senderTypeAgent 标识消息由 AI agent 发出（SenderType==1 为真人用户）。
const senderTypeAgent int16 = 2

// offlinePushAIDebounceWindow 是 AI 单条发送（非流式 send_msg）离线推送的合并窗口。
// AI 一轮回复常被连接器拆成多条 send_msg（先甩一句简短开场白，再发正式内容）；
// 逐条立即离线推会让用户先收到一条没什么信息量的短句通知。窗口内同一会话若又有
// 新的可合并 AI 消息，就取消重排、改用最新内容重新计时，窗口到点仍无新消息才真正推送。
// 变量（非常量）是为了让测试可以调小窗口，避免真等待秒级时间。
var offlinePushAIDebounceWindow = 4 * time.Second

var (
	offlinePushDebounceMu     sync.Mutex
	offlinePushDebounceTimers = make(map[string]*time.Timer)
)

var enqueueOfflinePushTask = func(userID int64, cmd string, payload any) error {
	if store.JS == nil {
		return errOfflinePushUnavailable
	}

	data, err := json.Marshal(map[string]any{
		"user_id": userID,
		"cmd":     cmd,
		"payload": payload,
	})
	if err != nil {
		return err
	}

	_, err = store.JS.Publish(fmt.Sprintf("im.push.offline.%d", userID), data)
	return err
}

// broadcastPushMsgToUser sends push_msg to online devices and falls back to
// offline push when no live route remains after stale-route pruning.
func broadcastPushMsgToUser(
	hub HubInterface,
	ctx context.Context,
	userID int64,
	payload protocol.PushMsgPayload,
) {
	broadcastToUserExceptDeviceWithOptions(
		hub,
		ctx,
		userID,
		"",
		protocol.CmdPushMsg,
		payload,
		true,
	)
}

// broadcastToUser sends a payload to all local connections of a user,
// plus publishes to Redis for cross-node delivery.
func broadcastToUser(hub HubInterface, ctx context.Context, userID int64, cmd string, payload any) {
	broadcastToUserExceptDeviceWithOptions(hub, ctx, userID, "", cmd, payload, false)
}

// BroadcastCallToUser exposes the existing local + Redis cross-node fan-out
// for call signaling initiated outside the handler package.
func BroadcastCallToUser(hub HubInterface, ctx context.Context, userID int64, cmd string, payload any) {
	broadcastToUser(hub, ctx, userID, cmd, payload)
}

// broadcastToUserExceptDevice sends a payload to all user devices except excludeDeviceID.
// It supports both local-node and cross-node fan-out.
func broadcastToUserExceptDevice(
	hub HubInterface,
	ctx context.Context,
	userID int64,
	excludeDeviceID string,
	cmd string,
	payload any,
) {
	broadcastToUserExceptDeviceWithOptions(hub, ctx, userID, excludeDeviceID, cmd, payload, false)
}

func broadcastToUserExceptDeviceWithOptions(
	hub HubInterface,
	ctx context.Context,
	userID int64,
	excludeDeviceID string,
	cmd string,
	payload any,
	enableOfflineFallback bool,
) (int, int) {
	logSessionChatFrontendPayload(userID, excludeDeviceID, cmd, payload)

	localDelivered := broadcastToLocalUserExceptDevice(hub, userID, excludeDeviceID, cmd, payload)

	remoteNodes, remoteLiveDevices, routeLookupOK := collectLiveRouteNodes(
		ctx,
		userID,
		excludeDeviceID,
		hub.GetNodeID(),
	)
	if routeLookupOK {
		publishToRouteNodes(ctx, userID, cmd, payload, excludeDeviceID, remoteNodes)
	}

	if enableOfflineFallback && localDelivered == 0 && (!routeLookupOK || remoteLiveDevices == 0) {
		enqueue := func() {
			if err := enqueueOfflinePushTask(userID, cmd, payload); err != nil {
				logger.L.Warnf("enqueue offline push failed user=%d cmd=%s err=%v", userID, cmd, err)
			}
		}
		if !TryDebounceOfflinePush(userID, cmd, payload, enqueue) {
			enqueue()
		}
	}

	return localDelivered, remoteLiveDevices
}

// offlinePushDebounceKeyFor 判定这条离线推送是否要走合并窗口，是则返回其去抖 key。
// 只合并 AI 的普通文字消息：真人消息、ForcePush/TimeSensitive（通话等）、审批/呼叫类
// 卡片消息一律保持原来的"立即推送"行为，不能因为等待而延误。
func offlinePushDebounceKeyFor(userID int64, cmd string, payload any) (string, bool) {
	if cmd != protocol.CmdPushMsg {
		return "", false
	}
	pm, ok := payload.(protocol.PushMsgPayload)
	if !ok {
		return "", false
	}
	if pm.SenderType != senderTypeAgent || pm.ForcePush || pm.TimeSensitive {
		return "", false
	}
	if pm.MsgType != 1 {
		return "", false
	}
	if isOfflinePushCardContent(pm.Content) {
		return "", false
	}
	return fmt.Sprintf("%d:%s", userID, pm.SessionID), true
}

// isOfflinePushCardContent 识别审批/呼叫类卡片消息，与 internal/push.detectCardPushText
// 判定的卡片种类保持一致——这类消息必达，不进入合并窗口。
func isOfflinePushCardContent(content string) bool {
	return strings.Contains(content, "grix://card/exec_approval") ||
		strings.Contains(content, "[Exec Approval]") ||
		strings.Contains(content, "grix://card/exec_status") ||
		strings.Contains(content, "[Exec Status]") ||
		strings.Contains(content, "grix://card/call_owner")
}

// TryDebounceOfflinePush 判定一条离线推送该不该并入 AI 消息合并窗口。
//
// 该合并时把 enqueue 挂进窗口并返回 true——调用方不要再自行入队，窗口到点会调它；
// 不该合并时返回 false，调用方按原路立即入队。
//
// 离线推送有两个入口，都必须经由这里，否则窗口只挡住一半：
//   - 本包 broadcastToUserExceptDeviceWithOptions：没有任何活路由时的离线兜底；
//   - internal/ws.Conn.enqueueOfflinePushAfterAckTimeout：消息投出去了但客户端
//     不回 ACK 的补推——App 退后台、僵尸路由挂着都走这条，恰恰是最常见的场景。
//
// 入队动作由调用方以闭包传入，而不是在这里统一入队：两个入口的 payload 表示不同
// （本包是 PushMsgPayload 结构体，conn 那边是发给客户端的原始 JSON bytes），
// 若在这里强行统一，就得反序列化再序列化一次，结构体没定义的字段会被悄悄丢掉。
// 判定读结构体，入队用各自的原始表示，两边都不失真。
//
// 两个入口共用同一份 timer map，所以同一 (用户,会话) 的消息不论从哪条路来都会合并，
// 不会各自开一个窗口。
func TryDebounceOfflinePush(userID int64, cmd string, payload any, enqueue func()) bool {
	debounceKey, ok := offlinePushDebounceKeyFor(userID, cmd, payload)
	if !ok || enqueue == nil {
		return false
	}
	scheduleDebouncedOfflinePush(debounceKey, enqueue)
	return true
}

// scheduleDebouncedOfflinePush 用一个短暂窗口合并同一(用户,会话)内连续到达的 AI 消息离线推送：
// 窗口内再来一条就取消旧定时器、用最新内容重新计时；窗口到点没有新消息才真正入队推送。
func scheduleDebouncedOfflinePush(debounceKey string, enqueue func()) {
	offlinePushDebounceMu.Lock()
	if timer, exists := offlinePushDebounceTimers[debounceKey]; exists {
		timer.Stop()
	}
	var timer *time.Timer
	timer = time.AfterFunc(offlinePushAIDebounceWindow, func() {
		offlinePushDebounceMu.Lock()
		// 旧 timer 触发的同时可能已被新消息的 timer 覆盖（Stop() 来不及拦下已触发的
		// 回调）：先核对身份，不是自己登记的那个就说明已经被取代，让新 timer 去推最新内容。
		if offlinePushDebounceTimers[debounceKey] != timer {
			offlinePushDebounceMu.Unlock()
			return
		}
		delete(offlinePushDebounceTimers, debounceKey)
		offlinePushDebounceMu.Unlock()
		enqueue()
	})
	offlinePushDebounceTimers[debounceKey] = timer
	offlinePushDebounceMu.Unlock()
}

func broadcastToLocalUserExceptDevice(
	hub HubInterface,
	userID int64,
	excludeDeviceID string,
	cmd string,
	payload any,
) int {
	conns := hub.GetUserConns(userID)
	delivered := 0
	for _, c := range conns {
		if excludeDeviceID != "" && c.GetDeviceID() == excludeDeviceID {
			continue
		}
		// 网站访客端不展示 agent 内部过程消息（工具执行/思考等卡片）。
		// 计入 delivered 视为已处理，避免误触发离线推送兜底。
		if pm, ok := payload.(protocol.PushMsgPayload); ok &&
			WidgetDropPush(c.GetPlatform(), cmd, pm.Content, pm.Extra) {
			delivered++
			continue
		}
		delivered++
		c.SendPayload(cmd, c.NextSeq(), payload)
	}
	return delivered
}

func collectLiveRouteNodes(
	ctx context.Context,
	userID int64,
	excludeDeviceID string,
	skipNodeID string,
) ([]string, int, bool) {
	if store.RDB == nil {
		return nil, 0, false
	}

	routeKey := fmt.Sprintf("im:ws:route:%d", userID)
	devices, err := store.RDB.HGetAll(ctx, routeKey).Result()
	if err != nil {
		logger.L.Warnf("load route map failed user=%d err=%v", userID, err)
		return nil, 0, false
	}

	nodes := make([]string, 0, len(devices))
	seenNodes := make(map[string]struct{}, len(devices))
	remoteLiveDevices := 0
	staleDevices := make([]string, 0, len(devices))

	for deviceID, rawNodeID := range devices {
		nodeID := strings.TrimSpace(rawNodeID)
		if nodeID == "" {
			staleDevices = append(staleDevices, deviceID)
			continue
		}

		exists, aliveKnown := routeAlive(ctx, userID, deviceID)
		if aliveKnown && !exists {
			staleDevices = append(staleDevices, deviceID)
			continue
		}

		if excludeDeviceID != "" && deviceID == excludeDeviceID {
			continue
		}
		if skipNodeID != "" && nodeID == skipNodeID {
			continue
		}

		remoteLiveDevices++
		if _, ok := seenNodes[nodeID]; ok {
			continue
		}
		seenNodes[nodeID] = struct{}{}
		nodes = append(nodes, nodeID)
	}

	if len(staleDevices) > 0 {
		if err := store.RDB.HDel(ctx, routeKey, staleDevices...).Err(); err != nil {
			logger.L.Warnf("prune stale routes failed user=%d devices=%v err=%v", userID, staleDevices, err)
		} else {
			logger.L.Infof("pruned stale routes user=%d devices=%v", userID, staleDevices)
		}
	}

	return nodes, remoteLiveDevices, true
}

func routeAlive(ctx context.Context, userID int64, deviceID string) (bool, bool) {
	if store.RDB == nil {
		return false, false
	}

	exists, err := store.RDB.Exists(ctx, aliveRouteKey(userID, deviceID)).Result()
	if err != nil {
		logger.L.Warnf("check route alive failed user=%d device=%s err=%v", userID, deviceID, err)
		return true, false
	}
	return exists > 0, true
}

func aliveRouteKey(userID int64, deviceID string) string {
	return fmt.Sprintf("im:ws:alive:%d:%s", userID, deviceID)
}

func publishToRouteNodes(
	ctx context.Context,
	userID int64,
	cmd string,
	payload any,
	excludeDeviceID string,
	nodeIDs []string,
) {
	if store.RDB == nil || len(nodeIDs) == 0 {
		return
	}

	data, err := json.Marshal(map[string]any{
		"user_id":           userID,
		"cmd":               cmd,
		"payload":           payload,
		"exclude_device_id": excludeDeviceID,
	})
	if err != nil {
		logger.L.Warnf("marshal cross-node payload failed user=%d cmd=%s err=%v", userID, cmd, err)
		return
	}

	for _, nodeID := range nodeIDs {
		if err := store.RDB.Publish(ctx, fmt.Sprintf("chan:%s", nodeID), string(data)).Err(); err != nil {
			logger.L.Warnf("publish cross-node payload failed user=%d node=%s cmd=%s err=%v", userID, nodeID, cmd, err)
		}
	}
}
