package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
)

func loadAdminNames(adminIDs map[int64]struct{}) (map[int64]string, error) {
	if len(adminIDs) == 0 {
		return map[int64]string{}, nil
	}
	ids := make([]int64, 0, len(adminIDs))
	for id := range adminIDs {
		ids = append(ids, id)
	}

	var admins []model.AdminUser
	if err := store.DB.Select("id", "nickname", "username").
		Where("id IN ?", ids).
		Find(&admins).Error; err != nil {
		return nil, err
	}

	result := make(map[int64]string, len(admins))
	for _, item := range admins {
		name := strings.TrimSpace(item.Nickname)
		if name == "" {
			name = strings.TrimSpace(item.Username)
		}
		result[item.ID] = name
	}
	return result, nil
}

func parseSnapshot(raw datatypes.JSON) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	result := map[string]any{}
	_ = json.Unmarshal(raw, &result)
	return result
}

func buildReportListReporter(snapshot map[string]any) (string, string) {
	nickname := readString(snapshot["nickname"])
	username := readString(snapshot["username"])
	userID := readString(snapshot["user_id"])
	display := nickname
	if display == "" {
		display = username
	}
	if display == "" {
		display = userID
	}

	info := ""
	if username != "" {
		info = "@" + username
	} else if userID != "" {
		info = "ID " + userID
	}
	return display, info
}

func buildReportListTarget(snapshot map[string]any, targetType int16) (string, string) {
	switch targetType {
	case model.ReportTargetTypeUser:
		nickname := readString(snapshot["nickname"])
		username := readString(snapshot["username"])
		userID := readString(snapshot["user_id"])
		title := nickname
		if title == "" {
			title = username
		}
		if title == "" {
			title = userID
		}
		info := ""
		if username != "" {
			info = "@" + username
		} else if userID != "" {
			info = "ID " + userID
		}
		return title, info
	case model.ReportTargetTypeGroup:
		groupName := readString(snapshot["group_name"])
		sessionID := readString(snapshot["session_id"])
		memberCount := readInt64(snapshot["member_count"])
		title := groupName
		if title == "" {
			title = sessionID
		}
		info := ""
		if memberCount > 0 {
			info = fmt.Sprintf("%d 人", memberCount)
		} else if sessionID != "" {
			info = sessionID
		}
		return title, info
	default:
		return "-", ""
	}
}

func buildReporterView(snapshot map[string]any) ReportPersonView {
	userID := readString(snapshot["user_id"])
	username := readString(snapshot["username"])
	nickname := readString(snapshot["nickname"])
	displayName := nickname
	if displayName == "" {
		displayName = username
	}
	if displayName == "" {
		displayName = userID
	}
	return ReportPersonView{
		UserID:      userID,
		Username:    username,
		Nickname:    nickname,
		AvatarURL:   readString(snapshot["avatar_url"]),
		DisplayName: displayName,
	}
}

func buildTargetView(snapshot map[string]any, targetType int16) ReportTargetView {
	switch targetType {
	case model.ReportTargetTypeUser:
		userID := readString(snapshot["user_id"])
		username := readString(snapshot["username"])
		nickname := readString(snapshot["nickname"])
		title := nickname
		if title == "" {
			title = username
		}
		if title == "" {
			title = userID
		}
		subtitle := ""
		if username != "" {
			subtitle = "@" + username
		}
		return ReportTargetView{
			UserID:    userID,
			Username:  username,
			Title:     title,
			Subtitle:  subtitle,
			AvatarURL: readString(snapshot["avatar_url"]),
		}
	case model.ReportTargetTypeGroup:
		groupName := readString(snapshot["group_name"])
		sessionID := readString(snapshot["session_id"])
		memberCount := readInt64(snapshot["member_count"])
		subtitleParts := make([]string, 0, 2)
		if sessionID != "" {
			subtitleParts = append(subtitleParts, sessionID)
		}
		if memberCount > 0 {
			subtitleParts = append(subtitleParts, fmt.Sprintf("%d 人", memberCount))
		}
		title := groupName
		if title == "" {
			title = sessionID
		}
		return ReportTargetView{
			SessionID:   sessionID,
			Title:       title,
			Subtitle:    strings.Join(subtitleParts, " · "),
			OwnerID:     readString(snapshot["owner_id"]),
			MemberCount: memberCount,
		}
	default:
		return ReportTargetView{}
	}
}

func parseActionLogDetail(raw datatypes.JSON) (string, string) {
	detail := parseSnapshot(raw)
	return reportResolutionActionText(readString(detail["resolution"])), readString(detail["note"])
}

func reportStatusText(status int16) string {
	switch status {
	case model.ReportStatusPending:
		return "待处理"
	case model.ReportStatusReview:
		return "处理中"
	case model.ReportStatusResolved:
		return "已处理"
	default:
		return "未知"
	}
}

func reportResolutionText(resolution int16) string {
	switch resolution {
	case model.ReportResolutionReject:
		return "驳回"
	case model.ReportResolutionBanUser:
		return "封禁用户"
	case model.ReportResolutionBanGroup:
		return "封禁群"
	case model.ReportResolutionNoAction:
		return "无动作结案"
	case model.ReportResolutionDuplicate:
		return "重复举报"
	default:
		return "-"
	}
}

func reportResolutionActionText(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "reject":
		return "驳回"
	case "ban_user":
		return "封禁用户"
	case "ban_group":
		return "封禁群"
	case "no_action":
		return "无动作结案"
	case "duplicate":
		return "重复举报"
	default:
		return "-"
	}
}

func reportReasonText(reason string) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "harassment":
		return "骚扰辱骂"
	case "pornography":
		return "色情低俗"
	case "violence":
		return "暴力威胁"
	case "fraud":
		return "诈骗欺诈"
	case "spam":
		return "垃圾信息"
	case "impersonation":
		return "冒充他人"
	case "illegal":
		return "违法内容"
	case "other":
		return "其他"
	default:
		return reason
	}
}

func reportTargetTypeText(targetType int16) string {
	switch targetType {
	case model.ReportTargetTypeUser:
		return "用户"
	case model.ReportTargetTypeGroup:
		return "群聊"
	default:
		return "未知"
	}
}

func reportActionText(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "resolve":
		return "处理"
	case "assign":
		return "分配"
	case "reopen":
		return "重新打开"
	default:
		return action
	}
}

func readString(value any) string {
	if value == nil {
		return ""
	}

	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case int64:
		return strconv.FormatInt(typed, 10)
	case int:
		return strconv.Itoa(typed)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	}
}

func readInt64(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}
