package notification

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	pkgagentapi "github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/push/provider"
	"github.com/askie/grix/backend/internal/store"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capturePush records the payloads a notifier delivers.
type capturePush struct {
	mu       sync.Mutex
	payloads []*provider.PushPayload
	users    []int64
}

func (c *capturePush) fn(_ context.Context, userID int64, p *provider.PushPayload) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.users = append(c.users, userID)
	c.payloads = append(c.payloads, p)
	return nil
}

func (c *capturePush) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.payloads)
}

func (c *capturePush) last() *provider.PushPayload {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.payloads) == 0 {
		return nil
	}
	return c.payloads[len(c.payloads)-1]
}

// presenceTestEnv wires store.DB (sqlite) + store.RDB (miniredis) and restores
// the globals on cleanup.
func presenceTestEnv(t *testing.T) {
	t.Helper()
	tdb := testutil.NewTestDB()
	rdb := testutil.NewMockRedis()
	prevDB, prevRDB := store.DB, store.RDB
	store.DB, store.RDB = tdb.DB, rdb
	t.Cleanup(func() {
		store.DB, store.RDB = prevDB, prevRDB
		tdb.Close()
		_ = rdb.Close()
	})
}

func seedAgent(t *testing.T, id, ownerID int64, name string) {
	t.Helper()
	require.NoError(t, store.DB.Create(&model.Agent{ID: id, AgentName: name, OwnerID: ownerID}).Error)
}

func markConnInfoLive(t *testing.T, agentID, ownerID int64) {
	t.Helper()
	require.NoError(t, store.RDB.Set(context.Background(),
		pkgagentapi.ConnInfoKey(agentID, ownerID), "{}", time.Minute).Err())
}

func makeOfflineDue(t *testing.T, agentID, ownerID int64) {
	t.Helper()
	pastMs := time.Now().Add(-time.Second).UnixMilli()
	require.NoError(t, store.RDB.ZAdd(context.Background(), presenceOfflinePendingKey,
		redis.Z{Score: float64(pastMs), Member: presenceMember(agentID, ownerID)}).Err())
}

func TestPresenceOnlineImmediateAndDedup(t *testing.T) {
	presenceTestEnv(t)
	const owner, agent = int64(100), int64(1)
	seedAgent(t, agent, owner, "四喜")
	cap := &capturePush{}
	p := &PresenceNotifier{push: cap.fn, instanceToken: "test"}
	ctx := context.Background()

	// First online → immediate leading push.
	p.OnOnlineSignal(ctx, owner, agent)
	require.Equal(t, 1, cap.count(), "first online should push immediately")
	assert.Equal(t, EventAgentOnline, cap.last().EventKey)
	assert.Equal(t, "四喜 已上线", cap.last().Body)
	assert.True(t, cap.last().ForcePush)
	assert.Equal(t, owner, cap.last().RecipientID)

	// Already online → no duplicate push.
	p.OnOnlineSignal(ctx, owner, agent)
	assert.Equal(t, 1, cap.count(), "re-online while online must not push again")
}

func TestPresenceOfflineConfirmedAfterFlap(t *testing.T) {
	presenceTestEnv(t)
	const owner, agent = int64(100), int64(2)
	seedAgent(t, agent, owner, "小助手")
	cap := &capturePush{}
	p := &PresenceNotifier{push: cap.fn, instanceToken: "test"}
	ctx := context.Background()

	// Bring it online first so an offline is a real transition.
	p.OnOnlineSignal(ctx, owner, agent)
	require.Equal(t, 1, cap.count())

	// Schedule an offline confirmation that is already due, agent not live.
	makeOfflineDue(t, agent, owner)
	p.processOfflinePending(ctx)
	require.Equal(t, 2, cap.count(), "offline past flap window should push")
	assert.Equal(t, EventAgentOffline, cap.last().EventKey)
	assert.Equal(t, "小助手 已离线", cap.last().Body)

	// A second confirmation for the same (already offline) agent must not push.
	makeOfflineDue(t, agent, owner)
	p.processOfflinePending(ctx)
	assert.Equal(t, 2, cap.count(), "already-offline agent must not re-notify")
}

