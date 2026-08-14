package handler

import (
	"context"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
)

// InProgressLister 查询进行中通话的 DB 接口，由 store.CallRecordStore 满足。
type InProgressLister interface {
	ListInProgress(ctx context.Context) ([]model.CallRecord, error)
	UpdateEnd(ctx context.Context, callID int64, state int16, endReason string, endedAt time.Time, durationSec *int) error
}

// CleanupOrphanCalls 在 ws 启动时调用：
// 扫描 DB 中所有处于进行中状态的通话，释放对应的 Redis busy key 并将通话状态置为 error。
// 这些通话的内存状态在上次 ws 重启时已丢失，无法继续管理，统一视为异常结束。
func CleanupOrphanCalls(ctx context.Context, store InProgressLister) {
	records, err := store.ListInProgress(ctx)
	if err != nil {
		logger.L.Warnf("orphan_call_cleanup: list failed: %v", err)
		return
	}
	if len(records) == 0 {
		return
	}
	logger.L.Infof("orphan_call_cleanup: found %d in-progress calls, cleaning up", len(records))
	now := time.Now()
	for _, rec := range records {
		// 释放 caller busy key
		releaseCallBusyForRecord(ctx, rec)
		// 释放 agent 并发计数（若有）
		if rec.DelegatedAgentID != nil {
			releaseVoiceConcurrent(*rec.DelegatedAgentID, rec.ID)
		}
		// 释放通话归属（call_owner），避免跨节点路由指向已失效节点（原悬挂 6h）。
		forgetCallOwner(ctx, rec.ID)
		// 释放 owner 参与锁（直拨 owner=caller、客服 owner=callee，两侧都试），
		// 避免锁悬挂阻塞该 owner 后续所有通话/设备（原最长 2h）。
		releaseParticipateLockByCall(ctx, rec.CallerID, rec.ID)
		releaseParticipateLockByCall(ctx, rec.CalleeID, rec.ID)
		// 将通话状态置为 error，reason 标记为 server_restart
		if err := store.UpdateEnd(ctx, rec.ID, model.CallStateError, "server_restart", now, nil); err != nil {
			logger.L.Warnf("orphan_call_cleanup: update_end failed call=%d: %v", rec.ID, err)
		} else {
			logger.L.Infof("orphan_call_cleanup: cleaned call=%d caller=%d callee=%d", rec.ID, rec.CallerID, rec.CalleeID)
		}
	}
}
