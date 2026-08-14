package agentapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestHandleSkillsUpdate_RefreshesRedisProfileSkills(t *testing.T) {
	previousRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previousRDB
	}()

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:    9101,
		ownerID:    1001,
		clientID:   "agent-skills-update",
		clientType: "kiro",
		adapterID:  "kiro/base",
		send:       make(chan []byte, 4),
	}
	mgr.register(conn)
	defer mgr.unregister(conn)
	stateEmits := 0
	mgr.SetAgentStateHandler(func(_ int64, _ protocol.AgentStateSyncPayload) {
		stateEmits++
	})

	payload := SkillsUpdatePayload{
		Skills: json.RawMessage(`[
			{"name":"grix-log-locator","description":"日志定位","source":"project"},
			{"name":"graphify","description":"知识图谱","trigger":"/graphify","source":"global"}
		]`),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	mgr.handleSkillsUpdate(conn, &protocol.Packet{
		Cmd:     protocol.CmdAgentSkillsUpdate,
		Seq:     1,
		Payload: raw,
	})

	profile, ok, err := toolruntime.LoadProfile(context.Background(), conn.agentID)
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}
	if !ok {
		t.Fatalf("profile should be stored after skills update")
	}
	if got := len(profile.Skills); got != 2 {
		t.Fatalf("skills count=%d want=2 profile=%+v", got, profile.Skills)
	}
	wantNames := map[string]bool{"grix-log-locator": false, "graphify": false}
	for _, s := range profile.Skills {
		if _, expected := wantNames[s.Name]; !expected {
			t.Fatalf("unexpected skill name=%q", s.Name)
		}
		wantNames[s.Name] = true
	}
	for name, seen := range wantNames {
		if !seen {
			t.Fatalf("skill %q missing from profile", name)
		}
	}
	if stateEmits != 1 {
		t.Fatalf("agent state emits=%d want=1; one skills update must refresh lease exactly once", stateEmits)
	}
}

func TestHandleSkillsUpdate_InvalidPayloadDoesNotRefreshLease(t *testing.T) {
	previousRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previousRDB
	}()

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:  9104,
		ownerID:  1001,
		clientID: "agent-skills-invalid",
		send:     make(chan []byte, 4),
	}
	mgr.register(conn)
	defer mgr.unregister(conn)
	stateEmits := 0
	mgr.SetAgentStateHandler(func(_ int64, _ protocol.AgentStateSyncPayload) {
		stateEmits++
	})

	mgr.handleSkillsUpdate(conn, &protocol.Packet{
		Cmd:     protocol.CmdAgentSkillsUpdate,
		Seq:     1,
		Payload: json.RawMessage(`{"skills":`),
	})

	if stateEmits != 0 {
		t.Fatalf("agent state emits=%d want=0; invalid skills payload must not refresh lease", stateEmits)
	}
	select {
	case <-conn.send:
		// Invalid payloads must still receive the existing protocol error response.
	default:
		t.Fatal("invalid skills payload should enqueue an error response")
	}
}

