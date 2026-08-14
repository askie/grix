package claudeaccess

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/store"
	"github.com/redis/go-redis/v9"
)

const (
	DefaultPolicy        = "allowlist"
	PolicyOpen           = "open"
	PolicyDisabled       = "disabled"
	PolicyAllowlist      = "allowlist"
	StatusInfo           = "info"
	StatusSuccess        = "success"
	StatusWarning        = "warning"
	StatusError          = "error"
	StatusCategoryAccess = "access"

	// pairingTTL 是访问申请的待批窗口。过期后访客再 @ 会重新生成申请、主人重新收卡。
	pairingTTL = 10 * time.Minute
	// deniedTTL 是拒绝的粘性窗口：被拒后再 @ 不再生成新申请、不再打扰主人，
	// 直到窗口过期或主人主动 allow。防止「拒绝→再@→新卡」无限刷屏。
	deniedTTL          = 24 * time.Hour
	stateSchemaVersion = 1
	pairingCommandHint = "/grix access pair <code>"
)

var (
	errRedisUnavailable = errors.New("claude access redis unavailable")
	errPairingNotFound  = errors.New("pairing code not found or expired")
)

type allowEntry struct {
	SenderID string `json:"sender_id"`
	PairedAt int64  `json:"paired_at"`
}

type pendingPair struct {
	SenderID  string `json:"sender_id"`
	SessionID string `json:"session_id"`
	ExpiresAt int64  `json:"expires_at"`
}

type accessState struct {
	SchemaVersion int                    `json:"schema_version"`
	Policy        string                 `json:"policy"`
	Allowlist     map[string]allowEntry  `json:"allowlist"`
	PendingPairs  map[string]pendingPair `json:"pending_pairs"`
	// DeniedSenders 记录被主人拒绝的发送者及其粘性截止时间（毫秒）。
	DeniedSenders map[string]int64 `json:"denied_senders,omitempty"`
}

type AllowlistEntry struct {
	SenderID string `json:"sender_id"`
	PairedAt int64  `json:"paired_at"`
}

type PendingPairEntry struct {
	Code      string `json:"code"`
	SenderID  string `json:"sender_id"`
	SessionID string `json:"session_id"`
	ExpiresAt int64  `json:"expires_at"`
}

type Status struct {
	Policy           string             `json:"policy"`
	AllowlistCount   int                `json:"allowlist_count"`
	PendingPairCount int                `json:"pending_pair_count"`
	Allowlist        []AllowlistEntry   `json:"allowlist"`
	PendingPairs     []PendingPairEntry `json:"pending_pairs"`
}

type EvaluateInboundResult struct {
	Dispatch    bool   `json:"dispatch"`
	Policy      string `json:"policy"`
	Reason      string `json:"reason,omitempty"`
	PairingCode string `json:"pairing_code,omitempty"`
}

type PairingResult struct {
	Code              string `json:"code,omitempty"`
	SenderID          string `json:"sender_id"`
	SessionID         string `json:"session_id"`
	Policy            string `json:"policy"`
	PairingNoticeSent bool   `json:"pairing_notice_sent,omitempty"`
}

type SenderResult struct {
	SenderID string `json:"sender_id"`
	Policy   string `json:"policy"`
	Removed  bool   `json:"removed,omitempty"`
	// AllowlistCount 是操作后的名单人数，供调用方（agent）向主人反馈现状。
	AllowlistCount int `json:"allowlist_count"`
}

func defaultState() accessState {
	return accessState{
		SchemaVersion: stateSchemaVersion,
		Policy:        DefaultPolicy,
		Allowlist:     map[string]allowEntry{},
		PendingPairs:  map[string]pendingPair{},
		DeniedSenders: map[string]int64{},
	}
}

func normalizeString(value string) string {
	return strings.TrimSpace(value)
}

func normalizePolicy(value string) string {
	switch strings.TrimSpace(value) {
	case PolicyOpen:
		return PolicyOpen
	case PolicyDisabled:
		return PolicyDisabled
	case PolicyAllowlist:
		return PolicyAllowlist
	default:
		return DefaultPolicy
	}
}

func stateKey(agentID int64) string {
	return fmt.Sprintf("im:claude:access:%d", agentID)
}

func parseState(raw string) accessState {
	state := defaultState()
	if strings.TrimSpace(raw) == "" {
		return state
	}
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return defaultState()
	}
	if state.SchemaVersion != stateSchemaVersion {
		return defaultState()
	}
	state.Policy = normalizePolicy(state.Policy)
	if state.Allowlist == nil {
		state.Allowlist = map[string]allowEntry{}
	}
	if state.PendingPairs == nil {
		state.PendingPairs = map[string]pendingPair{}
	}
	if state.DeniedSenders == nil {
		state.DeniedSenders = map[string]int64{}
	}
	pruneExpiredPairs(&state, time.Now().UnixMilli())
	return state
}

