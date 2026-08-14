package service

type QRLoginCreateResp struct {
	QRSessionID    string `json:"qr_session_id"`
	QRText         string `json:"qr_text"`
	PollToken      string `json:"poll_token"`
	ExpiresIn      int64  `json:"expires_in"`
	PollIntervalMS int64  `json:"poll_interval_ms"`
}

type QRLoginScannerUser struct {
	UserID    string `json:"user_id"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatar_url"`
}

type QRLoginStatusResp struct {
	Status      string              `json:"status"`
	ExpiresIn   int64               `json:"expires_in"`
	ScannerUser *QRLoginScannerUser `json:"scanner_user,omitempty"`
}

type QRLoginScanResp struct {
	QRSessionID string `json:"qr_session_id"`
	Status      string `json:"status"`
	ExpiresIn   int64  `json:"expires_in"`
}

type QRLoginConfirmResp struct {
	QRSessionID string `json:"qr_session_id"`
	Status      string `json:"status"`
}
