package service

import (
	"errors"
	"strings"
	"time"

	apiservice "github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

const reportAttachmentViewTTL = 5 * time.Minute

var ErrReportAttachmentNotFound = errors.New("举报截图不存在")

var reportAttachmentViewURLBuilder = apiservice.PresignReportAssetViewURL

func GetReportAttachmentViewURL(reportID, attachmentID int64) (string, error) {
	var attachment model.ReportAttachment
	if err := store.DB.Where("report_id = ? AND id = ?", reportID, attachmentID).
		First(&attachment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrReportAttachmentNotFound
		}
		return "", err
	}

	return BuildReportAttachmentViewURL(attachment.ObjectKey)
}

func BuildReportAttachmentViewURL(objectKey string) (string, error) {
	viewURL, err := reportAttachmentViewURLBuilder(objectKey, reportAttachmentViewTTL)
	if err != nil {
		if errors.Is(err, apiservice.ErrReportAssetNotFound) {
			return "", ErrReportAttachmentNotFound
		}
		return "", err
	}
	if strings.TrimSpace(viewURL) == "" {
		return "", ErrReportAttachmentNotFound
	}
	return viewURL, nil
}
