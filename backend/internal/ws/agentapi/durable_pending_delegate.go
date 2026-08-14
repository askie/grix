package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	durablePendingDelegateTTL        = 48 * time.Hour
	durablePendingDelegateDrainBatch = 128

	durablePendingDelegateStageAck     = "ack"
	durablePendingDelegateStageResult  = "result"
	durablePendingDelegateStageIntent  = "terminal_intent"
	durablePendingDelegateStageSettled = "terminal_settled"
)

type durablePendingDelegateRecord struct {
	Event              DelegateEventPayload   `json:"event"`
	Attempt            int                    `json:"attempt"`
	Stage              string                 `json:"stage"`
	StartedAt          int64                  `json:"started_at,omitempty"`
	CallTurn           bool                   `json:"call_turn,omitempty"`
	DispatchGeneration int64                  `json:"dispatch_generation,omitempty"`
	ReceivedAt         int64                  `json:"received_at,omitempty"`
	RetryToken         string                 `json:"retry_token,omitempty"`
	RetryClaimUntil    int64                  `json:"retry_claim_until,omitempty"`
	RetryDispatchedAt  int64                  `json:"retry_dispatched_at,omitempty"`
	Terminal           *durableTerminalIntent `json:"terminal,omitempty"`
	Version            int64                  `json:"version"`
	UpdatedAt          int64                  `json:"updated_at"`
}

type durableTerminalIntent struct {
	Status     string `json:"status"`
	Code       string `json:"code,omitempty"`
	Msg        string `json:"msg,omitempty"`
	ClaimToken string `json:"claim_token,omitempty"`
	ClaimUntil int64  `json:"claim_until,omitempty"`
	CreatedAt  int64  `json:"created_at"`
	SettledAt  int64  `json:"settled_at,omitempty"`
}

type durableRetryEnvelope struct {
	EventID string `json:"event_id"`
	AgentID int64  `json:"agent_id,string"`
	OwnerID int64  `json:"owner_id,string"`
	Attempt int    `json:"attempt"`
	Token   string `json:"token"`
}

type durableRetryClaim struct {
	Envelope durableRetryEnvelope
	Record   *durablePendingDelegateRecord
	Won      bool
}

type terminalIntentDisposition int

const (
	terminalIntentMissing terminalIntentDisposition = iota
	terminalIntentClaimed
	terminalIntentPending
	terminalIntentSettled
	terminalIntentConflict
	terminalIntentUnauthorized
)

type terminalIntentClaim struct {
	Disposition terminalIntentDisposition
	Record      *durablePendingDelegateRecord
	Token       string
}

// Terminal settlement includes synchronous DB fencing and notification hooks.
// Keep the lease comfortably above their normal latency so a slow winner is
// not reclaimed mid-side-effect; crash recovery still occurs from connector
// outbox retries after this bounded interval.
const terminalIntentClaimLease = 30 * time.Second

var claimDurableRetryScript = redis.NewScript(`
local raw = redis.call("GET", KEYS[1])
if not raw then
  return {0, ""}
end
local record = cjson.decode(raw)
if record["stage"] ~= "ack" then
  return {-1, raw}
end
local event = record["event"] or {}
if tostring(event["agent_id"] or "") ~= ARGV[1] or tostring(event["owner_id"] or "") ~= ARGV[2] then
  return {-2, raw}
end
local now = tonumber(ARGV[3])
local current_token = tostring(record["retry_token"] or "")
local claim_until = tonumber(record["retry_claim_until"] or "0")
if current_token ~= "" and claim_until > now then
  return {2, raw}
end
local attempt = tonumber(record["attempt"] or "1")
local max_attempts = tonumber(ARGV[4])
if attempt >= max_attempts then
  return {3, raw}
end
attempt = attempt + 1
record["attempt"] = attempt
record["retry_token"] = ARGV[5]
record["retry_claim_until"] = tonumber(ARGV[6])
record["retry_dispatched_at"] = 0
record["version"] = tonumber(record["version"] or "0") + 1
record["updated_at"] = now
local updated = cjson.encode(record)
redis.call("SET", KEYS[1], updated, "EX", ARGV[7])
return {1, updated}
`)

var markDurableRetryDispatchedScript = redis.NewScript(`
local raw = redis.call("GET", KEYS[1])
if not raw then
  return 0
end
local record = cjson.decode(raw)
if record["stage"] ~= "ack" then
  return 0
end
local event = record["event"] or {}
if tostring(event["agent_id"] or "") ~= ARGV[1] or tostring(event["owner_id"] or "") ~= ARGV[2] then
  return 0
end
if tonumber(record["attempt"] or "0") ~= tonumber(ARGV[3]) then
  return 0
end
if tostring(record["retry_token"] or "") ~= ARGV[4] then
  return 0
end
if tonumber(record["retry_dispatched_at"] or "0") > 0 then
  return 0
end
record["retry_dispatched_at"] = tonumber(ARGV[5])
record["version"] = tonumber(record["version"] or "0") + 1
record["updated_at"] = tonumber(ARGV[5])
redis.call("SET", KEYS[1], cjson.encode(record), "EX", ARGV[6])
return 1
`)