func TestPresenceOfflineSuppressedWhenReconnected(t *testing.T) {
	presenceTestEnv(t)
	const owner, agent = int64(100), int64(3)
	seedAgent(t, agent, owner, "reconnected")
	cap := &capturePush{}
	p := &PresenceNotifier{push: cap.fn, instanceToken: "test"}
	ctx := context.Background()

	p.OnOnlineSignal(ctx, owner, agent) // online, 1 push
	require.Equal(t, 1, cap.count())

	// Agent is live again (conn info present) when the confirmation comes due.
	makeOfflineDue(t, agent, owner)
	markConnInfoLive(t, agent, owner)
	p.processOfflinePending(ctx)
	assert.Equal(t, 1, cap.count(), "reconnected agent must not fire offline push")
}

func TestPresenceBlipNoOnlinePush(t *testing.T) {
	presenceTestEnv(t)
	const owner, agent = int64(100), int64(4)
	seedAgent(t, agent, owner, "blip")
	cap := &capturePush{}
	p := &PresenceNotifier{push: cap.fn, instanceToken: "test"}
	ctx := context.Background()

	// First connect → online push (first-ever). State = online.
	p.OnOnlineSignal(ctx, owner, agent)
	require.Equal(t, 1, cap.count())

	// A brief blip: SignalAgentOffline schedules, then SignalAgentOnline (ws)
	// cancels it, and the online signal finds state already online → no push.
	SignalAgentOffline(agent, owner)
	SignalAgentOnline(agent, owner) // cancels the pending offline (ZRem)
	p.OnOnlineSignal(ctx, owner, agent)
	assert.Equal(t, 1, cap.count(), "blip reconnect must not push online again")

	// The pending offline was cancelled, so a tick finds nothing to confirm.
	p.processOfflinePending(ctx)
	assert.Equal(t, 1, cap.count(), "cancelled offline must not fire")
}

func TestPresenceMergeTrailing(t *testing.T) {
	presenceTestEnv(t)
	const owner = int64(200)
	seedAgent(t, 10, owner, "A")
	seedAgent(t, 11, owner, "B")
	seedAgent(t, 12, owner, "C")
	cap := &capturePush{}
	p := &PresenceNotifier{push: cap.fn, instanceToken: "test"}
	ctx := context.Background()

	// Three agents come online close together: first is leading (immediate),
	// the other two are batched into the trailing merge.
	p.OnOnlineSignal(ctx, owner, 10)
	p.OnOnlineSignal(ctx, owner, 11)
	p.OnOnlineSignal(ctx, owner, 12)
	require.Equal(t, 1, cap.count(), "only the leading online should push immediately")
	assert.Equal(t, "A 已上线", cap.last().Body)

	// Simulate the merge window expiring, then flush the trailing batch.
	require.NoError(t, store.RDB.Del(ctx, presenceWindowKey(presenceKindOnline, owner)).Err())
	p.flushTrailing(ctx, presenceKindOnline)
	require.Equal(t, 2, cap.count(), "trailing batch should produce one merged push")
	assert.Equal(t, "2 个 Agent 已上线", cap.last().Body)
	assert.Equal(t, EventAgentOnline, cap.last().EventKey)
	// Merged push has no single session to open.
	assert.Empty(t, cap.last().SessionID)
}