func TestHandleSkillsUpdate_RefreshesRedisProfileLibrarySkills(t *testing.T) {
	previousRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previousRDB
	}()

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:    9103,
		ownerID:    1001,
		clientID:   "agent-library-skills-update",
		clientType: "kiro",
		adapterID:  "kiro/base",
		send:       make(chan []byte, 4),
	}
	mgr.register(conn)
	defer mgr.unregister(conn)

	payload := SkillsUpdatePayload{
		Skills: json.RawMessage(`[]`),
		LibrarySkills: json.RawMessage(`[
			{"name":"grix-log-locator","description":"日志定位","digest":"abc123","dir":"/home/u/grix/skills/grix-log-locator","owner_id":"1001","enable_scopes":{"global":"link","project":"none"}},
			{"name":"graphify","description":"知识图谱","system":true,"enable_scopes":{"global":"unmanaged","project":"unavailable"}}
		]`),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	mgr.handleSkillsUpdate(conn, &protocol.Packet{
		Cmd:     protocol.CmdAgentSkillsUpdate,
		Seq:     1,
		Payload: raw,
	})

	profile, ok, err := toolruntime.LoadProfile(context.Background(), conn.agentID)
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}
	if !ok {
		t.Fatalf("profile should be stored after skills update")
	}
	if got := len(profile.LibrarySkills); got != 2 {
		t.Fatalf("library_skills count=%d want=2 profile=%+v", got, profile.LibrarySkills)
	}
	byName := make(map[string]toolruntime.LibrarySkillEntry, len(profile.LibrarySkills))
	for _, s := range profile.LibrarySkills {
		byName[s.Name] = s
	}
	locator, ok := byName["grix-log-locator"]
	if !ok {
		t.Fatalf("grix-log-locator missing from library_skills: %+v", profile.LibrarySkills)
	}
	if locator.OwnerID != 1001 {
		t.Fatalf("owner_id=%d want=1001", locator.OwnerID)
	}
	if locator.EnableScopes.Global != "link" || locator.EnableScopes.Project != "none" {
		t.Fatalf("enable_scopes=%+v want global=link project=none", locator.EnableScopes)
	}
	graphify, ok := byName["graphify"]
	if !ok {
		t.Fatalf("graphify missing from library_skills: %+v", profile.LibrarySkills)
	}
	if !graphify.System {
		t.Fatalf("graphify.System=%v want=true", graphify.System)
	}
	if graphify.EnableScopes.Project != "unavailable" {
		t.Fatalf("graphify.enable_scopes.project=%q want=%q", graphify.EnableScopes.Project, "unavailable")
	}
}

func TestHandleSkillsUpdate_SharedProductionEpochStoresOwnerRuntimeProfile(t *testing.T) {
	previousRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previousRDB
	}()

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	mgr.SetNodeID("shared-skills-node")
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:         9105,
		ownerID:         1002,
		isPrimary:       false,
		clientID:        "agent-shared-skills-update",
		clientType:      "codex",
		adapterID:       "codex/base",
		connectionEpoch: 17,
		connectedAt:     time.Now(),
		send:            make(chan []byte, 4),
		done:            make(chan struct{}),
	}
	if !mgr.attachConn(conn) {
		t.Fatal("shared production connection should claim authority")
	}

	payload := SkillsUpdatePayload{
		Skills: json.RawMessage(`[{"name":"project-skill","source":"project"}]`),
		LibrarySkills: json.RawMessage(`[
			{"name":"project-skill","digest":"shared-v2","enable_scopes":{"global":"none","project":"link"}}
		]`),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	mgr.handleSkillsUpdate(conn, &protocol.Packet{
		Cmd:     protocol.CmdAgentSkillsUpdate,
		Seq:     1,
		Payload: raw,
	})

	profile, ok, err := toolruntime.LoadProfileForOwner(context.Background(), conn.agentID, conn.ownerID)
	if err != nil || !ok {
		t.Fatalf("load shared owner runtime profile ok=%t err=%v", ok, err)
	}
	if profile.OwnerID != conn.ownerID || len(profile.LibrarySkills) != 1 {
		t.Fatalf("shared runtime profile=%+v", profile)
	}
	if got := profile.LibrarySkills[0].EnableScopes.Project; got != "link" {
		t.Fatalf("shared project scope=%q want=link", got)
	}
	if _, ok, err := toolruntime.LoadProfile(context.Background(), conn.agentID); err != nil || ok {
		t.Fatalf("shared refresh must not replace primary profile ok=%t err=%v", ok, err)
	}
	mgr.unregister(conn)
	if _, ok, err := toolruntime.LoadProfileForOwner(context.Background(), conn.agentID, conn.ownerID); err != nil || ok {
		t.Fatalf("shared disconnect must clear owner profile ok=%t err=%v", ok, err)
	}
}

