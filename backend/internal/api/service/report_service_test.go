package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/minio/minio-go/v7"
)

func setupReportServiceTest(t *testing.T) (*testutil.TestDB, *testutil.FixtureBuilder, func()) {
	t.Helper()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	fixture := testutil.NewFixtureBuilder(testDB.DB)

	originalEnsure := reportAssetEnsureOSSReady
	originalStat := reportAssetStatObject

	reportAssetEnsureOSSReady = func() error { return nil }

	cleanup := func() {
		reportAssetEnsureOSSReady = originalEnsure
		reportAssetStatObject = originalStat
		testDB.Close()
	}

	return testDB, fixture, cleanup
}

func TestCreateReport_UserSuccess(t *testing.T) {
	testDB, fixture, cleanup := setupReportServiceTest(t)
	defer cleanup()

	reporter := fixture.CreateUser(func(u *model.User) {
		u.ID = 1001
		u.Username = "reporter"
		u.Nickname = "Reporter"
	})
	target := fixture.CreateUser(func(u *model.User) {
		u.ID = 1002
		u.Username = "target"
		u.Nickname = "Target"
	})

	assetKey := reportAssetObjectPrefix(reporter.ID) + "evidence-1.png"
	reportAssetStatObject = func(
		ctx context.Context,
		bucket string,
		objectKey string,
	) (minio.ObjectInfo, error) {
		if objectKey != assetKey {
			return minio.ObjectInfo{}, errors.New("unexpected asset key")
		}
		return minio.ObjectInfo{
			Key:         objectKey,
			Size:        2048,
			ContentType: "image/png",
		}, nil
	}

	resp, err := CreateReport(reporter.ID, CreateReportReq{
		TargetType:   "user",
		TargetUserID: target.ID,
		ReasonCode:   "fraud",
		Description:  "诱导转账",
		AssetKeys:    []string{assetKey},
	})
	if err != nil {
		t.Fatalf("CreateReport() error = %v", err)
	}
	if resp == nil {
		t.Fatal("CreateReport() resp is nil")
	}
	if resp.Status != "pending" {
		t.Fatalf("expected pending status, got %s", resp.Status)
	}

	var report model.Report
	if err := testDB.DB.First(&report, resp.ReportID).Error; err != nil {
		t.Fatalf("load report failed: %v", err)
	}
	if report.ReporterUserID != reporter.ID {
		t.Fatalf("expected reporter %d, got %d", reporter.ID, report.ReporterUserID)
	}
	if report.TargetType != model.ReportTargetTypeUser {
		t.Fatalf("expected target type user, got %d", report.TargetType)
	}
	if report.TargetUserID != target.ID {
		t.Fatalf("expected target user %d, got %d", target.ID, report.TargetUserID)
	}

	var attachments []model.ReportAttachment
	if err := testDB.DB.Where("report_id = ?", report.ID).Find(&attachments).Error; err != nil {
		t.Fatalf("load attachments failed: %v", err)
	}
	if len(attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(attachments))
	}
	if attachments[0].ObjectKey != assetKey {
		t.Fatalf("expected attachment key %s, got %s", assetKey, attachments[0].ObjectKey)
	}
}

