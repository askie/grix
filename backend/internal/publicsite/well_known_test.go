package publicsite

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/askie/grix/backend/config"
	"github.com/gin-gonic/gin"
)

func TestWellKnownRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origIOS := config.C.Server.DeepLinkIOSAppID
	origPkg := config.C.Server.DeepLinkAndroidPackage
	origCerts := config.C.Server.DeepLinkAndroidSHA256Certs
	t.Cleanup(func() {
		config.C.Server.DeepLinkIOSAppID = origIOS
		config.C.Server.DeepLinkAndroidPackage = origPkg
		config.C.Server.DeepLinkAndroidSHA256Certs = origCerts
	})

	config.C.Server.DeepLinkIOSAppID = "MYTEAMID.pub.dhf.grix"
	config.C.Server.DeepLinkAndroidPackage = "pub.dhf.grix"
	config.C.Server.DeepLinkAndroidSHA256Certs = "AA:BB:CC, DD:EE:FF"

	r := gin.New()
	registerWellKnownRoutes(r)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/apple-app-site-association", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("AASA status=%d", w.Code)
	}

	var aasa map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &aasa); err != nil {
		t.Fatalf("decode AASA: %v", err)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/.well-known/assetlinks.json", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assetlinks status=%d", w.Code)
	}

	var assetlinks []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &assetlinks); err != nil {
		t.Fatalf("decode assetlinks: %v", err)
	}
	if len(assetlinks) != 1 {
		t.Fatalf("unexpected assetlinks length=%d", len(assetlinks))
	}
}
