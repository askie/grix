package service

import (
	"errors"
	"testing"
	"time"

	apiservice "github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
)

func TestGetReportDetail_AttachmentUsesAdminRoute(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	originalBuilder := reportAttachmentViewURLBuilder
	t.Cleanup(func() {
		reportAttachmentViewURLBuilder = originalBuilder
	})

	report := createReportFixture(t, testDB, func(r *model.Report) {
		r.ID = 9101
	})
	attachment := &model.ReportAttachment{
		ID:        9201,
		ReportID:  report.ID,
		ObjectKey: "aibot/report/report-assets/1/a.png",
		SlotNo:    1,
		MimeType:  "image/png",
		SizeBytes: 1234,
	}
	if err := testDB.DB.Create(attachment).Error; err != nil {
		t.Fatalf("create attachment fixture: %v", err)
	}

	reportAttachmentViewURLBuilder = func(objectKey string, expiry time.Duration) (string, error) {
		t.Fatalf("GetReportDetail should not presign attachment URLs, got key=%q expiry=%s", objectKey, expiry)
		return "", nil
	}

	detail, err := GetReportDetail(report.ID)
	if err != nil {
		t.Fatalf("GetReportDetail() error = %v", err)
	}
	if len(detail.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(detail.Attachments))
	}
	if detail.Attachments[0].ID != attachment.ID {
		t.Fatalf("expected attachment ID %d, got %d", attachment.ID, detail.Attachments[0].ID)
	}
}

func TestBuildReportAttachmentViewURL_UsesSignedURL(t *testing.T) {
	originalBuilder := reportAttachmentViewURLBuilder
	t.Cleanup(func() {
		reportAttachmentViewURLBuilder = originalBuilder
	})

	reportAttachmentViewURLBuilder = func(objectKey string, expiry time.Duration) (string, error) {
		if objectKey != "aibot/report/report-assets/1/a.png" {
			t.Fatalf("unexpected object key: %q", objectKey)
		}
		if expiry != reportAttachmentViewTTL {
			t.Fatalf("unexpected expiry: %s", expiry)
		}
		return "https://oss.example.com/signed.png?token=abc", nil
	}

	viewURL, err := BuildReportAttachmentViewURL("aibot/report/report-assets/1/a.png")
	if err != nil {
		t.Fatalf("BuildReportAttachmentViewURL() error = %v", err)
	}
	if viewURL != "https://oss.example.com/signed.png?token=abc" {
		t.Fatalf("unexpected signed URL: %q", viewURL)
	}
}

func TestBuildReportAttachmentViewURL_NotFound(t *testing.T) {
	originalBuilder := reportAttachmentViewURLBuilder
	t.Cleanup(func() {
		reportAttachmentViewURLBuilder = originalBuilder
	})

	reportAttachmentViewURLBuilder = func(string, time.Duration) (string, error) {
		return "", apiservice.ErrReportAssetNotFound
	}

	_, err := BuildReportAttachmentViewURL("report-assets/1/missing.png")
	if !errors.Is(err, ErrReportAttachmentNotFound) {
		t.Fatalf("expected ErrReportAttachmentNotFound, got %v", err)
	}
}
