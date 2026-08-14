package service

import (
	"errors"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
)

func setupQRLoginServiceTest(t *testing.T) (*testutil.TestDB, *testutil.FixtureBuilder) {
	t.Helper()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	jwtpkg.Init("test-secret-for-qr-login", 3600, 86400)
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("snowflake init error: %v", err)
	}

	if err := systemsetting.SaveAuthSettings(systemsetting.DefaultAuthSettings(), nil); err != nil {
		t.Fatalf("save auth settings error: %v", err)
	}

	return testDB, testutil.NewFixtureBuilder(testDB.DB)
}

func TestQRLoginFlowSuccessAndConsume(t *testing.T) {
	testDB, fixture := setupQRLoginServiceTest(t)
	defer testDB.Close()

	user := fixture.CreateUser(func(u *model.User) {
		u.ID = 920001
		u.Username = "qr_flow_user"
		u.Email = "qr_flow_user@example.com"
		u.Nickname = "QR Flow User"
	})

	createResp, err := CreateQRLoginSession("127.0.0.1", "test-agent", "Chrome Desktop")
	if err != nil {
		t.Fatalf("CreateQRLoginSession error: %v", err)
	}
	if createResp.QRSessionID == "" || createResp.QRText == "" || createResp.PollToken == "" {
		t.Fatalf("invalid create response: %+v", createResp)
	}

	scanResp, err := ScanQRLoginSession(user.ID, createResp.QRText)
	if err != nil {
		t.Fatalf("ScanQRLoginSession error: %v", err)
	}
	if scanResp.Status != "scanned" {
		t.Fatalf("expected scanned status, got %s", scanResp.Status)
	}

	confirmResp, err := ConfirmQRLoginSession(user.ID, createResp.QRSessionID, true)
	if err != nil {
		t.Fatalf("ConfirmQRLoginSession error: %v", err)
	}
	if confirmResp.Status != "confirmed" {
		t.Fatalf("expected confirmed status, got %s", confirmResp.Status)
	}

	statusResp, err := QueryQRLoginStatus(createResp.QRSessionID, createResp.PollToken)
	if err != nil {
		t.Fatalf("QueryQRLoginStatus error: %v", err)
	}
	if statusResp.Status != "confirmed" {
		t.Fatalf("expected confirmed status from status query, got %s", statusResp.Status)
	}
	if statusResp.ScannerUser == nil || statusResp.ScannerUser.UserID != "920001" {
		t.Fatalf("expected scanner user 920001, got %+v", statusResp.ScannerUser)
	}

	exchangeResp, err := ExchangeQRLoginSession(createResp.QRSessionID, createResp.PollToken, "qr-device-1", "web", testAuthLanguage)
	if err != nil {
		t.Fatalf("ExchangeQRLoginSession error: %v", err)
	}
	if exchangeResp.AccessToken == "" || exchangeResp.RefreshToken == "" {
		t.Fatalf("expected tokens after exchange, got %+v", exchangeResp)
	}
	if exchangeResp.User.ID != user.ID {
		t.Fatalf("expected user %d, got %d", user.ID, exchangeResp.User.ID)
	}
	assertLoginDeviceSessionCreated(t, testDB, user.ID, exchangeResp.AccessToken, "qr-device-1", "web")

	_, err = ExchangeQRLoginSession(createResp.QRSessionID, createResp.PollToken, "qr-device-1", "web", testAuthLanguage)
	if !errors.Is(err, ErrQRLoginAlreadyConsumed) {
		t.Fatalf("expected ErrQRLoginAlreadyConsumed, got %v", err)
	}
}

func TestQRLoginConfirmForbiddenByOtherUser(t *testing.T) {
	testDB, fixture := setupQRLoginServiceTest(t)
	defer testDB.Close()

	scanner := fixture.CreateUser(func(u *model.User) {
		u.ID = 920011
		u.Username = "qr_scanner"
		u.Email = "qr_scanner@example.com"
	})
	other := fixture.CreateUser(func(u *model.User) {
		u.ID = 920012
		u.Username = "qr_other"
		u.Email = "qr_other@example.com"
	})

	createResp, err := CreateQRLoginSession("127.0.0.1", "test-agent", "Desktop")
	if err != nil {
		t.Fatalf("CreateQRLoginSession error: %v", err)
	}
	if _, err := ScanQRLoginSession(scanner.ID, createResp.QRText); err != nil {
		t.Fatalf("ScanQRLoginSession error: %v", err)
	}

	_, err = ConfirmQRLoginSession(other.ID, createResp.QRSessionID, true)
	if !errors.Is(err, ErrQRLoginForbidden) {
		t.Fatalf("expected ErrQRLoginForbidden, got %v", err)
	}
}

