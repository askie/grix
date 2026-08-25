package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/redis/go-redis/v9"
)

const agentStateTTL = 48 * time.Hour

type agentStateExtra struct {
	Source          string `json:"source,omitempty"`
	Connected       bool   `json:"connected"`
	LeaseUntil      int64  `json:"lease_until,omitempty"`
	ConnectionEpoch int64  `json:"connection_epoch,omitempty"`
}

func agentStateHashKey(userID int64) string {
	return fmt.Sprintf("im:agent_state:%d", userID)
}

func agentStateEpochHashKey(userID int64) string {
	return fmt.Sprintf("im:agent_state_epoch:%d", userID)
}

func agentStateEpochSequenceHashKey(userID int64) string {
	return fmt.Sprintf("im:agent_state_epoch_seq:%d", userID)
}

// reserveAgentConnectionEpochScript allocates a globally monotonic generation
// for one owner+agent pair without advancing the accepted state epoch. Keeping
// the reservation sequence separate is important: if a process reserves a
// generation and crashes before publishing online, the previous live
// connection must still be able to refresh its already-accepted generation.
//
// max(sequence, accepted epoch)+1 also migrates safely from the historical
// wall-clock microsecond epochs without allowing a smaller generation. The
// sequence hash intentionally has no TTL so generations never move backwards
// after an inactive period; state and accepted-epoch hashes retain their TTL.
var reserveAgentConnectionEpochScript = redis.NewScript(`
local sequence_raw = redis.call("HGET", KEYS[1], ARGV[1])
local accepted_raw = redis.call("HGET", KEYS[2], ARGV[1])
local sequence = tonumber(sequence_raw or "0") or 0
local accepted = tonumber(accepted_raw or "0") or 0
local next_epoch = math.max(sequence, accepted) + 1

redis.call("HSET", KEYS[1], ARGV[1], next_epoch)
return next_epoch
`)

// recordAgentStateScript atomically persists state only when it belongs to the
// newest known connection for this owner+agent pair. Equal epochs are accepted
// because one connection emits repeated online leases and a final offline
// state. Epoch zero preserves last-write-wins behavior for legacy senders until
// a positive epoch has been observed.
var recordAgentStateScript = redis.NewScript(`
if ARGV[5] == "1" then
  local current_state = redis.call("HGET", KEYS[1], ARGV[1])
  if not current_state or current_state ~= ARGV[6] then
    return 0
  end
end

local current_raw = redis.call("HGET", KEYS[2], ARGV[1])
local current_epoch = tonumber(current_raw or "0") or 0
local incoming_epoch = tonumber(ARGV[3]) or 0
if current_raw and incoming_epoch < current_epoch then
  return 0
end

redis.call("HSET", KEYS[1], ARGV[1], ARGV[2])
redis.call("HSET", KEYS[2], ARGV[1], incoming_epoch)
redis.call("EXPIRE", KEYS[1], ARGV[4])
redis.call("EXPIRE", KEYS[2], ARGV[4])
return 1
`)

