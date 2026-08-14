package resolver

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

type resolverRunCall struct {
	ownerID   int64
	sessionID string
	agentID   int64
}

type recordingResolverRunProvider struct {
	runs  map[int64]toolruntime.RunState
	calls []resolverRunCall
}

func (p *recordingResolverRunProvider) LoadRunState(_ context.Context, ownerID int64, sessionID string, agentID int64) toolruntime.RunState {
	p.calls = append(p.calls, resolverRunCall{ownerID: ownerID, sessionID: sessionID, agentID: agentID})
	return p.runs[ownerID]
}

func setupResolverShareTest(t *testing.T) (*testutil.TestDB, func()) {
	t.Helper()
	tdb := testutil.NewTestDB()
	store.DB = tdb.DB
	return tdb, func() {
		tdb.Close()
		store.DB = nil
	}
}

func seedResolverUser(t *testing.T, id int64, username string) {
	t.Helper()
	if err := store.DB.Create(&model.User{
		ID:       id,
		Username: username,
		Email:    username + "@test.local",
		Status:   model.UserStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed user %d: %v", id, err)
	}
}

func seedResolverAPIAgent(t *testing.T, agentID, ownerID int64) {
	t.Helper()
	if err := store.DB.Create(&model.Agent{
		ID:              agentID,
		AgentName:       "toolbar-share-agent",
		OwnerID:         ownerID,
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeCodex,
		Status:          model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed agent %d: %v", agentID, err)
	}
}

func seedResolverDirectSession(t *testing.T, sessionID string, humanID, agentID int64) {
	t.Helper()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     humanID,
		SessionType: model.SessionTypeDirect,
	}).Error; err != nil {
		t.Fatalf("seed session %s: %v", sessionID, err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: humanID, MemberType: 1},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("seed member %d: %v", m.MemberID, err)
		}
	}
}

func seedResolverGroupSession(t *testing.T, sessionID string, humanIDs []int64, agentID int64) {
	t.Helper()
	ownerID := humanIDs[0]
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: model.SessionTypeGroup,
	}).Error; err != nil {
		t.Fatalf("seed group session %s: %v", sessionID, err)
	}
	for _, humanID := range humanIDs {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:  sessionID,
			MemberID:   humanID,
			MemberType: 1,
		}).Error; err != nil {
			t.Fatalf("seed human member %d: %v", humanID, err)
		}
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:  sessionID,
		MemberID:   agentID,
		MemberType: 2,
	}).Error; err != nil {
		t.Fatalf("seed agent member %d: %v", agentID, err)
	}
}

