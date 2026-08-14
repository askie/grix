package service

import (
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/google/uuid"
)

const qrLoginCreateRetryMax = 4

func CreateQRLoginSession(requestIP, requestUserAgent, requestDeviceLabel string) (*QRLoginCreateResp, error) {
	now := time.Now().UTC()
	for i := 0; i < qrLoginCreateRetryMax; i++ {
		qrToken, err := generateQRLoginToken(qrLoginTokenByteLength)
		if err != nil {
			return nil, err
		}
		pollToken, err := generateQRLoginToken(qrLoginTokenByteLength)
		if err != nil {
			return nil, err
		}

		sessionID := uuid.NewString()
		expiresAt := now.Add(qrLoginSessionTTL)

		rec := &model.AuthQRLoginSession{
			SessionID:          sessionID,
			QRTokenHash:        hashQRLoginToken(qrToken),
			PollTokenHash:      hashQRLoginToken(pollToken),
			Status:             model.AuthQRLoginStatusPendingScan,
			Scene:              model.AuthQRLoginSceneWebDesktop,
			RequestIP:          strings.TrimSpace(requestIP),
			RequestUserAgent:   strings.TrimSpace(requestUserAgent),
			RequestDeviceLabel: strings.TrimSpace(requestDeviceLabel),
			ExpiresAt:          expiresAt,
		}
		if err := createQRLoginSession(rec); err != nil {
			if isUniqueConstraintErr(err) {
				continue
			}
			return nil, err
		}

		return &QRLoginCreateResp{
			QRSessionID:    sessionID,
			QRText:         buildQRLoginText(sessionID, qrToken, qrLoginDeployRegion()),
			PollToken:      pollToken,
			ExpiresIn:      int64(qrLoginSessionTTL.Seconds()),
			PollIntervalMS: qrLoginPollIntervalMS,
		}, nil
	}

	return nil, ErrQRLoginCreateFailed
}
