package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"

	"github.com/askie/grix/backend/internal/api/service/identity"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/secretcrypto"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/textutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const DirectReachEventKey = "direct_user_message"

var ErrReachSMSNotConfigured = errors.New("reach sms dispatcher not configured")

type SendDirectUserReachReq struct {
	UserID    int64  `json:"user_id,string"`
	Title     string `json:"title"`
	LongText  string `json:"long_text"`
	ShortText string `json:"short_text"`
	EventKey  string `json:"event_key"`
	DedupKey  string `json:"dedup_key"`
	CreatedBy int64  `json:"-"`
}

type DirectUserReachAttempt struct {
	Channel string `json:"channel"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
	LogID   int64  `json:"log_id,string,omitempty"`
}

type SendDirectUserReachResult struct {
	Task     *model.ReachTask         `json:"task"`
	Channel  string                   `json:"channel"`
	Status   string                   `json:"status"`
	Attempts []DirectUserReachAttempt `json:"attempts"`
}

type ReachSMSRequest struct {
	UserID      int64
	PhoneE164   string
	CountryCode string
	Region      string
	Text        string
}

var sendDirectReachEmail = SendReachEmail

var sendDirectReachSMS = SendReachSMS

// SendDirectUserReach delivers one message to one user through the first
// available successful channel: app/customer-service message, then email, then
// SMS. It always records a reach task and per-attempt send logs for admin
// visibility once the target user exists.
func SendDirectUserReach(ctx context.Context, req SendDirectUserReachReq) (*SendDirectUserReachResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req = normalizeDirectReachReq(req)
	if req.UserID <= 0 {
		return nil, errors.New("user_id required")
	}
	if req.LongText == "" && req.ShortText == "" {
		return nil, errors.New("long_text or short_text required")
	}

	var user model.User
	if err := store.DB.WithContext(ctx).
		Select("id, email, status, region, phone_e164, phone_country, phone_cipher").
		Where("id = ?", req.UserID).
		First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("user not found")
		}
		return nil, err
	}
	if user.Status != model.UserStatusActive {
		return nil, errors.New("user not active")
	}

	task, created, err := createDirectReachTask(ctx, req, user)
	if err != nil {
		return nil, err
	}
	if !created {
		return directReachResultFromExistingTask(ctx, task)
	}

	result := &SendDirectUserReachResult{
		Task:     task,
		Status:   model.ReachStatusFailed,
		Attempts: make([]DirectUserReachAttempt, 0, 3),
	}
	attempted := make([]string, 0, 3)
	try := func(channel string, available bool, deliver func(model.ReachSendLog) error) bool {
		if !available {
			result.Attempts = append(result.Attempts, DirectUserReachAttempt{
				Channel: channel,
				Status:  model.ReachSendStatusSkipped,
				Error:   "channel unavailable",
			})
			return false
		}
		attempted = append(attempted, channel)
		logRow, err := createDirectReachSendLog(ctx, task.ID, user, channel)
		if err != nil {
			result.Attempts = append(result.Attempts, DirectUserReachAttempt{
				Channel: channel,
				Status:  model.ReachSendStatusFailed,
				Error:   err.Error(),
			})
			return false
		}
		attempt := DirectUserReachAttempt{Channel: channel, LogID: logRow.ID}
		if err := deliver(logRow); err != nil {
			attempt.Status = model.ReachSendStatusFailed
			attempt.Error = err.Error()
			markReachSendLog(ctx, logRow.ID, model.ReachSendStatusFailed, err.Error())
			result.Attempts = append(result.Attempts, attempt)
			return false
		}
		attempt.Status = model.ReachSendStatusSent
		markReachSendLog(ctx, logRow.ID, model.ReachSendStatusSent, "")
		result.Attempts = append(result.Attempts, attempt)
		result.Channel = channel
		result.Status = model.ReachStatusSent
		return true
	}

	settings, settingsErr := systemsetting.GetAuthSettings()
	customerUserID := settings.AutoAddCustomerUserID
	if settingsErr != nil {
		result.Attempts = append(result.Attempts, DirectUserReachAttempt{
			Channel: model.ReachChannelInApp,
			Status:  model.ReachSendStatusSkipped,
			Error:   settingsErr.Error(),
		})
	} else {
		appAvailable := customerUserID > 0 && hasDirectReachAppChannel(ctx, user.ID)
		if try(model.ReachChannelInApp, appAvailable, func(model.ReachSendLog) error {
			return deliverDirectReachInApp(ctx, customerUserID, user.ID, req)
		}) {
			return finishDirectReachTask(ctx, task.ID, attempted, result)
		}
	}

	if try(model.ReachChannelEmail, strings.TrimSpace(user.Email) != "", func(logRow model.ReachSendLog) error {
		subject, body := directReachEmailContent(req)
		return sendDirectReachEmail(strings.TrimSpace(user.Email), subject, InjectEmailTracking(body, logRow.ID))
	}) {
		return finishDirectReachTask(ctx, task.ID, attempted, result)
	}

	phone, countryCode, phoneErr := directReachPhone(user)
	smsAvailable := phoneErr == nil && phone != ""
	if phoneErr != nil {
		result.Attempts = append(result.Attempts, DirectUserReachAttempt{
			Channel: model.ReachChannelSMS,
			Status:  model.ReachSendStatusSkipped,
			Error:   phoneErr.Error(),
		})
	} else if try(model.ReachChannelSMS, smsAvailable, func(model.ReachSendLog) error {
		return sendDirectReachSMS(ctx, ReachSMSRequest{
			UserID:      user.ID,
			PhoneE164:   phone,
			CountryCode: countryCode,
			Region:      user.Region,
			Text:        req.ShortText,
		})
	}) {
		return finishDirectReachTask(ctx, task.ID, attempted, result)
	}

	return finishDirectReachTask(ctx, task.ID, attempted, result)
}

func normalizeDirectReachReq(req SendDirectUserReachReq) SendDirectUserReachReq {
	req.Title = strings.TrimSpace(req.Title)
	req.LongText = strings.TrimSpace(req.LongText)
	req.ShortText = strings.TrimSpace(req.ShortText)
	req.EventKey = strings.TrimSpace(req.EventKey)
	req.DedupKey = strings.TrimSpace(req.DedupKey)
	if req.EventKey == "" {
		req.EventKey = DirectReachEventKey
	}
	if req.LongText == "" {
		req.LongText = req.ShortText
	}
	if req.ShortText == "" {
		req.ShortText = textutil.TruncateRunes(req.LongText, 120)
	}
	if req.Title == "" {
		req.Title = textutil.TruncateRunes(req.ShortText, 60)
	}
	return req
}

func createDirectReachTask(ctx context.Context, req SendDirectUserReachReq, user model.User) (*model.ReachTask, bool, error) {
	contentJSON, _ := json.Marshal(map[string]string{
		"title":      req.Title,
		"long_text":  req.LongText,
		"short_text": req.ShortText,
	})
	audienceJSON, _ := json.Marshal(map[string]any{"user_id": req.UserID})
	channelsJSON, _ := json.Marshal([]string{model.ReachChannelInApp, model.ReachChannelEmail, model.ReachChannelSMS})
	statsJSON, _ := json.Marshal(map[string]int{"sent": 0, "failed": 0, "skipped": 0})

	var dedupKey *string
	if req.DedupKey != "" {
		v := req.DedupKey
		dedupKey = &v
	}
	task := model.ReachTask{
		ID:        snowflake.GenID(),
		Kind:      model.ReachKindDirect,
		EventKey:  req.EventKey,
		DedupKey:  dedupKey,
		Channels:  channelsJSON,
		Audience:  audienceJSON,
		Content:   contentJSON,
		Status:    model.ReachStatusSending,
		Stats:     statsJSON,
		Region:    user.Region,
		CreatedBy: req.CreatedBy,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	db := store.DB.WithContext(ctx)
	if dedupKey == nil {
		if err := db.Create(&task).Error; err != nil {
			return nil, false, err
		}
		return &task, true, nil
	}

	res := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "dedup_key"}},
		DoNothing: true,
	}).Create(&task)
	if res.Error != nil {
		return nil, false, res.Error
	}
	if res.RowsAffected > 0 {
		return &task, true, nil
	}

	var existing model.ReachTask
	if err := db.Where("dedup_key = ?", *dedupKey).First(&existing).Error; err != nil {
		return nil, false, err
	}
	return &existing, false, nil
}

func createDirectReachSendLog(ctx context.Context, taskID int64, user model.User, channel string) (model.ReachSendLog, error) {
	logRow := model.ReachSendLog{
		ID:      snowflake.GenID(),
		TaskID:  taskID,
		UserID:  user.ID,
		Channel: channel,
		Status:  model.ReachSendStatusPending,
		Region:  user.Region,
	}
	res := store.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&logRow)
	if res.Error != nil {
		return model.ReachSendLog{}, res.Error
	}
	if res.RowsAffected == 0 {
		return model.ReachSendLog{}, errors.New("send log already exists")
	}
	return logRow, nil
}

func markReachSendLog(ctx context.Context, logID int64, status, errText string) {
	updates := map[string]any{"status": status}
	if errText != "" {
		updates["error"] = errText
	}
	store.DB.WithContext(ctx).Model(&model.ReachSendLog{}).Where("id = ?", logID).Updates(updates)
}

func finishDirectReachTask(ctx context.Context, taskID int64, channels []string, result *SendDirectUserReachResult) (*SendDirectUserReachResult, error) {
	stats := map[string]int{"sent": 0, "failed": 0, "skipped": 0}
	for _, a := range result.Attempts {
		switch a.Status {
		case model.ReachSendStatusSent:
			stats["sent"]++
		case model.ReachSendStatusFailed:
			stats["failed"]++
		case model.ReachSendStatusSkipped:
			stats["skipped"]++
		}
	}
	channelsJSON, _ := json.Marshal(channels)
	statsJSON, _ := json.Marshal(stats)
	if err := store.DB.WithContext(ctx).Model(&model.ReachTask{}).Where("id = ?", taskID).
		Updates(map[string]any{
			"channels":   string(channelsJSON),
			"stats":      string(statsJSON),
			"status":     result.Status,
			"updated_at": time.Now().UTC(),
		}).Error; err != nil {
		return nil, err
	}
	var task model.ReachTask
	if err := store.DB.WithContext(ctx).Where("id = ?", taskID).First(&task).Error; err != nil {
		return nil, err
	}
	result.Task = &task
	return result, nil
}

func directReachResultFromExistingTask(ctx context.Context, task *model.ReachTask) (*SendDirectUserReachResult, error) {
	var logs []model.ReachSendLog
	if err := store.DB.WithContext(ctx).
		Where("task_id = ?", task.ID).
		Order("created_at ASC").
		Find(&logs).Error; err != nil {
		return nil, err
	}
	result := &SendDirectUserReachResult{
		Task:     task,
		Status:   task.Status,
		Attempts: make([]DirectUserReachAttempt, 0, len(logs)),
	}
	for _, logRow := range logs {
		attempt := DirectUserReachAttempt{
			Channel: logRow.Channel,
			Status:  logRow.Status,
			Error:   logRow.Error,
			LogID:   logRow.ID,
		}
		result.Attempts = append(result.Attempts, attempt)
		if result.Channel == "" && logRow.Status == model.ReachSendStatusSent {
			result.Channel = logRow.Channel
		}
	}
	return result, nil
}

func hasDirectReachAppChannel(ctx context.Context, userID int64) bool {
	if hasOnlineRealtimeRoute(userID) {
		return true
	}
	if store.JS == nil {
		return false
	}
	var count int64
	if err := store.DB.WithContext(ctx).Model(&model.Device{}).
		Where("user_id = ? AND is_active = ?", userID, true).
		Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

func deliverDirectReachInApp(ctx context.Context, customerUserID, userID int64, req SendDirectUserReachReq) error {
	msg, err := WriteMarketingSystemMessage(customerUserID, userID, req.Title, req.LongText)
	if err != nil {
		return err
	}
	payload := protocol.PushMsgPayload{
		InboxSeq:    msg.InboxSeq,
		MsgID:       msg.MsgID,
		SessionID:   msg.SessionID,
		SessionType: 1,
		SenderID:    customerUserID,
		SenderType:  3,
		MsgType:     3,
		Content:     msg.Content,
		CreatedAt:   msg.CreatedAt.UnixMilli(),
	}
	if hasOnlineRealtimeRoute(userID) {
		pushRealtimeEvent(userID, protocol.CmdPushMsg, payload)
		return nil
	}
	return publishDirectReachOfflineEvent(userID, protocol.CmdPushMsg, payload)
}

func publishDirectReachOfflineEvent(userID int64, cmd string, payload interface{}) error {
	if userID <= 0 || cmd == "" {
		return errors.New("invalid offline push target")
	}
	if store.JS == nil {
		return errors.New("offline push unavailable")
	}
	data, err := json.Marshal(map[string]interface{}{
		"user_id": userID,
		"cmd":     cmd,
		"payload": payload,
	})
	if err != nil {
		return fmt.Errorf("marshal offline event: %w", err)
	}
	subject := fmt.Sprintf("im.push.offline.%d", userID)
	if _, err := store.JS.Publish(subject, data, nats.Context(context.Background())); err != nil {
		logger.L.Warnf("direct reach offline publish failed user=%d cmd=%s err=%v", userID, cmd, err)
		return fmt.Errorf("publish offline event: %w", err)
	}
	return nil
}

func directReachEmailContent(req SendDirectUserReachReq) (subject, body string) {
	subject = strings.NewReplacer("\r", " ", "\n", " ").Replace(req.Title)
	escapedTitle := html.EscapeString(req.Title)
	markdownBody := directReachMarkdownHTML(req.LongText)
	body = fmt.Sprintf(`<!DOCTYPE html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head>
<body style="margin:0;padding:0;background:#f5f5f5;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif">
<table width="100%%" cellpadding="0" cellspacing="0" style="max-width:600px;margin:24px auto;background:#fff;border-radius:8px;overflow:hidden">
<tr><td style="background:#4A90D9;padding:24px;text-align:center"><h1 style="margin:0;font-size:20px;color:#fff">%s</h1></td></tr>
<tr><td style="padding:24px"><div style="font-size:14px;color:#333;line-height:1.6">%s</div></td></tr>
<tr><td style="padding:12px 24px 24px;text-align:center;border-top:1px solid #eee"><p style="margin:0;font-size:12px;color:#999">Grix</p></td></tr>
</table>
</body></html>`, escapedTitle, markdownBody)
	return subject, body
}

func directReachMarkdownHTML(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	var out bytes.Buffer
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))
	if err := md.Convert([]byte(input), &out); err != nil {
		return strings.ReplaceAll(html.EscapeString(input), "\n", "<br>")
	}
	return out.String()
}

func directReachPhone(user model.User) (phone, countryCode string, err error) {
	if strings.TrimSpace(user.PhoneCipher) != "" {
		phone, err = secretcrypto.Decrypt(user.PhoneCipher)
		if err != nil {
			return "", "", fmt.Errorf("decrypt phone: %w", err)
		}
	} else {
		phone = strings.TrimSpace(user.PhoneE164)
	}
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return "", "", nil
	}
	countryCode = strings.TrimSpace(user.PhoneCountry)
	if countryCode == "" {
		countryCode, err = identity.ParseCountryCode(phone)
		if err != nil {
			return "", "", err
		}
	}
	return phone, countryCode, nil
}
