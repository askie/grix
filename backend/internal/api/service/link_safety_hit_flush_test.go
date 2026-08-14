package service

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 命中计数走内存聚合：bump 立即返回，flush 时一次性 UPDATE。
// 这样高频命中场景从"每次一次 UPDATE"降到"O(规则数 / 周期)"次。
func TestLinkBlocklistHit_AggregationAndFlush(t *testing.T) {
	// 隔离测试间状态
	linkHitAggregator.mu.Lock()
	linkHitAggregator.counts = map[int64]int64{}
	linkHitAggregator.lastHit = map[int64]time.Time{}
	linkHitAggregator.mu.Unlock()

	prevDB := store.DB
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	defer func() { store.DB = prevDB }()

	// 准备一条规则
	rule := model.LinkBlocklistRule{
		ID: 1001, Kind: "domain", Value: "evil.com",
		Severity: "malicious", Source: "manual", Enabled: true,
	}
	require.NoError(t, store.DB.Create(&rule).Error)

	ctx := context.Background()

	// 1) 多次 bump 应只在内存累加，不立刻写 DB
	for i := 0; i < 100; i++ {
		bumpLinkBlocklistHit(ctx, 1001)
	}
	var before model.LinkBlocklistRule
	require.NoError(t, store.DB.First(&before, "id = ?", 1001).Error)
	assert.EqualValues(t, 0, before.HitCount, "bump 不应立刻写 DB")

	// 2) flush 一次后 DB 应一次性加 100
	flushLinkBlocklistHits()
	var after model.LinkBlocklistRule
	require.NoError(t, store.DB.First(&after, "id = ?", 1001).Error)
	assert.EqualValues(t, 100, after.HitCount, "flush 后 hit_count 应=累加增量")
	assert.NotNil(t, after.LastHitAt)

	// 3) 重复 flush（无新增）应是 no-op
	flushLinkBlocklistHits()
	var after2 model.LinkBlocklistRule
	require.NoError(t, store.DB.First(&after2, "id = ?", 1001).Error)
	assert.EqualValues(t, 100, after2.HitCount, "无新增 bump 时 flush 应 no-op")
}

func TestLinkBlocklistHit_ZeroRuleIDIgnored(t *testing.T) {
	linkHitAggregator.mu.Lock()
	before := len(linkHitAggregator.counts)
	linkHitAggregator.mu.Unlock()

	bumpLinkBlocklistHit(context.Background(), 0)

	linkHitAggregator.mu.Lock()
	after := len(linkHitAggregator.counts)
	linkHitAggregator.mu.Unlock()
	assert.Equal(t, before, after, "ruleID=0 应被忽略")
}
