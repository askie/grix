package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

func validateDirectSessionPeer(userID, peerID int64, peerType int16) error {
	if peerID <= 0 {
		return ErrInvalidMemberID
	}
	if peerType == 2 {
		return validateDirectSessionAgentPeer(userID, peerID)
	}
	return validateDirectSessionHumanPeer(userID, peerID)
}

func validateDirectSessionHumanPeer(userID, peerID int64) error {
	if err := EnsureUserNotBlocked(peerID, userID); err != nil {
		if errors.Is(err, ErrUserBlockedByPeer) {
			return ErrMemberNotFriend
		}
		return err
	}

	var count int64
	if err := store.DB.Model(&model.Friend{}).
		Where("user_id = ? AND friend_id = ?", userID, peerID).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrMemberNotFriend
	}
	return nil
}

func validateDirectSessionAgentPeer(userID, agentID int64) error {
	var agent model.Agent
	if err := store.DB.Select("id", "owner_id", "status").
		Where("id = ?", agentID).
		First(&agent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrMemberAgentNotFound
		}
		return err
	}
	if agent.Status != 1 {
		return ErrMemberAgentUnavailable
	}
	// 主人本人，或被该 agent 共享的账户，都可建私聊（agent 共享）。
	if agent.OwnerID != userID {
		shared, err := hasActiveAgentShare(agentID, userID)
		if err != nil {
			return err
		}
		if !shared {
			return ErrMemberAgentNotOwned
		}
	}
	return nil
}

func SessionCreate(userID int64, peerID int64, peerType int16) (*CreateSessionResp, error) {
	if peerType != 2 {
		peerType = 1
	}
	if err := validateDirectSessionPeer(userID, peerID, peerType); err != nil {
		return nil, err
	}
	directKey := buildDirectKey(userID, peerID, peerType)
	sessionID := newSessionID()
	now := time.Now()
	directKeyValue := directKey
	session := model.Session{
		DirectKey:   &directKeyValue,
		SessionID:   sessionID,
		OwnerID:     userID,
		SessionType: 1,
	}
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&session).Error; err != nil {
			return err
		}

		members := []model.SessionMember{
			{SessionID: sessionID, MemberID: userID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
			{SessionID: sessionID, MemberID: peerID, MemberType: peerType, Role: 1, LastActiveAt: now, JoinedAt: now},
		}
		if err := tx.Create(&members).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}

	ensureAutoDelegateForPrivateSession(sessionID, userID, peerID, peerType)

	return &CreateSessionResp{SessionID: sessionID, IsNew: true}, nil
}

func SessionCreateForAgentBinding(ownerID int64, agentID int64, directKeySuffix string, title string) (*CreateSessionResp, error) {
	if err := validateDirectSessionAgentPeer(ownerID, agentID); err != nil {
		return nil, err
	}
	suffix := strings.TrimSpace(directKeySuffix)
	if suffix == "" {
		suffix = newSessionID()
	}
	sum := sha256.Sum256([]byte(suffix))
	directKey := fmt.Sprintf("agent-session:%d:%d:%s", ownerID, agentID, hex.EncodeToString(sum[:])[:32])

	var existing model.Session
	if err := store.DB.
		Where("direct_key = ? AND session_type = 1 AND is_deleted = false", directKey).
		Order("updated_at DESC").
		First(&existing).Error; err == nil {
		return &CreateSessionResp{SessionID: existing.SessionID, IsNew: false}, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	sessionID := newSessionID()
	now := time.Now()
	directKeyValue := directKey
	customTitle, err := normalizeCustomSessionTitle(title)
	if err != nil {
		return nil, err
	}
	session := model.Session{
		DirectKey:      &directKeyValue,
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    1,
		LastMsgSummary: customTitle,
	}
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&session).Error; err != nil {
			return err
		}
		members := []model.SessionMember{
			{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, CustomTitle: customTitle, LastActiveAt: now, JoinedAt: now},
			{SessionID: sessionID, MemberID: agentID, MemberType: 2, Role: 1, LastActiveAt: now, JoinedAt: now},
		}
		if err := tx.Create(&members).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return &CreateSessionResp{SessionID: sessionID, IsNew: true}, nil
}

