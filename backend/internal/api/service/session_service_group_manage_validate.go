package service

import (
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

func normalizeGroupMembers(operatorID int64, memberIDs []int64, memberTypes []int16) ([]int64, []int16, error) {
	memberIDs, memberTypes, err := normalizeMemberTargets(memberIDs, memberTypes)
	if err != nil {
		return nil, nil, err
	}

	normalizedIDs := make([]int64, 0, len(memberIDs))
	normalizedTypes := make([]int16, 0, len(memberIDs))
	for i, memberID := range memberIDs {
		memberType := memberTypes[i]
		if memberType == 1 && memberID == operatorID {
			continue
		}
		normalizedIDs = append(normalizedIDs, memberID)
		normalizedTypes = append(normalizedTypes, memberType)
	}

	return normalizedIDs, normalizedTypes, nil
}

func normalizeMemberTargets(memberIDs []int64, memberTypes []int16) ([]int64, []int16, error) {
	if len(memberTypes) != 0 && len(memberTypes) != len(memberIDs) {
		return nil, nil, ErrMemberTypesMismatch
	}

	seen := make(map[memberIdentity]struct{}, len(memberIDs))
	normalizedIDs := make([]int64, 0, len(memberIDs))
	normalizedTypes := make([]int16, 0, len(memberIDs))

	for i, mid := range memberIDs {
		if mid <= 0 {
			return nil, nil, ErrInvalidMemberID
		}

		mt := int16(1)
		if len(memberTypes) > 0 {
			mt = memberTypes[i]
		}
		if mt != 1 && mt != 2 {
			return nil, nil, ErrInvalidMemberType
		}
		key := memberIdentity{
			MemberID:   mid,
			MemberType: mt,
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalizedIDs = append(normalizedIDs, mid)
		normalizedTypes = append(normalizedTypes, mt)
	}

	return normalizedIDs, normalizedTypes, nil
}

func validateGroupMemberTargets(operatorID int64, memberIDs []int64, memberTypes []int16) error {
	if len(memberIDs) == 0 {
		return nil
	}

	humanIDs := make([]int64, 0, len(memberIDs))
	agentIDs := make([]int64, 0, len(memberIDs))
	for i, mid := range memberIDs {
		if memberTypes[i] == 2 {
			agentIDs = append(agentIDs, mid)
		} else {
			humanIDs = append(humanIDs, mid)
		}
	}

	if len(humanIDs) > 0 {
		var users []model.User
		if err := store.DB.Select("id").
			Where("id IN ?", humanIDs).
			Find(&users).Error; err != nil {
			return err
		}
		userIDSet := make(map[int64]struct{}, len(users))
		for _, u := range users {
			userIDSet[u.ID] = struct{}{}
		}
		for _, uid := range humanIDs {
			if _, ok := userIDSet[uid]; !ok {
				return ErrMemberUserNotFound
			}
		}

		var friends []model.Friend
		if err := store.DB.Select("friend_id").
			Where("user_id = ? AND friend_id IN ?", operatorID, humanIDs).
			Find(&friends).Error; err != nil {
			return err
		}
		friendIDSet := make(map[int64]struct{}, len(friends))
		for _, f := range friends {
			friendIDSet[f.FriendID] = struct{}{}
		}
		for _, uid := range humanIDs {
			if _, ok := friendIDSet[uid]; !ok {
				return ErrMemberNotFriend
			}
		}
	}

	if len(agentIDs) > 0 {
		var agents []model.Agent
		if err := store.DB.Select("id", "owner_id", "status", "media_capability").
			Where("id IN ?", agentIDs).
			Find(&agents).Error; err != nil {
			return err
		}
		agentMap := make(map[int64]model.Agent, len(agents))
		for _, a := range agents {
			agentMap[a.ID] = a
		}
		for _, aid := range agentIDs {
			agent, ok := agentMap[aid]
			if !ok {
				return ErrMemberAgentNotFound
			}
			if agent.OwnerID != operatorID {
				return ErrMemberAgentNotOwned
			}
			if agent.Status != 1 {
				return ErrMemberAgentUnavailable
			}
			// 语音 agent 仅用于实时语音通话，禁止加入群聊
			if agent.MediaCapability == model.AgentMediaCapabilityVoice {
				return ErrMemberAgentVoiceNotAllowed
			}
		}
	}

	return nil
}

func collectHumanMemberIDs(memberIDs []int64, memberTypes []int16) []int64 {
	if len(memberIDs) == 0 {
		return nil
	}

	userIDs := make([]int64, 0, len(memberIDs))
	for i, memberID := range memberIDs {
		if memberID <= 0 {
			continue
		}
		memberType := int16(1)
		if i < len(memberTypes) && memberTypes[i] == 2 {
			memberType = 2
		}
		if memberType != 1 {
			continue
		}
		userIDs = append(userIDs, memberID)
	}
	return uniquePositiveInt64s(userIDs)
}

func collectHumanMemberIDsFromSessionMembers(members []model.SessionMember) []int64 {
	if len(members) == 0 {
		return nil
	}

	userIDs := make([]int64, 0, len(members))
	for _, member := range members {
		if member.MemberType != 1 || member.MemberID <= 0 {
			continue
		}
		userIDs = append(userIDs, member.MemberID)
	}
	return uniquePositiveInt64s(userIDs)
}

func validateHumanTargetsAllowGroupInvite(userIDs []int64) error {
	hasBlockedTarget, err := hasAnyUserDisallowGroupInvite(userIDs)
	if err != nil {
		return err
	}
	if hasBlockedTarget {
		return ErrSessionTargetGroupInviteRejected
	}
	return nil
}

func uniqueMemberTypes(memberTypes []int16) []int16 {
	if len(memberTypes) == 0 {
		return []int16{1}
	}
	seen := make(map[int16]struct{}, len(memberTypes))
	result := make([]int16, 0, len(memberTypes))
	for _, mt := range memberTypes {
		if _, ok := seen[mt]; ok {
			continue
		}
		seen[mt] = struct{}{}
		result = append(result, mt)
	}
	return result
}
