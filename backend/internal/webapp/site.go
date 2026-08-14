package webapp

import (
	"bytes"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

const embeddedDistDir = "dist"

var reservedPrefixes = []string{
	"/admin",
	"/legal",
	"/public/assets",
	"/v1",
	"/ws",
}

var reservedExactPaths = map[string]struct{}{
	"/health":  {},
	"/support": {},
}

//go:embed all:dist
var embeddedFiles embed.FS

func init() {
	_ = mime.AddExtensionType(".wasm", "application/wasm")
}

type siteHandler struct {
	root fs.FS
}

func RegisterRoutes(r *gin.Engine) bool {
	if r == nil {
		return false
	}

	rootFS, err := fs.Sub(embeddedFiles, embeddedDistDir)
	if err != nil {
		panic(err)
	}
	return registerRoutesWithRootFS(r, rootFS)
}

func registerRoutesWithRootFS(r *gin.Engine, rootFS fs.FS) bool {
	if !fileExists(rootFS, "index.html") {
		return false
	}

	handler := siteHandler{root: rootFS}
	r.NoRoute(handler.handleNoRoute)
	return true
}

func (h siteHandler) handleNoRoute(c *gin.Context) {
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	requestPath := normalizeRequestPath(c.Request.URL.Path)
	if isReservedPath(requestPath) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	assetPath, serveIndex := h.resolve(requestPath)
	if serveIndex {
		h.serveFile(c, "index.html")
		return
	}
	if assetPath == "" {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	h.serveFile(c, assetPath)
}

func (h siteHandler) resolve(requestPath string) (assetPath string, serveIndex bool) {
	if requestPath == "/" {
		return "index.html", true
	}

	candidate := strings.TrimPrefix(requestPath, "/")
	if fileExists(h.root, candidate) {
		return candidate, false
	}
	if path.Ext(candidate) != "" {
		return "", false
	}
	return "index.html", true
}

func (h siteHandler) serveFile(c *gin.Context, name string) {
	data, err := fs.ReadFile(h.root, name)
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}

	setCacheHeaders(c, name)

	c.DataFromReader(
		http.StatusOK,
		int64(len(data)),
		contentType,
		bytes.NewReader(data),
		nil,
	)
}

func setCacheHeaders(c *gin.Context, name string) {
	switch name {
	case "index.html",
		"flutter_bootstrap.js",
		"flutter_service_worker.js",
		"sw.js",
		"manifest.json",
		"version.json",
		".last_build_id":
		c.Header("Cache-Control", "no-cache, must-revalidate")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		return
	}

	if c.Query("build") != "" {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		return
	}

	c.Header("Cache-Control", "public, max-age=0, must-revalidate")
}

func normalizeRequestPath(rawPath string) string {
	cleaned := path.Clean("/" + rawPath)
	if cleaned == "." {
		return "/"
	}
	return cleaned
}

func isReservedPath(requestPath string) bool {
	if _, ok := reservedExactPaths[requestPath]; ok {
		return true
	}
	for _, prefix := range reservedPrefixes {
		if requestPath == prefix || strings.HasPrefix(requestPath, prefix+"/") {
			return true
		}
	}
	return false
}

func fileExists(root fs.FS, name string) bool {
	info, err := fs.Stat(root, name)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
