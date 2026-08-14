package handler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

const defaultSessionChatFrontendLogFile = "/tmp/aibot_session_chat_frontend.log"

var (
	sessionChatFrontendLogMu   sync.Mutex
	sessionChatFrontendLogFile *os.File
	sessionChatFrontendLogPath string
)

type sessionChatFrontendLogEntry struct {
	LoggedAt        string          `json:"logged_at"`
	UserID          int64           `json:"user_id,string"`
	ExcludeDeviceID string          `json:"exclude_device_id,omitempty"`
	Cmd             string          `json:"cmd"`
	Payload         json.RawMessage `json:"payload"`
}

func logSessionChatFrontendPayload(userID int64, excludeDeviceID, cmd string, payload any) {
	if cmd != protocol.CmdPushMsg {
		return
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		logger.L.Warnf("marshal session chat frontend payload failed user=%d cmd=%s err=%v", userID, cmd, err)
		return
	}

	entryJSON, err := json.Marshal(sessionChatFrontendLogEntry{
		LoggedAt:        time.Now().UTC().Format(time.RFC3339Nano),
		UserID:          userID,
		ExcludeDeviceID: strings.TrimSpace(excludeDeviceID),
		Cmd:             cmd,
		Payload:         payloadJSON,
	})
	if err != nil {
		logger.L.Warnf("marshal session chat frontend log entry failed user=%d cmd=%s err=%v", userID, cmd, err)
		return
	}

	sessionChatFrontendLogMu.Lock()
	defer sessionChatFrontendLogMu.Unlock()

	file, err := ensureSessionChatFrontendLogFileLocked()
	if err != nil {
		logger.L.Warnf("open session chat frontend log file failed user=%d cmd=%s err=%v", userID, cmd, err)
		return
	}

	if _, err := file.Write(append(entryJSON, '\n')); err != nil {
		logger.L.Warnf("write session chat frontend log failed user=%d cmd=%s err=%v", userID, cmd, err)
	}
}

func ensureSessionChatFrontendLogFileLocked() (*os.File, error) {
	path := resolveSessionChatFrontendLogFilePath()
	if sessionChatFrontendLogFile != nil && sessionChatFrontendLogPath == path {
		return sessionChatFrontendLogFile, nil
	}

	if sessionChatFrontendLogFile != nil {
		_ = sessionChatFrontendLogFile.Close()
		sessionChatFrontendLogFile = nil
		sessionChatFrontendLogPath = ""
	}

	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}

	sessionChatFrontendLogFile = file
	sessionChatFrontendLogPath = path
	return sessionChatFrontendLogFile, nil
}

func resolveSessionChatFrontendLogFilePath() string {
	if path := strings.TrimSpace(os.Getenv("AIBOT_SESSION_CHAT_LOG_FILE")); path != "" {
		return path
	}
	return defaultSessionChatFrontendLogFile
}

// closeSessionChatFrontendLogFile closes the global log file (for test cleanup)
func closeSessionChatFrontendLogFile() {
	sessionChatFrontendLogMu.Lock()
	defer sessionChatFrontendLogMu.Unlock()

	if sessionChatFrontendLogFile != nil {
		_ = sessionChatFrontendLogFile.Close()
		sessionChatFrontendLogFile = nil
		sessionChatFrontendLogPath = ""
	}
}
