package admin

import (
	"net/http"
	"strconv"

	eggservice "github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func registerEggAPIRoutes(g *gin.RouterGroup) {
	// 分类
	g.GET("/eggs/categories", apiListEggCategories)
	g.POST("/eggs/categories", apiCreateEggCategory)
	g.PUT("/eggs/categories/:id", apiUpdateEggCategory)
	g.POST("/eggs/categories/:id/status", apiUpdateEggCategoryStatus)
	// 彩蛋
	g.GET("/eggs", apiListEggs)
	g.GET("/eggs/:id", apiGetEgg)
	g.POST("/eggs", apiCreateEgg)
	g.PUT("/eggs/:id", apiUpdateEgg)
	g.POST("/eggs/:id/status", apiUpdateEggStatus)
	g.POST("/eggs/:id/pin", apiSetEggPinned)
	// 版本
	g.GET("/eggs/:id/versions", apiListEggVersions)
	g.POST("/eggs/:id/versions/presign", apiEggVersionPresign)
	g.POST("/eggs/:id/versions", apiCreateEggVersion)
	g.PUT("/eggs/:id/versions/:version", apiUpdateEggVersion)
}

func apiListEggCategories(c *gin.Context) {
	list, ec := eggservice.AdminEggCategoryList(eggservice.AdminEggCategoryListReq{})
	if ec != nil { response.Fail(c, ec.HTTPStatus, 10004, ec.Msg); return }
	response.OK(c, gin.H{"categories": list})
}

func apiCreateEggCategory(c *gin.Context) {
	var req eggservice.AdminEggCategoryCreateReq
	if err := c.ShouldBindJSON(&req); err != nil { response.Fail(c, http.StatusBadRequest, 10002, "参数错误"); return }
	created, ec := eggservice.AdminEggCategoryCreate(req)
	if ec != nil { response.Fail(c, ec.HTTPStatus, 10006, ec.Msg); return }
	response.OK(c, gin.H{"category": created})
}

func apiUpdateEggCategory(c *gin.Context) {
	var req eggservice.AdminEggCategoryUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil { response.Fail(c, http.StatusBadRequest, 10002, "参数错误"); return }
	updated, ec := eggservice.AdminEggCategoryUpdate(c.Param("id"), req)
	if ec != nil { response.Fail(c, ec.HTTPStatus, 10006, ec.Msg); return }
	response.OK(c, gin.H{"category": updated})
}

func apiUpdateEggCategoryStatus(c *gin.Context) {
	var req eggservice.AdminEggCategoryStatusReq
	if err := c.ShouldBindJSON(&req); err != nil { response.Fail(c, http.StatusBadRequest, 10002, "参数错误"); return }
	ec := eggservice.AdminEggCategoryUpdateStatus(c.Param("id"), req)
	if ec != nil { response.Fail(c, ec.HTTPStatus, 10006, ec.Msg); return }
	response.OK(c, gin.H{"ok": true})
}

func apiListEggs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	result, ec := eggservice.AdminEggList(eggservice.AdminEggListReq{
		Status: c.Query("status"), CategoryID: c.Query("category_id"),
		Keyword: c.Query("q"), Page: page, PageSize: pageSize,
	})
	if ec != nil { response.Fail(c, ec.HTTPStatus, 10004, ec.Msg); return }
	response.OK(c, gin.H{"list": result.List, "total": result.Total, "page": result.Page, "page_size": result.PageSize})
}

func apiGetEgg(c *gin.Context) {
	egg, ec := eggservice.AdminEggGet(c.Param("id"))
	if ec != nil { response.Fail(c, ec.HTTPStatus, 10005, ec.Msg); return }
	response.OK(c, gin.H{"egg": egg})
}

func apiCreateEgg(c *gin.Context) {
	var req eggservice.AdminEggCreateReq
	if err := c.ShouldBindJSON(&req); err != nil { response.Fail(c, http.StatusBadRequest, 10002, "参数错误"); return }
	created, ec := eggservice.AdminEggCreate(req)
	if ec != nil { response.Fail(c, ec.HTTPStatus, 10006, ec.Msg); return }
	response.OK(c, gin.H{"egg": created})
}

func apiUpdateEgg(c *gin.Context) {
	var req eggservice.AdminEggUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil { response.Fail(c, http.StatusBadRequest, 10002, "参数错误"); return }
	updated, ec := eggservice.AdminEggUpdate(c.Param("id"), req)
	if ec != nil { response.Fail(c, ec.HTTPStatus, 10006, ec.Msg); return }
	response.OK(c, gin.H{"egg": updated})
}

func apiUpdateEggStatus(c *gin.Context) {
	var req eggservice.AdminEggStatusReq
	if err := c.ShouldBindJSON(&req); err != nil { response.Fail(c, http.StatusBadRequest, 10002, "参数错误"); return }
	ec := eggservice.AdminEggUpdateStatus(c.Param("id"), req)
	if ec != nil { response.Fail(c, ec.HTTPStatus, 10006, ec.Msg); return }
	response.OK(c, gin.H{"ok": true})
}

func apiSetEggPinned(c *gin.Context) {
	var req eggservice.AdminEggPinReq
	if err := c.ShouldBindJSON(&req); err != nil { response.Fail(c, http.StatusBadRequest, 10002, "参数错误"); return }
	ec := eggservice.AdminEggSetPinned(c.Param("id"), req)
	if ec != nil { response.Fail(c, ec.HTTPStatus, 10006, ec.Msg); return }
	response.OK(c, gin.H{"ok": true})
}

func apiListEggVersions(c *gin.Context) {
	versions, ec := eggservice.AdminEggVersionList(c.Param("id"))
	if ec != nil { response.Fail(c, ec.HTTPStatus, 10004, ec.Msg); return }
	response.OK(c, gin.H{"versions": versions})
}

func apiEggVersionPresign(c *gin.Context) {
	var req eggservice.AdminEggVersionPresignReq
	if err := c.ShouldBindJSON(&req); err != nil { response.Fail(c, http.StatusBadRequest, 10002, "参数错误"); return }
	resp, ec := eggservice.AdminEggVersionPresign(c.Param("id"), req)
	if ec != nil { response.Fail(c, ec.HTTPStatus, 10006, ec.Msg); return }
	response.OK(c, gin.H{"upload_url": resp.UploadURL, "object_key": resp.ObjectKey, "zip_url": resp.ZipURL})
}

func apiCreateEggVersion(c *gin.Context) {
	var req eggservice.AdminEggVersionCreateReq
	if err := c.ShouldBindJSON(&req); err != nil { response.Fail(c, http.StatusBadRequest, 10002, "参数错误"); return }
	created, ec := eggservice.AdminEggVersionCreate(c.Param("id"), req)
	if ec != nil { response.Fail(c, ec.HTTPStatus, 10006, ec.Msg); return }
	response.OK(c, gin.H{"version": created})
}

func apiUpdateEggVersion(c *gin.Context) {
	version, _ := strconv.Atoi(c.Param("version"))
	var req eggservice.AdminEggVersionUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil { response.Fail(c, http.StatusBadRequest, 10002, "参数错误"); return }
	updated, ec := eggservice.AdminEggVersionUpdate(c.Param("id"), version, req)
	if ec != nil { response.Fail(c, ec.HTTPStatus, 10006, ec.Msg); return }
	response.OK(c, gin.H{"version": updated})
}