func TestHandleSkillsUpdate_EmptyListClearsRedisSkills(t *testing.T) {
	previousRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previousRDB
	}()

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:    9102,
		ownerID:    1001,
		clientID:   "agent-skills-clear",
		clientType: "kiro",
		adapterID:  "kiro/base",
		skills: []toolruntime.SkillEntry{
			{Name: "stale", Description: "previous"},
		},
		send: make(chan []byte, 4),
	}
	mgr.register(conn)
	defer mgr.unregister(conn)

	mgr.handleSkillsUpdate(conn, &protocol.Packet{
		Cmd:     protocol.CmdAgentSkillsUpdate,
		Seq:     1,
		Payload: json.RawMessage(`{"skills":[]}`),
	})

	profile, ok, err := toolruntime.LoadProfile(context.Background(), conn.agentID)
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}
	if !ok {
		t.Fatalf("profile should be stored")
	}
	if len(profile.Skills) != 0 {
		t.Fatalf("skills should be empty after update, got=%+v", profile.Skills)
	}
}

func TestHandleSkillsUpdate_OwnerLibrarySyncPropagatesPeersPreservingScopes(t *testing.T) {
	previousRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previousRDB
	}()

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	reporter := &agentConn{
		agentID:    9201,
		ownerID:    2001,
		clientID:   "reporter",
		clientType: "codex",
		adapterID:  "codex/base",
		send:       make(chan []byte, 4),
		done:       make(chan struct{}),
	}
	peer := &agentConn{
		agentID:    9202,
		ownerID:    2001,
		clientID:   "peer",
		clientType: "codex",
		adapterID:  "codex/base",
		librarySkills: []toolruntime.LibrarySkillEntry{{
			Name:   "keep-me",
			Digest: "old",
			EnableScopes: toolruntime.LibrarySkillEnableScopes{
				Global:  "link",
				Project: "link",
			},
		}},
		send: make(chan []byte, 4),
		done: make(chan struct{}),
	}
	otherType := &agentConn{
		agentID:    9203,
		ownerID:    2001,
		clientID:   "claude-peer",
		clientType: "claude",
		adapterID:  "claude/base",
		librarySkills: []toolruntime.LibrarySkillEntry{{
			Name:   "keep-me",
			Digest: "claude-old",
			EnableScopes: toolruntime.LibrarySkillEnableScopes{
				Global: "unmanaged",
			},
		}},
		send: make(chan []byte, 4),
		done: make(chan struct{}),
	}
	mgr.register(reporter)
	mgr.register(peer)
	mgr.register(otherType)
	defer mgr.unregister(reporter)
	defer mgr.unregister(peer)
	defer mgr.unregister(otherType)

	payload := SkillsUpdatePayload{
		Skills: json.RawMessage(`[{"name":"active","source":"global"}]`),
		LibrarySkills: json.RawMessage(`[
			{"name":"keep-me","digest":"new","enable_scopes":{"global":"none","project":"unavailable"}},
			{"name":"brand-new","digest":"n1","enable_scopes":{"global":"none","project":"unavailable"}}
		]`),
		OwnerLibrarySync: true,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	mgr.handleSkillsUpdate(reporter, &protocol.Packet{
		Cmd:     protocol.CmdAgentSkillsUpdate,
		Seq:     1,
		Payload: raw,
	})

	peerProfile, ok, err := toolruntime.LoadProfile(context.Background(), peer.agentID)
	if err != nil || !ok {
		t.Fatalf("load peer profile ok=%t err=%v", ok, err)
	}
	if got := len(peerProfile.LibrarySkills); got != 2 {
		t.Fatalf("peer library_skills=%d want=2 %+v", got, peerProfile.LibrarySkills)
	}
	byName := map[string]toolruntime.LibrarySkillEntry{}
	for _, s := range peerProfile.LibrarySkills {
		byName[s.Name] = s
	}
	if byName["keep-me"].Digest != "new" {
		t.Fatalf("peer keep-me digest=%q want=new", byName["keep-me"].Digest)
	}
	if byName["keep-me"].EnableScopes.Global != "link" || byName["keep-me"].EnableScopes.Project != "link" {
		t.Fatalf("peer keep-me scopes=%+v want preserved link/link", byName["keep-me"].EnableScopes)
	}
	if _, ok := byName["brand-new"]; !ok {
		t.Fatalf("peer missing brand-new: %+v", peerProfile.LibrarySkills)
	}
	if byName["brand-new"].EnableScopes.Project != "unavailable" {
		t.Fatalf("new skill project=%q want=unavailable", byName["brand-new"].EnableScopes.Project)
	}
	if len(peerProfile.Skills) != 0 {
		t.Fatalf("owner_library_sync must not overwrite peer active skills, got %+v", peerProfile.Skills)
	}

	otherProfile, ok, err := toolruntime.LoadProfile(context.Background(), otherType.agentID)
	if err != nil || !ok {
		// otherType may never have been refreshed — profile might not exist
		if ok {
			t.Fatalf("unexpected err=%v", err)
		}
	} else if otherProfile.LibrarySkills[0].Digest != "claude-old" {
		t.Fatalf("different client_type must not receive catalog fan-in, digest=%q", otherProfile.LibrarySkills[0].Digest)
	}
	if otherType.librarySkills[0].Digest != "claude-old" {
		t.Fatalf("in-memory otherType digest=%q want=claude-old", otherType.librarySkills[0].Digest)
	}
}

