package admin

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func registerAppReleaseAPIRoutes(g *gin.RouterGroup) {
	g.GET("/app/releases", apiListAppReleases)
	g.POST("/app/releases", apiCreateAppRelease)
	g.POST("/app/releases/:id/publish", apiPublishAppRelease)
	g.POST("/app/releases/:id/pause", apiPauseAppRelease)
	g.POST("/app/releases/:id/resume", apiResumeAppRelease)
	g.POST("/app/releases/:id/revoke", apiRevokeAppRelease)
	g.DELETE("/app/releases/:id", apiDeleteAppRelease)
	g.GET("/app/releases/:id/rules", apiListAppRolloutRules)
	g.POST("/app/rollout-rules", apiCreateAppRolloutRule)
	g.POST("/app/rollout-rules/:id/toggle", apiToggleAppRolloutRule)
	g.DELETE("/app/rollout-rules/:id", apiDeleteAppRolloutRule)
	g.GET("/app/stats/:id", apiAppDownloadStats)
}

func apiListAppReleases(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	result, ec := service.ListAppReleases(service.ListAppReleasesReq{
		Platform: c.Query("platform"),
		Channel:  c.Query("channel"),
		Page:     page,
		PageSize: pageSize,
	})
	if ec != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, ec.Msg)
		return
	}
	response.OK(c, gin.H{"releases": result.Releases, "total": result.Total, "page": page, "page_size": pageSize})
}

func apiCreateAppRelease(c *gin.Context) {
	var body struct {
		Version      string `json:"version"`
		BuildNumber  int    `json:"build_number"`
		Platform     string `json:"platform"`
		Channel      string `json:"channel"`
		Changelog    string `json:"changelog"`
		MinBuild     *int   `json:"min_build"`
		UpdateMethod string `json:"update_method"`
		DownloadURL  string `json:"download_url"`
		AppStoreURL  string `json:"app_store_url"`
		FileSize     int64  `json:"file_size"`
		Sha256       string `json:"sha256"`
		EddsaSig     string `json:"eddsa_signature"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	created, ec := service.CreateAppRelease(service.CreateAppReleaseReq{
		Version: body.Version, BuildNumber: body.BuildNumber,
		Platform: body.Platform, Channel: body.Channel,
		Changelog: body.Changelog, MinBuild: body.MinBuild,
		UpdateMethod: body.UpdateMethod, DownloadURL: body.DownloadURL,
		AppStoreURL: body.AppStoreURL, FileSize: body.FileSize, Sha256: body.Sha256,
		EddsaSignature: body.EddsaSig,
	})
	if ec != nil {
		response.Fail(c, http.StatusBadRequest, 10006, ec.Msg)
		return
	}
	response.OK(c, gin.H{"release": created})
}

func apiPublishAppRelease(c *gin.Context) { appReleaseStateAction(c, service.PublishAppRelease) }
func apiPauseAppRelease(c *gin.Context)   { appReleaseStateAction(c, service.PauseAppRelease) }
func apiResumeAppRelease(c *gin.Context)  { appReleaseStateAction(c, service.ResumeAppRelease) }
func apiRevokeAppRelease(c *gin.Context)  { appReleaseStateAction(c, service.RevokeAppRelease) }

func appReleaseStateAction(c *gin.Context, fn func(int64) (*service.AppReleaseResp, *errcode.ErrCode)) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "无效ID")
		return
	}
	_, ec := fn(id)
	if ec != nil {
		response.Fail(c, http.StatusBadRequest, 10006, ec.Msg)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func apiDeleteAppRelease(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "无效ID")
		return
	}
	if ec := service.DeleteAppRelease(id); ec != nil {
		response.Fail(c, http.StatusBadRequest, 10006, ec.Msg)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func apiListAppRolloutRules(c *gin.Context) {
	releaseID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	rules, ec := service.ListAppRolloutRules(releaseID)
	if ec != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, ec.Msg)
		return
	}
	response.OK(c, gin.H{"rules": rules})
}

func apiCreateAppRolloutRule(c *gin.Context) {
	var body struct {
		ReleaseID int64           `json:"release_id,string"`
		RuleType  string          `json:"rule_type"`
		RuleValue json.RawMessage `json:"rule_value"`
		Priority  int             `json:"priority"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	_, ec := service.CreateAppRolloutRule(service.CreateAppRolloutRuleReq{
		ReleaseID: body.ReleaseID, RuleType: body.RuleType,
		RuleValue: body.RuleValue, Priority: body.Priority,
	})
	if ec != nil {
		response.Fail(c, http.StatusBadRequest, 10006, ec.Msg)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func apiToggleAppRolloutRule(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var body struct {
		Status int16 `json:"status"`
	}
	_ = c.ShouldBindJSON(&body)
	_, ec := service.UpdateAppRolloutRuleStatus(id, body.Status)
	if ec != nil {
		response.Fail(c, http.StatusBadRequest, 10006, ec.Msg)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func apiDeleteAppRolloutRule(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if ec := service.DeleteAppRolloutRule(id); ec != nil {
		response.Fail(c, http.StatusBadRequest, 10006, ec.Msg)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func apiAppDownloadStats(c *gin.Context) {
	releaseID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	stats, ec := service.GetAppDownloadStats(releaseID)
	if ec != nil {
		response.Fail(c, http.StatusNotFound, 10005, ec.Msg)
		return
	}
	reports, _ := service.ListAppDownloadReports(service.ListAppDownloadReportsReq{
		ReleaseID: releaseID, Page: 1, PageSize: 50,
	})
	var reportList []service.AppDownloadReportResp
	var reportTotal int64
	if reports != nil {
		reportList = reports.Reports
		reportTotal = reports.Total
	}
	response.OK(c, gin.H{"stats": stats, "reports": reportList, "report_total": reportTotal})
}
