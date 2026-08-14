package agentapi

import (
	"context"
	"strings"

	geminiadapter "github.com/askie/grix/backend/internal/agentadapter/gemini"
	"github.com/askie/grix/backend/internal/geminisession"
	"github.com/askie/grix/backend/internal/pkg/logger"
)

func (c *agentConn) applyGeminiSessionContext(evt DelegateEventPayload) DelegateEventPayload {
	if c == nil || c.adapterID != geminiadapter.AdapterID {
		return evt
	}
	if c.agentID <= 0 || strings.TrimSpace(evt.SessionID) == "" {
		return evt
	}

	incoming := geminiadapter.ExtractSessionConfig(evt.Extra)
	stored, ok, err := geminisession.Load(context.Background(), c.agentID, evt.SessionID)
	if err != nil {
		logger.L.Warnf("load gemini session context failed agent=%d session=%s err=%v", c.agentID, evt.SessionID, err)
	}

	merged := geminisession.Snapshot{
		AgentID:   c.agentID,
		SessionID: strings.TrimSpace(evt.SessionID),
		ModeID:    strings.TrimSpace(incoming.ModeID),
		ModelID:   strings.TrimSpace(incoming.ModelID),
	}
	if ok {
		if merged.ModeID == "" {
			merged.ModeID = stored.ModeID
		}
		if merged.ModelID == "" {
			merged.ModelID = stored.ModelID
		}
	}

	if merged.ModeID == "" && merged.ModelID == "" {
		return evt
	}

	if !ok || merged != stored {
		if err := geminisession.Upsert(context.Background(), merged); err != nil {
			logger.L.Warnf("upsert gemini session context failed agent=%d session=%s err=%v", c.agentID, evt.SessionID, err)
		}
	}

	evt.Extra = geminiadapter.MergeSessionConfig(evt.Extra, geminiadapter.SessionConfig{
		ModeID:  merged.ModeID,
		ModelID: merged.ModelID,
	})
	return evt
}
