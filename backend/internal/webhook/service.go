package webhook

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/sessionguard"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/textutil"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

type Sender interface {
	SendAsUser(ctx context.Context, req SendRequest) (*SendResult, error)
}

type SendRequest struct {
	UserID      int64
	SessionID   string
	Content     string
	MsgType     int16
	ClientMsgID string
}

type SendResult struct {
	MessageID int64
	SentAt    time.Time
}

type CreateRequest struct {
	UserID    int64
	SessionID string
	ExpiresAt *time.Time
	BaseURL   string
}

type EndpointView struct {
	ID           int64      `json:"id,string"`
	SessionID    string     `json:"session_id,omitempty"`
	SessionTitle string     `json:"session_title,omitempty"`
	SessionType  int16      `json:"session_type,omitempty"`
	URL          string     `json:"url"`
	CreatedAt    time.Time  `json:"created_at"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
	Status       string     `json:"status"`
}

type DeliverRequest struct {
	Content     string `json:"content"`
	MsgType     string `json:"msg_type"`
	ClientMsgID string `json:"client_msg_id"`
}

// MaxActiveEndpointsPerSession 每个会话（按主人）允许的未删除且未过期入口上限，防止被刷。
const MaxActiveEndpointsPerSession = 20

type Service struct{}

func NewService() *Service { return &Service{} }

func (s *Service) CreateEndpoint(ctx context.Context, req CreateRequest) (*EndpointView, error) {
	if req.UserID <= 0 || strings.TrimSpace(req.SessionID) == "" {
		return nil, ErrInvalidPayload
	}
	if err := ensureSessionMember(ctx, req.UserID, req.SessionID); err != nil {
		return nil, ErrForbidden
	}
	now := time.Now().UTC()
	if req.ExpiresAt != nil && !req.ExpiresAt.After(now) {
		return nil, ErrExpiresInPast
	}
	var active int64
	if err := store.DB.WithContext(ctx).Model(&model.WebhookEndpoint{}).
		Where("user_id = ? AND session_id = ? AND deleted_at IS NULL AND (expires_at IS NULL OR expires_at > ?)", req.UserID, req.SessionID, now).
		Count(&active).Error; err != nil {
		return nil, err
	}
	if active >= MaxActiveEndpointsPerSession {
		return nil, ErrLimitExceeded
	}
	token, err := generateToken()
	if err != nil {
		return nil, err
	}
	tokenCipher, err := encryptToken(token)
	if err != nil {
		return nil, err
	}
	entity := model.WebhookEndpoint{
		ID:          snowflake.GenID(),
		UserID:      req.UserID,
		SessionID:   req.SessionID,
		TokenHash:   hashToken(token),
		TokenValue:  tokenCipher,
		TokenPrefix: tokenPrefix(token),
		ExpiresAt:   req.ExpiresAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.DB.WithContext(ctx).Create(&entity).Error; err != nil {
		return nil, err
	}
	return endpointView(entity, req.BaseURL, token), nil
}

func (s *Service) ListEndpoints(ctx context.Context, userID int64, sessionID, baseURL string) ([]EndpointView, error) {
	if userID <= 0 || strings.TrimSpace(sessionID) == "" {
		return nil, ErrInvalidPayload
	}
	if err := ensureSessionMember(ctx, userID, sessionID); err != nil {
		return nil, ErrForbidden
	}
	var rows []model.WebhookEndpoint
	if err := store.DB.WithContext(ctx).
		Where("user_id = ? AND session_id = ? AND deleted_at IS NULL", userID, sessionID).
		Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]EndpointView, 0, len(rows))
	for _, row := range rows {
		rawToken, decErr := decryptToken(row.TokenValue)
		if decErr != nil || strings.TrimSpace(rawToken) == "" {
			continue
		}
		items = append(items, *endpointView(row, baseURL, rawToken))
	}
	return items, nil
}

func (s *Service) DeleteEndpoint(ctx context.Context, userID, endpointID int64) error {
	if userID <= 0 || endpointID <= 0 {
		return ErrInvalidPayload
	}
	now := time.Now().UTC()
	res := store.DB.WithContext(ctx).Model(&model.WebhookEndpoint{}).
		Where("id = ? AND user_id = ? AND deleted_at IS NULL", endpointID, userID).
		Updates(map[string]any{"deleted_at": now, "updated_at": now})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) ListEndpointsByUser(ctx context.Context, userID int64, baseURL string, limit, offset int) ([]EndpointView, error) {
	if userID <= 0 {
		return nil, ErrInvalidPayload
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}

	var rows []model.WebhookEndpoint
	if err := store.DB.WithContext(ctx).
		Where("user_id = ? AND deleted_at IS NULL", userID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []EndpointView{}, nil
	}

	sessionIDs := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		sid := strings.TrimSpace(row.SessionID)
		if sid == "" {
			continue
		}
		if _, ok := seen[sid]; ok {
			continue
		}
		seen[sid] = struct{}{}
		sessionIDs = append(sessionIDs, sid)
	}

	// Query sessions for type and group name.
	sessionsBySID := make(map[string]model.Session, len(sessionIDs))
	if len(sessionIDs) > 0 {
		var sessions []model.Session
		if err := store.DB.WithContext(ctx).
			Select("session_id", "session_type", "group_name").
			Where("session_id IN ?", sessionIDs).
			Find(&sessions).Error; err != nil {
			return nil, err
		}
		for _, sess := range sessions {
			sessionsBySID[sess.SessionID] = sess
		}
	}

	// Query custom titles.
	customTitleBySID := make(map[string]string, len(sessionIDs))
	if len(sessionIDs) > 0 {
		var members []model.SessionMember
		if err := store.DB.WithContext(ctx).
			Select("session_id", "custom_title").
			Where("member_id = ? AND member_type = 1 AND session_id IN ?", userID, sessionIDs).
			Find(&members).Error; err != nil {
			return nil, err
		}
		for _, item := range members {
			sid := strings.TrimSpace(item.SessionID)
			if sid == "" {
				continue
			}
			if title := strings.TrimSpace(item.CustomTitle); title != "" {
				customTitleBySID[sid] = title
			}
		}
	}

	// For direct sessions without custom title, collect peer members to resolve names.
	type peerInfo struct {
		memberID   int64
		memberType int16
	}
	peerBySession := make(map[string]peerInfo, len(sessionIDs))
	var humanIDs []int64
	var agentIDs []int64
	for _, sid := range sessionIDs {
		if customTitleBySID[sid] != "" {
			continue
		}
		sess, ok := sessionsBySID[sid]
		if !ok || sess.SessionType != model.SessionTypeDirect {
			continue
		}
		var peer model.SessionMember
		if err := store.DB.WithContext(ctx).
			Select("member_id", "member_type").
			Where("session_id = ? AND member_id != ?", sid, userID).
			First(&peer).Error; err != nil {
			continue
		}
		peerBySession[sid] = peerInfo{memberID: peer.MemberID, memberType: peer.MemberType}
		if peer.MemberType == 1 {
			humanIDs = append(humanIDs, peer.MemberID)
		} else {
			agentIDs = append(agentIDs, peer.MemberID)
		}
	}

	peerNameByID := make(map[int64]string)
	if len(humanIDs) > 0 {
		var users []model.User
		if err := store.DB.WithContext(ctx).
			Select("id", "nickname", "username").
			Where("id IN ?", humanIDs).
			Find(&users).Error; err != nil {
			return nil, err
		}
		for _, u := range users {
			name := strings.TrimSpace(u.Nickname)
			if name == "" {
				name = strings.TrimSpace(u.Username)
			}
			if name != "" {
				peerNameByID[u.ID] = name
			}
		}
	}
	if len(agentIDs) > 0 {
		var agents []model.Agent
		if err := store.DB.WithContext(ctx).
			Select("id", "agent_name").
			Where("id IN ?", agentIDs).
			Find(&agents).Error; err != nil {
			return nil, err
		}
		for _, a := range agents {
			if name := strings.TrimSpace(a.AgentName); name != "" {
				peerNameByID[a.ID] = name
			}
		}
	}

	// Load first message title for each session (same logic as session list).
	firstMsgTitleBySID := make(map[string]string, len(sessionIDs))
	if len(sessionIDs) > 0 {
		type firstMsgRow struct {
			SessionID string
			Content   string
		}
		var msgRows []firstMsgRow
		if err := store.DB.WithContext(ctx).Raw(`
			SELECT ranked.session_id, ranked.content
			FROM (
				SELECT session_id, content,
					ROW_NUMBER() OVER (PARTITION BY session_id ORDER BY created_at ASC, msg_id ASC) AS rn
				FROM messages
				WHERE session_id IN ? AND is_deleted = false
			) AS ranked
			WHERE ranked.rn = 1
		`, sessionIDs).Scan(&msgRows).Error; err != nil {
			return nil, err
		}
		for _, r := range msgRows {
			sid := strings.TrimSpace(r.SessionID)
			if sid == "" {
				continue
			}
			compact := strings.Join(strings.Fields(strings.TrimSpace(r.Content)), " ")
			if compact != "" {
				firstMsgTitleBySID[sid] = textutil.TruncateRunes(compact, 24)
			}
		}
	}

	// Build title resolution: custom_title > group_name (group) > first message (direct) > peer name (direct) > session_id.
	titleBySession := make(map[string]string, len(sessionIDs))
	for _, sid := range sessionIDs {
		if title := customTitleBySID[sid]; title != "" {
			titleBySession[sid] = title
			continue
		}
		sess, ok := sessionsBySID[sid]
		if !ok {
			titleBySession[sid] = sid
			continue
		}
		if sess.SessionType == model.SessionTypeGroup {
			if name := strings.TrimSpace(sess.GroupName); name != "" {
				titleBySession[sid] = name
			} else {
				titleBySession[sid] = sid
			}
			continue
		}
		// Direct chat: first message title > peer name.
		if ftitle := firstMsgTitleBySID[sid]; ftitle != "" {
			titleBySession[sid] = ftitle
			continue
		}
		if peer, found := peerBySession[sid]; found {
			if name := peerNameByID[peer.memberID]; name != "" {
				titleBySession[sid] = name
				continue
			}
		}
		titleBySession[sid] = sid
	}

	items := make([]EndpointView, 0, len(rows))
	for _, row := range rows {
		rawToken, decErr := decryptToken(row.TokenValue)
		if decErr != nil || strings.TrimSpace(rawToken) == "" {
			continue
		}
		view := endpointView(row, baseURL, rawToken)
		view.SessionID = strings.TrimSpace(row.SessionID)
		view.SessionTitle = titleBySession[view.SessionID]
		if sess, ok := sessionsBySID[view.SessionID]; ok {
			view.SessionType = sess.SessionType
		}
		items = append(items, *view)
	}
	return items, nil
}

func (s *Service) Deliver(ctx context.Context, token string, req DeliverRequest, sender Sender) (*SendResult, error) {
	if sender == nil {
		return nil, errors.New("sender unavailable")
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return nil, ErrInvalidPayload
	}
	msgType := int16(1)
	if mt := strings.TrimSpace(strings.ToLower(req.MsgType)); mt != "" && mt != "text" {
		return nil, ErrInvalidPayload
	}
	var endpoint model.WebhookEndpoint
	err := store.DB.WithContext(ctx).
		Where("token_hash = ? AND deleted_at IS NULL", hashToken(token)).
		First(&endpoint).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	now := time.Now().UTC()
	if endpoint.ExpiresAt != nil && endpoint.ExpiresAt.Before(now) {
		return nil, ErrExpired
	}
	if err := sessionguard.ValidateSpeakPermission(ctx, nil, endpoint.SessionID, endpoint.UserID, 1); err != nil {
		return nil, ErrForbidden
	}
	clientMsgID := strings.TrimSpace(req.ClientMsgID)
	if clientMsgID == "" {
		clientMsgID = fmt.Sprintf("webhook-%d-%d", endpoint.ID, now.UnixNano())
	}
	out, err := sender.SendAsUser(ctx, SendRequest{
		UserID:      endpoint.UserID,
		SessionID:   endpoint.SessionID,
		Content:     content,
		MsgType:     msgType,
		ClientMsgID: clientMsgID,
	})
	if err != nil {
		return nil, err
	}
	if out != nil {
		_ = store.DB.WithContext(ctx).Model(&model.WebhookEndpoint{}).
			Where("id = ?", endpoint.ID).
			Updates(map[string]any{"last_used_at": now, "updated_at": now}).Error
	}
	return out, nil
}

func ensureSessionMember(ctx context.Context, userID int64, sessionID string) error {
	var count int64
	err := store.DB.WithContext(ctx).Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, userID).
		Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrForbidden
	}
	return nil
}

func endpointView(entity model.WebhookEndpoint, baseURL, token string) *EndpointView {
	return &EndpointView{
		ID:         entity.ID,
		URL:        strings.TrimRight(baseURL, "/") + "/v1/webhook/incoming/" + token,
		CreatedAt:  entity.CreatedAt,
		ExpiresAt:  entity.ExpiresAt,
		LastUsedAt: entity.LastUsedAt,
		Status:     statusOf(entity.ExpiresAt),
	}
}

func statusOf(expiresAt *time.Time) string {
	if expiresAt != nil && expiresAt.Before(time.Now().UTC()) {
		return "expired"
	}
	return "active"
}

// BaseURL 返回对外暴露的 webhook 入口基地址（scheme://host），
// 依次取 AgentAPIDomain / FriendQRBaseURL / GroupQRBaseURL 中第一个可解析的主机。
func BaseURL() string {
	for _, candidate := range []string{
		strings.TrimSpace(config.C.Server.AgentAPIDomain),
		strings.TrimSpace(config.C.Server.FriendQRBaseURL),
		strings.TrimSpace(config.C.Server.GroupQRBaseURL),
	} {
		if candidate == "" {
			continue
		}
		u, err := url.Parse(candidate)
		if err != nil || u.Host == "" {
			continue
		}
		scheme := u.Scheme
		switch scheme {
		case "ws":
			scheme = "http"
		case "wss":
			scheme = "https"
		}
		if scheme != "http" && scheme != "https" {
			scheme = "https"
		}
		return scheme + "://" + u.Host
	}
	return ""
}