var releaseDurableRetryDispatchScript = redis.NewScript(`
local raw = redis.call("GET", KEYS[1])
if not raw then
  return 0
end
local record = cjson.decode(raw)
if record["stage"] ~= "ack" or tostring(record["retry_token"] or "") ~= ARGV[1] then
  return 0
end
record["retry_dispatched_at"] = 0
record["retry_claim_until"] = tonumber(ARGV[2])
record["version"] = tonumber(record["version"] or "0") + 1
record["updated_at"] = tonumber(ARGV[2])
redis.call("SET", KEYS[1], cjson.encode(record), "EX", ARGV[3])
return 1
`)

var advanceDurableAckScript = redis.NewScript(`
local raw = redis.call("GET", KEYS[1])
if not raw then
  return {0, ""}
end
local record = cjson.decode(raw)
if record["stage"] ~= "ack" then
  return {2, raw}
end
local event = record["event"] or {}
if tostring(event["agent_id"] or "") ~= ARGV[1] or tostring(event["owner_id"] or "") ~= ARGV[2] then
  return {-1, raw}
end
record["stage"] = "result"
record["received_at"] = tonumber(ARGV[3])
record["retry_token"] = nil
record["retry_claim_until"] = nil
record["retry_dispatched_at"] = nil
record["version"] = tonumber(record["version"] or "0") + 1
record["updated_at"] = tonumber(ARGV[4])
local updated = cjson.encode(record)
redis.call("SET", KEYS[1], updated, "EX", ARGV[5])
return {1, updated}
`)

var deleteDurableIfUnchangedScript = redis.NewScript(`
local raw = redis.call("GET", KEYS[1])
if not raw then
  return 1
end
local record = cjson.decode(raw)
local event = record["event"] or {}
if tostring(event["agent_id"] or "") ~= ARGV[1]
    or tostring(event["owner_id"] or "") ~= ARGV[2] then
  return 0
end
if tonumber(record["version"] or "0") ~= tonumber(ARGV[3])
    or tostring(record["stage"] or "") ~= ARGV[4]
    or tonumber(record["attempt"] or "0") ~= tonumber(ARGV[5]) then
  return 0
end
local expected_generation = tonumber(ARGV[6] or "0")
if expected_generation > 0
    and tonumber(record["dispatch_generation"] or "0") ~= expected_generation then
  return 0
end
redis.call("DEL", KEYS[1])
return 1
`)

var claimTerminalIntentScript = redis.NewScript(`
local raw = redis.call("GET", KEYS[1])
local record
if raw then
  record = cjson.decode(raw)
else
  if ARGV[8] == "" then
    return {0, ""}
  end
  record = cjson.decode(ARGV[8])
  record["attempt"] = tonumber(record["attempt"] or "0")
  record["stage"] = "result"
  record["version"] = tonumber(record["version"] or "0")
  record["updated_at"] = tonumber(ARGV[6])
end
local event = record["event"] or {}
if tostring(event["agent_id"] or "") ~= ARGV[1] or tostring(event["owner_id"] or "") ~= ARGV[2] then
  return {-2, raw or ""}
end
local terminal = record["terminal"]
if terminal then
  if tostring(terminal["status"] or "") ~= ARGV[3]
      or tostring(terminal["code"] or "") ~= ARGV[4]
      or tostring(terminal["msg"] or "") ~= ARGV[5] then
    return {-1, cjson.encode(record)}
  end
  if record["stage"] == "terminal_settled" or tonumber(terminal["settled_at"] or "0") > 0 then
    return {3, cjson.encode(record)}
  end
  local claim_until = tonumber(terminal["claim_until"] or "0")
  if claim_until > tonumber(ARGV[6]) then
    return {2, cjson.encode(record)}
  end
  terminal["claim_token"] = ARGV[7]
  terminal["claim_until"] = tonumber(ARGV[9])
  record["terminal"] = terminal
  record["stage"] = "terminal_intent"
  record["version"] = tonumber(record["version"] or "0") + 1
  record["updated_at"] = tonumber(ARGV[6])
  local reclaimed = cjson.encode(record)
  redis.call("SET", KEYS[1], reclaimed, "EX", ARGV[10])
  return {1, reclaimed}
end
record["terminal"] = {
  status = ARGV[3],
  code = ARGV[4],
  msg = ARGV[5],
  claim_token = ARGV[7],
  claim_until = tonumber(ARGV[9]),
  created_at = tonumber(ARGV[6])
}
record["stage"] = "terminal_intent"
record["retry_token"] = nil
record["retry_claim_until"] = nil
record["retry_dispatched_at"] = nil
record["version"] = tonumber(record["version"] or "0") + 1
record["updated_at"] = tonumber(ARGV[6])
local updated = cjson.encode(record)
redis.call("SET", KEYS[1], updated, "EX", ARGV[10])
return {1, updated}
`)