// SessionCreateForAgentDispatch 为一次 dispatch_agent 派发新建一条独立的 owner↔agent 私聊。
// 每次调用都生成全新会话（唯一 directKey），不复用任何历史会话；title 写入会话自定义标题。
func SessionCreateForAgentDispatch(ownerID, agentID int64, title string) (*CreateSessionResp, error) {
	if err := validateDirectSessionAgentPeer(ownerID, agentID); err != nil {
		return nil, err
	}
	customTitle, err := normalizeCustomSessionTitle(title)
	if err != nil {
		return nil, err
	}
	sessionID := newSessionID()
	// 唯一 directKey：保证每次派发都是独立新会话，不与历史会话去重复用。
	directKey := fmt.Sprintf("agent-dispatch:%d:%d:%s", ownerID, agentID, sessionID)
	now := time.Now()
	directKeyValue := directKey
	session := model.Session{
		DirectKey:      &directKeyValue,
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    1,
		LastMsgSummary: customTitle,
	}
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&session).Error; err != nil {
			return err
		}
		members := []model.SessionMember{
			{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, CustomTitle: customTitle, LastActiveAt: now, JoinedAt: now},
			{SessionID: sessionID, MemberID: agentID, MemberType: 2, Role: 1, LastActiveAt: now, JoinedAt: now},
		}
		if err := tx.Create(&members).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return &CreateSessionResp{SessionID: sessionID, IsNew: true}, nil
}

func SessionOpenLatest(userID int64, peerID int64, peerType int16) (*CreateSessionResp, error) {
	if peerType != 2 {
		peerType = 1
	}
	if err := validateDirectSessionPeer(userID, peerID, peerType); err != nil {
		return nil, err
	}
	directKey := buildDirectKey(userID, peerID, peerType)

	var existing model.Session
	if err := store.DB.
		Where("direct_key = ? AND session_type = 1 AND is_deleted = false", directKey).
		Order("updated_at DESC").
		First(&existing).Error; err == nil {
		ensureAutoDelegateForPrivateSession(existing.SessionID, userID, peerID, peerType)
		return &CreateSessionResp{
			SessionID: existing.SessionID,
			IsNew:     false,
		}, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	return SessionCreate(userID, peerID, peerType)
}

func SessionCreateGroup(userID int64, name string, memberIDs []int64, memberTypes []int16) (*CreateSessionResp, error) {
	memberIDs, memberTypes, err := normalizeGroupMembers(userID, memberIDs, memberTypes)
	if err != nil {
		return nil, err
	}
	if err := validateGroupMemberTargets(userID, memberIDs, memberTypes); err != nil {
		return nil, err
	}
	if err := validateHumanTargetsAllowGroupInvite(collectHumanMemberIDs(memberIDs, memberTypes)); err != nil {
		return nil, err
	}

	sessionID := newSessionID()
	now := time.Now()

	session := model.Session{
		SessionID:         sessionID,
		OwnerID:           userID,
		SessionType:       2,
		GroupName:         name,
		AllowMemberInvite: true,
		LastMsgSummary:    name,
	}
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&session).Error; err != nil {
			return err
		}

		members := []model.SessionMember{
			{SessionID: sessionID, MemberID: userID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
		}
		for i, mid := range memberIDs {
			mt := memberTypes[i]
			members = append(members, model.SessionMember{
				SessionID: sessionID, MemberID: mid, MemberType: mt, Role: 1, LastActiveAt: now, JoinedAt: now,
			})
		}
		if err := tx.Create(&members).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}
	ensureAutoDelegateForGroupSessionMembers(sessionID, userID, memberIDs, memberTypes)
	invitedHumanMemberIDs := make([]int64, 0, len(memberIDs))
	for i, mid := range memberIDs {
		if memberTypes[i] != 1 || mid <= 0 {
			continue
		}
		invitedHumanMemberIDs = append(invitedHumanMemberIDs, mid)
	}
	if len(invitedHumanMemberIDs) > 0 {
		notifySessionMemberChanged(
			sessionID,
			"add",
			userID,
			invitedHumanMemberIDs,
			sessionMemberChangedNotifyMeta{},
		)
		sessionMemberAddedOfflinePushRunner(
			sessionID,
			userID,
			session.GroupName,
			invitedHumanMemberIDs,
		)
	}

	return &CreateSessionResp{SessionID: sessionID, IsNew: true}, nil
}
