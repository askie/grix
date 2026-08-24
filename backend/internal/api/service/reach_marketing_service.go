package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ReachAudience 营销受众筛选。活跃度以 users.updated_at 为近似口径。
type ReachAudience struct {
	Region           string   `json:"region,omitempty"`
	ActiveWithinDay  int      `json:"active_within_days,omitempty"`
	InactiveDays     int      `json:"inactive_days,omitempty"`     // 超过 N 天未活跃（沉睡老用户）
	RegisteredBefore string   `json:"registered_before,omitempty"` // YYYY-MM-DD，注册早于该日
	HasEmail         bool     `json:"has_email,omitempty"`
	HasPhone         bool     `json:"has_phone,omitempty"`
	TestUserIDs      []string `json:"test_user_ids,omitempty"`
}

// applyReachAudienceFilter 把受众条件叠加到活跃用户查询上；执行与人数预估共用同一口径。
func applyReachAudienceFilter(q *gorm.DB, audience *ReachAudience) (*gorm.DB, error) {
	if audience == nil {
		return q, nil
	}
	now := time.Now().UTC()
	if audience.Region != "" {
		q = q.Where("region = ?", audience.Region)
	}
	if audience.ActiveWithinDay > 0 {
		q = q.Where("updated_at >= ?", now.AddDate(0, 0, -audience.ActiveWithinDay))
	}
	if audience.InactiveDays > 0 {
		q = q.Where("updated_at < ?", now.AddDate(0, 0, -audience.InactiveDays))
	}
	if s := strings.TrimSpace(audience.RegisteredBefore); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return nil, fmt.Errorf("registered_before must be YYYY-MM-DD")
		}
		q = q.Where("created_at < ?", t)
	}
	if audience.HasEmail {
		q = q.Where("email <> ''")
	}
	if audience.HasPhone {
		q = q.Where("(COALESCE(phone_cipher, '') <> '' OR COALESCE(phone_e164, '') <> '')")
	}
	return q, nil
}