func TestQRLoginScanRejectedWhenScannedByAnotherUser(t *testing.T) {
	testDB, fixture := setupQRLoginServiceTest(t)
	defer testDB.Close()

	first := fixture.CreateUser(func(u *model.User) {
		u.ID = 920021
		u.Username = "qr_first"
		u.Email = "qr_first@example.com"
	})
	second := fixture.CreateUser(func(u *model.User) {
		u.ID = 920022
		u.Username = "qr_second"
		u.Email = "qr_second@example.com"
	})

	createResp, err := CreateQRLoginSession("127.0.0.1", "test-agent", "Desktop")
	if err != nil {
		t.Fatalf("CreateQRLoginSession error: %v", err)
	}
	if _, err := ScanQRLoginSession(first.ID, createResp.QRText); err != nil {
		t.Fatalf("ScanQRLoginSession first error: %v", err)
	}

	_, err = ScanQRLoginSession(second.ID, createResp.QRText)
	if !errors.Is(err, ErrQRLoginAlreadyScannedByPeer) {
		t.Fatalf("expected ErrQRLoginAlreadyScannedByPeer, got %v", err)
	}
}

func TestQRLoginExchangeExpired(t *testing.T) {
	testDB, fixture := setupQRLoginServiceTest(t)
	defer testDB.Close()

	user := fixture.CreateUser(func(u *model.User) {
		u.ID = 920031
		u.Username = "qr_expired"
		u.Email = "qr_expired@example.com"
	})

	createResp, err := CreateQRLoginSession("127.0.0.1", "test-agent", "Desktop")
	if err != nil {
		t.Fatalf("CreateQRLoginSession error: %v", err)
	}
	if _, err := ScanQRLoginSession(user.ID, createResp.QRText); err != nil {
		t.Fatalf("ScanQRLoginSession error: %v", err)
	}
	if _, err := ConfirmQRLoginSession(user.ID, createResp.QRSessionID, true); err != nil {
		t.Fatalf("ConfirmQRLoginSession error: %v", err)
	}

	expiredAt := time.Now().UTC().Add(-1 * time.Minute)
	if err := testDB.DB.Model(&model.AuthQRLoginSession{}).
		Where("session_id = ?", createResp.QRSessionID).
		Update("expires_at", expiredAt).Error; err != nil {
		t.Fatalf("force expires_at error: %v", err)
	}

	_, err = ExchangeQRLoginSession(createResp.QRSessionID, createResp.PollToken, "qr-device-1", "web", testAuthLanguage)
	if !errors.Is(err, ErrQRLoginExpired) {
		t.Fatalf("expected ErrQRLoginExpired, got %v", err)
	}
}

func TestQRLoginScanRegionMismatch(t *testing.T) {
	testDB, fixture := setupQRLoginServiceTest(t)
	defer testDB.Close()

	user := fixture.CreateUser(func(u *model.User) {
		u.ID = 920041
		u.Username = "qr_region_user"
		u.Email = "qr_region_user@example.com"
	})

	// 本区默认 global（config 未设置 Region 时归一化为 global）。
	// 二维码文本携带 rg=cn，模拟"CN 区网页生成的码被全球区服务端扫描"。
	crossRegionText := buildQRLoginText("11111111-2222-3333-4444-555555555555", "fake-token", "cn")
	_, err := ScanQRLoginSession(user.ID, crossRegionText)
	if !errors.Is(err, ErrQRLoginRegionMismatch) {
		t.Fatalf("expected ErrQRLoginRegionMismatch, got %v", err)
	}

	// 同区二维码走正常流程：create 出的码带本区标识，扫码成功。
	createResp, err := CreateQRLoginSession("127.0.0.1", "test-agent", "Desktop")
	if err != nil {
		t.Fatalf("CreateQRLoginSession error: %v", err)
	}
	if _, err := ScanQRLoginSession(user.ID, createResp.QRText); err != nil {
		t.Fatalf("same-region scan should succeed, got %v", err)
	}

	// 旧版二维码不带 rg：跳过区域检查，落到查库逻辑（此处记录不存在 → 二维码无效）。
	legacyText := "grix://auth/qr-login?sid=66666666-7777-8888-9999-000000000000&qt=legacy-token"
	_, err = ScanQRLoginSession(user.ID, legacyText)
	if !errors.Is(err, ErrQRLoginInvalidCode) {
		t.Fatalf("expected ErrQRLoginInvalidCode for legacy code, got %v", err)
	}
}
