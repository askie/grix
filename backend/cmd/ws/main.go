package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/askie/grix/backend/config"
	apiservice "github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/callbridge"
	"github.com/askie/grix/backend/internal/callsegment"
	"github.com/askie/grix/backend/internal/llmclient"
	"github.com/askie/grix/backend/internal/model"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/proclock"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/voicerefiner"
	"github.com/askie/grix/backend/internal/ws"
	"github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/agentmsg"
	"github.com/askie/grix/backend/internal/ws/handler"
	"github.com/askie/grix/backend/internal/ws/protocol"
	// lksdk "github.com/livekit/server-sdk-go/v2" // TODO: 恢复 Egress 时取消注释
)

func main() {
	configPath := "config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	logger.Init()

	lock := proclock.Acquire("ws")
	defer lock.Release()

	config.Load(configPath)

	// Initialize infrastructure
	store.InitPostgres(config.C.Postgres)
	store.MaybeInitSchema()
	store.InitRedis(config.C.Redis)
	store.InitNATS(config.C.NATS)

	if err := snowflake.Init(config.C.Snowflake.MachineID); err != nil {
		logger.L.Fatalf("snowflake init error: %v", err)
	}

	if err := apiservice.InitOSS(); err != nil {
		logger.L.Warnf("oss init warning (media signing will be skipped): %v", err)
	}

	jwtpkg.Init(config.C.JWT.Secret, config.C.JWT.AccessTTL, config.C.JWT.RefreshTTL)

	// Initialize Call Controller
	lkCfg := config.C.LiveKit
	var roomProvider call.RoomProvider
	if lkCfg.URL != "" && lkCfg.APIKey != "" && lkCfg.APISecret != "" {
		roomProvider = call.NewLiveKitRoomManager(lkCfg.URL, lkCfg.PublicURL, lkCfg.APIKey, lkCfg.APISecret)
		if lkCfg.PublicURL != "" {
			logger.L.Infof("livekit room manager initialized control_url=%s public_url=%s", lkCfg.URL, lkCfg.PublicURL)
		} else {
			logger.L.Infof("livekit room manager initialized control_url=%s public_url=<same-as-control>", lkCfg.URL)
		}
	} else {
		roomProvider = &call.NoopRoomManager{}
		logger.L.Warn("livekit not configured, using noop room manager")
	}
	callRecordStore := store.NewCallRecordStore(store.DB)

	// Start WS server
	agentAPIPath := strings.TrimRight(config.C.Server.AgentAPIPath, "/") + "/" +
		strings.TrimLeft(config.C.Server.AgentAPIWSPath, "/")
	server := ws.NewServer(
		config.C.Server.WSPort,
		config.C.Server.NodeID,
		config.C.Server.AllowedWebOrigins,
		agentAPIPath,
		config.C.Server.AgentAPIHeartbeat,
		config.C.Server.PprofSecret,
		config.C.Server.WidgetEnabled,
	)

	// Egress recorder (temporarily disabled — 未部署 Egress 服务，启用会导致通话延迟)
	// TODO: 部署 LiveKit Egress 后恢复以下代码
	// var egressRecorder *call.EgressRecorder
	// if lkCfg.URL != "" && lkCfg.APIKey != "" && lkCfg.APISecret != "" {
	// 	egressClient := lksdk.NewEgressClient(lkCfg.URL, lkCfg.APIKey, lkCfg.APISecret)
	// 	egressRecorder = call.NewEgressRecorder(egressClient)
	// }
	var egressRecorder *call.EgressRecorder // nil = recording disabled

	// Wire Call Controller
	bridgeMgr := callbridge.NewNATSBridgeManager(store.NC)
	callCtrl := call.NewWithBridge(roomProvider, callRecordStore, server.NotifyUser, bridgeMgr)
	callCtrl.SetRecorder(egressRecorder)
	callCtrl.SetEndHook(makeCallSummaryHook(server))
	handler.SetCallController(callCtrl)
	handler.SetResyncHub(server.GetHub())

	// 启动时清理孤立通话：释放上次 ws 重启前未清理的 busy key，并将通话状态置为 error。
	handler.CleanupOrphanCalls(context.Background(), callRecordStore)

	// 注入 TURN/STUN ICE 服务器配置，随 invite_ack 下发给客户端
	if lkCfg.Turn.Enabled && len(lkCfg.Turn.URLs) > 0 {
		handler.SetICEServers([]protocol.ICEServer{
			{URLs: lkCfg.Turn.URLs, Username: lkCfg.Turn.Username, Credential: lkCfg.Turn.Credential},
		})
	}

	// 用户所有设备离线时自动结束其通话，防止通话因客户端消失而卡死
	server.SetOnUserAllDevicesOffline(callCtrl.CleanupUserCalls)

	// 用户上线/重连时的"语音中"状态对账已统一并入 pull_sync 全量快照
	// （HandlePullSync → active_voice_calls，以 DB 为准、客户端整份覆盖），
	// 不再单独走 OnUserRegistered 重推，避免两套并行机制。

	// Phase 3: NATS transcript consumer
	if store.NC != nil {
		// Refiner：复用 Translation LLM 配置做转写改写；未配置 API Key 时降级为 Noop
		var refiner voicerefiner.TranscriptRefiner = &voicerefiner.NoopRefiner{}
		if key := strings.TrimSpace(config.C.LLM.Translation.APIKey); key != "" {
			lc, lerr := llmclient.New(llmclient.Config{
				APIKey:          key,
				BaseURL:         strings.TrimSpace(config.C.LLM.Translation.BaseURL),
				ProxyURL:        strings.TrimSpace(config.C.LLM.Translation.ProxyURL),
				APIStyle:        strings.TrimSpace(config.C.LLM.Translation.APIStyle),
				DefaultModel:    strings.TrimSpace(config.C.LLM.Translation.Model),
				ReasoningEffort: strings.TrimSpace(config.C.LLM.Translation.ReasoningEffort),
				RequestTimeout:  time.Duration(config.C.LLM.Translation.RequestTimeoutSec) * time.Second,
			})
			if lerr != nil {
				logger.L.Warnf("voice refiner llm init failed, fallback to noop: %v", lerr)
			} else {
				refiner = voicerefiner.NewLLMRefiner(lc, strings.TrimSpace(config.C.LLM.Translation.Model))
			}
		}
		countUpdater := func(ctx context.Context, callID string) error {
			id, _ := strconv.ParseInt(callID, 10, 64)
			if id == 0 {
				return nil
			}
			return callRecordStore.UpdateSegmentCount(ctx, id)
		}
		segmentWriter := callsegment.New(
			refiner,
			func(ctx context.Context, req agentapi.SendMessageReq) (*agentapi.SendMessageResult, error) {
				return server.AgentAPISend(ctx, req)
			},
			countUpdater,
		)
		transcriptConsumer := callsegment.NewNATSConsumer(
			store.NC,
			segmentWriter,
			callsegment.DBLookup(callRecordStore),
		)
		transcriptConsumer.SetMemoryLookup(func(callID int64) (callsegment.TranscriptRoute, bool) {
			sessionID, ownerID, agentID, callerID, calleeID, ok := callCtrl.GetTranscriptRouteByCallID(callID)
			if !ok {
				return callsegment.TranscriptRoute{}, false
			}
			return callsegment.TranscriptRoute{
				SessionID: sessionID,
				OwnerID:   ownerID,
				AgentID:   agentID,
				CallerID:  callerID,
				CalleeID:  calleeID,
				// 与 callsegment.DBLookup 同款判定，内存兜底路径不得丢失直拨标记
				DirectAICall: agentID > 0 && agentID == calleeID,
			}, true
		})
		// 跨节点活跃通话反查兜底：转写消费（queue group 随机节点）和 Hermes 回复
		// （agent 连接节点）不一定落在 call owner 节点，内存态查不到时以 DB 状态为准。
		// 时间窗限定接听 40 分钟内（单通硬上限 30 分钟），歧义（同会话多通活跃）宁可不注入。
		handler.SetActiveVoiceCallDBLookup(func(ctx context.Context, sessionID string) (int64, string, bool, bool) {
			records, err := callRecordStore.ListActiveAIDelegatedBySession(ctx, sessionID, 40*time.Minute)
			if err != nil || len(records) == 0 {
				return 0, "", false, false
			}
			if len(records) > 1 {
				logger.L.Warnf("voice active call db lookup ambiguous session=%s n=%d, skip", sessionID, len(records))
				return 0, "", false, false
			}
			rec := records[0]
			direct := rec.DelegatedAgentID != nil && *rec.DelegatedAgentID > 0 && *rec.DelegatedAgentID == rec.CalleeID
			return rec.ID, rec.AIProvider, direct, true
		})
		// 接点A：访客(caller)转写写入后触发会话已配置的文字托管（文字大脑）。
		// senderType=1（人类访客）、msgType=6（call_segment）、全员可见；
		// checkDelegates 自身会跳过 sender 本人的成员托管并复用现有限流。
		// 防御性guard：仅当该会话存在活跃AI通话时才触发，避免手动NATS注入/历史补写误触发。
		// 反查须跨节点正确（LookupActiveVoiceCall：内存优先 + DB 兜底）。
		transcriptConsumer.SetDelegateTrigger(func(ctx context.Context, sessionID string, senderID int64, triggerMsgID int64, content string, selfTrigger bool) {
			if _, _, _, ok := handler.LookupActiveVoiceCall(ctx, sessionID); !ok {
				return // 非通话期间，不触发文字托管
			}
			// 记录 caller 转写时间戳，供接点B超时兜底判定（架构文档 29 §5）
			handler.RecordCallerTranscript(sessionID)
			if selfTrigger {
				// 直拨通话：说话人即托管 owner 本人，放行 skip-self（架构文档 33）
				handler.TriggerSelfDelegateForMessage(
					server.GetHub(), ctx, sessionID, senderID, 1,
					triggerMsgID, 0, int16(model.MsgTypeCallSegment), content, nil, nil,
				)
				return
			}
			handler.TriggerDelegatesForMessage(
				server.GetHub(), ctx, sessionID, senderID, 1,
				triggerMsgID, 0, int16(model.MsgTypeCallSegment), content, nil, nil,
			)
		})
		handler.SetTranscriptFlusher(transcriptConsumer.FlushCall)
		if err := transcriptConsumer.Start(); err != nil {
			logger.L.Warnf("transcript consumer start failed: %v", err)
		}
		defer transcriptConsumer.Stop()
		logger.L.Info("transcript consumer started")

		// Bridge 退出兜底：voicebridge 进程异常退出时自动结束通话、释放 busy key
		bridgeExitConsumer := callbridge.NewBridgeExitConsumer(store.NC, func(ctx context.Context, callID int64) {
			callCtrl.EndCallOnBridgeExit(ctx, callID)
		})
		if err := bridgeExitConsumer.Start(); err != nil {
			logger.L.Warnf("bridge_exit consumer start failed: %v", err)
		}
		defer bridgeExitConsumer.Stop()
	}

	// 僵尸路由清道夫：清掉进程被强杀时来不及注销的 im:ws:route 残留条目
	janitorCtx, stopJanitor := context.WithCancel(context.Background())
	defer stopJanitor()
	ws.StartRouteJanitor(janitorCtx)

	go func() {
		if err := server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.L.Fatalf("ws server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.L.Info("ws server shutting down")
	stopJanitor()
	callCtrl.Shutdown(context.Background())
	server.Shutdown()
	// 内容审核 worker 是 ws 进程拉起来的（发消息触发），关停时要等它把手上的
	// 审核任务做完再退——半途掐断就是一条违规消息没被撤回。
	apiservice.StopContentModerationWorkers()
}

func makeCallSummaryHook(server *ws.Server) func(ctx context.Context, rec model.CallRecord, recordingURL string) {
	return func(ctx context.Context, rec model.CallRecord, recordingURL string) {
		sessionID := rec.SessionID
		if sessionID == "" {
			return
		}
		durationSec := 0
		if rec.DurationSeconds != nil {
			durationSec = *rec.DurationSeconds
		}
		extra := call.BuildSummaryExtra(rec.ID, durationSec, recordingURL)
		content := fmt.Sprintf("通话结束，时长 %d 秒", durationSec)
		if recordingURL != "" {
			content += "，含录音"
		}
		ownerID := rec.CalleeID
		agentID := int64(0)
		if rec.DelegatedAgentID != nil {
			agentID = *rec.DelegatedAgentID
		}
		// DirectAICall: agent == CalleeID → owner = CallerID
		if agentID > 0 && agentID == rec.CalleeID {
			ownerID = rec.CallerID
		}
		// 非 AI 托管通话没有可用 agent 身份，不走 AgentAPISend，避免无关 permission denied 噪音。
		if agentID <= 0 {
			return
		}
		_, err := server.AgentAPISend(ctx, agentapi.SendMessageReq{
			AgentID:      agentID,
			OwnerID:      ownerID,
			IdentityMode: agentmsg.ModeDelegate,
			SessionID:    sessionID,
			ClientMsgID:  fmt.Sprintf("voice-call-summary:%d", rec.ID),
			MsgType:      model.MsgTypeSystem,
			Content:      content,
			Extra:        extra,
		})
		if err != nil {
			logger.L.Warnf("call_summary send failed call=%d err=%v", rec.ID, err)
		}
	}
}