func seedResolverShare(t *testing.T, agentID, ownerID, sharedTo int64, status int16) {
	t.Helper()
	now := time.Now().UTC()
	if err := store.DB.Create(&model.AgentShare{
		ID:        now.UnixNano(),
		AgentID:   agentID,
		OwnerID:   ownerID,
		SharedTo:  sharedTo,
		Status:    status,
		CreatedAt: now,
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed share agent=%d shared_to=%d: %v", agentID, sharedTo, err)
	}
}

func TestResolveAllowsActiveSharedUserPrivateSession(t *testing.T) {
	_, cleanup := setupResolverShareTest(t)
	defer cleanup()

	const (
		ownerID   = int64(91001)
		sharedTo  = int64(91002)
		agentID   = int64(92001)
		sessionID = "toolbar-share-private-1"
	)
	seedResolverUser(t, ownerID, "toolbar-owner")
	seedResolverUser(t, sharedTo, "toolbar-shared")
	seedResolverAPIAgent(t, agentID, ownerID)
	seedResolverDirectSession(t, sessionID, sharedTo, agentID)
	seedResolverShare(t, agentID, ownerID, sharedTo, model.AgentShareStatusActive)

	runProvider := &recordingResolverRunProvider{runs: map[int64]toolruntime.RunState{
		sharedTo: {
			HasActiveRun: true,
			RunID:        "viewer-scoped-private-run",
			AgentID:      agentID,
		},
	}}
	r := New(runProvider)
	in, err := r.Resolve(context.Background(), sharedTo, sessionID, 0)
	if err != nil {
		t.Fatalf("shared user Resolve err=%v", err)
	}
	if in.OwnerID != sharedTo {
		t.Fatalf("BuildInput.OwnerID=%d want shared user %d (shared connection identity)", in.OwnerID, sharedTo)
	}
	if in.Agent.AgentID != agentID {
		t.Fatalf("AgentID=%d want %d", in.Agent.AgentID, agentID)
	}
	if in.Agent.OwnerID != ownerID {
		t.Fatalf("Agent.OwnerID=%d want real owner %d", in.Agent.OwnerID, ownerID)
	}
	if !in.Run.HasActiveRun || in.Run.RunID != "viewer-scoped-private-run" {
		t.Fatalf("Run=%+v want viewer-scoped active run", in.Run)
	}
	if len(runProvider.calls) != 1 || runProvider.calls[0].ownerID != sharedTo {
		t.Fatalf("run lookup calls=%+v want viewer %d only", runProvider.calls, sharedTo)
	}
}

func TestResolveUsesSharedOwnerRuntimeProfile(t *testing.T) {
	_, cleanup := setupResolverShareTest(t)
	defer cleanup()
	previousRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previousRDB
	}()

	const (
		ownerID   = int64(91101)
		sharedTo  = int64(91102)
		agentID   = int64(92101)
		sessionID = "toolbar-share-owner-runtime"
	)
	seedResolverUser(t, ownerID, "toolbar-runtime-owner")
	seedResolverUser(t, sharedTo, "toolbar-runtime-shared")
	seedResolverAPIAgent(t, agentID, ownerID)
	seedResolverDirectSession(t, sessionID, sharedTo, agentID)
	seedResolverShare(t, agentID, ownerID, sharedTo, model.AgentShareStatusActive)

	if err := toolruntime.StoreProfile(context.Background(), toolruntime.Profile{
		AgentID: agentID,
		OwnerID: ownerID,
		LibrarySkills: []toolruntime.LibrarySkillEntry{{
			Name:         "project-skill",
			EnableScopes: toolruntime.LibrarySkillEnableScopes{Project: "unavailable"},
		}},
	}, time.Minute); err != nil {
		t.Fatalf("store primary profile: %v", err)
	}
	if err := toolruntime.StoreProfileForOwner(context.Background(), toolruntime.Profile{
		AgentID: agentID,
		OwnerID: sharedTo,
		LibrarySkills: []toolruntime.LibrarySkillEntry{{
			Name:         "project-skill",
			EnableScopes: toolruntime.LibrarySkillEnableScopes{Project: "link"},
		}},
	}, time.Minute); err != nil {
		t.Fatalf("store shared profile: %v", err)
	}

	in, err := New(nil).Resolve(context.Background(), sharedTo, sessionID, 0)
	if err != nil {
		t.Fatalf("shared user Resolve err=%v", err)
	}
	if in.Runtime.OwnerID != sharedTo {
		t.Fatalf("runtime owner=%d want shared owner %d", in.Runtime.OwnerID, sharedTo)
	}
	if got := in.Runtime.LibrarySkills[0].EnableScopes.Project; got != "link" {
		t.Fatalf("resolved shared project scope=%q want=link", got)
	}
}

func TestResolveRejectsStrangerEvenIfSessionMember(t *testing.T) {
	_, cleanup := setupResolverShareTest(t)
	defer cleanup()

	const (
		ownerID   = int64(91011)
		stranger  = int64(91012)
		agentID   = int64(92011)
		sessionID = "toolbar-share-private-stranger"
	)
	seedResolverUser(t, ownerID, "toolbar-owner-2")
	seedResolverUser(t, stranger, "toolbar-stranger")
	seedResolverAPIAgent(t, agentID, ownerID)
	// 陌生人误入会话成员（权限异常数据）时，仍不得看工具栏。
	seedResolverDirectSession(t, sessionID, stranger, agentID)

	r := New(nil)
	_, err := r.Resolve(context.Background(), stranger, sessionID, 0)
	if err != core.ErrSessionForbidden {
		t.Fatalf("Resolve err=%v want %v", err, core.ErrSessionForbidden)
	}
}

func TestResolveRejectsRevokedShare(t *testing.T) {
	_, cleanup := setupResolverShareTest(t)
	defer cleanup()

	const (
		ownerID   = int64(91021)
		sharedTo  = int64(91022)
		agentID   = int64(92021)
		sessionID = "toolbar-share-revoked"
	)
	seedResolverUser(t, ownerID, "toolbar-owner-3")
	seedResolverUser(t, sharedTo, "toolbar-shared-3")
	seedResolverAPIAgent(t, agentID, ownerID)
	seedResolverDirectSession(t, sessionID, sharedTo, agentID)
	seedResolverShare(t, agentID, ownerID, sharedTo, model.AgentShareStatusRevoked)

	r := New(nil)
	_, err := r.Resolve(context.Background(), sharedTo, sessionID, 0)
	if err != core.ErrSessionForbidden {
		t.Fatalf("Resolve err=%v want %v", err, core.ErrSessionForbidden)
	}
}

