package call

import "github.com/askie/grix/backend/internal/model"

// TestInjectCall 仅测试用：向活跃通话表注入一条记录。
func (c *Controller) TestInjectCall(callID int64, rec model.CallRecord, spec VoiceBridgeSpec) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls[callID] = &callEntry{record: rec, aiSpec: spec}
}

// TestRemoveCall 仅测试用：移除活跃通话。
func (c *Controller) TestRemoveCall(callID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.calls, callID)
}