func encodeState(state accessState) (string, error) {
	state.Policy = normalizePolicy(state.Policy)
	if state.Allowlist == nil {
		state.Allowlist = map[string]allowEntry{}
	}
	if state.PendingPairs == nil {
		state.PendingPairs = map[string]pendingPair{}
	}
	if state.DeniedSenders == nil {
		state.DeniedSenders = map[string]int64{}
	}
	pruneExpiredPairs(&state, time.Now().UnixMilli())
	data, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func pruneExpiredPairs(state *accessState, nowMS int64) {
	if state == nil {
		return
	}
	for code, entry := range state.PendingPairs {
		if entry.ExpiresAt <= 0 || entry.ExpiresAt <= nowMS {
			delete(state.PendingPairs, code)
		}
	}
	for senderID, expiresAt := range state.DeniedSenders {
		if expiresAt <= nowMS {
			delete(state.DeniedSenders, senderID)
		}
	}
}

func evaluateInboundState(state *accessState, senderID, sessionID string, sessionType int16, nowMS int64) (EvaluateInboundResult, error) {
	if state == nil {
		return EvaluateInboundResult{}, errors.New("state required")
	}
	senderID = normalizeString(senderID)
	sessionID = normalizeString(sessionID)
	if senderID == "" || sessionID == "" {
		return EvaluateInboundResult{}, errors.New("sender_id and session_id are required")
	}

	pruneExpiredPairs(state, nowMS)
	policy := normalizePolicy(state.Policy)
	groupSession := sessionType == 2
	_, senderAllowlisted := state.Allowlist[senderID]

	if policy == PolicyDisabled {
		return EvaluateInboundResult{
			Dispatch: false,
			Policy:   policy,
			Reason:   "policy_disabled",
		}, nil
	}

	// 私聊硬规则：私聊是主人专线，除主人/被共享者（上层豁免）外一律不放行——
	// 访问名单授予的只是群聊使用权，policy=open 也只对群聊生效。
	// 陌生人不在私聊发配对码；上层据此回一句“请到群里找我”。
	if !groupSession {
		return EvaluateInboundResult{
			Dispatch: false,
			Policy:   policy,
			Reason:   "private_owner_only",
		}, nil
	}

	// open：主人显式放开群聊。
	if policy == PolicyOpen {
		return EvaluateInboundResult{
			Dispatch: true,
			Policy:   policy,
		}, nil
	}

	if senderAllowlisted {
		return EvaluateInboundResult{
			Dispatch: true,
			Policy:   policy,
		}, nil
	}

	// 拒绝粘性：主人拒过的人在窗口内不再生成新申请、不再打扰主人。
	if expiresAt, denied := state.DeniedSenders[senderID]; denied && expiresAt > nowMS {
		return EvaluateInboundResult{
			Dispatch: false,
			Policy:   policy,
			Reason:   "pairing_denied",
		}, nil
	}

	// 群聊 + 访客不在名单：走主人审批。已有未过期的待批申请则静默（防刷屏），
	// 仅首次生成新申请码时通知主人。
	for _, entry := range state.PendingPairs {
		if entry.SenderID == senderID && entry.SessionID == sessionID && entry.ExpiresAt > nowMS {
			return EvaluateInboundResult{
				Dispatch: false,
				Policy:   policy,
				Reason:   "pairing_pending",
			}, nil
		}
	}

	code, err := generatePairingCode(state.PendingPairs)
	if err != nil {
		return EvaluateInboundResult{}, err
	}
	state.PendingPairs[code] = pendingPair{
		SenderID:  senderID,
		SessionID: sessionID,
		ExpiresAt: nowMS + pairingTTL.Milliseconds(),
	}
	return EvaluateInboundResult{
		Dispatch:    false,
		Policy:      policy,
		Reason:      "pairing_required",
		PairingCode: code,
	}, nil
}

func generatePairingCode(existing map[string]pendingPair) (string, error) {
	alphabet := "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	if existing == nil {
		existing = map[string]pendingPair{}
	}
	buffer := make([]byte, 6)
	for attempts := 0; attempts < 16; attempts++ {
		if _, err := rand.Read(buffer); err != nil {
			return "", err
		}
		var builder strings.Builder
		builder.Grow(len(buffer))
		for _, item := range buffer {
			builder.WriteByte(alphabet[int(item)%len(alphabet)])
		}
		code := builder.String()
		if _, ok := existing[code]; !ok {
			return code, nil
		}
	}
	return "", errors.New("failed to allocate unique pairing code")
}