func TestResolveRejectsExpiredShare(t *testing.T) {
	_, cleanup := setupResolverShareTest(t)
	defer cleanup()

	const (
		ownerID   = int64(91061)
		sharedTo  = int64(91062)
		agentID   = int64(92061)
		sessionID = "toolbar-share-expired"
	)
	seedResolverUser(t, ownerID, "toolbar-owner-exp")
	seedResolverUser(t, sharedTo, "toolbar-shared-exp")
	seedResolverAPIAgent(t, agentID, ownerID)
	seedResolverDirectSession(t, sessionID, sharedTo, agentID)
	past := time.Now().UTC().Add(-time.Hour)
	now := time.Now().UTC()
	if err := store.DB.Create(&model.AgentShare{
		ID:        now.UnixNano(),
		AgentID:   agentID,
		OwnerID:   ownerID,
		SharedTo:  sharedTo,
		Status:    model.AgentShareStatusActive,
		ExpiresAt: &past,
		CreatedAt: now,
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed expired share: %v", err)
	}

	r := New(nil)
	_, err := r.Resolve(context.Background(), sharedTo, sessionID, 0)
	if err != core.ErrSessionForbidden {
		t.Fatalf("Resolve err=%v want %v", err, core.ErrSessionForbidden)
	}
}

func TestResolveRejectsBannedSharedUser(t *testing.T) {
	_, cleanup := setupResolverShareTest(t)
	defer cleanup()

	const (
		ownerID   = int64(91071)
		sharedTo  = int64(91072)
		agentID   = int64(92071)
		sessionID = "toolbar-share-banned"
	)
	seedResolverUser(t, ownerID, "toolbar-owner-ban")
	seedResolverUser(t, sharedTo, "toolbar-shared-ban")
	if err := store.DB.Model(&model.User{}).
		Where("id = ?", sharedTo).
		Update("status", model.UserStatusBanned).Error; err != nil {
		t.Fatalf("ban shared user: %v", err)
	}
	seedResolverAPIAgent(t, agentID, ownerID)
	seedResolverDirectSession(t, sessionID, sharedTo, agentID)
	seedResolverShare(t, agentID, ownerID, sharedTo, model.AgentShareStatusActive)

	r := New(nil)
	_, err := r.Resolve(context.Background(), sharedTo, sessionID, 0)
	if err != core.ErrSessionForbidden {
		t.Fatalf("Resolve err=%v want %v", err, core.ErrSessionForbidden)
	}
}

func TestResolveAllowsSharedUserInGroupWithTargetAgent(t *testing.T) {
	_, cleanup := setupResolverShareTest(t)
	defer cleanup()

	const (
		ownerID   = int64(91031)
		sharedTo  = int64(91032)
		agentID   = int64(92031)
		sessionID = "toolbar-share-group-1"
	)
	seedResolverUser(t, ownerID, "toolbar-owner-g")
	seedResolverUser(t, sharedTo, "toolbar-shared-g")
	seedResolverAPIAgent(t, agentID, ownerID)
	seedResolverGroupSession(t, sessionID, []int64{ownerID, sharedTo}, agentID)
	seedResolverShare(t, agentID, ownerID, sharedTo, model.AgentShareStatusActive)

	runProvider := &recordingResolverRunProvider{runs: map[int64]toolruntime.RunState{
		ownerID: {
			HasActiveRun: true,
			RunID:        "owner-scoped-group-run",
			AgentID:      agentID,
		},
	}}
	r := New(runProvider)
	in, err := r.Resolve(context.Background(), sharedTo, sessionID, agentID)
	if err != nil {
		t.Fatalf("shared group Resolve err=%v", err)
	}
	if in.OwnerID != sharedTo {
		t.Fatalf("BuildInput.OwnerID=%d want %d", in.OwnerID, sharedTo)
	}
	if in.Agent.AgentID != agentID {
		t.Fatalf("AgentID=%d want %d", in.Agent.AgentID, agentID)
	}
	if !in.Run.HasActiveRun || in.Run.RunID != "owner-scoped-group-run" {
		t.Fatalf("Run=%+v want owner-scoped active run", in.Run)
	}
	if len(runProvider.calls) != 2 || runProvider.calls[0].ownerID != sharedTo || runProvider.calls[1].ownerID != ownerID {
		t.Fatalf("run lookup calls=%+v want viewer %d then agent owner %d", runProvider.calls, sharedTo, ownerID)
	}
}

func TestResolveSharedGroupRunLookupDoesNotMixTargetAgent(t *testing.T) {
	_, cleanup := setupResolverShareTest(t)
	defer cleanup()

	const (
		ownerID     = int64(91081)
		sharedTo    = int64(91082)
		targetAgent = int64(92081)
		otherAgent  = int64(92082)
		sessionID   = "toolbar-share-group-agent-filter"
	)
	seedResolverUser(t, ownerID, "toolbar-owner-filter")
	seedResolverUser(t, sharedTo, "toolbar-shared-filter")
	seedResolverAPIAgent(t, targetAgent, ownerID)
	seedResolverGroupSession(t, sessionID, []int64{ownerID, sharedTo}, targetAgent)
	seedResolverShare(t, targetAgent, ownerID, sharedTo, model.AgentShareStatusActive)

	runProvider := &recordingResolverRunProvider{runs: map[int64]toolruntime.RunState{
		sharedTo: {
			HasActiveRun: true,
			RunID:        "wrong-viewer-run",
			AgentID:      otherAgent,
		},
		ownerID: {
			HasActiveRun: true,
			RunID:        "wrong-owner-run",
			AgentID:      otherAgent,
		},
	}}
	r := New(runProvider)
	in, err := r.Resolve(context.Background(), sharedTo, sessionID, targetAgent)
	if err != nil {
		t.Fatalf("shared group Resolve err=%v", err)
	}
	if in.Run.HasActiveRun {
		t.Fatalf("Run=%+v must not expose another agent's active run", in.Run)
	}
	if len(runProvider.calls) != 2 || runProvider.calls[0].agentID != targetAgent || runProvider.calls[1].agentID != targetAgent {
		t.Fatalf("run lookup calls=%+v want target agent %d only", runProvider.calls, targetAgent)
	}
}

func TestResolveRejectsGroupMemberWithoutShare(t *testing.T) {
	_, cleanup := setupResolverShareTest(t)
	defer cleanup()

	const (
		ownerID   = int64(91041)
		memberID  = int64(91042)
		agentID   = int64(92041)
		sessionID = "toolbar-share-group-no-share"
	)
	seedResolverUser(t, ownerID, "toolbar-owner-g2")
	seedResolverUser(t, memberID, "toolbar-member-g2")
	seedResolverAPIAgent(t, agentID, ownerID)
	seedResolverGroupSession(t, sessionID, []int64{ownerID, memberID}, agentID)

	runProvider := &recordingResolverRunProvider{runs: map[int64]toolruntime.RunState{}}
	r := New(runProvider)
	_, err := r.Resolve(context.Background(), memberID, sessionID, agentID)
	if err != core.ErrSessionForbidden {
		t.Fatalf("Resolve err=%v want %v", err, core.ErrSessionForbidden)
	}
	if len(runProvider.calls) != 0 {
		t.Fatalf("unauthorized member triggered run lookup calls=%+v", runProvider.calls)
	}
}

func TestResolveOwnerStillWorks(t *testing.T) {
	_, cleanup := setupResolverShareTest(t)
	defer cleanup()

	const (
		ownerID   = int64(91051)
		agentID   = int64(92051)
		sessionID = "toolbar-share-owner"
	)
	seedResolverUser(t, ownerID, "toolbar-owner-ok")
	seedResolverAPIAgent(t, agentID, ownerID)
	seedResolverDirectSession(t, sessionID, ownerID, agentID)

	r := New(nil)
	in, err := r.Resolve(context.Background(), ownerID, sessionID, 0)
	if err != nil {
		t.Fatalf("owner Resolve err=%v", err)
	}
	if in.OwnerID != ownerID || in.Agent.OwnerID != ownerID {
		t.Fatalf("OwnerID mismatch request=%d agent_owner=%d", in.OwnerID, in.Agent.OwnerID)
	}
}
