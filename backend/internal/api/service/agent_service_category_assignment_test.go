package service

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

func TestAgentAssignCategory(t *testing.T) {
	_, cleanup := setupAgentScopeServiceTest(t)
	defer cleanup()

	const (
		ownerID      = int64(88101)
		foreignOwner = int64(88102)
		agentID      = int64(88201)
		categoryID   = int64(88301)
	)

	seedAgentScopeOwner(t, ownerID)
	seedAgentScopeOwner(t, foreignOwner)
	seedAgentScopeAgent(t, model.Agent{
		ID:           agentID,
		AgentName:    "category-target-agent",
		OwnerID:      ownerID,
		CategoryID:   0,
		ProviderType: model.AgentProviderRemote,
		Status:       model.AgentStatusActive,
	})
	if err := store.DB.Create(&model.AgentCategory{
		ID:       categoryID,
		OwnerID:  ownerID,
		ParentID: 0,
		Name:     "Workspace",
	}).Error; err != nil {
		t.Fatalf("seed category error: %v", err)
	}

	resp, ec := AgentAssignCategory(ownerID, agentID, categoryID)
	if ec != nil {
		t.Fatalf("AgentAssignCategory error: %+v", ec)
	}
	if resp.CategoryID != categoryID {
		t.Fatalf("expected category_id=%d, got %d", categoryID, resp.CategoryID)
	}

	resp, ec = AgentAssignCategory(ownerID, agentID, 0)
	if ec != nil {
		t.Fatalf("AgentAssignCategory clear error: %+v", ec)
	}
	if resp.CategoryID != 0 {
		t.Fatalf("expected cleared category_id=0, got %d", resp.CategoryID)
	}

	var stored model.Agent
	if err := store.DB.First(&stored, agentID).Error; err != nil {
		t.Fatalf("query updated agent error: %v", err)
	}
	if stored.CategoryID != 0 {
		t.Fatalf("expected persisted category_id=0, got %d", stored.CategoryID)
	}
}

func TestAgentAssignCategoryRejectsForeignCategory(t *testing.T) {
	_, cleanup := setupAgentScopeServiceTest(t)
	defer cleanup()

	const (
		ownerID      = int64(88111)
		foreignOwner = int64(88112)
		agentID      = int64(88211)
		categoryID   = int64(88311)
	)

	seedAgentScopeOwner(t, ownerID)
	seedAgentScopeOwner(t, foreignOwner)
	seedAgentScopeAgent(t, model.Agent{
		ID:           agentID,
		AgentName:    "category-foreign-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderLocal,
		Status:       model.AgentStatusActive,
	})
	if err := store.DB.Create(&model.AgentCategory{
		ID:       categoryID,
		OwnerID:  foreignOwner,
		ParentID: 0,
		Name:     "Foreign Workspace",
	}).Error; err != nil {
		t.Fatalf("seed foreign category error: %v", err)
	}

	_, ec := AgentAssignCategory(ownerID, agentID, categoryID)
	if ec == nil {
		t.Fatal("expected invalid category error")
	}
	if ec.BizCode != 10002 {
		t.Fatalf("expected biz code 10002, got %d", ec.BizCode)
	}
}
