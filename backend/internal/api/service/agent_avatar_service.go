package service

import (
	"bytes"
	"context"
	"log"
	"mime/multipart"
	"strings"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/store"
	"github.com/minio/minio-go/v7"
)

func UploadAgentAvatar(userID, agentID int64, fileHeader *multipart.FileHeader) (*AgentResp, *errcode.ErrCode, error) {
	if fileHeader == nil {
		return nil, nil, ErrAvatarFileRequired
	}
	if err := ensureAvatarOSSReady(); err != nil {
		return nil, nil, err
	}
	if fileHeader.Size <= 0 {
		return nil, nil, ErrAvatarFileRequired
	}
	if fileHeader.Size > userAvatarMaxUploadBytes {
		return nil, nil, ErrAvatarFileTooLarge
	}

	var agent model.Agent
	if err := store.DB.First(&agent, agentID).Error; err != nil {
		return nil, &errcode.ErrAgentNotFound, nil
	}
	if agent.OwnerID != userID {
		return nil, &errcode.ErrAgentForbidden, nil
	}
	if agent.Status == 3 {
		return nil, &errcode.ErrAgentNotFound, nil
	}

	file, err := fileHeader.Open()
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	raw, err := readAvatarUploadBytes(file, userAvatarMaxUploadBytes+1)
	if err != nil {
		return nil, nil, err
	}
	if int64(len(raw)) > userAvatarMaxUploadBytes {
		return nil, nil, ErrAvatarFileTooLarge
	}

	jpegBytes, err := normalizeAvatarImage(raw)
	if err != nil {
		return nil, nil, err
	}

	previousAvatarURL := strings.TrimSpace(agent.AvatarURL)
	objectKey := buildAgentAvatarVersionedObjectKey(agentID, time.Now().Unix())
	_, err = getOSSClient(ossStorageAvatar).PutObject(
		context.Background(),
		config.C.OSS.Avatar.Bucket,
		objectKey,
		bytes.NewReader(jpegBytes),
		int64(len(jpegBytes)),
		minio.PutObjectOptions{ContentType: "image/jpeg"},
	)
	if err != nil {
		return nil, nil, err
	}

	avatarURL := buildAvatarAccessURL(objectKey)
	if err := store.DB.Model(&agent).Updates(map[string]any{
		"avatar_url": avatarURL,
		"updated_at": time.Now(),
	}).Error; err != nil {
		return nil, nil, err
	}
	if err := deletePreviousAgentAvatarObject(userID, agentID, previousAvatarURL, objectKey); err != nil {
		log.Printf(
			"UploadAgentAvatar cleanup old avatar failed: user_id=%d agent_id=%d old_avatar_url=%q error=%v",
			userID,
			agentID,
			previousAvatarURL,
			err,
		)
	}

	if err := store.DB.First(&agent, agentID).Error; err != nil {
		return nil, nil, err
	}
	resp := agentToResp(&agent, userID)
	return &resp, nil, nil
}

func deletePreviousAgentAvatarObject(userID, agentID int64, previousAvatarURL, currentObjectKey string) error {
	previousObjectKey := resolveAvatarObjectKey(previousAvatarURL)
	if previousObjectKey == "" || previousObjectKey == currentObjectKey {
		return nil
	}
	if !isAgentAvatarObjectKey(userID, agentID, previousObjectKey) {
		return nil
	}
	return getOSSClient(ossStorageAvatar).RemoveObject(
		context.Background(),
		config.C.OSS.Avatar.Bucket,
		previousObjectKey,
		minio.RemoveObjectOptions{},
	)
}