func updateState(ctx context.Context, agentID int64, mutator func(*accessState) error) error {
	if agentID <= 0 {
		return errors.New("agent_id required")
	}
	if store.RDB == nil {
		return errRedisUnavailable
	}
	key := stateKey(agentID)
	for attempts := 0; attempts < 8; attempts++ {
		err := store.RDB.Watch(ctx, func(tx *redis.Tx) error {
			raw, err := tx.Get(ctx, key).Result()
			if err != nil && !errors.Is(err, redis.Nil) {
				return err
			}
			state := parseState(raw)
			if err := mutator(&state); err != nil {
				return err
			}
			encoded, err := encodeState(state)
			if err != nil {
				return err
			}
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, key, encoded, 365*24*time.Hour)
				return nil
			})
			return err
		}, key)
		if errors.Is(err, redis.TxFailedErr) {
			continue
		}
		return err
	}
	return errors.New("claude access update retry exhausted")
}

func GetStatus(ctx context.Context, agentID int64) (Status, error) {
	if agentID <= 0 {
		return Status{}, errors.New("agent_id required")
	}
	if store.RDB == nil {
		return Status{}, errRedisUnavailable
	}
	raw, err := store.RDB.Get(ctx, stateKey(agentID)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return Status{}, err
	}
	state := parseState(raw)
	return buildStatus(state), nil
}

func buildStatus(state accessState) Status {
	allowlist := make([]AllowlistEntry, 0, len(state.Allowlist))
	for _, entry := range state.Allowlist {
		if normalizeString(entry.SenderID) == "" {
			continue
		}
		allowlist = append(allowlist, AllowlistEntry{
			SenderID: entry.SenderID,
			PairedAt: entry.PairedAt,
		})
	}
	sort.SliceStable(allowlist, func(i, j int) bool {
		return allowlist[i].SenderID < allowlist[j].SenderID
	})

	pairs := make([]PendingPairEntry, 0, len(state.PendingPairs))
	for code, entry := range state.PendingPairs {
		if normalizeString(code) == "" || normalizeString(entry.SenderID) == "" || normalizeString(entry.SessionID) == "" {
			continue
		}
		pairs = append(pairs, PendingPairEntry{
			Code:      code,
			SenderID:  entry.SenderID,
			SessionID: entry.SessionID,
			ExpiresAt: entry.ExpiresAt,
		})
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		return pairs[i].Code < pairs[j].Code
	})

	return Status{
		Policy:           normalizePolicy(state.Policy),
		AllowlistCount:   len(allowlist),
		PendingPairCount: len(pairs),
		Allowlist:        allowlist,
		PendingPairs:     pairs,
	}
}

func EvaluateInbound(ctx context.Context, agentID int64, senderID, sessionID string, sessionType int16) (EvaluateInboundResult, error) {
	var result EvaluateInboundResult
	err := updateState(ctx, agentID, func(state *accessState) error {
		nowMS := time.Now().UnixMilli()
		evaluated, err := evaluateInboundState(state, senderID, sessionID, sessionType, nowMS)
		if err != nil {
			return err
		}
		result = evaluated
		return nil
	})
	return result, err
}

func ApprovePairing(ctx context.Context, agentID int64, code string) (PairingResult, error) {
	var result PairingResult
	err := updateState(ctx, agentID, func(state *accessState) error {
		nowMS := time.Now().UnixMilli()
		pruneExpiredPairs(state, nowMS)
		normalizedCode := strings.ToUpper(normalizeString(code))
		pair, ok := state.PendingPairs[normalizedCode]
		if !ok {
			return errPairingNotFound
		}
		state.Allowlist[pair.SenderID] = allowEntry{
			SenderID: pair.SenderID,
			PairedAt: nowMS,
		}
		delete(state.PendingPairs, normalizedCode)
		delete(state.DeniedSenders, pair.SenderID)
		result = PairingResult{
			Code:      normalizedCode,
			SenderID:  pair.SenderID,
			SessionID: pair.SessionID,
			Policy:    normalizePolicy(state.Policy),
		}
		return nil
	})
	return result, err
}

