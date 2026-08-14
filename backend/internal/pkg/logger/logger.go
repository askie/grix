package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	L          *zap.SugaredLogger
	initMu     sync.Mutex
	activeFile *os.File
)

func Init() {
	initMu.Lock()
	defer initMu.Unlock()

	if L != nil {
		_ = L.Sync()
	}
	if activeFile != nil {
		_ = activeFile.Close()
		activeFile = nil
	}

	enc := zapcore.NewJSONEncoder(zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	})

	level := parseLogLevel(os.Getenv("AIBOT_LOG_LEVEL"))
	cores := []zapcore.Core{
		zapcore.NewCore(enc, zapcore.AddSync(os.Stdout), level),
	}

	logFilePath := strings.TrimSpace(os.Getenv("AIBOT_LOG_FILE"))
	if logFilePath != "" {
		file, err := openLogFile(logFilePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "logger: open log file failed path=%s err=%v\n", logFilePath, err)
		} else {
			activeFile = file
			cores = append(cores, zapcore.NewCore(enc, zapcore.AddSync(file), level))
		}
	}

	core := zapcore.NewTee(cores...)
	logger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
	L = logger.Sugar()
}

func parseLogLevel(raw string) zapcore.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return zapcore.DebugLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	case "dpanic":
		return zapcore.DPanicLevel
	case "panic":
		return zapcore.PanicLevel
	case "fatal":
		return zapcore.FatalLevel
	default:
		return zapcore.InfoLevel
	}
}

func openLogFile(path string) (*os.File, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("empty log file path")
	}
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}
