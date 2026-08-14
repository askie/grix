package handler

import (
	"strings"

	"github.com/askie/grix/backend/internal/pkg/mention"
	"github.com/askie/grix/backend/internal/store"
)

type sessionMentionCandidateRow struct {
	MemberID      int64  `gorm:"column:member_id"`
	MemberType    int16  `gorm:"column:member_type"`
	CustomTitle   string `gorm:"column:custom_title"`
	GroupNickname string `gorm:"column:group_nickname"`
	RemarkName    string `gorm:"column:remark_name"`
	Username      string `gorm:"column:username"`
	Nickname      string `gorm:"column:nickname"`
	AgentName     string `gorm:"column:agent_name"`
}

// ResolveMentionCandidatesForSession loads session members and their aliases
// that can be used to resolve @username/@nickname/@agent_name style mentions.
func ResolveMentionCandidatesForSession(sessionID string, viewerUserID int64) []mention.Candidate {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil
	}

	var rows []sessionMentionCandidateRow
	if err := store.DB.Table("session_members AS sm").
		Select("sm.member_id, sm.member_type, sm.custom_title, sm.group_nickname, fr.remark_name, u.username, u.nickname, a.agent_name").
		Joins("LEFT JOIN users u ON u.id = sm.member_id AND sm.member_type = 1").
		Joins("LEFT JOIN friends fr ON fr.user_id = ? AND fr.friend_id = sm.member_id AND sm.member_type = 1", viewerUserID).
		Joins("LEFT JOIN agents a ON a.id = sm.member_id AND sm.member_type = 2").
		Where("sm.session_id = ? AND sm.member_type IN ?", sid, []int16{1, 2}).
		Scan(&rows).Error; err != nil {
		return nil
	}
	if len(rows) == 0 {
		return nil
	}

	out := make([]mention.Candidate, 0, len(rows))
	for _, row := range rows {
		if row.MemberID <= 0 {
			continue
		}
		aliases := make([]string, 0, 3)
		switch row.MemberType {
		case 1:
			if s := strings.TrimSpace(row.RemarkName); s != "" {
				aliases = append(aliases, s)
			}
			if s := strings.TrimSpace(row.GroupNickname); s != "" {
				aliases = append(aliases, s)
			}
			if s := strings.TrimSpace(row.Username); s != "" {
				aliases = append(aliases, s)
			}
			if s := strings.TrimSpace(row.Nickname); s != "" {
				aliases = append(aliases, s)
			}
		case 2:
			if s := strings.TrimSpace(row.AgentName); s != "" {
				aliases = append(aliases, s)
			}
		default:
			continue
		}
		if s := strings.TrimSpace(row.CustomTitle); s != "" {
			aliases = append(aliases, s)
		}
		out = append(out, mention.Candidate{
			UserID:  row.MemberID,
			Aliases: aliases,
		})
	}
	return out
}
