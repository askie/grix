package service

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
)

func TestContactSearch(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	t.Run("searches owner friends and active agents", func(t *testing.T) {
		testDB.Cleanup()
		ownerID := int64(19501)
		friendRemarkID := int64(19502)
		friendNicknameID := int64(19503)
		hiddenFriendID := int64(19504)
		activeAgentID := int64(19505)
		disabledAgentID := int64(19506)

		seedUser(t, testDB, ownerID)
		seedUser(t, testDB, friendRemarkID)
		seedUser(t, testDB, friendNicknameID)
		seedUser(t, testDB, hiddenFriendID)

		if err := testDB.DB.Model(&model.User{}).
			Where("id = ?", hiddenFriendID).
			Update("username", "delegate_owner_hidden_contact").Error; err != nil {
			t.Fatalf("update hidden friend username error: %v", err)
		}
		if err := testDB.DB.Model(&model.User{}).
			Where("id = ?", friendNicknameID).
			Updates(map[string]any{
				"username":     "budget_owner",
				"nickname":     "Budget Buddy",
				"introduction": "Budget friendly contact",
			}).Error; err != nil {
			t.Fatalf("update friend nickname error: %v", err)
		}

		now := time.Now()
		friends := []model.Friend{
			{ID: 90001, UserID: ownerID, FriendID: friendRemarkID, RemarkName: "Atlas Remark", CreatedAt: now},
			{ID: 90002, UserID: ownerID, FriendID: friendNicknameID, CreatedAt: now.Add(-time.Minute)},
			{ID: 90003, UserID: ownerID, FriendID: hiddenFriendID, CreatedAt: now.Add(-2 * time.Minute)},
		}
		if err := testDB.DB.Create(&friends).Error; err != nil {
			t.Fatalf("seed friends error: %v", err)
		}
		agents := []model.Agent{
			{
				ID:           activeAgentID,
				OwnerID:      ownerID,
				AgentName:    "Atlas Assistant",
				Introduction: "Atlas automation helper",
				ProviderType: model.AgentProviderRemote,
				Status:       model.AgentStatusActive,
				CreatedAt:    now.Add(2 * time.Minute),
				UpdatedAt:    now.Add(2 * time.Minute),
			},
			{
				ID:           disabledAgentID,
				OwnerID:      ownerID,
				AgentName:    "Atlas Disabled",
				ProviderType: model.AgentProviderRemote,
				Status:       model.AgentStatusDisabled,
				CreatedAt:    now.Add(3 * time.Minute),
				UpdatedAt:    now.Add(3 * time.Minute),
			},
		}
		if err := testDB.DB.Create(&agents).Error; err != nil {
			t.Fatalf("seed agents error: %v", err)
		}

		atlasResp, err := ContactSearch(ownerID, "atlas", 20, 0)
		if err != nil {
			t.Fatalf("ContactSearch(atlas) error = %v", err)
		}
		if len(atlasResp.List) != 2 {
			t.Fatalf("expected 2 atlas results, got %d", len(atlasResp.List))
		}
		if atlasResp.List[0].PeerID != friendRemarkID || atlasResp.List[0].PeerType != 1 {
			t.Fatalf("expected exact remark match first, got %#v", atlasResp.List[0])
		}
		if atlasResp.List[0].DisplayName != "Atlas Remark" {
			t.Fatalf("expected friend display name to prefer remark, got %q", atlasResp.List[0].DisplayName)
		}
		if atlasResp.List[0].Introduction != "" {
			t.Fatalf("expected empty friend introduction, got %q", atlasResp.List[0].Introduction)
		}
		if atlasResp.List[1].PeerID != activeAgentID || atlasResp.List[1].PeerType != 2 {
			t.Fatalf("expected agent contain match second, got %#v", atlasResp.List[1])
		}
		if atlasResp.List[1].DisplayName != "Atlas Assistant" {
			t.Fatalf("expected agent display name, got %q", atlasResp.List[1].DisplayName)
		}
		if atlasResp.List[1].Introduction != "Atlas automation helper" {
			t.Fatalf("expected agent introduction, got %q", atlasResp.List[1].Introduction)
		}

		budgetResp, err := ContactSearch(ownerID, "budget", 20, 0)
		if err != nil {
			t.Fatalf("ContactSearch(budget) error = %v", err)
		}
		if len(budgetResp.List) != 1 {
			t.Fatalf("expected 1 budget result, got %d", len(budgetResp.List))
		}
		if budgetResp.List[0].PeerID != friendNicknameID || budgetResp.List[0].PeerType != 1 {
			t.Fatalf("expected budget result to be friend, got %#v", budgetResp.List[0])
		}
		if budgetResp.List[0].DisplayName != "Budget Buddy" {
			t.Fatalf("expected display name Budget Buddy, got %q", budgetResp.List[0].DisplayName)
		}
		if budgetResp.List[0].Introduction != "Budget friendly contact" {
			t.Fatalf("expected friend introduction, got %q", budgetResp.List[0].Introduction)
		}

		hiddenResp, err := ContactSearch(ownerID, "delegate", 20, 0)
		if err != nil {
			t.Fatalf("ContactSearch(delegate) error = %v", err)
		}
		if len(hiddenResp.List) != 0 {
			t.Fatalf("expected hidden contacts filtered out, got %#v", hiddenResp.List)
		}

		disabledResp, err := ContactSearch(ownerID, "disabled", 20, 0)
		if err != nil {
			t.Fatalf("ContactSearch(disabled) error = %v", err)
		}
		if len(disabledResp.List) != 0 {
			t.Fatalf("expected disabled agents filtered out, got %#v", disabledResp.List)
		}
	})

	t.Run("supports compact multi token and id prefix keyword matching", func(t *testing.T) {
		testDB.Cleanup()
		ownerID := int64(19601)
		friendCompactID := int64(196021)
		friendSpacedID := int64(196022)
		agentID := int64(196023)
		seedUser(t, testDB, ownerID)

		seedUser(t, testDB, friendCompactID)
		seedUser(t, testDB, friendSpacedID)
		now := time.Now()
		if err := testDB.DB.Model(&model.User{}).
			Where("id = ?", friendCompactID).
			Updates(map[string]any{
				"username": "atlas_user",
				"nickname": "Atlas Helper",
			}).Error; err != nil {
			t.Fatalf("update compact friend user error: %v", err)
		}
		if err := testDB.DB.Model(&model.User{}).
			Where("id = ?", friendSpacedID).
			Update("nickname", "Atlas Project Owner").Error; err != nil {
			t.Fatalf("update multi token friend nickname error: %v", err)
		}
		if err := testDB.DB.Create(&model.Friend{
			ID:        91000,
			UserID:    ownerID,
			FriendID:  friendCompactID,
			CreatedAt: now,
		}).Error; err != nil {
			t.Fatalf("seed compact friend error: %v", err)
		}
		if err := testDB.DB.Create(&model.Friend{
			ID:        91001,
			UserID:    ownerID,
			FriendID:  friendSpacedID,
			CreatedAt: now.Add(-time.Minute),
		}).Error; err != nil {
			t.Fatalf("seed multi token friend error: %v", err)
		}
		if err := testDB.DB.Create(&model.Agent{
			ID:           agentID,
			OwnerID:      ownerID,
			AgentName:    "Atlas AI Assistant",
			ProviderType: model.AgentProviderRemote,
			Status:       model.AgentStatusActive,
			CreatedAt:    now.Add(-30 * time.Second),
			UpdatedAt:    now.Add(-30 * time.Second),
		}).Error; err != nil {
			t.Fatalf("seed agent error: %v", err)
		}

		compactResp, err := ContactSearch(ownerID, "atlasuser", 20, 0)
		if err != nil {
			t.Fatalf("ContactSearch compact error = %v", err)
		}
		if len(compactResp.List) != 1 || compactResp.List[0].PeerID != friendCompactID {
			t.Fatalf("expected compact username match, got %#v", compactResp.List)
		}

		tokenResp, err := ContactSearch(ownerID, "atlas assistant", 20, 0)
		if err != nil {
			t.Fatalf("ContactSearch multi token error = %v", err)
		}
		if len(tokenResp.List) != 1 || tokenResp.List[0].PeerID != agentID {
			t.Fatalf("expected multi token agent match, got %#v", tokenResp.List)
		}

		idPrefixResp, err := ContactSearch(ownerID, "19602", 20, 0)
		if err != nil {
			t.Fatalf("ContactSearch id prefix error = %v", err)
		}
		if len(idPrefixResp.List) != 3 {
			t.Fatalf("expected 3 id prefix matches, got %#v", idPrefixResp.List)
		}
		if idPrefixResp.List[0].PeerID != friendCompactID || idPrefixResp.List[1].PeerID != agentID || idPrefixResp.List[2].PeerID != friendSpacedID {
			t.Fatalf("expected id prefix results ordered by created_at desc after relevance tie, got %#v", idPrefixResp.List)
		}
	})

	t.Run("paginates after relevance ordering", func(t *testing.T) {
		testDB.Cleanup()
		ownerID := int64(19801)
		exactFriendID := int64(19802)
		prefixFriendID := int64(19803)
		containAgentID := int64(19804)
		seedUser(t, testDB, ownerID)
		seedUser(t, testDB, exactFriendID)
		seedUser(t, testDB, prefixFriendID)

		now := time.Now()
		if err := testDB.DB.Model(&model.User{}).
			Where("id = ?", exactFriendID).
			Update("nickname", "Alpha").Error; err != nil {
			t.Fatalf("update exact friend nickname error: %v", err)
		}
		if err := testDB.DB.Model(&model.User{}).
			Where("id = ?", prefixFriendID).
			Update("nickname", "Alpha Two").Error; err != nil {
			t.Fatalf("update prefix friend nickname error: %v", err)
		}
		friends := []model.Friend{
			{ID: 92000, UserID: ownerID, FriendID: exactFriendID, CreatedAt: now.Add(-2 * time.Minute)},
			{ID: 92001, UserID: ownerID, FriendID: prefixFriendID, CreatedAt: now.Add(-time.Minute)},
		}
		if err := testDB.DB.Create(&friends).Error; err != nil {
			t.Fatalf("seed paged relevance friends error: %v", err)
		}
		if err := testDB.DB.Create(&model.Agent{
			ID:           containAgentID,
			OwnerID:      ownerID,
			AgentName:    "Project Alpha Review",
			ProviderType: model.AgentProviderRemote,
			Status:       model.AgentStatusActive,
			CreatedAt:    now,
			UpdatedAt:    now,
		}).Error; err != nil {
			t.Fatalf("seed relevance agent error: %v", err)
		}

		page1, err := ContactSearch(ownerID, "alpha", 2, 0)
		if err != nil {
			t.Fatalf("ContactSearch relevance page1 error = %v", err)
		}
		if !page1.HasMore {
			t.Fatalf("expected relevance page1 has_more=true")
		}
		if len(page1.List) != 2 {
			t.Fatalf("expected 2 items on relevance page1, got %d", len(page1.List))
		}
		if page1.List[0].PeerID != exactFriendID || page1.List[1].PeerID != prefixFriendID {
			t.Fatalf("expected exact then prefix ordering, got %#v", page1.List)
		}

		page2, err := ContactSearch(ownerID, "alpha", 2, 2)
		if err != nil {
			t.Fatalf("ContactSearch relevance page2 error = %v", err)
		}
		if page2.HasMore {
			t.Fatalf("expected relevance page2 has_more=false")
		}
		if len(page2.List) != 1 || page2.List[0].PeerID != containAgentID {
			t.Fatalf("unexpected relevance page2 results: %#v", page2.List)
		}
	})
}

