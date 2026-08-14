package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/redis/go-redis/v9"
)

const (
	internalCmdCallRPCRequest  = "internal:call_rpc_request"
	internalCmdCallRPCResponse = "internal:call_rpc_response"

	callRPCActionAnswer       = "answer"
	callRPCActionAnswerWithAI = "answer_with_ai"
	callRPCActionReject       = "reject"
	callRPCActionHangup       = "hangup"
	callRPCActionTakeover     = "takeover"
	callRPCActionHandBack     = "hand_back"
	callRPCActionListen       = "listen"
	callRPCActionLeave        = "leave"

	callOwnerTTL   = 6 * time.Hour
	callBusyTTL    = 6 * time.Hour
	callRPCTimeout = 5 * time.Second
)

type callRPCRequest struct {
	CorrelationID string `json:"correlation_id"`
	ReplyToNode   string `json:"reply_to_node"`
	Action        string `json:"action"`
	CallID        int64  `json:"call_id,string"`
	UserID        int64  `json:"user_id,string"`
	DeviceID      string `json:"device_id,omitempty"`
	Reason        string `json:"reason,omitempty"`
	AgentID       int64  `json:"agent_id,string,omitempty"`
}

type callRPCResponse struct {
	CorrelationID string          `json:"correlation_id"`
	OK            bool            `json:"ok"`
	Error         string          `json:"error,omitempty"`
	Cmd           string          `json:"cmd,omitempty"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

var (
	callRPCSeq     uint64
	callRPCPending sync.Map // correlationID -> chan callRPCResponse
)

// resyncHub 由 ws.Server 启动时注入(与 callCtrl 同期),供通话生命周期回调
// (如 cleanupCallGuards 广播 call:voice_status_end、队列晋升)跨节点 broadcastToUser 使用。
var resyncHub HubInterface

// SetResyncHub 注入 hub 引用。
func SetResyncHub(h HubInterface) {
	resyncHub = h
}

func callOwnerKey(callID int64) string {
	return fmt.Sprintf("im:voice:call_owner:%d", callID)
}

func rememberCallOwner(ctx context.Context, callID int64, nodeID string) error {
	if callID <= 0 || strings.TrimSpace(nodeID) == "" {
		return fmt.Errorf("invalid call owner")
	}
	if store.RDB == nil {
		return nil
	}
	if err := store.RDB.Set(ctx, callOwnerKey(callID), nodeID, callOwnerTTL).Err(); err != nil {
		logger.L.Warnf("remember call owner failed call=%d node=%s err=%v", callID, nodeID, err)
		return err
	}
	return nil
}

func forgetCallOwner(ctx context.Context, callID int64) {
	if store.RDB == nil || callID <= 0 {
		return
	}
	if err := store.RDB.Del(ctx, callOwnerKey(callID)).Err(); err != nil {
		logger.L.Warnf("forget call owner failed call=%d err=%v", callID, err)
	}
}

func lookupCallOwner(ctx context.Context, callID int64) (string, bool) {
	if store.RDB == nil || callID <= 0 {
		return "", false
	}
	nodeID, err := store.RDB.Get(ctx, callOwnerKey(callID)).Result()
	if err != nil {
		return "", false
	}
	nodeID = strings.TrimSpace(nodeID)
	return nodeID, nodeID != ""
}

func refreshCallOwner(ctx context.Context, callID int64) {
	if store.RDB == nil || callID <= 0 {
		return
	}
	_ = store.RDB.Expire(ctx, callOwnerKey(callID), callOwnerTTL).Err()
}

func callBusyKey(userID int64) string {
	return call.UserBusyKey(userID)
}

// ── owner 参与锁 ─────────────────────────────────────────────
// 不变量：一个 owner 同一时刻只能"亲自参与"一通通话（一台设备、一通 call）。
// 旁听(call:listen)与接管(call:takeover)都算参与。AI 代接不占用此锁，
// 故 AI 可并发接待多访客，而真人只单线接管。锁随 leave/hangup/断连释放。

const callParticipateTTL = 2 * time.Hour

func callParticipateKey(ownerID int64) string {
	return fmt.Sprintf("im:voice:participate:%d", ownerID)
}

func callParticipateValue(callID int64, deviceID string) string {
	return fmt.Sprintf("%d:%s", callID, deviceID)
}

// acquireParticipateLock 尝试为 owner 获取参与锁。
// 成功返回 (true, "")；已被自己同设备同通话持有则刷新 TTL 并视为成功（重入）；
// 被其它设备/其它通话持有则返回 (false, 当前持有者值)。
func acquireParticipateLock(ctx context.Context, ownerID, callID int64, deviceID string) (bool, string) {
	if store.RDB == nil {
		return true, ""
	}
	key := callParticipateKey(ownerID)
	val := callParticipateValue(callID, deviceID)
	ok, err := store.RDB.SetNX(ctx, key, val, callParticipateTTL).Result()
	if err != nil {
		logger.L.Warnf("call trace: participate acquire redis_err owner=%d call=%d err=%v", ownerID, callID, err)
		return true, "" // redis 抖动放行，避免误杀
	}
	if ok {
		return true, ""
	}
	cur, _ := store.RDB.Get(ctx, key).Result()
	if cur == val {
		store.RDB.Expire(ctx, key, callParticipateTTL)
		return true, ""
	}
	logger.L.Infof("call trace: participate busy owner=%d call=%d holder=%s", ownerID, callID, cur)
	return false, cur
}

// refreshParticipateLock 续期参与锁（owner 仍在房间内时调用）。
func refreshParticipateLock(ctx context.Context, ownerID, callID int64, deviceID string) {
	if store.RDB == nil {
		return
	}
	key := callParticipateKey(ownerID)
	cur, err := store.RDB.Get(ctx, key).Result()
	if err != nil || cur != callParticipateValue(callID, deviceID) {
		return
	}
	store.RDB.Expire(ctx, key, callParticipateTTL)
}

// releaseParticipateLock 释放参与锁：仅当当前持有者正是该 call+device 时删除（幂等）。
func releaseParticipateLock(ctx context.Context, ownerID, callID int64, deviceID string) {
	if store.RDB == nil || ownerID <= 0 {
		return
	}
	key := callParticipateKey(ownerID)
	cur, err := store.RDB.Get(ctx, key).Result()
	if err != nil {
		return
	}
	if cur != callParticipateValue(callID, deviceID) {
		return
	}
	if err := store.RDB.Del(ctx, key).Err(); err != nil {
		logger.L.Warnf("call trace: participate release_err owner=%d call=%d err=%v", ownerID, callID, err)
	}
}

// releaseParticipateLockByDevice 断连兜底：owner 某设备掉线时，
// 若参与锁正被该设备持有则释放，避免锁悬挂阻塞其它设备。
// releaseParticipateLockByCall 通话结束兜底：若参与锁正被该通话持有则释放（不限设备）。
func releaseParticipateLockByCall(ctx context.Context, ownerID, callID int64) {
	if store.RDB == nil || ownerID <= 0 || callID <= 0 {
		return
	}
	key := callParticipateKey(ownerID)
	cur, err := store.RDB.Get(ctx, key).Result()
	if err != nil {
		return
	}
	if !strings.HasPrefix(cur, strconv.FormatInt(callID, 10)+":") {
		return
	}
	if err := store.RDB.Del(ctx, key).Err(); err != nil {
		logger.L.Warnf("call trace: participate release_by_call_err owner=%d call=%d err=%v", ownerID, callID, err)
	}
}

func releaseParticipateLockByDevice(ctx context.Context, ownerID int64, deviceID string) {
	if store.RDB == nil || ownerID <= 0 || deviceID == "" {
		return
	}
	key := callParticipateKey(ownerID)
	cur, err := store.RDB.Get(ctx, key).Result()
	if err != nil {
		return
	}
	// value 形如 "<callID>:<deviceID>"，按设备后缀匹配
	if !strings.HasSuffix(cur, ":"+deviceID) {
		return
	}
	if err := store.RDB.Del(ctx, key).Err(); err != nil {
		logger.L.Warnf("call trace: participate release_by_device_err owner=%d device=%s err=%v", ownerID, deviceID, err)
	}
}

type callBusyGuard struct {
	callID int64
	users  []int64
	active bool
}

func reserveCallBusy(ctx context.Context, callID int64, userIDs ...int64) (*callBusyGuard, error) {
	if store.RDB == nil {
		return &callBusyGuard{callID: callID}, nil
	}
	guard := &callBusyGuard{callID: callID}
	for _, userID := range userIDs {
		if userID <= 0 {
			continue
		}
		already := false
		for _, existing := range guard.users {
			if existing == userID {
				already = true
				break
			}
		}
		if already {
			continue
		}
		ok, err := store.RDB.SetNX(ctx, callBusyKey(userID), strconv.FormatInt(callID, 10), callBusyTTL).Result()
		if err != nil {
			guard.release(ctx)
			return nil, err
		}
		if !ok {
			existingCallID, _ := store.RDB.Get(ctx, callBusyKey(userID)).Result()
			logger.L.Warnf("call trace: reserve_busy conflict call=%d user=%d existing_call=%s", callID, userID, existingCallID)
			guard.release(ctx)
			return nil, call.ErrCallerBusy
		}
		guard.users = append(guard.users, userID)
	}
	guard.active = true
	logger.L.Infof("call trace: reserve_busy ok call=%d users=%v", callID, guard.users)
	return guard, nil
}

func releaseCallBusyForRecord(ctx context.Context, rec model.CallRecord) {
	releaseCallBusyForUsers(ctx, rec.ID, rec.CallerID, rec.CalleeID)
}

func releaseCallBusyForUsers(ctx context.Context, callID int64, userIDs ...int64) {
	if store.RDB == nil || callID <= 0 {
		return
	}
	for _, userID := range userIDs {
		if userID <= 0 {
			continue
		}
		releaseCallBusyKey(ctx, callID, userID)
	}
}

func releaseCallBusyKey(ctx context.Context, callID, userID int64) {
	key := callBusyKey(userID)
	current, err := store.RDB.Get(ctx, key).Result()
	if err != nil {
		if err != redis.Nil {
			logger.L.Warnf("load call busy key failed call=%d user=%d err=%v", callID, userID, err)
		}
		return
	}
	if current != strconv.FormatInt(callID, 10) {
		return
	}
	if err := store.RDB.Del(ctx, key).Err(); err != nil {
		logger.L.Warnf("release call busy key failed call=%d user=%d err=%v", callID, userID, err)
	}
}

func (g *callBusyGuard) release(ctx context.Context) {
	if g == nil || !g.active {
		return
	}
	for _, userID := range g.users {
		releaseCallBusyKey(ctx, g.callID, userID)
	}
	g.active = false
}

func cleanupCallGuards(ctx context.Context, rec model.CallRecord) {
	logger.L.Infof("call trace: cleanup_guards call=%d caller=%d callee=%d", rec.ID, rec.CallerID, rec.CalleeID)
	forgetCallOwner(ctx, rec.ID)
	releaseCallBusyForRecord(ctx, rec)
	// AI 代接通话结束时释放 agent 并发计数（仅访客客服路径会预留，SREM 幂等，非成员无副作用）。
	// 释放后立即晋升队列中的下一个等待访客。
	if rec.DelegatedAgentID != nil {
		releaseVoiceConcurrent(*rec.DelegatedAgentID, rec.ID)
		promoteNextQueued(*rec.DelegatedAgentID, resyncHub)
	}
	// 释放 owner 参与锁（直拨 owner=caller、客服 owner=callee，两侧都试），
	// 避免锁悬挂阻塞其它通话/设备。
	releaseParticipateLockByCall(ctx, rec.CalleeID, rec.ID)
	releaseParticipateLockByCall(ctx, rec.CallerID, rec.ID)
	// 广播"语音结束"，让 owner 各端清除会话列表"语音中"徽标。
	if resyncHub != nil && rec.CalleeID > 0 {
		broadcastToUser(resyncHub, ctx, rec.CalleeID, protocol.CmdCallVoiceStatusEnd, protocol.CallVoiceStatusEndPayload{
			CallID:    strconv.FormatInt(rec.ID, 10),
			SessionID: rec.SessionID,
		})
	}
	// 通话结束时清理接点B超时兜底用的 caller 转写时间戳，避免按 session 持续累积。
	ClearCallerTranscript(rec.SessionID)
	// flush 尚未提交的访客句子缓冲（debounce 窗口内的最后一段话）。
	FlushCallTranscript(rec.ID)
}

func maybeRouteCallRPC(ctx context.Context, hub HubInterface, req callRPCRequest) (callRPCResponse, bool) {
	ownerNode, ok := lookupCallOwner(ctx, req.CallID)
	if !ok || ownerNode == hub.GetNodeID() {
		return callRPCResponse{}, false
	}
	resp, err := publishCallRPC(ctx, hub.GetNodeID(), ownerNode, req)
	if err != nil {
		return callRPCResponse{OK: false, Error: err.Error()}, true
	}
	return resp, true
}

func publishCallRPC(ctx context.Context, selfNodeID, ownerNode string, req callRPCRequest) (callRPCResponse, error) {
	if store.RDB == nil {
		return callRPCResponse{}, fmt.Errorf("redis unavailable")
	}
	req.CorrelationID = newCallRPCCorrelationID(selfNodeID)
	req.ReplyToNode = selfNodeID

	ch := make(chan callRPCResponse, 1)
	callRPCPending.Store(req.CorrelationID, ch)
	defer callRPCPending.Delete(req.CorrelationID)

	data, err := json.Marshal(map[string]any{
		"cmd":     internalCmdCallRPCRequest,
		"payload": req,
	})
	if err != nil {
		return callRPCResponse{}, err
	}
	if err := store.RDB.Publish(ctx, fmt.Sprintf("chan:%s", ownerNode), string(data)).Err(); err != nil {
		return callRPCResponse{}, err
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(callRPCTimeout):
		return callRPCResponse{}, fmt.Errorf("call owner node timeout: %s", ownerNode)
	case <-ctx.Done():
		return callRPCResponse{}, ctx.Err()
	}
}

func newCallRPCCorrelationID(nodeID string) string {
	seq := atomic.AddUint64(&callRPCSeq, 1)
	return fmt.Sprintf("%s-%d-%d", nodeID, time.Now().UnixNano(), seq)
}

func deliverCallRPCResponse(conn ConnInterface, seq int64, resp callRPCResponse) {
	if !resp.OK {
		conn.SendPayload(protocol.CmdError, seq, errPayload(resp.Error))
		return
	}
	if resp.Cmd == "" {
		return
	}
	conn.SendPacket(&protocol.Packet{Cmd: resp.Cmd, Seq: seq, Payload: resp.Payload})
}

func handleCallRPCRequest(raw json.RawMessage) bool {
	var req callRPCRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		logger.L.Warnf("call rpc request unmarshal error: %v", err)
		return true
	}
	resp := executeCallRPCRequest(req)
	resp.CorrelationID = req.CorrelationID
	publishCallRPCResponse(req.ReplyToNode, resp)
	return true
}

func handleCallRPCResponse(raw json.RawMessage) bool {
	var resp callRPCResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		logger.L.Warnf("call rpc response unmarshal error: %v", err)
		return true
	}
	if v, ok := callRPCPending.Load(resp.CorrelationID); ok {
		ch, ok := v.(chan callRPCResponse)
		if ok {
			select {
			case ch <- resp:
			default:
			}
		}
	}
	return true
}

func publishCallRPCResponse(replyToNode string, resp callRPCResponse) {
	if store.RDB == nil || strings.TrimSpace(replyToNode) == "" {
		return
	}
	data, err := json.Marshal(map[string]any{
		"cmd":     internalCmdCallRPCResponse,
		"payload": resp,
	})
	if err != nil {
		logger.L.Warnf("marshal call rpc response failed correlation=%s err=%v", resp.CorrelationID, err)
		return
	}
	if err := store.RDB.Publish(context.Background(), fmt.Sprintf("chan:%s", replyToNode), string(data)).Err(); err != nil {
		logger.L.Warnf("publish call rpc response failed node=%s correlation=%s err=%v", replyToNode, resp.CorrelationID, err)
	}
}

func executeCallRPCRequest(req callRPCRequest) callRPCResponse {
	logger.L.Infof("call trace: execute_rpc action=%s call=%d user=%d", req.Action, req.CallID, req.UserID)
	if callCtrl == nil {
		return callRPCResponse{OK: false, Error: "call service unavailable"}
	}
	refreshCallOwner(context.Background(), req.CallID)
	switch req.Action {
	case callRPCActionAnswer:
		token, roomURL, err := callCtrl.Answer(context.Background(), req.CallID, req.UserID)
		if err != nil {
			return callRPCError(err)
		}
		return callRPCPacket(protocol.CmdCallPeerAnswered, protocol.CallPeerAnsweredPayload{
			CallID:     strconv.FormatInt(req.CallID, 10),
			Mode:       "human",
			RoomToken:  token,
			RoomURL:    roomURL,
			ICEServers: callICEServers(),
		})
	case callRPCActionAnswerWithAI:
		spec, err := resolveAgentVoiceSpec(req.AgentID, "")
		if err != nil {
			return callRPCError(err)
		}
		if !reserveVoiceDailyQuota(req.AgentID, spec.DailyLimit) {
			return callRPCResponse{OK: false, Error: "今日语音托管次数已达上限"}
		}
		token, roomURL, err := callCtrl.AnswerWithAI(context.Background(), req.CallID, req.UserID, spec)
		if err != nil {
			return callRPCError(err)
		}
		return callRPCPacket(protocol.CmdCallPeerAnswered, protocol.CallPeerAnsweredPayload{
			CallID:     strconv.FormatInt(req.CallID, 10),
			Mode:       "ai_delegated",
			RoomToken:  token,
			RoomURL:    roomURL,
			ICEServers: callICEServers(),
		})
	case callRPCActionReject:
		if err := callCtrl.Reject(context.Background(), req.CallID, req.UserID, req.Reason); err != nil {
			return callRPCError(err)
		}
		forgetCallOwner(context.Background(), req.CallID)
		return callRPCResponse{OK: true}
	case callRPCActionHangup:
		if err := callCtrl.Hangup(context.Background(), req.CallID, req.UserID); err != nil {
			return callRPCError(err)
		}
		forgetCallOwner(context.Background(), req.CallID)
		return callRPCResponse{OK: true}
	case callRPCActionTakeover:
		if err := callCtrl.Takeover(context.Background(), req.CallID, req.UserID); err != nil {
			return callRPCError(err)
		}
		return callRPCPacket(protocol.CmdCallState, protocol.CallAIStatePayload{
			CallID: strconv.FormatInt(req.CallID, 10),
			Mode:   "human_active",
			Ts:     time.Now().UnixMilli(),
		})
	case callRPCActionHandBack:
		if err := callCtrl.HandBack(context.Background(), req.CallID, req.UserID); err != nil {
			return callRPCError(err)
		}
		return callRPCPacket(protocol.CmdCallState, protocol.CallAIStatePayload{
			CallID: strconv.FormatInt(req.CallID, 10),
			Mode:   "ai_delegated",
			Ts:     time.Now().UnixMilli(),
		})
	case callRPCActionListen:
		return doCallListen(context.Background(), req.CallID, req.UserID, req.DeviceID)
	case callRPCActionLeave:
		return doCallLeave(context.Background(), req.CallID, req.UserID, req.DeviceID)
	default:
		return callRPCResponse{OK: false, Error: "unknown call rpc action"}
	}
}

func callRPCError(err error) callRPCResponse {
	if err == nil {
		return callRPCResponse{OK: true}
	}
	return callRPCResponse{OK: false, Error: err.Error()}
}

func callRPCPacket(cmd string, payload any) callRPCResponse {
	data, err := json.Marshal(payload)
	if err != nil {
		return callRPCError(err)
	}
	return callRPCResponse{OK: true, Cmd: cmd, Payload: data}
}

func routeOrSendCallRPC(ctx context.Context, hub HubInterface, conn ConnInterface, seq int64, req callRPCRequest) bool {
	resp, routed := maybeRouteCallRPC(ctx, hub, req)
	if !routed {
		return false
	}
	deliverCallRPCResponse(conn, seq, resp)
	if resp.OK && (req.Action == callRPCActionAnswer || req.Action == callRPCActionAnswerWithAI) {
		notifyCallAnsweredElsewhere(hub, req.UserID, req.CallID, req.DeviceID)
	}
	return true
}

func isCallerBusy(err error) bool {
	return err != nil && strings.Contains(err.Error(), call.ErrCallerBusy.Error())
}
