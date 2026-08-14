package customercoach

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/redis/go-redis/v9"
)

// Deterministic pre-dispatch gate. The coach trigger fires on every WS auth,
// so "should we stay silent" must be decided here — not by the model.
//
// Two rules, evaluated atomically against the state recorded at the last
// granted dispatch:
//   - cooldown: at most one dispatch grant per user per coachDispatchCooldown;
//   - dedup: skip when the missing onboarding steps are unchanged since the
//     last grant (nothing new to say). A changed step set means real progress
//     and always re-allows a dispatch.
//
// Check-and-record is a single Lua script so concurrent WS connections of the
// same user cannot both pass the gate.
const (
	coachDispatchCooldown  = 24 * time.Hour
	coachDispatchStateTTL  = 7 * 24 * time.Hour
	coachDispatchKeyPrefix = "customercoach:dispatch:"
)

// coachDispatchGrantScript atomically evaluates the gate and, when granting,
// records the new state. Returns 1 to grant, 0 to skip. Corrupt or missing
// state grants (and rewrites state); the gate is best-effort anti-spam, not a
// security control.
var coachDispatchGrantScript = redis.NewScript(`
local raw = redis.call("GET", KEYS[1])
local now = tonumber(ARGV[1])
local missing = ARGV[2]
local cooldown = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])
if raw then
  local ok, state = pcall(cjson.decode, raw)
  if ok and type(state) == "table" and state.missing == missing and (now - (tonumber(state.last_at) or 0)) < cooldown then
    return 0
  end
end
redis.call("SET", KEYS[1], cjson.encode({last_at = now, missing = missing}), "EX", ttl)
return 1
`)

func coachDispatchKey(userID int64) string {
	return coachDispatchKeyPrefix + strconv.FormatInt(userID, 10)
}

// coachDispatchState is the JSON shape written by coachDispatchGrantScript.
// Keep the field names in sync with the script.
type coachDispatchState struct {
	LastAt  int64  `json:"last_at"`
	Missing string `json:"missing"`
}

// acquireCoachDispatch atomically evaluates the cooldown and dedup gates and,
// when they pass, records the grant. The slot is consumed at gate time rather
// than after the dispatch is accepted: a subsequent dispatch failure (agent
// offline) simply means this round is skipped, which is acceptable for a
// best-effort anti-spam gate. Fail-open on Redis unavailability.
func acquireCoachDispatch(ctx context.Context, userID int64, snapshot Snapshot) bool {
	if store.RDB == nil {
		logger.L.Warnf("customer coach dispatch gate: redis unavailable, fail-open user=%d", userID)
		return true
	}
	missing := strings.Join(missingCoachSteps(snapshot), ",")
	result, err := coachDispatchGrantScript.Run(ctx, store.RDB,
		[]string{coachDispatchKey(userID)},
		time.Now().Unix(),
		missing,
		int64(coachDispatchCooldown/time.Second),
		int64(coachDispatchStateTTL/time.Second),
	).Int64()
	if err != nil {
		logger.L.Warnf("customer coach dispatch gate: grant script failed user=%d err=%v (fail-open)", userID, err)
		return true
	}
	return result == 1
}
