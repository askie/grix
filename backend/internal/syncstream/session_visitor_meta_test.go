package syncstream

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

func openVisitorMetaTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openNamedTestDB(t, "visitor-site")
	if err := db.AutoMigrate(&model.WidgetSite{}, &model.WidgetSession{}); err != nil {
		t.Fatal(err)
	}
	prev := store.DB
	store.DB = db
	t.Cleanup(func() { store.DB = prev })
	return db
}

func decodeEventPayload(t *testing.T, payload any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal payload %s: %v", raw, err)
	}
	return decoded
}

func seedVisitorSession(t *testing.T, db *gorm.DB, siteID, widgetSessionID int64, sessionID, firstOrigin, siteName string) {
	t.Helper()
	now := time.Now().UTC()
	if err := db.Create(&model.WidgetSite{
		ID: siteID, OwnerUserID: 1201, SiteKey: "wk_" + sessionID, SiteSecretHash: "hash",
		SiteName: siteName, FirstOrigin: firstOrigin, Status: model.WidgetSiteStatusActive,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed widget site: %v", err)
	}
	if err := db.Create(&model.WidgetSession{
		ID: widgetSessionID, SiteID: siteID, OwnerUserID: 1201, VisitorID: 2201,
		VisitorKey: "vk_" + sessionID, SessionID: sessionID, Status: model.WidgetSessionStatusActive,
		CreatedAt: now, UpdatedAt: now, LastActiveAt: now,
	}).Error; err != nil {
		t.Fatalf("seed widget session: %v", err)
	}
}

func sessionUpsertPayloads(t *testing.T, events []Event) []map[string]any {
	t.Helper()
	var payloads []map[string]any
	for _, event := range events {
		if event.Kind != "session.upsert" {
			continue
		}
		payloads = append(payloads, decodeEventPayload(t, event.Payload))
	}
	return payloads
}

// v2 增量 session.upsert 必须带 is_visitor 和站点字段：v2 客户端靠消息推送里
// 的这条事件新建会话，否则永远识别不出访客身份。
func TestSessionUpsertCarriesVisitorSite(t *testing.T) {
	t.Setenv("AIBOT_SYNC_V2_ENABLED", "1")
	t.Setenv("AIBOT_SYNC_COMPOUND_ENABLED", "")
	db := openVisitorMetaTestDB(t)

	seedVisitorSession(t, db, 7401, 9401, "s_visitor_upsert", "shop.example.com", "Shop")

	session := model.Session{SessionID: "s_visitor_upsert", OwnerID: 1201, SessionType: model.SessionTypeDirect, LastMsgSummary: "hi", StateVersion: 3}
	message := model.Message{MsgID: 201, SessionID: "s_visitor_upsert", SenderID: 2201, Content: "hi", StateVersion: 1}
	payloads := sessionUpsertPayloads(t, MessageDeliveryEvents(MessageDelivery{
		UserID: 1201, SessionID: session.SessionID, Message: message, Session: &session,
	}))
	if len(payloads) != 1 {
		t.Fatalf("session.upsert payloads=%d", len(payloads))
	}
	payload := payloads[0]
	if payload["is_visitor"] != true {
		t.Fatalf("session.upsert is_visitor=%v want true (payload=%v)", payload["is_visitor"], payload)
	}
	if payload["visitor_site_id"] != "7401" {
		t.Fatalf("session.upsert visitor_site_id=%v want %q", payload["visitor_site_id"], "7401")
	}
	if payload["visitor_site_domain"] != "shop.example.com" {
		t.Fatalf("session.upsert visitor_site_domain=%v want %q", payload["visitor_site_domain"], "shop.example.com")
	}
	if payload["session_id"] != "s_visitor_upsert" {
		t.Fatalf("session.upsert lost session_id: %v", payload)
	}

	// 站点没有记录域名时回退 site_name。
	seedVisitorSession(t, db, 7402, 9402, "s_visitor_fallback", "", "Fallback Shop")
	fallbackSession := model.Session{SessionID: "s_visitor_fallback", OwnerID: 1201, SessionType: model.SessionTypeDirect, StateVersion: 4}
	fallbackMessage := model.Message{MsgID: 202, SessionID: "s_visitor_fallback", SenderID: 2201, Content: "hi", StateVersion: 1}
	fallbackPayloads := sessionUpsertPayloads(t, MessageDeliveryEvents(MessageDelivery{
		UserID: 1201, SessionID: fallbackSession.SessionID, Message: fallbackMessage, Session: &fallbackSession,
	}))
	if len(fallbackPayloads) != 1 {
		t.Fatalf("fallback session.upsert payloads=%d", len(fallbackPayloads))
	}
	if fallbackPayloads[0]["visitor_site_domain"] != "Fallback Shop" {
		t.Fatalf("fallback visitor_site_domain=%v want site_name", fallbackPayloads[0]["visitor_site_domain"])
	}
}

// 回执/历史导入路径直接用 SessionUpsertEvent 生产事件，同样要带访客字段。
func TestSessionUpsertEventCarriesVisitorSite(t *testing.T) {
	t.Setenv("AIBOT_SYNC_V2_ENABLED", "1")
	db := openVisitorMetaTestDB(t)

	seedVisitorSession(t, db, 7501, 9501, "s_visitor_revoke", "revoke.example.com", "Revoke")

	session := model.Session{SessionID: "s_visitor_revoke", OwnerID: 1201, SessionType: model.SessionTypeDirect, StateVersion: 5}
	payload := decodeEventPayload(t, SessionUpsertEvent(1201, session.SessionID, session, "revoke-1").Payload)
	if payload["is_visitor"] != true {
		t.Fatalf("is_visitor=%v want true", payload["is_visitor"])
	}
	if payload["visitor_site_id"] != "7501" {
		t.Fatalf("visitor_site_id=%v want %q", payload["visitor_site_id"], "7501")
	}
	if payload["visitor_site_domain"] != "revoke.example.com" {
		t.Fatalf("visitor_site_domain=%v want %q", payload["visitor_site_domain"], "revoke.example.com")
	}
}

// 反向用例：普通私聊/群聊的 session.upsert payload 不新增任何字段。
func TestSessionUpsertOmitsVisitorSiteForPlainSessions(t *testing.T) {
	t.Setenv("AIBOT_SYNC_V2_ENABLED", "1")
	t.Setenv("AIBOT_SYNC_COMPOUND_ENABLED", "")
	openVisitorMetaTestDB(t)

	plain := model.Session{SessionID: "s_plain_upsert", OwnerID: 1201, SessionType: model.SessionTypeDirect, LastMsgSummary: "hi", StateVersion: 6}
	group := model.Session{SessionID: "s_group_upsert", OwnerID: 1201, SessionType: model.SessionTypeGroup, GroupName: "Team", StateVersion: 7}
	events := MessageDeliveryEvents(
		MessageDelivery{UserID: 1201, SessionID: plain.SessionID, Message: model.Message{MsgID: 301, SessionID: plain.SessionID, SenderID: 1201, Content: "hi", StateVersion: 1}, Session: &plain},
		MessageDelivery{UserID: 1201, SessionID: group.SessionID, Message: model.Message{MsgID: 302, SessionID: group.SessionID, SenderID: 1201, Content: "hi", StateVersion: 1}, Session: &group},
	)
	payloads := sessionUpsertPayloads(t, events)
	if len(payloads) != 2 {
		t.Fatalf("session.upsert payloads=%d", len(payloads))
	}
	for _, payload := range payloads {
		for _, key := range []string{"is_visitor", "visitor_site_id", "visitor_site_domain"} {
			if _, ok := payload[key]; ok {
				t.Fatalf("plain session payload must not carry %q: %v", key, payload)
			}
		}
	}
}

// compound_v1 的复合 message.upsert 里内嵌的 session 部分同样要带访客字段。
func TestCompoundMessageUpsertEmbedsVisitorSite(t *testing.T) {
	t.Setenv("AIBOT_SYNC_V2_ENABLED", "1")
	t.Setenv("AIBOT_SYNC_COMPOUND_ENABLED", "1")
	db := openVisitorMetaTestDB(t)

	seedVisitorSession(t, db, 7601, 9601, "s_visitor_compound", "compound.example.com", "Compound")

	session := model.Session{SessionID: "s_visitor_compound", OwnerID: 1201, SessionType: model.SessionTypeDirect, StateVersion: 8}
	message := model.Message{MsgID: 401, SessionID: session.SessionID, SenderID: 2201, Content: "hi", StateVersion: 1}
	events := MessageDeliveryEvents(MessageDelivery{UserID: 1201, SessionID: session.SessionID, Message: message, Session: &session})
	if len(events) != 1 || events[0].Span != 2 {
		t.Fatalf("events=%d span=%v", len(events), events[0].Span)
	}
	raw, err := json.Marshal(events[0].Payload)
	if err != nil {
		t.Fatalf("marshal compound payload: %v", err)
	}
	var outer struct {
		Session map[string]any `json:"session"`
	}
	if err := json.Unmarshal(raw, &outer); err != nil {
		t.Fatalf("decode compound payload: %v", err)
	}
	if outer.Session["is_visitor"] != true {
		t.Fatalf("embedded session is_visitor=%v want true (%v)", outer.Session["is_visitor"], outer.Session)
	}
	if outer.Session["visitor_site_id"] != "7601" {
		t.Fatalf("embedded session visitor_site_id=%v", outer.Session["visitor_site_id"])
	}
	if outer.Session["visitor_site_domain"] != "compound.example.com" {
		t.Fatalf("embedded session visitor_site_domain=%v", outer.Session["visitor_site_domain"])
	}
}
