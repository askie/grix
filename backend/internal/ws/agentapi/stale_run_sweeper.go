package agentapi

import (
	"context"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

const (
	// staleRunningReapThreshold 判定僵尸 running 的保守阈值：chat_states 行
	// 停留在 state=running 且 updated_at 超过该时长未动，才进入候选集。
	// 取 2 小时是为了盖住真实长任务（深度研究/长构建）的正常耗时——阈值越保守，
	// 误收活 run 的概率越低；僵尸行的代价只是"正在输入"多挂一会儿。
	staleRunningReapThreshold = 2 * time.Hour
	// staleRunningSweepInterval 清扫周期。僵尸不是时效敏感问题，10 分钟一轮足够。
	staleRunningSweepInterval = 10 * time.Minute
	// staleRunningSweepBatch 单轮处理的候选行上限，余下的留给下一轮。
	staleRunningSweepBatch = 200
	// staleRunningReapedStopReason 僵尸结算写入的 stop_reason，标明来源是清扫器
	// 而非 connector 终态，便于线上排查区分。
	staleRunningReapedStopReason = "stale_running_reaped"
)

// StartStaleRunSweeper 启动僵尸 running 周期清扫。
//
// 背景：chat_states 的终态只由 connector 的 event_result 写入（"connector
// 权威"，pending_event_tracker 的超时仅观测不结算）。当 connector/agent 进程在
// 任务结束后、终态上报前重启或崩溃，行就永远停在 running：chat_state_query 一直
// 显示"运行中"，前端"正在输入"胶囊也永不消失。本清扫器是独立于观测逻辑的兜底：
// 对超时未动的 running 行，在全部保守条件满足时结算为 idle 并广播终态。
//
// 多节点部署下的安全性靠山：
//   - durable run 记录（Redis，跨节点可见）：别节点仍存活的 run 会有未过期的
//     ack/result 记录，本节点不会误收；
//   - DB 行的 last_run_id / run_generation 守卫：结算只在行仍是扫描时的那个
//     run、且仍非终态时生效，并发到达的正常终态或新 run 不会被覆盖；
//   - 保守阈值（2h）：即便 durable 记录恰好也过期，真实任务跨越 2 小时且无任何
//     心跳刷新的概率极低。
//
// 生命周期挂在 Manager 的后台工作组上：随 Shutdown 的 stop 广播退出，
// 不会活过关停后继续读写已关闭的 DB。
func (m *Manager) StartStaleRunSweeper() {
	if m == nil {
		return
	}
	m.goBackground(func() {
		// 启动即先扫一轮：进程重启往往正是僵尸产生的时刻（旧进程崩溃留下的行），
		// 无需等第一个 tick。
		m.sweepStaleRunningRuns()
		ticker := time.NewTicker(staleRunningSweepInterval)
		defer ticker.Stop()
		stop := m.stopping()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				m.sweepStaleRunningRuns()
			}
		}
	})
}

// sweepStaleRunningRuns 执行一轮清扫：捞出超时候选行，逐行过保守条件后结算。
func (m *Manager) sweepStaleRunningRuns() {
	if m == nil {
		return
	}
	cutoff := time.Now().Add(-staleRunningReapThreshold)
	rows, err := store.ListStaleRunningSessionAgentStates(cutoff, staleRunningSweepBatch)
	if err != nil {
		logger.L.Warnf("stale running sweep: list candidates failed err=%v", err)
		return
	}
	for _, row := range rows {
		m.maybeReapStaleRunningRow(row, cutoff)
	}
}

