package service

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateABTest_MinVariants(t *testing.T) {
	setupReachTestDB(t)

	tpl, _ := CreateReachTemplate(CreateReachTemplateReq{Name: "AB", Title: "AB Test"})
	_, err := CreateABTest(context.Background(), CreateABTestReq{
		Variants: []ABVariantReq{{Variant: "A", TemplateID: tpl.ID}},
		Channels: []string{"in_app"},
	}, 0)
	assert.Error(t, err, "should require at least 2 variants")
}

func TestCreateABTest_Success(t *testing.T) {
	setupReachTestDB(t)

	tplA, _ := CreateReachTemplate(CreateReachTemplateReq{Name: "A-tpl", Title: "Variant A"})
	tplB, _ := CreateReachTemplate(CreateReachTemplateReq{Name: "B-tpl", Title: "Variant B"})

	future := time.Now().UTC().Add(1 * time.Hour)
	result, err := CreateABTest(context.Background(), CreateABTestReq{
		Variants: []ABVariantReq{
			{Variant: "A", TemplateID: tplA.ID},
			{Variant: "B", TemplateID: tplB.ID},
		},
		Channels:    []string{"in_app"},
		ScheduledAt: &future,
	}, 0)
	require.NoError(t, err)
	assert.Len(t, result.Tasks, 2)
	assert.NotEmpty(t, result.ABGroupID)
	assert.Equal(t, result.ABGroupID, result.Tasks[0].ABGroupID)
	assert.Equal(t, result.ABGroupID, result.Tasks[1].ABGroupID)
	assert.Equal(t, "A", result.Tasks[0].ABVariant)
	assert.Equal(t, "B", result.Tasks[1].ABVariant)
}

func TestGetABTestStats(t *testing.T) {
	setupReachTestDB(t)

	tplA, _ := CreateReachTemplate(CreateReachTemplateReq{Name: "stats-A", Title: "Stats A"})
	tplB, _ := CreateReachTemplate(CreateReachTemplateReq{Name: "stats-B", Title: "Stats B"})

	groupID := "ab_test_stats"
	taskA := model.ReachTask{
		ID: snowflake.GenID(), Kind: model.ReachKindMarketing, TemplateID: tplA.ID,
		Channels: []byte(`["in_app"]`), Status: model.ReachStatusSent,
		ABGroupID: groupID, ABVariant: "A",
	}
	taskB := model.ReachTask{
		ID: snowflake.GenID(), Kind: model.ReachKindMarketing, TemplateID: tplB.ID,
		Channels: []byte(`["in_app"]`), Status: model.ReachStatusSent,
		ABGroupID: groupID, ABVariant: "B",
	}
	require.NoError(t, store.DB.Create(&taskA).Error)
	require.NoError(t, store.DB.Create(&taskB).Error)

	store.DB.Create(&model.ReachSendLog{
		ID: snowflake.GenID(), TaskID: taskA.ID, UserID: 1,
		Channel: model.ReachChannelInApp, Status: model.ReachSendStatusSent,
	})
	store.DB.Create(&model.ReachSendLog{
		ID: snowflake.GenID(), TaskID: taskA.ID, UserID: 2,
		Channel: model.ReachChannelInApp, Status: model.ReachSendStatusSent,
	})
	store.DB.Create(&model.ReachSendLog{
		ID: snowflake.GenID(), TaskID: taskB.ID, UserID: 3,
		Channel: model.ReachChannelInApp, Status: model.ReachSendStatusSent,
	})

	stats, err := GetABTestStats(groupID)
	require.NoError(t, err)
	assert.Equal(t, groupID, stats.ABGroupID)
	assert.Len(t, stats.Variants, 2)

	var varA, varB *ABVariantStats
	for i := range stats.Variants {
		if stats.Variants[i].Variant == "A" {
			varA = &stats.Variants[i]
		}
		if stats.Variants[i].Variant == "B" {
			varB = &stats.Variants[i]
		}
	}
	require.NotNil(t, varA)
	require.NotNil(t, varB)
	assert.Equal(t, int64(2), varA.Sent)
	assert.Equal(t, int64(1), varB.Sent)
}

func TestGetABTestStats_NotFound(t *testing.T) {
	setupReachTestDB(t)
	_, err := GetABTestStats("nonexistent")
	assert.Error(t, err)
}