func TestCreateReport_GroupSuccess(t *testing.T) {
	testDB, fixture, cleanup := setupReportServiceTest(t)
	defer cleanup()

	reporter := fixture.CreateUser(func(u *model.User) {
		u.ID = 2001
		u.Username = "group_reporter"
		u.Nickname = "Group Reporter"
	})
	owner := fixture.CreateUser(func(u *model.User) {
		u.ID = 2002
		u.Username = "group_owner"
		u.Nickname = "Group Owner"
	})

	session := fixture.CreateSession(func(s *model.Session) {
		s.SessionID = "session-group-report-1"
		s.OwnerID = owner.ID
		s.SessionType = model.SessionTypeGroup
		s.GroupName = "Fraud Group"
		s.CreatedAt = time.Now().UTC()
		s.UpdatedAt = time.Now().UTC()
	})

	members := []model.SessionMember{
		{
			SessionID:    session.SessionID,
			MemberID:     reporter.ID,
			MemberType:   1,
			Role:         1,
			JoinedAt:     time.Now().UTC(),
			LastActiveAt: time.Now().UTC(),
		},
		{
			SessionID:    session.SessionID,
			MemberID:     owner.ID,
			MemberType:   1,
			Role:         3,
			JoinedAt:     time.Now().UTC(),
			LastActiveAt: time.Now().UTC(),
		},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create session members failed: %v", err)
	}

	assetKey := reportAssetObjectPrefix(reporter.ID) + "group-evidence-1.png"
	reportAssetStatObject = func(
		ctx context.Context,
		bucket string,
		objectKey string,
	) (minio.ObjectInfo, error) {
		if objectKey != assetKey {
			return minio.ObjectInfo{}, errors.New("unexpected asset key")
		}
		return minio.ObjectInfo{
			Key:         objectKey,
			Size:        4096,
			ContentType: "image/png",
		}, nil
	}

	resp, err := CreateReport(reporter.ID, CreateReportReq{
		TargetType:      "group",
		TargetSessionID: session.SessionID,
		SourceSessionID: session.SessionID,
		ReasonCode:      "spam",
		Description:     "群里连续刷屏",
		AssetKeys:       []string{assetKey},
	})
	if err != nil {
		t.Fatalf("CreateReport() error = %v", err)
	}
	if resp == nil {
		t.Fatal("CreateReport() resp is nil")
	}

	var report model.Report
	if err := testDB.DB.First(&report, resp.ReportID).Error; err != nil {
		t.Fatalf("load report failed: %v", err)
	}
	if report.TargetType != model.ReportTargetTypeGroup {
		t.Fatalf("expected group target type, got %d", report.TargetType)
	}
	if report.TargetSessionID != session.SessionID {
		t.Fatalf("expected target session %s, got %s", session.SessionID, report.TargetSessionID)
	}
	if report.SourceSessionID != session.SessionID {
		t.Fatalf("expected source session %s, got %s", session.SessionID, report.SourceSessionID)
	}
}

func TestCreateReport_RejectsSelfReport(t *testing.T) {
	_, fixture, cleanup := setupReportServiceTest(t)
	defer cleanup()

	reporter := fixture.CreateUser(func(u *model.User) {
		u.ID = 3001
		u.Username = "self_reporter"
		u.Nickname = "Self Reporter"
	})

	assetKey := reportAssetObjectPrefix(reporter.ID) + "self-evidence-1.png"
	reportAssetStatObject = func(
		ctx context.Context,
		bucket string,
		objectKey string,
	) (minio.ObjectInfo, error) {
		return minio.ObjectInfo{
			Key:         objectKey,
			Size:        1024,
			ContentType: "image/png",
		}, nil
	}

	_, err := CreateReport(reporter.ID, CreateReportReq{
		TargetType:   "user",
		TargetUserID: reporter.ID,
		ReasonCode:   "other",
		AssetKeys:    []string{assetKey},
	})
	if !errors.Is(err, ErrReportSelfNotAllowed) {
		t.Fatalf("expected ErrReportSelfNotAllowed, got %v", err)
	}
}

func TestCreateReport_RejectsForeignAsset(t *testing.T) {
	_, fixture, cleanup := setupReportServiceTest(t)
	defer cleanup()

	reporter := fixture.CreateUser(func(u *model.User) {
		u.ID = 4001
		u.Username = "asset_reporter"
		u.Nickname = "Asset Reporter"
	})
	target := fixture.CreateUser(func(u *model.User) {
		u.ID = 4002
		u.Username = "asset_target"
		u.Nickname = "Asset Target"
	})

	foreignAssetKey := reportAssetObjectPrefix(9999) + "foreign.png"

	_, err := CreateReport(reporter.ID, CreateReportReq{
		TargetType:   "user",
		TargetUserID: target.ID,
		ReasonCode:   "harassment",
		AssetKeys:    []string{foreignAssetKey},
	})
	if !errors.Is(err, ErrReportAssetOwnershipDenied) {
		t.Fatalf("expected ErrReportAssetOwnershipDenied, got %v", err)
	}
}
