package agentapi

import (
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
)

// 累计违规阈值与窗口。
// 60 秒内累计 ≥ 20 次 4xxx 业务错误 → 主动断开连接,避免恶意 / 失控 Agent 持续骚扰。
const (
	violationWindow        = 60 * time.Second
	violationKickThreshold = 20
)

// recordViolation 在每次返回 4xxx 错误时调用,自动滑窗,
// 超过阈值时关闭连接（不阻塞当前 goroutine,close 由调度器处理）。
//
// 调用方约定：只有"协议层不接受"的拒绝才记一次违规;
// 5xxx 内部错误 / 业务侧业务校验失败（如 session 无权限的 agentmsg 错误）不记录。
func (c *agentConn) recordViolation() {
	if c == nil {
		return
	}
	now := time.Now().UnixMilli()
	windowStart := c.violationWindowStart.Load()
	if windowStart == 0 || now-windowStart > int64(violationWindow/time.Millisecond) {
		// 重置窗口
		c.violationWindowStart.Store(now)
		c.violations.Store(1)
		return
	}
	count := c.violations.Add(1)
	if count >= violationKickThreshold {
		logger.L.Warnf(
			"agentapi violation_kick agent=%d owner=%d count=%d window_start=%d",
			c.agentID,
			c.ownerID,
			count,
			windowStart,
		)
		// 触发关闭。不在这里直接 close ws,
		// 由 ws_gateway 的读循环检测 closed 后退出。
		go c.close()
	}
}
