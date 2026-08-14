package handler

import (
	"net/http"

	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// --- User endpoint ---

type featureGateFeaturesResponse struct {
	Features []string `json:"features"`
}

// PublicGetFeatures returns globally enabled feature keys (no auth required).
func PublicGetFeatures(c *gin.Context) {
	features, err := featuregate.GetPublicFeatures()
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	if features == nil {
		features = []string{}
	}
	response.OK(c, featureGateFeaturesResponse{Features: features})
}

// UserGetFeatures returns the list of feature keys visible to the current user.
func UserGetFeatures(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == 0 {
		response.Fail(c, http.StatusUnauthorized, 10001, "未登录")
		return
	}

	features, err := featuregate.GetUserFeatures(userID)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	if features == nil {
		features = []string{}
	}
	response.OK(c, featureGateFeaturesResponse{Features: features})
}