func TestPresenceSingleOnlineDeepLink(t *testing.T) {
	presenceTestEnv(t)
	const owner, agent = int64(300), int64(20)
	seedAgent(t, agent, owner, "linky")
	// Seed the owner↔agent direct session so the push deep-links to it.
	dk := agentDirectKey(owner, agent)
	sess := &model.Session{
		SessionID:   "sess-abc",
		OwnerID:     owner,
		SessionType: model.SessionTypeDirect,
		DirectKey:   &dk,
	}
	require.NoError(t, store.DB.Create(sess).Error)

	cap := &capturePush{}
	p := &PresenceNotifier{push: cap.fn, instanceToken: "test"}
	p.OnOnlineSignal(context.Background(), owner, agent)
	require.Equal(t, 1, cap.count())
	assert.Equal(t, "sess-abc", cap.last().SessionID, "single-agent push should open its last session")
}

func TestPresenceRespectsDisabledPref(t *testing.T) {
	presenceTestEnv(t)
	const owner, agent = int64(400), int64(30)
	seedAgent(t, agent, owner, "muted")
	// Mirror the real UI flow: the settings page reads prefs (seeding default
	// rows) before the user toggles one off, so UpdatePrefs runs as an UPDATE.
	_, err := GetPrefs(owner)
	require.NoError(t, err)
	require.NoError(t, UpdatePrefs(owner, []PrefView{
		{EventKey: EventAgentOnline, Enabled: false, Channels: []string{ChannelPush}},
	}))
	cap := &capturePush{}
	p := &PresenceNotifier{push: cap.fn, instanceToken: "test"}
	p.OnOnlineSignal(context.Background(), owner, agent)
	assert.Equal(t, 0, cap.count(), "disabled online pref must suppress the push")
}

func TestPresenceOnlineTransitionRefreshesTTL(t *testing.T) {
	presenceTestEnv(t)
	const owner, agent = int64(600), int64(50)
	seedAgent(t, agent, owner, "longlived")
	cap := &capturePush{}
	p := &PresenceNotifier{push: cap.fn, instanceToken: "test"}
	ctx := context.Background()

	p.OnOnlineSignal(ctx, owner, agent) // first online, state set with TTL
	require.Equal(t, 1, cap.count())

	// Simulate the state key nearing expiry, then a flap reconnect (still-online
	// signal). The already-online branch must refresh the TTL, not leave it to
	// lapse — otherwise a long-lived agent would misfire a false "online" later.
	require.NoError(t, store.RDB.Expire(ctx, presenceStateKey(agent), 2*time.Second).Err())
	p.OnOnlineSignal(ctx, owner, agent) // dedup, but should EXPIRE-refresh
	assert.Equal(t, 1, cap.count(), "still-online signal must not push")

	ttl, err := store.RDB.TTL(ctx, presenceStateKey(agent)).Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, time.Hour, "TTL should be refreshed to the full window")
}

func TestPresenceReconcileCatchesCrash(t *testing.T) {
	presenceTestEnv(t)
	const owner, agent = int64(700), int64(60)
	seedAgent(t, agent, owner, "crashed")
	cap := &capturePush{}
	p := &PresenceNotifier{push: cap.fn, instanceToken: "test"}
	ctx := context.Background()

	// Agent came online (registered in the online set). A live conn-info key is
	// what a healthy connection would keep refreshed.
	markConnInfoLive(t, agent, owner)
	p.OnOnlineSignal(ctx, owner, agent)
	require.Equal(t, 1, cap.count())
	// Ignore the online push; assert only on what the crash path produces.
	cap.mu.Lock()
	cap.payloads, cap.users = nil, nil
	cap.mu.Unlock()

	// ws node crashes: no graceful unregister, so nothing scheduled the offline.
	// The conn-info lease simply expires.
	require.NoError(t, store.RDB.Del(ctx, pkgagentapi.ConnInfoKey(agent, owner)).Err())

	// First sweep detects the absence and schedules an offline confirmation.
	p.reconcileOnlineSet(ctx)
	assert.Equal(t, 0, cap.count(), "sweep only schedules; no immediate push")

	// Force the scheduled confirmation to be due, then confirm → offline push.
	makeOfflineDue(t, agent, owner)
	p.processOfflinePending(ctx)
	require.Equal(t, 1, cap.count(), "crash-detected agent should eventually push offline")
	assert.Equal(t, EventAgentOffline, cap.last().EventKey)

	// And the agent is dropped from the online set so it can notify online again.
	n, err := store.RDB.ZCard(ctx, presenceOnlineSetKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)
}

