package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/askie/grix/backend/internal/agentsync"
	bindingstore "github.com/askie/grix/backend/internal/agenttoolbar/store"
	"github.com/askie/grix/backend/internal/model"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"golang.org/x/sync/singleflight"
)

const (
	historySyncPageLimit = 100
	historySyncMaxPages  = 20
)

type historySyncClient interface {
	SendSessionHistorySyncActionAndWait(
		agentID, ownerID int64,
		aibotSessionID string,
		actorID string,
		cwd string,
		providerKey string,
		agentSessionID string,
		cursor string,
		limit int,
		syncRunID string,
	) (*wsagentapi.SessionHistorySyncResponse, error)
}

var (
	historySyncGroup          singleflight.Group
	historySyncClientProvider = func() historySyncClient {
		mgr := wsagentapi.GetGlobal()
		if mgr == nil {
			return nil
		}
		return mgr
	}
)

// SyncBoundSessionHistory imports connector-native history for every active
// binding in a session. Callers must verify that ownerID can read the session
// before invoking it.
func SyncBoundSessionHistory(ctx context.Context, ownerID int64, sessionID string) (int, error) {
	sessionID = strings.TrimSpace(sessionID)
	if ownerID <= 0 || sessionID == "" {
		return 0, nil
	}
	bindings, err := bindingstore.ListSyncableBindingsBySession(ctx, sessionID)
	if err != nil || len(bindings) == 0 {
		return 0, err
	}
	client := historySyncClientProvider()
	if client == nil {
		return 0, wsagentapi.ErrSessionHistorySyncAgentOffline
	}

	totalImported := 0
	var syncErrs []error
	for _, binding := range bindings {
		binding := binding
		key := fmt.Sprintf("%d:%d:%s:%s:%s", ownerID, binding.AgentID, sessionID, binding.ProviderKey, binding.BindingID)
		value, syncErr, _ := historySyncGroup.Do(key, func() (any, error) {
			return syncBinding(ctx, client, ownerID, binding)
		})
		if imported, ok := value.(int); ok {
			totalImported += imported
		}
		if syncErr != nil {
			syncErrs = append(syncErrs, syncErr)
		}
	}
	return totalImported, errors.Join(syncErrs...)
}

func syncBinding(ctx context.Context, client historySyncClient, ownerID int64, binding bindingstore.BindingRecord) (int, error) {
	ident := agentsync.SyncIdentity{
		AgentID:     binding.AgentID,
		OwnerID:     ownerID,
		SessionID:   strings.TrimSpace(binding.SessionID),
		ProviderKey: strings.TrimSpace(binding.ProviderKey),
		BindingID:   strings.TrimSpace(binding.BindingID),
		SyncRunID:   agentsync.NewSyncRunID(),
	}
	// Import gate: a sync state row is created only when a user explicitly
	// imports an existing provider-native session (agent_session_bind with an
	// agent_session_id). No row means live delivery owns the session — never
	// import, or every live turn appended to the provider's local transcript
	// would be re-imported as a duplicate. A completed row means the one-shot
	// import already finished and live delivery has taken over since; syncing
	// the transcript delta again would duplicate the live turns too.
	state, found, err := agentsync.LoadState(ctx, ident)
	if err != nil {
		return 0, err
	}
	if !found || state.Status == model.AgentSessionSyncStatusCompleted {
		return 0, nil
	}
	if err := agentsync.ValidateTarget(ctx, ident); err != nil {
		return 0, err
	}
	cursor := strings.TrimSpace(state.Cursor)
	if err := agentsync.QueueAtCursor(ctx, ident, cursor); err != nil {
		return 0, err
	}

	totalImported := 0
	actorID := strconv.FormatInt(ownerID, 10)
	resetInvalidCursor := false
	for page := 0; page < historySyncMaxPages; {
		if err := agentsync.MarkRunning(ctx, ident, cursor); err != nil {
			return totalImported, err
		}
		resp, err := client.SendSessionHistorySyncActionAndWait(
			ident.AgentID,
			ident.OwnerID,
			ident.SessionID,
			actorID,
			strings.TrimSpace(binding.Cwd),
			ident.ProviderKey,
			ident.BindingID,
			cursor,
			historySyncPageLimit,
			ident.SyncRunID,
		)
		if err != nil {
			if !resetInvalidCursor && cursor != "" && isInvalidCursorResponse(resp, err) {
				resetInvalidCursor = true
				cursor = ""
				if resetErr := agentsync.QueueAtCursor(ctx, ident, cursor); resetErr != nil {
					return totalImported, errors.Join(err, resetErr)
				}
				continue
			}
			_ = agentsync.MarkFailed(ctx, ident, cursor, totalImported, err)
			return totalImported, err
		}
		page++
		imported, err := agentsync.ImportPage(ctx, agentsync.ImportPageParams{
			SyncIdentity: ident,
			Messages:     resp.Messages,
			Cursor:       cursor,
		})
		totalImported += imported
		if err != nil {
			_ = agentsync.MarkFailed(ctx, ident, cursor, totalImported, err)
			return totalImported, err
		}

		nextCursor := strings.TrimSpace(resp.NextCursor)
		if nextCursor == "" {
			nextCursor = cursor
		}
		if !resp.HasMore {
			if err := agentsync.MarkCompleted(ctx, ident, nextCursor, totalImported); err != nil {
				return totalImported, err
			}
			return totalImported, nil
		}
		if nextCursor == cursor {
			err := errors.New("connector returned has_more without advancing history cursor")
			_ = agentsync.MarkFailed(ctx, ident, cursor, totalImported, err)
			return totalImported, err
		}
		cursor = nextCursor
		if err := agentsync.MarkPartial(ctx, ident, cursor, totalImported); err != nil {
			return totalImported, err
		}
	}

	return totalImported, nil
}

func isInvalidCursorResponse(resp *wsagentapi.SessionHistorySyncResponse, err error) bool {
	if resp != nil && strings.TrimSpace(resp.ErrorCode) == wsagentapi.SessionHistorySyncErrorInvalidCursor {
		return true
	}
	var syncErr *wsagentapi.SessionHistorySyncError
	return errors.As(err, &syncErr) && strings.TrimSpace(syncErr.Code) == wsagentapi.SessionHistorySyncErrorInvalidCursor
}
