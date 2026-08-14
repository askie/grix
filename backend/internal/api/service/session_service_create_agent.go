package service

func SessionCreateGroupByAgent(
	ownerID int64,
	agentID int64,
	name string,
	memberIDs []int64,
	memberTypes []int16,
) (*CreateSessionResp, error) {
	memberIDs, memberTypes = appendCurrentAgentToGroupMembers(agentID, memberIDs, memberTypes)
	return SessionCreateGroup(ownerID, name, memberIDs, memberTypes)
}

func appendCurrentAgentToGroupMembers(
	agentID int64,
	memberIDs []int64,
	memberTypes []int16,
) ([]int64, []int16) {
	groupMemberIDs := append([]int64(nil), memberIDs...)

	var groupMemberTypes []int16
	switch {
	case len(memberTypes) == 0 && len(memberIDs) > 0:
		groupMemberTypes = make([]int16, len(memberIDs))
		for i := range groupMemberTypes {
			groupMemberTypes[i] = 1
		}
	default:
		groupMemberTypes = append([]int16(nil), memberTypes...)
	}

	if agentID <= 0 {
		return groupMemberIDs, groupMemberTypes
	}

	groupMemberIDs = append(groupMemberIDs, agentID)
	groupMemberTypes = append(groupMemberTypes, 2)
	return groupMemberIDs, groupMemberTypes
}