// CountReachAudience 预估受众人数（不含客服号与退订判断，仅按筛选条件）。
func CountReachAudience(audience *ReachAudience) (int64, error) {
	q := store.DB.Model(&model.User{}).Where("status = ?", model.UserStatusActive)
	q, err := applyReachAudienceFilter(q, audience)
	if err != nil {
		return 0, err
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

const reachSMSSendInterval = 200 * time.Millisecond

const reachTestUserIDLimit = 50

// parseReachTestUserIDs returns the parsed test-send target IDs, or an error
// if any entry is not a valid positive integer or the list exceeds the limit.
func parseReachTestUserIDs(audience *ReachAudience) ([]int64, error) {
	if audience == nil || len(audience.TestUserIDs) == 0 {
		return nil, nil
	}
	if len(audience.TestUserIDs) > reachTestUserIDLimit {
		return nil, fmt.Errorf("test_user_ids exceeds limit of %d", reachTestUserIDLimit)
	}
	ids := make([]int64, 0, len(audience.TestUserIDs))
	for _, s := range audience.TestUserIDs {
		id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("invalid test user id: %q", s)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

type CreateMarketingTaskReq struct {
	TemplateID  int64          `json:"template_id,string"`
	Channels    []string       `json:"channels"`
	Region      string         `json:"region"`
	Audience    *ReachAudience `json:"audience,omitempty"`
	ScheduledAt *time.Time     `json:"scheduled_at,omitempty"`
}

func CreateMarketingReachTask(ctx context.Context, req CreateMarketingTaskReq, adminID int64) (*model.ReachTask, error) {
	var tpl model.ReachTemplate
	if err := store.DB.Where("id = ?", req.TemplateID).First(&tpl).Error; err != nil {
		return nil, err
	}
	if _, err := parseReachTestUserIDs(req.Audience); err != nil {
		return nil, err
	}
	if _, err := applyReachAudienceFilter(store.DB, req.Audience); err != nil {
		return nil, err
	}

	channelsJSON, _ := json.Marshal(req.Channels)
	audienceJSON, _ := json.Marshal(req.Audience)

	status := model.ReachStatusSending
	if req.ScheduledAt != nil && req.ScheduledAt.After(time.Now().UTC()) {
		status = model.ReachStatusScheduled
	}

	task := model.ReachTask{
		ID:          snowflake.GenID(),
		Kind:        model.ReachKindMarketing,
		TemplateID:  req.TemplateID,
		Channels:    channelsJSON,
		Audience:    audienceJSON,
		Status:      status,
		ScheduledAt: req.ScheduledAt,
		Region:      req.Region,
		CreatedBy:   adminID,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := store.DB.Create(&task).Error; err != nil {
		return nil, err
	}

	if status == model.ReachStatusSending {
		go executeMarketingTask(ctx, task.ID, tpl, req.Channels, req.Audience)
	}
	return &task, nil
}

func executeMarketingTask(ctx context.Context, taskID int64, tpl model.ReachTemplate, channels []string, audience *ReachAudience) {
	settings, err := systemsetting.GetAuthSettings()
	if err != nil {
		logger.L.Warnf("reach marketing: load auth settings err=%v, task=%d", err, taskID)
		updateMarketingStats(taskID, 0, 0, model.ReachStatusFailed)
		return
	}
	customerUserID := settings.AutoAddCustomerUserID
	if customerUserID <= 0 {
		logger.L.Warnf("reach marketing: no customer account configured, task=%d", taskID)
		updateMarketingStats(taskID, 0, 0, model.ReachStatusFailed)
		return
	}

	channelSet := make(map[string]bool)
	for _, ch := range channels {
		channelSet[ch] = true
	}

	if testIDs, _ := parseReachTestUserIDs(audience); len(testIDs) > 0 {
		executeMarketingTestSend(ctx, taskID, customerUserID, tpl, channelSet, testIDs)
		return
	}

	var lastID int64
	sent, skipped := 0, 0
	for {
		if status, err := loadReachTaskStatus(taskID); err == nil {
			if status == model.ReachStatusPaused || status == model.ReachStatusCancelled {
				updateMarketingStats(taskID, sent, skipped, status)
				return
			}
		}

		q := store.DB.
			Select(reachMarketingUserColumns).
			Where("status = ? AND id > ?", model.UserStatusActive, lastID)
		q, err := applyReachAudienceFilter(q, audience)
		if err != nil {
			logger.L.Warnf("reach marketing: invalid audience task=%d err=%v", taskID, err)
			updateMarketingStats(taskID, sent, skipped, model.ReachStatusFailed)
			return
		}

		var users []model.User
		if err := q.Order("id ASC").
			Limit(reachAudienceBatchSize).
			Find(&users).Error; err != nil {
			logger.L.Warnf("reach marketing: scan users err=%v", err)
			break
		}
		if len(users) == 0 {
			break
		}
		for _, u := range users {
			lastID = u.ID
			if u.ID == customerUserID {
				skipped++
				continue
			}
			if !IsUserSubscribedForReach(u.ID) {
				skipped++
				continue
			}
			if deliverMarketingToUser(ctx, taskID, customerUserID, u, tpl, channelSet) {
				sent++
			} else {
				skipped++
			}
		}
		if len(users) < reachAudienceBatchSize {
			break
		}
	}
	updateMarketingStats(taskID, sent, skipped, model.ReachStatusSent)
}

// executeMarketingTestSend delivers the template only to the explicitly listed
// user IDs. Subscription preferences are bypassed on purpose: a test send is an
// admin explicitly targeting known accounts to preview the content.
func executeMarketingTestSend(ctx context.Context, taskID, customerUserID int64, tpl model.ReachTemplate, channels map[string]bool, userIDs []int64) {
	var users []model.User
	if err := store.DB.
		Select(reachMarketingUserColumns).
		Where("status = ? AND id IN ?", model.UserStatusActive, userIDs).
		Find(&users).Error; err != nil {
		logger.L.Warnf("reach marketing: test send load users task=%d err=%v", taskID, err)
		updateMarketingStats(taskID, 0, 0, model.ReachStatusFailed)
		return
	}

	sent, skipped := 0, len(userIDs)-len(users)
	for _, u := range users {
		if u.ID == customerUserID {
			skipped++
			continue
		}
		if deliverMarketingToUser(ctx, taskID, customerUserID, u, tpl, channels) {
			sent++
		} else {
			skipped++
		}
	}
	updateMarketingStats(taskID, sent, skipped, model.ReachStatusSent)
}

func updateMarketingStats(taskID int64, sent, skipped int, status string) {
	statsJSON, _ := json.Marshal(map[string]int{"sent": sent, "skipped": skipped})
	// JSONB 列需要 string，[]byte 会被 pgx 简单协议当 bytea 处理（SQLSTATE 22P02）
	if err := store.DB.Model(&model.ReachTask{}).Where("id = ?", taskID).
		Updates(map[string]any{"stats": string(statsJSON), "updated_at": time.Now().UTC(), "status": status}).Error; err != nil {
		logger.L.Errorf("reach marketing: update final status failed task=%d status=%s err=%v", taskID, status, err)
		return
	}
	logger.L.Infof("reach marketing: task=%d status=%s sent=%d skipped=%d", taskID, status, sent, skipped)
}

const reachMarketingUserColumns = "id, email, region, phone_cipher, phone_e164, phone_country"

func deliverMarketingToUser(ctx context.Context, taskID, customerUserID int64, user model.User, tpl model.ReachTemplate, channels map[string]bool) bool {
	delivered := false

	if channels[model.ReachChannelInApp] && tpl.InAppBody != "" {
		logRow := model.ReachSendLog{
			ID: snowflake.GenID(), TaskID: taskID, UserID: user.ID,
			Channel: model.ReachChannelInApp, Status: model.ReachSendStatusPending,
			Region: user.Region,
		}
		res := store.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&logRow)
		if res.Error == nil && res.RowsAffected > 0 {
			msg, err := WriteMarketingSystemMessage(customerUserID, user.ID, tpl.Title, tpl.InAppBody)
			if err != nil {
				logger.L.Warnf("reach marketing: in-app user=%d err=%v", user.ID, err)
				store.DB.Model(&model.ReachSendLog{}).Where("id = ?", logRow.ID).
					Updates(map[string]any{"status": model.ReachSendStatusFailed, "error": err.Error()})
			} else {
				payload := protocol.PushMsgPayload{
					InboxSeq: msg.InboxSeq, MsgID: msg.MsgID, SessionID: msg.SessionID,
					SessionType: 1, SenderID: customerUserID, SenderType: 3, MsgType: 3,
					Content: msg.Content, CreatedAt: msg.CreatedAt.UnixMilli(),
				}
				if hasOnlineRealtimeRoute(user.ID) {
					pushRealtimeEvent(user.ID, protocol.CmdPushMsg, payload)
				} else {
					pushOfflineEvent(user.ID, protocol.CmdPushMsg, payload)
				}
				store.DB.Model(&model.ReachSendLog{}).Where("id = ?", logRow.ID).
					Update("status", model.ReachSendStatusSent)
				delivered = true
			}
		}
	}

	if channels[model.ReachChannelPush] {
		logRow := model.ReachSendLog{
			ID: snowflake.GenID(), TaskID: taskID, UserID: user.ID,
			Channel: model.ReachChannelPush, Status: model.ReachSendStatusPending,
			Region: user.Region,
		}
		res := store.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&logRow)
		if res.Error == nil && res.RowsAffected > 0 {
			pushText := tpl.PushBody
			if pushText == "" {
				pushText = tpl.Title
			}
			pushOfflineEvent(user.ID, protocol.CmdPushMsg, protocol.PushMsgPayload{
				Content: pushText, SenderType: 3, MsgType: 3,
			})
			store.DB.Model(&model.ReachSendLog{}).Where("id = ?", logRow.ID).
				Update("status", model.ReachSendStatusSent)
			delivered = true
		}
	}

	if channels[model.ReachChannelEmail] && user.Email != "" && tpl.EmailHTML != "" {
		logRow := model.ReachSendLog{
			ID: snowflake.GenID(), TaskID: taskID, UserID: user.ID,
			Channel: model.ReachChannelEmail, Status: model.ReachSendStatusPending,
			Region: user.Region,
		}
		res := store.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&logRow)
		if res.Error == nil && res.RowsAffected > 0 {
			trackedHTML := InjectEmailTracking(tpl.EmailHTML, logRow.ID)
			if err := SendReachEmail(user.Email, tpl.Title, trackedHTML); err != nil {
				logger.L.Warnf("reach marketing: email user=%d err=%v", user.ID, err)
				store.DB.Model(&model.ReachSendLog{}).Where("id = ?", logRow.ID).
					Updates(map[string]any{"status": model.ReachSendStatusFailed, "error": err.Error()})
			} else {
				store.DB.Model(&model.ReachSendLog{}).Where("id = ?", logRow.ID).
					Update("status", model.ReachSendStatusSent)
				delivered = true
			}
		}
	}

	if channels[model.ReachChannelSMS] && tpl.SmsBody != "" {
		phone, countryCode, err := directReachPhone(user)
		if err == nil && phone != "" {
			logRow := model.ReachSendLog{
				ID: snowflake.GenID(), TaskID: taskID, UserID: user.ID,
				Channel: model.ReachChannelSMS, Status: model.ReachSendStatusPending,
				Region: user.Region,
			}
			res := store.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&logRow)
			if res.Error == nil && res.RowsAffected > 0 {
				err := sendDirectReachSMS(ctx, ReachSMSRequest{
					UserID: user.ID, PhoneE164: phone, CountryCode: countryCode,
					Region: user.Region, Text: tpl.SmsBody,
				})
				if err != nil {
					logger.L.Warnf("reach marketing: sms user=%d err=%v", user.ID, err)
					store.DB.Model(&model.ReachSendLog{}).Where("id = ?", logRow.ID).
						Updates(map[string]any{"status": model.ReachSendStatusFailed, "error": err.Error()})
				} else {
					store.DB.Model(&model.ReachSendLog{}).Where("id = ?", logRow.ID).
						Update("status", model.ReachSendStatusSent)
					delivered = true
				}
				time.Sleep(reachSMSSendInterval)
			}
		} else if err != nil {
			logger.L.Warnf("reach marketing: sms phone user=%d err=%v", user.ID, err)
		}
	}

	return delivered
}
