package handler

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/askie/grix/backend/internal/store"
)

const (
	sendMsgMaxRunes   = 100_000
	sendMsgMaxBytes   = 128 * 1024
	sendMsgRateWindow = 10 * time.Second
	sendMsgRateLimit  = 8
)

type sendContentMetrics struct {
	TrimmedLengthBytes int
	TrimmedLengthRunes int
}

func validateSendContent(ctx context.Context, userID int64, sessionID, content string) (int, string) {
	metrics := inspectSendContentMetrics(content)
	if metrics.TrimmedLengthBytes == 0 {
		return 4001, "invalid send_msg payload"
	}
	if metrics.TrimmedLengthRunes > sendMsgMaxRunes || metrics.TrimmedLengthBytes > sendMsgMaxBytes {
		return 4004, "message too large"
	}
	if userID <= 0 || sessionID == "" {
		return 0, ""
	}

	rateKey := fmt.Sprintf("im:send_guard:rate:%d:%s", userID, sessionID)
	rateCount, err := store.RDB.Incr(ctx, rateKey).Result()
	if err == nil {
		if rateCount == 1 {
			store.RDB.Expire(ctx, rateKey, sendMsgRateWindow)
		}
		if rateCount > sendMsgRateLimit {
			return 4008, "send too fast"
		}
	}

	return 0, ""
}

func inspectSendContentMetrics(content string) sendContentMetrics {
	trimmed := strings.TrimSpace(content)
	return sendContentMetrics{
		TrimmedLengthBytes: len(trimmed),
		TrimmedLengthRunes: utf8.RuneCountInString(trimmed),
	}
}