// ReserveAgentConnectionEpoch returns a Redis-coordinated, globally monotonic
// connection generation for an owner+agent pair. Production callers must fail
// the websocket authentication when this allocation fails: falling back to a
// local clock or epoch zero would let a connection appear online without
// cross-node ordering guarantees.
func ReserveAgentConnectionEpoch(ctx context.Context, userID, agentID int64) (int64, error) {
	if userID <= 0 || agentID <= 0 {
		return 0, fmt.Errorf("invalid agent state epoch identity user=%d agent=%d", userID, agentID)
	}
	if store.RDB == nil {
		return 0, fmt.Errorf("agent state epoch allocator unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	epoch, err := reserveAgentConnectionEpochScript.Run(
		ctx,
		store.RDB,
		[]string{
			agentStateEpochSequenceHashKey(userID),
			agentStateEpochHashKey(userID),
		},
		strconv.FormatInt(agentID, 10),
	).Int64()
	if err != nil {
		return 0, fmt.Errorf("reserve agent state epoch: %w", err)
	}
	if epoch <= 0 {
		return 0, fmt.Errorf("reserve agent state epoch returned invalid generation %d", epoch)
	}
	return epoch, nil
}

func BuildAgentStatePayload(
	agentID int64,
	state string,
	connected bool,
	leaseUntil int64,
) protocol.AgentStateSyncPayload {
	return BuildAgentStatePayloadWithEpoch(agentID, state, connected, leaseUntil, 0)
}

func BuildAgentStatePayloadWithEpoch(
	agentID int64,
	state string,
	connected bool,
	leaseUntil int64,
	connectionEpoch int64,
) protocol.AgentStateSyncPayload {
	if state != protocol.AgentStateOnline {
		state = protocol.AgentStateOffline
		connected = false
		leaseUntil = 0
	}
	if connectionEpoch < 0 {
		connectionEpoch = 0
	}
	return protocol.AgentStateSyncPayload{
		AgentID: agentID,
		State:   state,
		Extra: marshalPayload(agentStateExtra{
			Source:          protocol.SessionActivitySourceAgentAPI,
			Connected:       connected,
			LeaseUntil:      leaseUntil,
			ConnectionEpoch: connectionEpoch,
		}),
	}
}

// RecordAgentState returns true only when the incoming state was accepted.
// Callers must gate fanout on the result so a stale cross-node disconnect
// cannot be broadcast after a newer connection has already come online.
func RecordAgentState(ctx context.Context, userID int64, payload protocol.AgentStateSyncPayload) bool {
	return recordAgentState(ctx, userID, payload, nil)
}

func recordAgentStateIfCurrentRaw(
	ctx context.Context,
	userID int64,
	payload protocol.AgentStateSyncPayload,
	expectedRaw string,
) bool {
	return recordAgentState(ctx, userID, payload, &expectedRaw)
}

func recordAgentState(
	ctx context.Context,
	userID int64,
	payload protocol.AgentStateSyncPayload,
	expectedRaw *string,
) bool {
	if userID <= 0 || payload.AgentID <= 0 {
		return false
	}
	if store.RDB == nil {
		// Preserve local-only behavior in deployments/tests without Redis.
		return true
	}
	if ctx == nil {
		ctx = context.Background()
	}

	data, err := json.Marshal(payload)
	if err != nil {
		logger.L.Warnf("record agent state marshal error user=%d agent=%d: %v", userID, payload.AgentID, err)
		return false
	}

	var extra agentStateExtra
	if len(payload.Extra) > 0 {
		_ = json.Unmarshal(payload.Extra, &extra)
	}
	if extra.ConnectionEpoch < 0 {
		extra.ConnectionEpoch = 0
	}

	key := agentStateHashKey(userID)
	epochKey := agentStateEpochHashKey(userID)
	field := strconv.FormatInt(payload.AgentID, 10)
	requireExpectedRaw := 0
	expected := ""
	if expectedRaw != nil {
		requireExpectedRaw = 1
		expected = *expectedRaw
	}
	accepted, err := recordAgentStateScript.Run(
		ctx,
		store.RDB,
		[]string{key, epochKey},
		field,
		data,
		extra.ConnectionEpoch,
		int64(agentStateTTL/time.Second),
		requireExpectedRaw,
		expected,
	).Int()
	if err != nil {
		logger.L.Warnf("record agent state redis error user=%d agent=%d: %v", userID, payload.AgentID, err)
		return false
	}
	return accepted == 1
}

func LoadAgentStates(ctx context.Context, userID int64) []protocol.AgentStateSyncPayload {
	if userID <= 0 {
		return nil
	}
	if store.RDB == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	items, err := store.RDB.HGetAll(ctx, agentStateHashKey(userID)).Result()
	if err != nil || len(items) == 0 {
		return nil
	}

	agentIDs := make([]int64, 0, len(items))
	parsed := make(map[int64]protocol.AgentStateSyncPayload, len(items))
	nowMs := time.Now().UnixMilli()
	for field, raw := range items {
		agentID, parseErr := strconv.ParseInt(field, 10, 64)
		if parseErr != nil || agentID <= 0 || raw == "" {
			continue
		}
		normalized, ok := resolveLoadedAgentState(ctx, userID, agentID, raw, nowMs)
		if !ok {
			continue
		}
		parsed[agentID] = normalized
		agentIDs = append(agentIDs, agentID)
	}

	sort.Slice(agentIDs, func(i, j int) bool {
		return agentIDs[i] < agentIDs[j]
	})

	result := make([]protocol.AgentStateSyncPayload, 0, len(agentIDs))
	for _, agentID := range agentIDs {
		result = append(result, parsed[agentID])
	}
	return result
}

const agentStateResolveMaxAttempts = 4

// resolveLoadedAgentState normalizes one HGETALL snapshot. If normalization
// loses the epoch CAS, the snapshot was superseded between HGETALL and HSET;
// returning it would leak a stale offline state to the caller even though it
// was correctly rejected by Redis. Read the current field back and resolve it
// again. Under continuous reconnect churn, fail closed after a small bound and
// let the next state sync/load provide the current value.
func resolveLoadedAgentState(
	ctx context.Context,
	userID int64,
	agentID int64,
	raw string,
	nowMs int64,
) (protocol.AgentStateSyncPayload, bool) {
	for attempt := 0; attempt < agentStateResolveMaxAttempts; attempt++ {
		var payload protocol.AgentStateSyncPayload
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			logger.L.Warnf("load agent state decode error user=%d agent=%d: %v", userID, agentID, err)
			return protocol.AgentStateSyncPayload{}, false
		}
		payload.AgentID = agentID
		normalized, changed := normalizeAgentStatePayload(payload, nowMs)
		if !changed || recordAgentStateIfCurrentRaw(ctx, userID, normalized, raw) {
			return normalized, true
		}

		currentRaw, err := store.RDB.HGet(
			ctx,
			agentStateHashKey(userID),
			strconv.FormatInt(agentID, 10),
		).Result()
		if err == redis.Nil {
			return protocol.AgentStateSyncPayload{}, false
		}
		if err != nil {
			logger.L.Warnf("reload agent state error user=%d agent=%d: %v", userID, agentID, err)
			return protocol.AgentStateSyncPayload{}, false
		}
		raw = currentRaw
	}

	logger.L.Warnf("resolve agent state exceeded retries user=%d agent=%d", userID, agentID)
	return protocol.AgentStateSyncPayload{}, false
}

