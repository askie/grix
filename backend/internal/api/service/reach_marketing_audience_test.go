package service

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCountReachAudience_FiltersInactiveRegisteredAndContact(t *testing.T) {
	setupReachTestDB(t)
	now := time.Now().UTC()

	fresh := model.User{ID: 2001, Username: "fresh", Email: "fresh@t.local", Status: model.UserStatusActive, Region: "cn", CreatedAt: now, UpdatedAt: now}
	dormant := model.User{ID: 2002, Username: "dormant", Email: "dormant@t.local", Status: model.UserStatusActive, Region: "cn", CreatedAt: now.AddDate(0, -6, 0), UpdatedAt: now.AddDate(0, 0, -60)}
	dormantNoEmail := model.User{ID: 2003, Username: "noemail", Status: model.UserStatusActive, Region: "global", PhoneE164: "+15550001111", CreatedAt: now.AddDate(0, -6, 0), UpdatedAt: now.AddDate(0, 0, -90)}
	for _, u := range []*model.User{&fresh, &dormant, &dormantNoEmail} {
		require.NoError(t, store.DB.Create(u).Error)
		// GORM 会用 autoUpdateTime 覆盖 UpdatedAt，回写显式值
		require.NoError(t, store.DB.Model(&model.User{}).Where("id = ?", u.ID).Updates(map[string]any{"updated_at": u.UpdatedAt, "created_at": u.CreatedAt}).Error)
	}

	n, err := CountReachAudience(nil)
	require.NoError(t, err)
	assert.EqualValues(t, 3, n)

	n, err = CountReachAudience(&ReachAudience{InactiveDays: 30})
	require.NoError(t, err)
	assert.EqualValues(t, 2, n)

	n, err = CountReachAudience(&ReachAudience{InactiveDays: 30, HasEmail: true})
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)

	n, err = CountReachAudience(&ReachAudience{HasPhone: true})
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)

	n, err = CountReachAudience(&ReachAudience{RegisteredBefore: now.AddDate(0, 0, -1).Format("2006-01-02")})
	require.NoError(t, err)
	assert.EqualValues(t, 2, n)

	_, err = CountReachAudience(&ReachAudience{RegisteredBefore: "bad"})
	assert.Error(t, err)
}

func TestDeliverMarketingToUser_SendsSmsFromTemplate(t *testing.T) {
	setupReachTestDB(t)
	orig := sendDirectReachSMS
	t.Cleanup(func() { sendDirectReachSMS = orig })

	var got ReachSMSRequest
	sendDirectReachSMS = func(_ context.Context, req ReachSMSRequest) error {
		got = req
		return nil
	}

	user := model.User{ID: 3001, Username: "sms", Status: model.UserStatusActive, Region: "global", PhoneE164: "+15550002222", PhoneCountry: "+1"}
	require.NoError(t, store.DB.Create(&user).Error)
	tpl, err := CreateReachTemplate(CreateReachTemplateReq{Name: "sms", Title: "t", SmsBody: "Grix 新版本上线，回复 STOP 退订"})
	require.NoError(t, err)
	task := model.ReachTask{ID: snowflake.GenID(), Kind: model.ReachKindMarketing, TemplateID: tpl.ID, Channels: []byte(`["sms"]`), Status: model.ReachStatusSending}
	require.NoError(t, store.DB.Create(&task).Error)

	delivered := deliverMarketingToUser(context.Background(), task.ID, 9001, user, *tpl, map[string]bool{model.ReachChannelSMS: true})
	assert.True(t, delivered)
	assert.Equal(t, "+15550002222", got.PhoneE164)
	assert.Equal(t, tpl.SmsBody, got.Text)

	var logRow model.ReachSendLog
	require.NoError(t, store.DB.Where("task_id = ? AND channel = ?", task.ID, model.ReachChannelSMS).First(&logRow).Error)
	assert.Equal(t, model.ReachSendStatusSent, logRow.Status)

	// 无手机号的用户不产生 sms 记录
	noPhone := model.User{ID: 3002, Username: "nophone", Email: "nophone@t.local", Status: model.UserStatusActive, Region: "global"}
	require.NoError(t, store.DB.Create(&noPhone).Error)
	assert.False(t, deliverMarketingToUser(context.Background(), task.ID, 9001, noPhone, *tpl, map[string]bool{model.ReachChannelSMS: true}))
}
