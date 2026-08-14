package adapterlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Enabled    bool
	LogRoot    string
	MaxSizeMB  int
	MaxAgeDays int
}

type LogEntry struct {
	Ts        string          `json:"ts"`
	Dir       string          `json:"dir"`
	Method    string          `json:"method"`
	Family    string          `json:"family"`
	AdapterID string          `json:"adapter_id"`
	SessionID string          `json:"session_id"`
	Input     json.RawMessage `json:"input"`
	Output    json.RawMessage `json:"output,omitempty"`
	Error     string          `json:"error,omitempty"`
}

type fileKey struct {
	Family    string
	SessionID string
}

type logFile struct {
	file *os.File
	size int64
	path string
}

type Manager struct {
	cfg   Config
	mu    sync.Mutex
	files map[fileKey]*logFile
	done  chan struct{}
}

func ConfigFromEnv() Config {
	maxSize, _ := strconv.Atoi(os.Getenv("AIBOT_ADAPTER_LOG_MAX_SIZE_MB"))
	if maxSize <= 0 {
		maxSize = 100
	}
	maxAge, _ := strconv.Atoi(os.Getenv("AIBOT_ADAPTER_LOG_MAX_AGE_DAYS"))
	if maxAge <= 0 {
		maxAge = 7
	}
	dir := strings.TrimSpace(os.Getenv("AIBOT_ADAPTER_LOG_DIR"))
	if dir == "" {
		dir = "adapter-logs"
	}
	return Config{
		Enabled:    true,
		LogRoot:    dir,
		MaxSizeMB:  maxSize,
		MaxAgeDays: maxAge,
	}
}

func NewManager(cfg Config) *Manager {
	m := &Manager{
		cfg:   cfg,
		files: make(map[fileKey]*logFile),
		done:  make(chan struct{}),
	}
	if cfg.Enabled {
		go m.cleanupLoop()
	}
	return m
}

func (m *Manager) Enabled() bool  { return m.cfg.Enabled }
func (m *Manager) LogRoot() string { return m.cfg.LogRoot }

// sanitizeLogSessionID 清洗用于拼接日志文件名的 session_id，剥离目录穿越成分，
// 防止 agent 上行的 session_id 把日志文件写到 LogRoot 之外。
func sanitizeLogSessionID(sessionID string) string {
	base := filepath.Base(sessionID)
	if base == "" || base == "." || base == ".." || strings.ContainsAny(base, `/\`) {
		return "_invalid"
	}
	return base
}

func (m *Manager) WriteEntry(family, sessionID string, entry LogEntry) error {
	if !m.cfg.Enabled {
		return nil
	}
	if sessionID == "" {
		sessionID = "_unknown"
	}
	sessionID = sanitizeLogSessionID(sessionID)

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("adapterlog: marshal entry: %w", err)
	}
	data = append(data, '\n')

	m.mu.Lock()
	defer m.mu.Unlock()

	key := fileKey{Family: family, SessionID: sessionID}
	lf, ok := m.files[key]
	if !ok {
		dir := filepath.Join(m.cfg.LogRoot, family)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("adapterlog: mkdir %s: %w", dir, err)
		}
		p := filepath.Join(dir, fmt.Sprintf("session_%s.jsonl", sessionID))
		f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("adapterlog: open %s: %w", p, err)
		}
		lf = &logFile{file: f, path: p}
		m.files[key] = lf
	}

	n, err := lf.file.Write(data)
	if err != nil {
		return fmt.Errorf("adapterlog: write %s: %w", lf.path, err)
	}
	lf.size += int64(n)

	if lf.size >= int64(m.cfg.MaxSizeMB)*1024*1024 {
		m.rotateFile(key, lf)
	}
	return nil
}

func (m *Manager) Close() {
	if !m.cfg.Enabled {
		return
	}
	close(m.done)
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, lf := range m.files {
		lf.file.Close()
		delete(m.files, key)
	}
}

func (m *Manager) rotateFile(key fileKey, lf *logFile) {
	ts := time.Now().Format("20060102_150405")
	rotated := lf.path[:len(lf.path)-4] + "_" + ts + ".log"
	lf.file.Close()
	os.Rename(lf.path, rotated)

	f, err := os.OpenFile(lf.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		delete(m.files, key)
		return
	}
	m.files[key] = &logFile{file: f, path: lf.path}
}

func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-m.done:
			return
		case <-ticker.C:
			m.cleanup()
		}
	}
}

func (m *Manager) cleanup() {
	cutoff := time.Now().AddDate(0, 0, -m.cfg.MaxAgeDays)

	entries, err := os.ReadDir(m.cfg.LogRoot)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		familyDir := filepath.Join(m.cfg.LogRoot, e.Name())
		files, err := os.ReadDir(familyDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				p := filepath.Join(familyDir, f.Name())
				m.mu.Lock()
				for key, lf := range m.files {
					if lf.path == p {
						lf.file.Close()
						delete(m.files, key)
					}
				}
				m.mu.Unlock()
				os.Remove(p)
			}
		}
	}
}