func PushStoredAgentStates(conn ConnInterface) {
	if conn == nil || conn.GetUserID() <= 0 {
		return
	}
	states := LoadAgentStates(context.Background(), conn.GetUserID())
	for _, state := range states {
		state.ServerNowMs = time.Now().UnixMilli()
		conn.SendPayload(protocol.CmdAgentStateSync, conn.NextSeq(), state)
	}
}

func normalizeAgentStatePayload(
	payload protocol.AgentStateSyncPayload,
	nowMs int64,
) (protocol.AgentStateSyncPayload, bool) {
	if payload.AgentID <= 0 {
		return payload, false
	}

	var extra agentStateExtra
	if len(payload.Extra) > 0 {
		_ = json.Unmarshal(payload.Extra, &extra)
	}

	if payload.State != protocol.AgentStateOnline {
		normalized := BuildAgentStatePayloadWithEpoch(
			payload.AgentID,
			protocol.AgentStateOffline,
			false,
			0,
			extra.ConnectionEpoch,
		)
		return normalized, !agentStatePayloadEqual(payload, normalized)
	}

	if extra.LeaseUntil <= nowMs {
		normalized := BuildAgentStatePayloadWithEpoch(
			payload.AgentID,
			protocol.AgentStateOffline,
			false,
			0,
			extra.ConnectionEpoch,
		)
		return normalized, true
	}

	normalized := BuildAgentStatePayloadWithEpoch(
		payload.AgentID,
		protocol.AgentStateOnline,
		true,
		extra.LeaseUntil,
		extra.ConnectionEpoch,
	)
	return normalized, !agentStatePayloadEqual(payload, normalized)
}

func agentStatePayloadEqual(a, b protocol.AgentStateSyncPayload) bool {
	return a.AgentID == b.AgentID && a.State == b.State && string(a.Extra) == string(b.Extra)
}