func TestContactSearchByID(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	t.Run("returns exact contact info in search format", func(t *testing.T) {
		testDB.Cleanup()
		ownerID := int64(19701)
		friendID := int64(19702)
		agentID := int64(19703)
		hiddenFriendID := int64(19704)

		seedUser(t, testDB, ownerID)
		seedUser(t, testDB, friendID)
		seedUser(t, testDB, hiddenFriendID)

		if err := testDB.DB.Model(&model.User{}).
			Where("id = ?", friendID).
			Updates(map[string]any{
				"username":     "atlas_user_by_id",
				"nickname":     "Atlas Nickname",
				"introduction": "Atlas by id introduction",
			}).Error; err != nil {
			t.Fatalf("update friend user error: %v", err)
		}
		if err := testDB.DB.Model(&model.User{}).
			Where("id = ?", hiddenFriendID).
			Update("username", "delegate_owner_hidden_contact_by_id").Error; err != nil {
			t.Fatalf("update hidden user error: %v", err)
		}

		now := time.Now()
		if err := testDB.DB.Create(&model.Friend{
			ID:         92001,
			UserID:     ownerID,
			FriendID:   friendID,
			RemarkName: "Atlas Remark",
			CreatedAt:  now,
		}).Error; err != nil {
			t.Fatalf("seed friend relation error: %v", err)
		}
		if err := testDB.DB.Create(&model.Friend{
			ID:        92002,
			UserID:    ownerID,
			FriendID:  hiddenFriendID,
			CreatedAt: now.Add(-time.Minute),
		}).Error; err != nil {
			t.Fatalf("seed hidden friend relation error: %v", err)
		}
		if err := testDB.DB.Create(&model.Agent{
			ID:           agentID,
			OwnerID:      ownerID,
			AgentName:    "Atlas Assistant",
			Introduction: "Atlas agent introduction",
			ProviderType: model.AgentProviderRemote,
			Status:       model.AgentStatusActive,
			CreatedAt:    now.Add(time.Minute),
			UpdatedAt:    now.Add(time.Minute),
		}).Error; err != nil {
			t.Fatalf("seed agent error: %v", err)
		}

		friendResp, err := ContactSearchByID(ownerID, friendID, 20, 0)
		if err != nil {
			t.Fatalf("ContactSearchByID(friend) error = %v", err)
		}
		if len(friendResp.List) != 1 {
			t.Fatalf("expected 1 friend result, got %d", len(friendResp.List))
		}
		if friendResp.List[0].PeerID != friendID || friendResp.List[0].PeerType != 1 {
			t.Fatalf("unexpected friend result: %#v", friendResp.List[0])
		}
		if friendResp.List[0].DisplayName != "Atlas Remark" {
			t.Fatalf("expected friend display name Atlas Remark, got %q", friendResp.List[0].DisplayName)
		}
		if friendResp.List[0].Introduction != "Atlas by id introduction" {
			t.Fatalf("expected friend introduction, got %q", friendResp.List[0].Introduction)
		}

		agentResp, err := ContactSearchByID(ownerID, agentID, 20, 0)
		if err != nil {
			t.Fatalf("ContactSearchByID(agent) error = %v", err)
		}
		if len(agentResp.List) != 1 {
			t.Fatalf("expected 1 agent result, got %d", len(agentResp.List))
		}
		if agentResp.List[0].PeerID != agentID || agentResp.List[0].PeerType != 2 {
			t.Fatalf("unexpected agent result: %#v", agentResp.List[0])
		}
		if agentResp.List[0].DisplayName != "Atlas Assistant" {
			t.Fatalf("expected agent display name Atlas Assistant, got %q", agentResp.List[0].DisplayName)
		}
		if agentResp.List[0].Introduction != "Atlas agent introduction" {
			t.Fatalf("expected agent introduction, got %q", agentResp.List[0].Introduction)
		}

		hiddenResp, err := ContactSearchByID(ownerID, hiddenFriendID, 20, 0)
		if err != nil {
			t.Fatalf("ContactSearchByID(hidden) error = %v", err)
		}
		if len(hiddenResp.List) != 0 {
			t.Fatalf("expected hidden contact filtered out, got %#v", hiddenResp.List)
		}
	})
}
