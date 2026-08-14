package service

import (
	"context"
	"encoding/json"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/minio/minio-go/v7"
)

var (
	messageAttachmentCleanupAsync = func(fn func()) {
		go fn()
	}
	messageAttachmentEnsureOSSReady = ensureOSSReady
	messageAttachmentRemoveObject   = func(ctx context.Context, bucket, objectKey string) error {
		return getOSSClient(ossStorageMedia).RemoveObject(
			ctx,
			bucket,
			objectKey,
			minio.RemoveObjectOptions{},
		)
	}
)

type messageAttachmentCleanupEnvelope struct {
	Attachments []messageAttachmentCleanupItem `json:"attachments"`
}

type messageAttachmentCleanupItem struct {
	MediaURL string `json:"media_url"`
}

func scheduleRevokedMessageAttachmentCleanup(msg model.Message) {
	objectKeys := collectRevokedMessageAttachmentObjectKeys(msg)
	if len(objectKeys) == 0 {
		return
	}

	messageAttachmentCleanupAsync(func() {
		cleanupRevokedMessageAttachmentObjects(msg.SessionID, msg.MsgID, objectKeys)
	})
}

func cleanupRevokedMessageAttachmentObjects(sessionID string, msgID int64, objectKeys []string) {
	if err := messageAttachmentEnsureOSSReady(); err != nil {
		logger.L.Warnf(
			"skip revoked message attachment cleanup session=%s msg_id=%d err=%v",
			sessionID,
			msgID,
			err,
		)
		return
	}

	bucket := getOSSConfig(ossStorageMedia).Bucket
	ctx := context.Background()
	for _, objectKey := range objectKeys {
		if err := messageAttachmentRemoveObject(ctx, bucket, objectKey); err != nil {
			logger.L.Warnf(
				"delete revoked message attachment failed session=%s msg_id=%d object_key=%s err=%v",
				sessionID,
				msgID,
				objectKey,
				err,
			)
		}
	}
}

func collectRevokedMessageAttachmentObjectKeys(msg model.Message) []string {
	if len(msg.Extra) == 0 || !json.Valid(msg.Extra) {
		return nil
	}

	var envelope messageAttachmentCleanupEnvelope
	if err := json.Unmarshal(msg.Extra, &envelope); err != nil {
		return nil
	}

	seen := make(map[string]struct{}, len(envelope.Attachments))
	objectKeys := make([]string, 0, len(envelope.Attachments))
	for _, attachment := range envelope.Attachments {
		objectKey := resolveMediaObjectKey(attachment.MediaURL)
		if !shouldDeleteRevokedMessageAttachmentObjectKey(msg, objectKey) {
			continue
		}
		if _, exists := seen[objectKey]; exists {
			continue
		}
		seen[objectKey] = struct{}{}
		objectKeys = append(objectKeys, objectKey)
	}
	if len(objectKeys) == 0 {
		return nil
	}
	return objectKeys
}

func shouldDeleteRevokedMessageAttachmentObjectKey(msg model.Message, objectKey string) bool {
	switch msg.SenderType {
	case 1:
		return isUserMediaObjectKey(msg.SenderID, objectKey)
	case 2:
		return isSessionMediaObjectKey(msg.SessionID, objectKey)
	default:
		return false
	}
}