func TestMergeLibrarySkillsCatalog_PreservesScopesAndDropsMissing(t *testing.T) {
	dst := []toolruntime.LibrarySkillEntry{{
		Name:   "a",
		Digest: "old",
		EnableScopes: toolruntime.LibrarySkillEnableScopes{Global: "link", Project: "none"},
	}}
	src := []toolruntime.LibrarySkillEntry{
		{Name: "a", Digest: "new", EnableScopes: toolruntime.LibrarySkillEnableScopes{Global: "none", Project: "unavailable"}},
		{Name: "b", Digest: "b1", EnableScopes: toolruntime.LibrarySkillEnableScopes{Global: "none", Project: "link"}},
	}
	out := mergeLibrarySkillsCatalog(dst, src)
	if len(out) != 2 {
		t.Fatalf("len=%d want=2", len(out))
	}
	if out[0].Digest != "new" || out[0].EnableScopes.Global != "link" {
		t.Fatalf("out[0]=%+v", out[0])
	}
	if out[1].Name != "b" || out[1].EnableScopes.Global != "none" {
		t.Fatalf("out[1]=%+v", out[1])
	}
	if out[1].EnableScopes.Project != "unavailable" {
		t.Fatalf("new skill project scope=%q want=unavailable (must not copy reporter project)", out[1].EnableScopes.Project)
	}
}

func TestHandleSkillsUpdate_OwnerLibrarySyncDoesNotPropagateDifferentOwner(t *testing.T) {
	previousRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previousRDB
	}()

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	reporter := &agentConn{
		agentID:    9301,
		ownerID:    3001,
		clientID:   "reporter",
		clientType: "codex",
		adapterID:  "codex/base",
		send:       make(chan []byte, 4),
		done:       make(chan struct{}),
	}
	otherOwner := &agentConn{
		agentID:    9302,
		ownerID:    3002,
		clientID:   "other-owner",
		clientType: "codex",
		adapterID:  "codex/base",
		librarySkills: []toolruntime.LibrarySkillEntry{{
			Name:   "keep-me",
			Digest: "other-old",
			EnableScopes: toolruntime.LibrarySkillEnableScopes{
				Global: "link",
			},
		}},
		send: make(chan []byte, 4),
		done: make(chan struct{}),
	}
	mgr.register(reporter)
	mgr.register(otherOwner)
	defer mgr.unregister(reporter)
	defer mgr.unregister(otherOwner)

	payload := SkillsUpdatePayload{
		LibrarySkills: json.RawMessage(`[
			{"name":"keep-me","digest":"new","enable_scopes":{"global":"none","project":"link"}},
			{"name":"brand-new","digest":"n1","enable_scopes":{"global":"none","project":"link"}}
		]`),
		OwnerLibrarySync: true,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	mgr.handleSkillsUpdate(reporter, &protocol.Packet{
		Cmd:     protocol.CmdAgentSkillsUpdate,
		Seq:     1,
		Payload: raw,
	})

	if got := otherOwner.librarySkills[0].Digest; got != "other-old" {
		t.Fatalf("different owner must not receive fan-in, digest=%q", got)
	}
	if len(otherOwner.librarySkills) != 1 {
		t.Fatalf("different owner library len=%d want=1", len(otherOwner.librarySkills))
	}
}