// CancelPending 撤销一条待批申请但不记拒绝粘性：用于「审批卡没送到主人手上」的回滚
// （访客下次 @ 应能重新发起）与被共享者幽灵待批的清理，与主人主动拒绝（DenyPairing）区分。
func CancelPending(ctx context.Context, agentID int64, code string) (PairingResult, error) {
	var result PairingResult
	err := updateState(ctx, agentID, func(state *accessState) error {
		nowMS := time.Now().UnixMilli()
		pruneExpiredPairs(state, nowMS)
		normalizedCode := strings.ToUpper(normalizeString(code))
		pair, ok := state.PendingPairs[normalizedCode]
		if !ok {
			return errPairingNotFound
		}
		delete(state.PendingPairs, normalizedCode)
		result = PairingResult{
			Code:      normalizedCode,
			SenderID:  pair.SenderID,
			SessionID: pair.SessionID,
			Policy:    normalizePolicy(state.Policy),
		}
		return nil
	})
	return result, err
}

func DenyPairing(ctx context.Context, agentID int64, code string) (PairingResult, error) {
	var result PairingResult
	err := updateState(ctx, agentID, func(state *accessState) error {
		nowMS := time.Now().UnixMilli()
		pruneExpiredPairs(state, nowMS)
		normalizedCode := strings.ToUpper(normalizeString(code))
		pair, ok := state.PendingPairs[normalizedCode]
		if !ok {
			return errPairingNotFound
		}
		delete(state.PendingPairs, normalizedCode)
		// 拒绝带粘性：窗口内该发送者再 @ 不再生成新申请、不再打扰主人。
		state.DeniedSenders[pair.SenderID] = nowMS + deniedTTL.Milliseconds()
		result = PairingResult{
			Code:      normalizedCode,
			SenderID:  pair.SenderID,
			SessionID: pair.SessionID,
			Policy:    normalizePolicy(state.Policy),
		}
		return nil
	})
	return result, err
}

func AllowSender(ctx context.Context, agentID int64, senderID string) (SenderResult, error) {
	var result SenderResult
	err := updateState(ctx, agentID, func(state *accessState) error {
		normalizedSenderID := normalizeString(senderID)
		if normalizedSenderID == "" {
			return errors.New("sender_id required")
		}
		state.Allowlist[normalizedSenderID] = allowEntry{
			SenderID: normalizedSenderID,
			PairedAt: time.Now().UnixMilli(),
		}
		delete(state.DeniedSenders, normalizedSenderID)
		result = SenderResult{
			SenderID:       normalizedSenderID,
			Policy:         normalizePolicy(state.Policy),
			AllowlistCount: len(state.Allowlist),
		}
		return nil
	})
	return result, err
}

func RemoveSender(ctx context.Context, agentID int64, senderID string) (SenderResult, error) {
	var result SenderResult
	err := updateState(ctx, agentID, func(state *accessState) error {
		normalizedSenderID := normalizeString(senderID)
		if normalizedSenderID == "" {
			return errors.New("sender_id required")
		}
		delete(state.Allowlist, normalizedSenderID)
		result = SenderResult{
			SenderID:       normalizedSenderID,
			Policy:         normalizePolicy(state.Policy),
			Removed:        true,
			AllowlistCount: len(state.Allowlist),
		}
		return nil
	})
	return result, err
}

func SetPolicy(ctx context.Context, agentID int64, policy string) (Status, error) {
	var result Status
	err := updateState(ctx, agentID, func(state *accessState) error {
		state.Policy = normalizePolicy(policy)
		result = buildStatus(*state)
		return nil
	})
	return result, err
}

func BuildStatusCardMarkdown(summary, status, referenceID string) string {
	params := url.Values{}
	params.Set("category", StatusCategoryAccess)
	params.Set("status", normalizeStatus(status))
	params.Set("summary", strings.TrimSpace(summary))
	if ref := normalizeString(referenceID); ref != "" {
		params.Set("reference_id", ref)
	}
	link := "grix://card/agent_status?" + params.Encode()
	return fmt.Sprintf("[%s](%s)", compactLabel(summary, "Access Status"), link)
}

func BuildPairingCardMarkdown(pairingCode string) string {
	params := url.Values{}
	params.Set("pairing_code", strings.TrimSpace(pairingCode))
	params.Set("command_hint", pairingCommandHint)
	link := "grix://card/agent_pairing?" + params.Encode()
	return fmt.Sprintf("[%s](%s)", compactLabel("Pairing Code", "Pairing Code"), link)
}

func normalizeStatus(value string) string {
	switch strings.TrimSpace(value) {
	case StatusSuccess:
		return StatusSuccess
	case StatusWarning:
		return StatusWarning
	case StatusError:
		return StatusError
	default:
		return StatusInfo
	}
}

func compactLabel(summary, fallback string) string {
	label := strings.Join(strings.Fields(strings.TrimSpace(summary)), " ")
	if label == "" {
		label = fallback
	}
	if len(label) <= 64 {
		return label
	}
	return label[:61] + "..."
}
