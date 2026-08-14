package handler

import (
	"errors"
	"net/http"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type reportAssetPresignReq struct {
	Filename    string `json:"filename" binding:"required"`
	ContentType string `json:"content_type" binding:"required"`
}

func ReportAssetPresign(c *gin.Context) {
	var req reportAssetPresignReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	userID := middleware.GetUserID(c)
	data, err := service.PresignReportAsset(userID, req.Filename, req.ContentType)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrReportAssetFilenameRequired),
			errors.Is(err, service.ErrReportAssetContentTypeMiss),
			errors.Is(err, service.ErrReportAssetContentTypeBad):
			response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		default:
			response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		}
		return
	}

	response.OK(c, data)
}

func ReportCreate(c *gin.Context) {
	var req service.CreateReportReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	userID := middleware.GetUserID(c)
	data, err := service.CreateReport(userID, req)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrReportTargetTypeInvalid),
			errors.Is(err, service.ErrReportReasonInvalid),
			errors.Is(err, service.ErrReportSelfNotAllowed),
			errors.Is(err, service.ErrReportDescriptionLong),
			errors.Is(err, service.ErrReportAssetFilenameRequired),
			errors.Is(err, service.ErrReportAssetContentTypeMiss),
			errors.Is(err, service.ErrReportAssetContentTypeBad),
			errors.Is(err, service.ErrReportAssetCountInvalid),
			errors.Is(err, service.ErrReportAssetTooLarge):
			response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		case errors.Is(err, service.ErrReportPermissionDenied),
			errors.Is(err, service.ErrReportAssetOwnershipDenied):
			response.Fail(c, http.StatusForbidden, 4003, err.Error())
		case errors.Is(err, service.ErrReportTargetNotFound),
			errors.Is(err, service.ErrReportAssetNotFound):
			response.Fail(c, http.StatusNotFound, 10004, err.Error())
		default:
			response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		}
		return
	}

	response.OK(c, data)
}