var settleTerminalIntentScript = redis.NewScript(`
local raw = redis.call("GET", KEYS[1])
if not raw then
  return 0
end
local record = cjson.decode(raw)
local terminal = record["terminal"]
if not terminal then
  return 0
end
if tostring(terminal["status"] or "") ~= ARGV[1]
    or tostring(terminal["code"] or "") ~= ARGV[2]
    or tostring(terminal["msg"] or "") ~= ARGV[3] then
  return -1
end
if record["stage"] == "terminal_settled" or tonumber(terminal["settled_at"] or "0") > 0 then
  return 1
end
if tostring(terminal["claim_token"] or "") ~= ARGV[4] then
  return 0
end
terminal["settled_at"] = tonumber(ARGV[5])
terminal["claim_until"] = nil
record["terminal"] = terminal
record["stage"] = "terminal_settled"
record["version"] = tonumber(record["version"] or "0") + 1
record["updated_at"] = tonumber(ARGV[5])
redis.call("SET", KEYS[1], cjson.encode(record), "EX", ARGV[6])
redis.call("ZREM", KEYS[2], ARGV[7])
return 1
`)

var repairSettledTerminalIntentScript = redis.NewScript(`
local record = cjson.decode(ARGV[1])
record["stage"] = "terminal_settled"
record["retry_token"] = nil
record["retry_claim_until"] = nil
record["retry_dispatched_at"] = nil
record["version"] = tonumber(record["version"] or "0") + 1
record["updated_at"] = tonumber(ARGV[2])
local terminal = record["terminal"] or {}
terminal["claim_token"] = nil
terminal["claim_until"] = nil
terminal["settled_at"] = tonumber(ARGV[2])
record["terminal"] = terminal
redis.call("SET", KEYS[1], cjson.encode(record), "EX", ARGV[3])
redis.call("ZREM", KEYS[2], ARGV[4])
return 1
`)

var createDurablePendingDelegateScript = redis.NewScript(`
local raw = redis.call("GET", KEYS[1])
if raw then
  local existing = cjson.decode(raw)
  local event = existing["event"] or {}
  if tostring(event["agent_id"] or "") ~= ARGV[1]
      or tostring(event["owner_id"] or "") ~= ARGV[2] then
    return {-1, raw}
  end
  return {0, raw}
end
local record = cjson.decode(ARGV[3])
record["version"] = 1
local updated = cjson.encode(record)
redis.call("SET", KEYS[1], updated, "EX", ARGV[4])
redis.call("ZADD", KEYS[2], ARGV[5], ARGV[6])
redis.call("EXPIRE", KEYS[2], ARGV[4])
return {1, updated}
`)

func durablePendingDelegateRecordKey(eventID string) string {
	return fmt.Sprintf("im:agent_api:pending_delegate:%s", strings.TrimSpace(eventID))
}

func durablePendingDelegateIndexKey(agentID int64) string {
	return fmt.Sprintf("im:agent_api:pending_delegate:index:%d", agentID)
}

func durablePendingDelegateTTLSeconds() int64 {
	return int64(durablePendingDelegateTTL / time.Second)
}

