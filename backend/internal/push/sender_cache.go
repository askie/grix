package push

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

const senderInfoCacheTTL = 5 * time.Minute

type senderCacheKey struct {
	recipientID int64
	senderID    int64
	senderType  int16
}

type senderDisplayInfo struct {
	Name      string
	AvatarURL string
	Initial   string
	loadedAt  time.Time
}

type senderInfoCache struct {
	mu    sync.RWMutex
	items map[senderCacheKey]*senderDisplayInfo
}

var globalSenderCache = &senderInfoCache{
	items: make(map[senderCacheKey]*senderDisplayInfo),
}

func (c *senderInfoCache) get(key senderCacheKey) (*senderDisplayInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.items[key]
	if !ok || time.Since(entry.loadedAt) > senderInfoCacheTTL {
		return nil, false
	}
	return entry, true
}

func (c *senderInfoCache) set(key senderCacheKey, info *senderDisplayInfo) {
	c.mu.Lock()
	c.items[key] = info
	c.mu.Unlock()
}

func resolveSenderDisplay(ctx context.Context, recipientID int64, senderID int64, senderType int16) *senderDisplayInfo {
	key := senderCacheKey{recipientID: recipientID, senderID: senderID, senderType: senderType}
	if info, ok := globalSenderCache.get(key); ok {
		return info
	}

	info := loadSenderDisplayFromDB(ctx, recipientID, senderID, senderType)
	globalSenderCache.set(key, info)
	return info
}

func loadSenderDisplayFromDB(ctx context.Context, recipientID int64, senderID int64, senderType int16) *senderDisplayInfo {
	if senderType == 2 {
		return loadAgentDisplay(senderID)
	}
	return loadHumanDisplay(ctx, recipientID, senderID)
}

func loadAgentDisplay(senderID int64) *senderDisplayInfo {
	info := &senderDisplayInfo{
		Name:      defaultPushSenderTitle,
		loadedAt:  time.Now(),
	}
	var agent model.Agent
	if err := store.DB.Select("agent_name, avatar_url").
		Where("id = ? AND status = 1", senderID).
		Take(&agent).Error; err != nil {
		if !isRecordNotFound(err) {
			logger.L.Warnf("load agent for push failed agent=%d err=%v", senderID, err)
		}
		return info
	}
	if agent.AgentName != "" {
		info.Name = agent.AgentName
	}
	info.AvatarURL = agent.AvatarURL
	info.Initial = firstRune(info.Name)
	return info
}

func loadHumanDisplay(ctx context.Context, recipientID int64, senderID int64) *senderDisplayInfo {
	info := &senderDisplayInfo{
		Name:      defaultPushSenderTitle,
		loadedAt:  time.Now(),
	}

	var sender model.User
	if err := store.DB.WithContext(ctx).
		Select("nickname, avatar_url").
		Where("id = ? AND status = 1", senderID).
		Take(&sender).Error; err != nil {
		if !isRecordNotFound(err) {
			logger.L.Warnf("load sender for push failed sender=%d err=%v", senderID, err)
		}
		return info
	}

	displayName := strings.TrimSpace(sender.Nickname)
	info.AvatarURL = strings.TrimSpace(sender.AvatarURL)

	if recipientID > 0 {
		var friend model.Friend
		if err := store.DB.WithContext(ctx).
			Select("remark_name").
			Where("user_id = ? AND friend_id = ?", recipientID, senderID).
			Take(&friend).Error; err == nil {
			if remark := strings.TrimSpace(friend.RemarkName); remark != "" {
				displayName = remark
			}
		}
	}

	if displayName != "" {
		info.Name = displayName
	}
	info.Initial = firstRune(info.Name)
	return info
}

func firstRune(s string) string {
	r, _ := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError || r == 0 {
		return ""
	}
	return string(r)
}

func isRecordNotFound(err error) bool {
	return err == gorm.ErrRecordNotFound
}
