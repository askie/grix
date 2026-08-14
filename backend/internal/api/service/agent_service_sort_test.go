package service

import (
	"fmt"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedAgentForSort(t *testing.T, db *testutil.TestDB, userID, agentID int64, categoryID int64, sortOrder int) {
	t.Helper()
	agent := model.Agent{
		ID:         agentID,
		AgentName:  fmt.Sprintf("SortAgent_%d", agentID),
		OwnerID:    userID,
		CategoryID: categoryID,
		SortOrder:  sortOrder,
		Status:     model.AgentStatusActive,
	}
	require.NoError(t, db.DB.Create(&agent).Error)
}

func seedCategoryForSort(t *testing.T, db *testutil.TestDB, ownerID, categoryID int64, name string) {
	t.Helper()
	cat := model.AgentCategory{
		ID:       categoryID,
		OwnerID:  ownerID,
		ParentID: 0,
		Name:     name,
		SortOrder: 0,
	}
	require.NoError(t, db.DB.Create(&cat).Error)
}

func TestAgentBatchSort_EmptyItems(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()
	_ = testDB

	ec := AgentBatchSort(1, nil)
	assert.Nil(t, ec)

	ec = AgentBatchSort(1, []AgentSortItem{})
	assert.Nil(t, ec)
}

func TestAgentBatchSort_MoveBetweenCategories(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	userID := int64(40001)
	catA := int64(50001)
	catB := int64(50002)
	agent1 := int64(60001)
	agent2 := int64(60002)

	seedUser(t, testDB, userID)
	seedCategoryForSort(t, testDB, userID, catA, "CatA")
	seedCategoryForSort(t, testDB, userID, catB, "CatB")
	seedAgentForSort(t, testDB, userID, agent1, catA, 0)
	seedAgentForSort(t, testDB, userID, agent2, catA, 1)

	// Move agent2 to catB with sort_order=0
	ec := AgentBatchSort(userID, []AgentSortItem{
		{AgentID: agent2, CategoryID: catB, SortOrder: 0},
	})
	assert.Nil(t, ec)

	// Verify agent2 is now in catB
	var agents2 []model.Agent
	require.NoError(t, store.DB.Where("id = ?", agent2).Find(&agents2).Error)
	require.Len(t, agents2, 1)
	assert.Equal(t, catB, agents2[0].CategoryID)
	assert.Equal(t, 0, agents2[0].SortOrder)

	// Verify agent1 is still in catA
	var agents1 []model.Agent
	require.NoError(t, store.DB.Where("id = ?", agent1).Find(&agents1).Error)
	require.Len(t, agents1, 1)
	assert.Equal(t, catA, agents1[0].CategoryID)
}

func TestAgentBatchSort_ReorderWithinCategory(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	userID := int64(40002)
	catA := int64(50003)
	agent1 := int64(60003)
	agent2 := int64(60004)

	seedUser(t, testDB, userID)
	seedCategoryForSort(t, testDB, userID, catA, "CatA")
	seedAgentForSort(t, testDB, userID, agent1, catA, 0)
	seedAgentForSort(t, testDB, userID, agent2, catA, 1)

	// Swap order
	ec := AgentBatchSort(userID, []AgentSortItem{
		{AgentID: agent1, CategoryID: catA, SortOrder: 1},
		{AgentID: agent2, CategoryID: catA, SortOrder: 0},
	})
	assert.Nil(t, ec)

	var a1, a2 model.Agent
	require.NoError(t, store.DB.Where("id = ?", agent1).Find(&a1).Error)
	require.NoError(t, store.DB.Where("id = ?", agent2).Find(&a2).Error)
	assert.Equal(t, 1, a1.SortOrder)
	assert.Equal(t, 0, a2.SortOrder)
}

func TestAgentBatchSort_InvalidCategory(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	userID := int64(40003)
	agent1 := int64(60005)
	fakeCat := int64(99999)

	seedUser(t, testDB, userID)
	seedAgentForSort(t, testDB, userID, agent1, 0, 0)

	ec := AgentBatchSort(userID, []AgentSortItem{
		{AgentID: agent1, CategoryID: fakeCat, SortOrder: 0},
	})
	assert.NotNil(t, ec)
	assert.Equal(t, 403, ec.HTTPStatus)
}

func TestAgentBatchSort_NotOwner(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	userID := int64(40004)
	otherUserID := int64(40005)
	catA := int64(50004)
	agent1 := int64(60006)

	seedUser(t, testDB, userID)
	seedUser(t, testDB, otherUserID)
	seedCategoryForSort(t, testDB, userID, catA, "CatA")
	seedAgentForSort(t, testDB, userID, agent1, catA, 0)

	// Try to move agent as different user
	ec := AgentBatchSort(otherUserID, []AgentSortItem{
		{AgentID: agent1, CategoryID: 0, SortOrder: 0},
	})
	assert.NotNil(t, ec)
}

func TestAgentBatchSort_MoveToUncategorized(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	userID := int64(40006)
	catA := int64(50005)
	agent1 := int64(60007)

	seedUser(t, testDB, userID)
	seedCategoryForSort(t, testDB, userID, catA, "CatA")
	seedAgentForSort(t, testDB, userID, agent1, catA, 0)

	// Move to uncategorized (category_id=0)
	ec := AgentBatchSort(userID, []AgentSortItem{
		{AgentID: agent1, CategoryID: 0, SortOrder: 5},
	})
	assert.Nil(t, ec)

	var agent model.Agent
	require.NoError(t, store.DB.Where("id = ?", agent1).Find(&agent).Error)
	assert.Equal(t, int64(0), agent.CategoryID)
	assert.Equal(t, 5, agent.SortOrder)
}
