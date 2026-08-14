package ws

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

// TestPickMcpTargetDevicePrefersForeground 验证：多设备在线时，APP 工具帧应投给
// 用户当前前台的设备，即使它的设备 ID 字典序更大。
func TestPickMcpTargetDevicePrefersForeground(t *testing.T) {
	logger.Init()
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = nil
	}()

	ctx := context.Background()
	const owner = int64(2001)
	routeKey := fmt.Sprintf("im:ws:route:%d", owner)
	// device-a 字典序更小但在后台；device-b 字典序更大但在前台。
	store.RDB.HSet(ctx, routeKey, "device-a", "node-1")
	store.RDB.HSet(ctx, routeKey, "device-b", "node-2")
	store.RDB.Set(ctx, fmt.Sprintf("im:ws:alive:%d:device-a", owner), "1", time.Minute)
	store.RDB.Set(ctx, fmt.Sprintf("im:ws:alive:%d:device-b", owner), "1", time.Minute)
	store.RDB.Set(ctx, fmt.Sprintf("im:ws:appstate:%d:device-a", owner), "background", time.Minute)
	store.RDB.Set(ctx, fmt.Sprintf("im:ws:appstate:%d:device-b", owner), "foreground", time.Minute)

	s := &Server{}
	dev, node := s.pickMcpTargetDevice(owner)
	if dev != "device-b" || node != "node-2" {
		t.Fatalf("pickMcpTargetDevice=(%q,%q) want (device-b,node-2) — should prefer foreground device", dev, node)
	}
}

// TestPickMcpTargetDeviceFallbackLexicographic 验证：无前台标记时，回退到字典序
// 最小的存活设备（保持原行为，且多节点算出同一台）。
func TestPickMcpTargetDeviceFallbackLexicographic(t *testing.T) {
	logger.Init()
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = nil
	}()

	ctx := context.Background()
	const owner = int64(2002)
	routeKey := fmt.Sprintf("im:ws:route:%d", owner)
	store.RDB.HSet(ctx, routeKey, "device-z", "node-z")
	store.RDB.HSet(ctx, routeKey, "device-a", "node-a")
	store.RDB.Set(ctx, fmt.Sprintf("im:ws:alive:%d:device-z", owner), "1", time.Minute)
	store.RDB.Set(ctx, fmt.Sprintf("im:ws:alive:%d:device-a", owner), "1", time.Minute)

	s := &Server{}
	dev, node := s.pickMcpTargetDevice(owner)
	if dev != "device-a" || node != "node-a" {
		t.Fatalf("pickMcpTargetDevice=(%q,%q) want (device-a,node-a) — lexicographic fallback", dev, node)
	}
}

// TestPickMcpTargetDeviceForegroundSkipsDeadDevice 验证：前台设备若已离线（无 alive），
// 不应被选中；应落到另一台存活设备。
func TestPickMcpTargetDeviceForegroundSkipsDeadDevice(t *testing.T) {
	logger.Init()
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = nil
	}()

	ctx := context.Background()
	const owner = int64(2003)
	routeKey := fmt.Sprintf("im:ws:route:%d", owner)
	// device-a 标着前台但已无 alive（僵尸路由）；device-b 存活但无前台标记。
	store.RDB.HSet(ctx, routeKey, "device-a", "node-1")
	store.RDB.HSet(ctx, routeKey, "device-b", "node-2")
	store.RDB.Set(ctx, fmt.Sprintf("im:ws:appstate:%d:device-a", owner), "foreground", time.Minute)
	store.RDB.Set(ctx, fmt.Sprintf("im:ws:alive:%d:device-b", owner), "1", time.Minute)

	s := &Server{}
	dev, node := s.pickMcpTargetDevice(owner)
	if dev != "device-b" || node != "node-2" {
		t.Fatalf("pickMcpTargetDevice=(%q,%q) want (device-b,node-2) — dead foreground device must be skipped", dev, node)
	}
}