// maybeReapStaleRunningRow 对单个候选行执行保守判定，全部通过才结算。
// 任何一项证据存疑（本节点有活跃 run、durable 记录未过期、有更新的 pending
// dispatch、检查本身出错）都直接放过——清扫器宁可漏收一轮，也绝不误收活 run。
func (m *Manager) maybeReapStaleRunningRow(row model.SessionAgentState, cutoff time.Time) {
	sessionID := strings.TrimSpace(row.SessionID)
	runID := strings.TrimSpace(row.LastRunID)
	if sessionID == "" || row.OwnerID <= 0 || row.AgentID <= 0 || runID == "" {
		return
	}

	// 条件一：本节点内存里没有该 session+owner 的活跃 run。
	if m.hasActiveRunForSessionOwner(sessionID, row.OwnerID) {
		return
	}
	// 条件二：Redis durable run 记录不存在或同样过期。记录未过期说明 run 可能
	// 仍在（本节点元数据丢失或别节点执行中），必须让路。
	if m.hasFreshDurableRunForSession(row.OwnerID, sessionID, row.AgentID, cutoff) {
		return
	}
	// 条件三：没有指向该会话的更新 pending dispatch（终端台账里的跨节点栅栏）。
	newer, err := store.HasNewerPendingAgentEventDispatch(runID, sessionID, row.OwnerID, row.AgentID)
	if err != nil {
		logger.L.Warnf(
			"stale running sweep: newer-dispatch check failed, skip session=%s owner=%d run=%s err=%v",
			sessionID, row.OwnerID, runID, err,
		)
		return
	}
	if newer {
		return
	}

	// 结算守卫与正常终态同款：行被并发正常 settle、或被新 run 接管时 changed=false，
	// 此时不广播——状态没有因清扫而变化，没必要制造一帧多余的终态。
	changed, err := store.SettleStaleRunningSessionAgentState(row, staleRunningReapedStopReason)
	if err != nil {
		logger.L.Warnf(
			"stale running sweep: settle failed session=%s owner=%d run=%s err=%v",
			sessionID, row.OwnerID, runID, err,
		)
		return
	}
	if !changed {
		return
	}

	logger.L.Warnf(
		"stale running reaped session=%s owner=%d agent=%d run=%s started_at=%v",
		sessionID, row.OwnerID, row.AgentID, runID, row.StartedAt,
	)
	// 内存里本就没有这个 run，造一个终态快照走 emitOutputStatus 的既有路径：
	// outputStatusFn（server.notifyAgentOutputStatus）会向主人广播
	// agent_output_status(stopped)，并在终态分支里清掉该会话的 composing 活动。
	var startedAtMs int64
	if row.StartedAt != nil {
		startedAtMs = row.StartedAt.UnixMilli()
	}
	reaped := &activeAgentRun{
		EventID:       runID,
		SessionID:     sessionID,
		OwnerID:       row.OwnerID,
		AgentID:       row.AgentID,
		State:         protocol.AgentOutputStateStopped,
		CanStop:       false,
		StopReason:    staleRunningReapedStopReason,
		StartedAt:     startedAtMs,
		RunGeneration: row.RunGeneration,
		UpdatedAt:     time.Now().UnixMilli(),
	}
	m.emitOutputStatus(reaped)
}

// hasFreshDurableRunForSession 只读检查 Redis durable 记录：存在处于 ack/result
// 阶段且 updated_at 未过 cutoff 的记录，说明对应 run 可能仍存活，清扫必须让路。
// 记录缺失、阶段已进入终态、或记录本身同样超时，才视为无活跃证据。
//
// 索引用 ZScan 全量迭代（与 hasOtherDurableActiveRun 同款），不允许截断：
// 高并发 agent 的 durable 索引可能超过一个批次，目标记录按 score 排在前面批次
// 之外时，截断扫描会把活 run 误判成无证据而误收。正常路径靠"找到即提前返回"
// 保持快速；索引很大时全扫一轮的代价可接受——清扫 10 分钟一轮、每个候选行一次。
// 读取失败（含迭代中途出错）按"有证据"处理，保守跳过本轮。
func (m *Manager) hasFreshDurableRunForSession(ownerID int64, sessionID string, agentID int64, cutoff time.Time) bool {
	if store.RDB == nil || ownerID <= 0 || agentID <= 0 {
		return false
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false
	}
	ctx := context.Background()
	cutoffMs := cutoff.UnixMilli()
	var cursor uint64
	for {
		members, next, err := store.RDB.ZScan(
			ctx,
			durablePendingDelegateIndexKey(agentID),
			cursor,
			"*",
			durablePendingDelegateDrainBatch,
		).Result()
		if err != nil {
			logger.L.Warnf(
				"stale running sweep: durable index scan failed, treat as alive session=%s owner=%d agent=%d err=%v",
				sessionID, ownerID, agentID, err,
			)
			return true
		}
		for index := 0; index+1 < len(members); index += 2 {
			eventID := strings.TrimSpace(members[index])
			if eventID == "" {
				continue
			}
			record, ok := loadDurablePendingDelegate(ctx, eventID)
			if !ok || record == nil {
				continue
			}
			if record.Event.OwnerID != ownerID ||
				record.Event.AgentID != agentID ||
				strings.TrimSpace(record.Event.SessionID) != sessionID {
				continue
			}
			if record.Stage != durablePendingDelegateStageAck &&
				record.Stage != durablePendingDelegateStageResult {
				continue
			}
			if record.UpdatedAt >= cutoffMs {
				return true
			}
		}
		cursor = next
		if cursor == 0 {
			return false
		}
	}
}
