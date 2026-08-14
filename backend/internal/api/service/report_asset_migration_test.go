package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/minio/minio-go/v7"
	"gorm.io/datatypes"
)

func setupReportAssetMigrationTest(t *testing.T) (*testutil.TestDB, func()) {
	t.Helper()

	testDB := testutil.NewTestDB()

	originalDB := store.DB
	originalConfig := config.C
	originalCopy := reportAssetMigrationCopyObject
	originalReportClient := getOSSClient(ossStorageReport)

	store.DB = testDB.DB
	config.C.OSS.Report.Endpoint = "report-oss.example.com"
	config.C.OSS.Report.AccessKey = "report-ak"
	config.C.OSS.Report.SecretKey = "report-sk"
	config.C.OSS.Report.Bucket = "report-bucket"
	config.C.OSS.Report.Region = "ap-shanghai"
	config.C.OSS.Report.UseSSL = true
	config.C.OSS.Report.StorageDir = "aibot/report"
	config.C.Migration.LegacyOSS.Endpoint = "legacy-oss.example.com"
	config.C.Migration.LegacyOSS.AccessKey = "legacy-ak"
	config.C.Migration.LegacyOSS.SecretKey = "legacy-sk"
	config.C.Migration.LegacyOSS.Bucket = "shared-bucket"
	config.C.Migration.LegacyOSS.Region = "ap-shanghai"
	config.C.Migration.LegacyOSS.UseSSL = true
	config.C.Migration.LegacyOSS.StorageDir = "aibot"
	setOSSClient(ossStorageReport, &minio.Client{})

	cleanup := func() {
		store.DB = originalDB
		config.C = originalConfig
		reportAssetMigrationCopyObject = originalCopy
		setOSSClient(ossStorageReport, originalReportClient)
		testDB.Close()
	}
	return testDB, cleanup
}

func TestRunReportAssetMigration_CopiesDistinctObjectKeys(t *testing.T) {
	testDB, cleanup := setupReportAssetMigrationTest(t)
	defer cleanup()

	report := &model.Report{
		ID:               8301,
		ReporterUserID:   1,
		TargetType:       model.ReportTargetTypeUser,
		ReasonCode:       "other",
		Status:           model.ReportStatusPending,
		Resolution:       model.ReportResolutionUnset,
		ReporterSnapshot: mustReportJSONData(t, map[string]any{}),
		TargetSnapshot:   mustReportJSONData(t, map[string]any{}),
	}
	if err := testDB.DB.Create(report).Error; err != nil {
		t.Fatalf("create report failed: %v", err)
	}

	attachments := []model.ReportAttachment{
		{ReportID: report.ID, SlotNo: 1, ObjectKey: "aibot/report/report-assets/1/c.png", MimeType: "image/png", SizeBytes: 56},
		{ReportID: report.ID, SlotNo: 2, ObjectKey: "aibot/report/report-assets/1/a.png", MimeType: "image/png", SizeBytes: 12},
		{ReportID: report.ID, SlotNo: 3, ObjectKey: "aibot/report/report-assets/1/b.png", MimeType: "image/png", SizeBytes: 34},
		{ReportID: report.ID, SlotNo: 4, ObjectKey: "aibot/report/report-assets/1/a.png", MimeType: "image/png", SizeBytes: 12},
	}
	if err := testDB.DB.Create(&attachments).Error; err != nil {
		t.Fatalf("create attachments failed: %v", err)
	}

	var copied [][4]string
	reportAssetMigrationCopyObject = func(
		_ context.Context,
		_ *minio.Client,
		_ *minio.Client,
		sourceBucket string,
		sourceObjectKey string,
		targetBucket string,
		targetObjectKey string,
	) error {
		copied = append(copied, [4]string{
			sourceBucket,
			sourceObjectKey,
			targetBucket,
			targetObjectKey,
		})
		return nil
	}

	if err := RunReportAssetMigration(context.Background()); err != nil {
		t.Fatalf("RunReportAssetMigration() error = %v", err)
	}

	if len(copied) != 3 {
		t.Fatalf("expected 3 distinct object copies, got %d", len(copied))
	}
	if copied[0] != [4]string{"shared-bucket", "aibot/report/report-assets/1/a.png", "report-bucket", "aibot/report/report-assets/1/a.png"} {
		t.Fatalf("unexpected first copy: %#v", copied[0])
	}
	if copied[1] != [4]string{"shared-bucket", "aibot/report/report-assets/1/b.png", "report-bucket", "aibot/report/report-assets/1/b.png"} {
		t.Fatalf("unexpected second copy: %#v", copied[1])
	}
	if copied[2] != [4]string{"shared-bucket", "aibot/report/report-assets/1/c.png", "report-bucket", "aibot/report/report-assets/1/c.png"} {
		t.Fatalf("unexpected third copy: %#v", copied[2])
	}
}

func mustReportJSONData(t *testing.T, value any) datatypes.JSON {
	t.Helper()

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal report json: %v", err)
	}
	return datatypes.JSON(raw)
}