func TestPresenceReconcileKeepsLiveAgent(t *testing.T) {
	presenceTestEnv(t)
	const owner, agent = int64(710), int64(61)
	seedAgent(t, agent, owner, "healthy")
	cap := &capturePush{}
	p := &PresenceNotifier{push: cap.fn, instanceToken: "test"}
	ctx := context.Background()

	markConnInfoLive(t, agent, owner)
	p.OnOnlineSignal(ctx, owner, agent)
	require.Equal(t, 1, cap.count())

	p.reconcileOnlineSet(ctx)
	// Live agent must not be scheduled for offline.
	n, err := store.RDB.ZCard(ctx, presenceOfflinePendingKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), n, "live agent must not be scheduled offline")
}

func TestPresenceOfflineMergeTrailing(t *testing.T) {
	presenceTestEnv(t)
	const owner = int64(720)
	seedAgent(t, 70, owner, "X")
	seedAgent(t, 71, owner, "Y")
	seedAgent(t, 72, owner, "Z")
	cap := &capturePush{}
	p := &PresenceNotifier{push: cap.fn, instanceToken: "test"}
	ctx := context.Background()
	// All three online first (so offline is a real transition each).
	for _, id := range []int64{70, 71, 72} {
		p.OnOnlineSignal(ctx, owner, id)
	}
	// Drop online-merge state so it doesn't interfere with the offline assertions.
	cap.mu.Lock()
	cap.payloads = nil
	cap.users = nil
	cap.mu.Unlock()
	require.NoError(t, store.RDB.Del(ctx, presenceWindowKey(presenceKindOnline, owner)).Err())

	// Three offlines confirmed in the same tick: leading single + trailing merge.
	for _, id := range []int64{70, 71, 72} {
		makeOfflineDue(t, id, owner)
	}
	p.processOfflinePending(ctx)
	require.Equal(t, 1, cap.count(), "only the leading offline pushes immediately")
	assert.Equal(t, EventAgentOffline, cap.last().EventKey)

	require.NoError(t, store.RDB.Del(ctx, presenceWindowKey(presenceKindOffline, owner)).Err())
	p.flushTrailing(ctx, presenceKindOffline)
	require.Equal(t, 2, cap.count(), "trailing offline batch merges into one push")
	assert.Equal(t, "2 个 Agent 已离线", cap.last().Body)
}

func TestParsePresenceMember(t *testing.T) {
	a, o, ok := parsePresenceMember("12:34")
	require.True(t, ok)
	assert.Equal(t, int64(12), a)
	assert.Equal(t, int64(34), o)
	_, _, ok = parsePresenceMember("bad")
	assert.False(t, ok)
	_, _, ok = parsePresenceMember(":5")
	assert.False(t, ok)
}

// Guard: agentDirectKey must match the human-side buildDirectKey ordering.
func TestAgentDirectKeyFormat(t *testing.T) {
	assert.Equal(t, "d:1:"+strconv.Itoa(7)+"|2:"+strconv.Itoa(9), agentDirectKey(7, 9))
}

// Ensure the JSON payload the connector receives carries the event key (sanity
// on the provider payload wiring).
func TestPresencePayloadShape(t *testing.T) {
	presenceTestEnv(t)
	const owner, agent = int64(500), int64(40)
	seedAgent(t, agent, owner, "shape")
	cap := &capturePush{}
	p := &PresenceNotifier{push: cap.fn, instanceToken: "test"}
	p.OnOnlineSignal(context.Background(), owner, agent)
	require.Equal(t, 1, cap.count())
	raw, err := json.Marshal(cap.last())
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"event_key":"agent_online"`)
}
