package service

import (
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrSessionFavoriteAlreadyExists = errors.New("会话已收藏")
	ErrSessionFavoriteNotFound      = errors.New("收藏记录不存在")
	ErrSessionFavoriteNotMember     = errors.New("非会话成员，不可收藏")
)

// FavoriteSessionItem is a single entry in the favorites list response.
type FavoriteSessionItem struct {
	SessionID   string       `json:"session_id"`
	SessionType int16        `json:"session_type"`
	Title       string       `json:"title"`
	Peer        *SessionPeer `json:"peer,omitempty"`
	LastMsg     string       `json:"last_msg"`
	FavoritedAt int64        `json:"favorited_at"`
}

type FavoriteSessionListResp struct {
	HasMore bool                  `json:"has_more"`
	List    []FavoriteSessionItem `json:"list"`
}

type FavoriteSessionStatusResp struct {
	IsFavorited bool `json:"is_favorited"`
}

func AddSessionFavorite(userID int64, sessionID string) error {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return errors.New("session_id 不能为空")
	}

	// Verify the user is a member of the session.
	var count int64
	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 1", sid, userID).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrSessionFavoriteNotMember
	}

	row := model.UserSessionFavorite{
		ID:        snowflake.GenID(),
		UserID:    userID,
		SessionID: sid,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.DB.Create(&row).Error; err != nil {
		if isSessionFavDuplicateKey(err) {
			return ErrSessionFavoriteAlreadyExists
		}
		return err
	}
	return nil
}

func RemoveSessionFavorite(userID int64, sessionID string) error {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return errors.New("session_id 不能为空")
	}
	result := store.DB.Where("user_id = ? AND session_id = ?", userID, sid).
		Delete(&model.UserSessionFavorite{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrSessionFavoriteNotFound
	}
	return nil
}

func GetSessionFavoriteStatus(userID int64, sessionID string) (*FavoriteSessionStatusResp, error) {
	sid := strings.TrimSpace(sessionID)
	var count int64
	err := store.DB.Model(&model.UserSessionFavorite{}).
		Where("user_id = ? AND session_id = ?", userID, sid).
		Count(&count).Error
	if err != nil {
		return nil, err
	}
	return &FavoriteSessionStatusResp{IsFavorited: count > 0}, nil
}

func ListFavoriteSessions(userID int64, limit, offset int) (*FavoriteSessionListResp, error) {
	if limit <= 0 {
		limit = sessionSearchDefaultLimit
	}
	if offset < 0 {
		offset = 0
	}

	// Load all favorites for this user ordered by recency.
	var favorites []model.UserSessionFavorite
	if err := store.DB.Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&favorites).Error; err != nil {
		return nil, err
	}
	if len(favorites) == 0 {
		return &FavoriteSessionListResp{List: []FavoriteSessionItem{}}, nil
	}

	favOrder := make(map[string]time.Time, len(favorites))
	sessionIDs := make([]string, 0, len(favorites))
	for _, f := range favorites {
		favOrder[f.SessionID] = f.CreatedAt
		sessionIDs = append(sessionIDs, f.SessionID)
	}

	// Load the session_member rows for only these sessions using the existing helper.
	var members []model.SessionMember
	if err := store.DB.Where("member_id = ? AND member_type = 1 AND session_id IN ?", userID, sessionIDs).
		Find(&members).Error; err != nil {
		return nil, err
	}

	// Resolve full session items (title JOIN, peer info, etc.) via shared helper.
	items, err := buildSessionItems(userID, members)
	if err != nil {
		return nil, err
	}

	// Map to favorites response, then sort by favorited_at DESC (matches DB order).
	result := make([]FavoriteSessionItem, 0, len(items))
	for _, item := range items {
		result = append(result, FavoriteSessionItem{
			SessionID:   item.SessionID,
			SessionType: item.SessionType,
			Title:       item.Title,
			Peer:        item.Peer,
			LastMsg:     item.LastMsg,
			FavoritedAt: favOrder[item.SessionID].Unix(),
		})
	}

	sort.SliceStable(result, func(i, j int) bool {
		return result[i].FavoritedAt > result[j].FavoritedAt
	})

	// Paginate in-memory (same pattern as SessionSearch).
	total := len(result)
	if offset >= total {
		return &FavoriteSessionListResp{List: []FavoriteSessionItem{}}, nil
	}
	end := offset + limit
	hasMore := end < total
	if end > total {
		end = total
	}
	return &FavoriteSessionListResp{HasMore: hasMore, List: result[offset:end]}, nil
}

// ListFavoriteSessionsForAgent returns a flattened list suitable for the WS agent tool,
// optionally filtered by keyword. Title matching reuses the existing session search scorer.
func ListFavoriteSessionsForAgent(ownerID int64, keyword string, limit, offset int) (*SessionSearchResp, error) {
	if limit <= 0 {
		limit = sessionSearchDefaultLimit
	}

	// Load all favorites without pagination for in-memory keyword filtering.
	resp, err := ListFavoriteSessions(ownerID, math.MaxInt32, 0)
	if err != nil {
		return nil, err
	}

	items := make([]SessionSearchItem, 0, len(resp.List))
	for _, f := range resp.List {
		items = append(items, SessionSearchItem{
			SessionID: f.SessionID,
			Title:     f.Title,
		})
	}

	if kw := strings.TrimSpace(keyword); kw != "" {
		searchKeyword := buildSessionSearchKeyword(kw)
		matched := make([]SessionSearchItem, 0, len(items))
		for _, item := range items {
			if sessionSearchMatchesKeyword(item.SessionID, item.Title, searchKeyword) {
				matched = append(matched, item)
			}
		}
		items = rankSessionSearchItems(matched, searchKeyword)
	}

	return buildSessionSearchResp(items, limit, offset), nil
}

func isSessionFavDuplicateKey(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	// SQLite (tests)
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "duplicate key")
}

// GetFavoriteSessionIDs returns all favorited session IDs for the user.
// Used by the frontend to bulk-load favorite state at startup.
func GetFavoriteSessionIDs(userID int64) ([]string, error) {
	var rows []model.UserSessionFavorite
	if err := store.DB.Select("session_id").
		Where("user_id = ?", userID).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.SessionID)
	}
	return ids, nil
}
