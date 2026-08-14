package publicsite

import (
	"net/http"
	"strings"

	"github.com/askie/grix/backend/config"
	"github.com/gin-gonic/gin"
)

func registerWellKnownRoutes(r gin.IRouter) {
	if r == nil {
		return
	}

	r.GET("/.well-known/apple-app-site-association", handleAASA)
	r.GET("/.well-known/assetlinks.json", handleAssetLinks)
}

func handleAASA(c *gin.Context) {
	appID := strings.TrimSpace(config.C.Server.DeepLinkIOSAppID)
	if appID == "" {
		c.Status(http.StatusNotFound)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"applinks": gin.H{
			"apps": []string{},
			"details": []gin.H{
				{
					"appID": appID,
					"paths": []string{"/u/*"},
				},
			},
		},
	})
}

func handleAssetLinks(c *gin.Context) {
	packageName := strings.TrimSpace(config.C.Server.DeepLinkAndroidPackage)
	fingerprints := splitCSVNonEmpty(config.C.Server.DeepLinkAndroidSHA256Certs)
	if packageName == "" || len(fingerprints) == 0 {
		c.Status(http.StatusNotFound)
		return
	}

	c.JSON(http.StatusOK, []gin.H{
		{
			"relation": []string{"delegate_permission/common.handle_all_urls"},
			"target": gin.H{
				"namespace":                "android_app",
				"package_name":             packageName,
				"sha256_cert_fingerprints": fingerprints,
			},
		},
	})
}

func splitCSVNonEmpty(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		normalized := strings.TrimSpace(part)
		if normalized == "" {
			continue
		}
		result = append(result, normalized)
	}
	return result
}
