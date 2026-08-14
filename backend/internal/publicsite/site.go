package publicsite

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

const (
	PrivacyPolicyPath   = "/legal/privacy-policy"
	TermsOfServicePath  = "/legal/terms-of-service"
	AccountDeletionPath = "/legal/account-deletion"
	SupportPath         = "/support"
	assetsBasePath      = "/public/assets"
	widgetBasePath      = "/public/widget"
)

type page struct {
	route    string
	fileName string
}

var pages = []page{
	{route: PrivacyPolicyPath, fileName: "privacy-policy.html"},
	{route: TermsOfServicePath, fileName: "terms-of-service.html"},
	{route: AccountDeletionPath, fileName: "account-deletion.html"},
	{route: SupportPath, fileName: "support.html"},
}

//go:embed assets/* assets/widget/* pages/*.html pages/widget/*.html
var embeddedFiles embed.FS

func RegisterRoutes(r gin.IRouter) {
	registerWellKnownRoutes(r)

	assetsFS, err := fs.Sub(embeddedFiles, "assets")
	if err != nil {
		panic(err)
	}
	pagesFS, err := fs.Sub(embeddedFiles, "pages")
	if err != nil {
		panic(err)
	}
	widgetPagesFS, err := fs.Sub(embeddedFiles, "pages/widget")
	if err != nil {
		panic(err)
	}

	r.StaticFS(assetsBasePath, http.FS(assetsFS))
	registerWidgetAsset(r, "/public/widget/widget.js", "assets/widget/widget.js")
	registerWidgetPage(r, widgetPagesFS, "/public/widget/frame.html", "frame.html")
	for _, page := range pages {
		currentPage := page
		handler := func(c *gin.Context) {
			servePage(c, pagesFS, currentPage.fileName)
		}
		r.GET(currentPage.route, handler)
		r.HEAD(currentPage.route, handler)
	}
}

func registerWidgetPage(r gin.IRouter, pagesFS fs.FS, route, fileName string) {
	handler := func(c *gin.Context) {
		servePage(c, pagesFS, fileName)
	}
	r.GET(route, handler)
	r.HEAD(route, handler)
}

func registerWidgetAsset(r gin.IRouter, route string, embeddedPath string) {
	handler := func(c *gin.Context) {
		serveEmbeddedFile(c, embeddedPath)
	}
	r.GET(route, handler)
	r.HEAD(route, handler)
}

func servePage(c *gin.Context, pagesFS fs.FS, fileName string) {
	if c.Request.Method == http.MethodHead {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Status(http.StatusOK)
		return
	}

	body, err := fs.ReadFile(pagesFS, fileName)
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load page")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", body)
}

func serveEmbeddedFile(c *gin.Context, embeddedPath string) {
	contentType := mime.TypeByExtension(filepath.Ext(embeddedPath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if c.Request.Method == http.MethodHead {
		c.Header("Content-Type", contentType)
		c.Status(http.StatusOK)
		return
	}
	body, err := embeddedFiles.ReadFile(embeddedPath)
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load file")
		return
	}
	c.Data(http.StatusOK, contentType, body)
}