func hasOtherDurableActiveRun(
	ctx context.Context,
	eventID string,
	sessionID string,
	ownerID int64,
	agentID int64,
) bool {
	if store.RDB == nil || ownerID <= 0 || agentID <= 0 ||
		strings.TrimSpace(sessionID) == "" {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var cursor uint64
	for {
		members, next, err := store.RDB.ZScan(
			ctx,
			durablePendingDelegateIndexKey(agentID),
			cursor,
			"*",
			durablePendingDelegateDrainBatch,
		).Result()
		if err != nil {
			return false
		}
		for index := 0; index+1 < len(members); index += 2 {
			candidateID := strings.TrimSpace(members[index])
			if candidateID == "" || candidateID == strings.TrimSpace(eventID) {
				continue
			}
			record, ok := loadDurablePendingDelegate(ctx, candidateID)
			if !ok || record == nil {
				continue
			}
			if record.Event.AgentID == agentID &&
				record.Event.OwnerID == ownerID &&
				strings.TrimSpace(record.Event.SessionID) == strings.TrimSpace(sessionID) &&
				(record.Stage == durablePendingDelegateStageAck ||
					record.Stage == durablePendingDelegateStageResult) {
				return true
			}
		}
		cursor = next
		if cursor == 0 {
			return false
		}
	}
}

func parseDurableScriptRecord(raw any) (*durablePendingDelegateRecord, error) {
	text := strings.TrimSpace(fmt.Sprint(raw))
	if text == "" {
		return nil, nil
	}
	var record durablePendingDelegateRecord
	if err := json.Unmarshal([]byte(text), &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func claimDurablePendingDelegateRetry(
	ctx context.Context,
	eventID string,
	agentID int64,
	ownerID int64,
	maxAttempts int,
	claimLease time.Duration,
) (durableRetryClaim, error) {
	var result durableRetryClaim
	if store.RDB == nil {
		return result, nil
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" || agentID <= 0 || ownerID <= 0 || maxAttempts <= 1 {
		return result, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if claimLease <= 0 {
		claimLease = 5 * time.Second
	}
	now := time.Now()
	token := uuid.NewString()
	values, err := claimDurableRetryScript.Run(
		ctx,
		store.RDB,
		[]string{durablePendingDelegateRecordKey(eventID)},
		strconv.FormatInt(agentID, 10),
		strconv.FormatInt(ownerID, 10),
		now.UnixMilli(),
		maxAttempts,
		token,
		now.Add(claimLease).UnixMilli(),
		durablePendingDelegateTTLSeconds(),
	).Slice()
	if err != nil {
		return result, err
	}
	if len(values) < 2 {
		return result, fmt.Errorf("invalid durable retry claim response")
	}
	code, err := strconv.Atoi(fmt.Sprint(values[0]))
	if err != nil {
		return result, err
	}
	record, err := parseDurableScriptRecord(values[1])
	if err != nil {
		return result, err
	}
	result.Record = record
	if code != 1 || record == nil {
		return result, nil
	}
	result.Won = true
	result.Envelope = durableRetryEnvelope{
		EventID: eventID,
		AgentID: agentID,
		OwnerID: ownerID,
		Attempt: record.Attempt,
		Token:   token,
	}
	return result, nil
}

func markDurablePendingDelegateRetryDispatched(
	ctx context.Context,
	envelope durableRetryEnvelope,
) (bool, error) {
	if store.RDB == nil {
		return false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	eventID := strings.TrimSpace(envelope.EventID)
	token := strings.TrimSpace(envelope.Token)
	if eventID == "" || envelope.AgentID <= 0 || envelope.OwnerID <= 0 ||
		envelope.Attempt <= 1 || token == "" {
		return false, nil
	}
	accepted, err := markDurableRetryDispatchedScript.Run(
		ctx,
		store.RDB,
		[]string{durablePendingDelegateRecordKey(eventID)},
		strconv.FormatInt(envelope.AgentID, 10),
		strconv.FormatInt(envelope.OwnerID, 10),
		envelope.Attempt,
		token,
		time.Now().UnixMilli(),
		durablePendingDelegateTTLSeconds(),
	).Int()
	return accepted == 1, err
}

func releaseDurablePendingDelegateRetryDispatch(
	ctx context.Context,
	envelope durableRetryEnvelope,
) {
	if store.RDB == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().UnixMilli()
	_ = releaseDurableRetryDispatchScript.Run(
		ctx,
		store.RDB,
		[]string{durablePendingDelegateRecordKey(envelope.EventID)},
		envelope.Token,
		now,
		durablePendingDelegateTTLSeconds(),
	).Err()
}

func advanceDurablePendingDelegateAck(
	ctx context.Context,
	eventID string,
	agentID int64,
	ownerID int64,
	receivedAt int64,
) (bool, *durablePendingDelegateRecord, error) {
	if store.RDB == nil {
		return false, nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().UnixMilli()
	values, err := advanceDurableAckScript.Run(
		ctx,
		store.RDB,
		[]string{durablePendingDelegateRecordKey(strings.TrimSpace(eventID))},
		strconv.FormatInt(agentID, 10),
		strconv.FormatInt(ownerID, 10),
		receivedAt,
		now,
		durablePendingDelegateTTLSeconds(),
	).Slice()
	if err != nil {
		return false, nil, err
	}
	if len(values) < 2 {
		return false, nil, fmt.Errorf("invalid durable ack advance response")
	}
	code, err := strconv.Atoi(fmt.Sprint(values[0]))
	if err != nil {
		return false, nil, err
	}
	record, err := parseDurableScriptRecord(values[1])
	if err != nil {
		return false, nil, err
	}
	return code == 1, record, nil
}

func claimDurableTerminalIntent(
	ctx context.Context,
	eventID string,
	agentID int64,
	ownerID int64,
	payload EventResultPayload,
	fallback *durablePendingDelegateRecord,
) (terminalIntentClaim, error) {
	result := terminalIntentClaim{Disposition: terminalIntentMissing}
	if store.RDB == nil {
		return result, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" || agentID <= 0 || ownerID <= 0 {
		return result, nil
	}
	fallbackRaw := ""
	if fallback != nil {
		cp := *fallback
		cp.Event.EventID = eventID
		cp.Event.AgentID = agentID
		cp.Event.OwnerID = ownerID
		if cp.Stage == "" {
			cp.Stage = durablePendingDelegateStageResult
		}
		data, err := json.Marshal(cp)
		if err != nil {
			return result, err
		}
		fallbackRaw = string(data)
	}
	now := time.Now()
	token := uuid.NewString()
	values, err := claimTerminalIntentScript.Run(
		ctx,
		store.RDB,
		[]string{durablePendingDelegateRecordKey(eventID)},
		strconv.FormatInt(agentID, 10),
		strconv.FormatInt(ownerID, 10),
		strings.TrimSpace(payload.Status),
		strings.TrimSpace(payload.Code),
		strings.TrimSpace(payload.Msg),
		now.UnixMilli(),
		token,
		fallbackRaw,
		now.Add(terminalIntentClaimLease).UnixMilli(),
		durablePendingDelegateTTLSeconds(),
	).Slice()
	if err != nil {
		return result, err
	}
	if len(values) < 2 {
		return result, fmt.Errorf("invalid terminal intent claim response")
	}
	code, err := strconv.Atoi(fmt.Sprint(values[0]))
	if err != nil {
		return result, err
	}
	record, err := parseDurableScriptRecord(values[1])
	if err != nil {
		return result, err
	}
	result.Record = record
	switch code {
	case 1:
		result.Disposition = terminalIntentClaimed
		result.Token = token
	case 2:
		result.Disposition = terminalIntentPending
	case 3:
		result.Disposition = terminalIntentSettled
	case -1:
		result.Disposition = terminalIntentConflict
	case -2:
		result.Disposition = terminalIntentUnauthorized
	default:
		result.Disposition = terminalIntentMissing
	}
	return result, nil
}

func settleDurableTerminalIntent(
	ctx context.Context,
	record *durablePendingDelegateRecord,
	token string,
) (bool, error) {
	if store.RDB == nil || record == nil || record.Terminal == nil {
		return false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	eventID := strings.TrimSpace(record.Event.EventID)
	if eventID == "" || strings.TrimSpace(token) == "" {
		return false, nil
	}
	settled, err := settleTerminalIntentScript.Run(
		ctx,
		store.RDB,
		[]string{
			durablePendingDelegateRecordKey(eventID),
			durablePendingDelegateIndexKey(record.Event.AgentID),
		},
		strings.TrimSpace(record.Terminal.Status),
		strings.TrimSpace(record.Terminal.Code),
		strings.TrimSpace(record.Terminal.Msg),
		strings.TrimSpace(token),
		time.Now().UnixMilli(),
		durablePendingDelegateTTLSeconds(),
		eventID,
	).Int()
	return settled == 1, err
}

// repairDurableTerminalIntentFromLedger makes the long-lived DB verdict
// authoritative over a missing/stale/opposite Redis coordination record.
func repairDurableTerminalIntentFromLedger(
	ctx context.Context,
	ledger *model.AgentEventTerminalLedger,
) (*durablePendingDelegateRecord, error) {
	if store.RDB == nil || ledger == nil {
		return nil, nil
	}
	record := durableRecordFromTerminalLedger(ledger)
	if record == nil {
		record = &durablePendingDelegateRecord{
			Event: DelegateEventPayload{
				EventID:     ledger.EventID,
				AgentID:     ledger.AgentID,
				OwnerID:     ledger.OwnerID,
				SessionID:   ledger.SessionID,
				SessionType: ledger.SessionType,
				SenderID:    ledger.SenderID,
				MsgID:       ledger.TriggerMsgID,
				MirrorMode:  ledger.MirrorMode,
			},
			StartedAt:          unixMillisPtr(ledger.StartedAt),
			ReceivedAt:         ledger.ReceivedAt,
			CallTurn:           ledger.CallTurn,
			DispatchGeneration: ledger.DispatchGeneration,
		}
	}
	record.Stage = durablePendingDelegateStageSettled
	record.Terminal = &durableTerminalIntent{
		Status: ledger.Status,
		Code:   ledger.Code,
		Msg:    ledger.Msg,
	}
	if record.Attempt <= 0 {
		record.Attempt = 1
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().UnixMilli()
	if _, err := repairSettledTerminalIntentScript.Run(
		ctx,
		store.RDB,
		[]string{
			durablePendingDelegateRecordKey(ledger.EventID),
			durablePendingDelegateIndexKey(ledger.AgentID),
		},
		string(raw),
		now,
		durablePendingDelegateTTLSeconds(),
		ledger.EventID,
	).Result(); err != nil {
		return nil, err
	}
	record.UpdatedAt = now
	record.Terminal.SettledAt = now
	return record, nil
}

func unixMillisPtr(value *time.Time) int64 {
	if value == nil {
		return 0
	}
	return value.UnixMilli()
}

func loadDurablePendingDelegate(ctx context.Context, eventID string) (*durablePendingDelegateRecord, bool) {
	if store.RDB == nil {
		return nil, false
	}
	normalizedEventID := strings.TrimSpace(eventID)
	if normalizedEventID == "" {
		return nil, false
	}
	if ctx == nil {
		ctx = context.Background()
	}

	raw, err := store.RDB.Get(ctx, durablePendingDelegateRecordKey(normalizedEventID)).Result()
	if err == redis.Nil {
		return nil, false
	}
	if err != nil {
		logger.L.Warnf("load durable pending delegate failed event=%s err=%v", normalizedEventID, err)
		return nil, false
	}

	var record durablePendingDelegateRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		logger.L.Warnf("decode durable pending delegate failed event=%s err=%v", normalizedEventID, err)
		return nil, false
	}
	if strings.TrimSpace(record.Event.EventID) == "" || record.Event.AgentID <= 0 {
		return nil, false
	}
	return &record, true
}

func persistDurablePendingDelegate(ctx context.Context, record durablePendingDelegateRecord) bool {
	stored, _, err := createDurablePendingDelegate(ctx, record)
	return err == nil && stored != nil
}

// createDurablePendingDelegate atomically creates first-delivery state or
// returns the existing same-owner record unchanged. Duplicate upstream
// dispatches must never reset a received/terminal record back to ACK stage.
func createDurablePendingDelegate(
	ctx context.Context,
	record durablePendingDelegateRecord,
) (*durablePendingDelegateRecord, bool, error) {
	if store.RDB == nil {
		return nil, false, nil
	}
	eventID := strings.TrimSpace(record.Event.EventID)
	if eventID == "" || record.Event.AgentID <= 0 {
		return nil, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if record.Attempt <= 0 {
		record.Attempt = 1
	}
	switch strings.TrimSpace(record.Stage) {
	case durablePendingDelegateStageAck,
		durablePendingDelegateStageResult,
		durablePendingDelegateStageIntent,
		durablePendingDelegateStageSettled:
	default:
		record.Stage = durablePendingDelegateStageAck
	}
	if record.UpdatedAt <= 0 {
		record.UpdatedAt = time.Now().UnixMilli()
	}

	raw, err := json.Marshal(record)
	if err != nil {
		logger.L.Warnf("marshal durable pending delegate failed event=%s err=%v", eventID, err)
		return nil, false, err
	}

	indexKey := durablePendingDelegateIndexKey(record.Event.AgentID)
	values, err := createDurablePendingDelegateScript.Run(
		ctx,
		store.RDB,
		[]string{
			durablePendingDelegateRecordKey(eventID),
			indexKey,
		},
		strconv.FormatInt(record.Event.AgentID, 10),
		strconv.FormatInt(record.Event.OwnerID, 10),
		string(raw),
		durablePendingDelegateTTLSeconds(),
		record.Event.CreatedAt,
		eventID,
	).Slice()
	if err != nil {
		logger.L.Warnf("persist durable pending delegate failed event=%s err=%v", eventID, err)
		return nil, false, err
	}
	if len(values) < 2 {
		return nil, false, fmt.Errorf("invalid durable pending create response")
	}
	code, err := strconv.Atoi(fmt.Sprint(values[0]))
	if err != nil {
		return nil, false, err
	}
	stored, err := parseDurableScriptRecord(values[1])
	if err != nil {
		return nil, false, err
	}
	if code == -1 {
		return stored, false, fmt.Errorf("event_id owned by another agent connection")
	}
	return stored, code == 1, nil
}

func deleteDurablePendingDelegate(ctx context.Context, eventID string, agentID int64) {
	if store.RDB == nil {
		return
	}
	normalizedEventID := strings.TrimSpace(eventID)
	if normalizedEventID == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	resolvedAgentID := agentID
	if resolvedAgentID <= 0 {
		record, ok := loadDurablePendingDelegate(ctx, normalizedEventID)
		if ok {
			resolvedAgentID = record.Event.AgentID
		}
	}

	pipe := store.RDB.TxPipeline()
	pipe.Del(ctx, durablePendingDelegateRecordKey(normalizedEventID))
	if resolvedAgentID > 0 {
		pipe.ZRem(ctx, durablePendingDelegateIndexKey(resolvedAgentID), normalizedEventID)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		logger.L.Warnf("delete durable pending delegate failed event=%s agent=%d err=%v", normalizedEventID, resolvedAgentID, err)
	}
}

func deleteDurablePendingDelegateIfUnchanged(
	ctx context.Context,
	eventID string,
	agentID int64,
	ownerID int64,
	version int64,
	stage string,
	attempt int,
	dispatchGeneration int64,
) bool {
	if store.RDB == nil || strings.TrimSpace(eventID) == "" ||
		agentID <= 0 || ownerID <= 0 || version < 0 ||
		strings.TrimSpace(stage) == "" || attempt <= 0 {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	deleted, err := deleteDurableIfUnchangedScript.Run(
		ctx,
		store.RDB,
		[]string{durablePendingDelegateRecordKey(eventID)},
		strconv.FormatInt(agentID, 10),
		strconv.FormatInt(ownerID, 10),
		version,
		strings.TrimSpace(stage),
		attempt,
		dispatchGeneration,
	).Int()
	if err != nil {
		logger.L.Warnf(
			"delete durable pending delegate CAS failed event=%s agent=%d owner=%d version=%d stage=%s attempt=%d generation=%d err=%v",
			eventID, agentID, ownerID, version, stage, attempt, dispatchGeneration, err,
		)
		return false
	}
	if deleted != 1 {
		return false
	}
	_ = store.RDB.ZRem(ctx, durablePendingDelegateIndexKey(agentID), strings.TrimSpace(eventID)).Err()
	return true
}

func (m *Manager) drainDurablePendingDelegateAcks(conn *agentConn, maxCount int) {
	if conn == nil || conn.agentID <= 0 || store.RDB == nil {
		return
	}
	if maxCount <= 0 {
		maxCount = durablePendingDelegateDrainBatch
	}

	ctx := context.Background()
	indexKey := durablePendingDelegateIndexKey(conn.agentID)
	var cursor uint64
	handled := 0
	seen := make(map[string]struct{})
	for {
		items, next, err := store.RDB.ZScan(
			ctx,
			indexKey,
			cursor,
			"*",
			durablePendingDelegateDrainBatch,
		).Result()
		if err != nil {
			logger.L.Warnf("scan durable pending delegate index failed agent=%d err=%v", conn.agentID, err)
			return
		}
		for i := 0; i+1 < len(items) && handled < maxCount; i += 2 {
			eventID := strings.TrimSpace(items[i])
			if eventID == "" {
				continue
			}
			if _, duplicate := seen[eventID]; duplicate {
				continue
			}
			seen[eventID] = struct{}{}
			record, ok := loadDurablePendingDelegate(ctx, eventID)
			if !ok {
				deleteDurablePendingDelegate(ctx, eventID, conn.agentID)
				continue
			}
			if record.Event.AgentID != conn.agentID {
				deleteDurablePendingDelegate(ctx, eventID, record.Event.AgentID)
				continue
			}
			// agent 共享：同一 agent 的索引下混有不同 owner 的记录，只补发本连接 owner 的，
			// 其它 owner 的留给对应连接（不删除）。
			if record.Event.OwnerID != conn.ownerID {
				continue
			}
			if record.Stage != durablePendingDelegateStageAck {
				continue
			}
			// maxCount limits ACK records actionable by this owner, not the raw
			// index prefix. Other owners and result-stage records never consume it.
			handled++
			m.drainDurablePendingDelegateAckRecord(ctx, conn, eventID, record)
		}
		if handled >= maxCount || next == 0 {
			return
		}
		cursor = next
	}
}

func (m *Manager) drainDurablePendingDelegateAckRecord(
	ctx context.Context,
	conn *agentConn,
	eventID string,
	record *durablePendingDelegateRecord,
) {
	if m == nil || conn == nil || record == nil {
		return
	}
	m.acksMu.Lock()
	_, tracked := m.pending[eventID]
	m.acksMu.Unlock()
	// A live manager already owns this ACK timer. Re-registering its
	// connection must not create a second retry source; only a previously
	// claimed-but-not-dispatched token may be resumed here. A restarted
	// manager has no local entry and recovers below.
	if tracked && strings.TrimSpace(record.RetryToken) == "" {
		return
	}
	if !tracked {
		if recovered := m.recoverPendingFromDurable(eventID, conn.agentID); recovered == nil {
			return
		}
	}
	nowMs := time.Now().UnixMilli()
	if strings.TrimSpace(record.RetryToken) != "" &&
		record.RetryClaimUntil > nowMs &&
		record.RetryDispatchedAt <= 0 {
		envelope := durableRetryEnvelope{
			EventID: eventID,
			AgentID: record.Event.AgentID,
			OwnerID: record.Event.OwnerID,
			Attempt: record.Attempt,
			Token:   record.RetryToken,
		}
		if !m.dispatchClaimedDelegateRetry(conn, envelope) {
			return
		}
		m.updatePendingAttemptFromDurable(eventID, record.Attempt)
		return
	}
	if record.RetryClaimUntil > nowMs && record.RetryDispatchedAt > 0 {
		return
	}
	claim, claimErr := claimDurablePendingDelegateRetry(
		ctx,
		eventID,
		record.Event.AgentID,
		record.Event.OwnerID,
		agentAPIDeliveryMaxAttempts,
		m.eventAckWait,
	)
	if claimErr != nil {
		logger.L.Warnf("claim reconnect retry failed event=%s err=%v", eventID, claimErr)
		return
	}
	if !claim.Won {
		if claim.Record != nil {
			m.acksMu.Lock()
			entry := m.pending[eventID]
			m.acksMu.Unlock()
			m.syncPendingEventFromDurable(eventID, entry, claim.Record)
		}
		return
	}
	m.updatePendingAttemptFromDurable(eventID, claim.Envelope.Attempt)
	if !m.dispatchClaimedDelegateRetry(conn, claim.Envelope) {
		return
	}
}

// durableRecordToSnapshot 把 durable 记录转为只读的 ActiveRunSnapshot。
// 仅用于跨节点的状态展示与停止路由：run 的权威仍在 agent 所在节点，
// 这里不在本节点重建 in-memory run，避免产生不会被 agent 更新的孤儿 run。
func durableRecordToSnapshot(record *durablePendingDelegateRecord) *ActiveRunSnapshot {
	return &ActiveRunSnapshot{
		EventID:             strings.TrimSpace(record.Event.EventID),
		TerminalCommitToken: strings.TrimSpace(record.Event.TerminalCommitToken),
		SessionID:           strings.TrimSpace(record.Event.SessionID),
		ThreadID:            strings.TrimSpace(record.Event.ThreadID),
		Scope:               resolveDelegateEventScope(record.Event),
		OwnerID:             record.Event.OwnerID,
		AgentID:             record.Event.AgentID,
		TriggerMsgID:        record.Event.MsgID,
		TriggerQuoted:       record.Event.QuotedMessageID,
		State:               protocol.AgentOutputStateStreaming,
		CanStop:             true,
		ClientStream:        true,
		UpdatedAt:           time.Now().UnixMilli(),
	}
}

// LookupDurableRunBySession 在本节点无 in-memory run 时，从 Redis durable 只读出
// 该 agent 在指定 session 的未完成 run 快照。用于前端连接落在非 agent 节点
// （或 ws 重启重连换节点）时，仍能解析出真实 run（含 event_id）以驱动停止。
func (m *Manager) LookupDurableRunBySession(ownerID int64, sessionID string, agentID int64) *ActiveRunSnapshot {
	if store.RDB == nil || ownerID <= 0 || agentID <= 0 {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	ctx := context.Background()
	eventIDs, err := store.RDB.ZRevRange(ctx, durablePendingDelegateIndexKey(agentID), 0, int64(durablePendingDelegateDrainBatch-1)).Result()
	if err != nil {
		return nil
	}
	// 一个 session 可能有多个未完成 event（正在跑的 + 排队的）。优先返回已进入
	// streaming(stage=result)的那个——即用户看到的"正在生成"的 run；没有则退回到
	// 任一未完成 event（如刚 ack 尚未 streaming），保证仍可停止。
	var fallback *ActiveRunSnapshot
	for _, eventID := range eventIDs {
		record, ok := loadDurablePendingDelegate(ctx, eventID)
		if !ok || record.Event.OwnerID != ownerID || strings.TrimSpace(record.Event.SessionID) != sessionID {
			continue
		}
		if record.Stage != durablePendingDelegateStageAck &&
			record.Stage != durablePendingDelegateStageResult {
			continue
		}
		if record.Stage == durablePendingDelegateStageResult {
			return durableRecordToSnapshot(record)
		}
		if fallback == nil {
			fallback = durableRecordToSnapshot(record)
		}
	}
	return fallback
}

// lookupDurableRunByEvent 按 event_id 从 Redis durable 只读出 run 快照，
// 并校验 owner/session 一致。用于停止入口在本节点无 in-memory run 时的跨节点解析。
func (m *Manager) lookupDurableRunByEvent(eventID string, ownerID int64, sessionID string) *ActiveRunSnapshot {
	record, ok := loadDurablePendingDelegate(context.Background(), strings.TrimSpace(eventID))
	if !ok {
		return nil
	}
	if record.Stage != durablePendingDelegateStageAck &&
		record.Stage != durablePendingDelegateStageResult {
		return nil
	}
	if ownerID > 0 && record.Event.OwnerID != ownerID {
		return nil
	}
	if s := strings.TrimSpace(sessionID); s != "" && strings.TrimSpace(record.Event.SessionID) != s {
		return nil
	}
	return durableRecordToSnapshot(record)
}
