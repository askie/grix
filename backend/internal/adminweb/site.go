package adminweb

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

const (
	embeddedDistDir = "dist"
	// routePrefix is the URL prefix under which the admin SPA is served.
	// It must match the Flutter build's --base-href.
	routePrefix = "/admin"
)

//go:embed all:dist
var embeddedFiles embed.FS

func init() {
	_ = mime.AddExtensionType(".wasm", "application/wasm")
}

type siteHandler struct {
	root fs.FS
}

// RegisterRoutes mounts the embedded admin web bundle under /admin.
// It returns false when no bundle is embedded (no dist/index.html), leaving
// the route unregistered so the caller can fall back to other handlers.
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
	// Serve the admin SPA from a global middleware rather than registered
	// routes. A registered catch-all (/admin/*filepath) conflicts with the
	// existing top-level /api route in gin's radix tree and panics at startup.
	// Middleware runs before route matching and adds nothing to the tree —
	// the same reason webapp uses NoRoute instead of a catch-all.
	r.Use(handler.middleware)
	return true
}

// middleware serves the embedded admin SPA for GET/HEAD requests under /admin
// and passes every other request straight through.
func (h siteHandler) middleware(c *gin.Context) {
	requestPath := normalizeRequestPath(c.Request.URL.Path)
	if !isAdminPath(requestPath) {
		c.Next()
		return
	}
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		c.Next()
		return
	}

	assetPath, serveIndex := h.resolve(requestPath)
	if serveIndex {
		h.serveFile(c, "index.html")
		c.Abort()
		return
	}
	if assetPath == "" {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	h.serveFile(c, assetPath)
	c.Abort()
}

// isAdminPath reports whether a normalized path targets the admin SPA mount.
// It matches "/admin" and "/admin/..." but not unrelated prefixes like
// "/adminx" or the API group "/admin/api/...".
func isAdminPath(normalizedPath string) bool {
	return normalizedPath == routePrefix || strings.HasPrefix(normalizedPath, routePrefix+"/")
}

// resolve maps a request path (already normalized, prefixed with /admin) to an
// embedded asset path. serveIndex signals the SPA fallback to index.html.
func (h siteHandler) resolve(requestPath string) (assetPath string, serveIndex bool) {
	rel := strings.TrimPrefix(requestPath, routePrefix)
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return "index.html", true
	}

	if fileExists(h.root, rel) {
		return rel, false
	}
	// A request for a concrete file (has an extension) that does not exist
	// is a genuine 404; anything else falls back to the SPA shell.
	if path.Ext(rel) != "" {
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

func fileExists(root fs.FS, name string) bool {
	info, err := fs.Stat(root, name)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
