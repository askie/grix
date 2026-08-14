package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
)

type userProfileFailResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func TestUpdateProfile_RejectsDirectAvatarURLInput(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", int64(1001))
		c.Next()
	})
	r.PUT("/users/profile", UpdateProfile)

	body := []byte(`{"nickname":"new nick","avatar_url":"https://example.com/a.png"}`)
	req, _ := http.NewRequest(http.MethodPut, "/users/profile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
	}

	var resp userProfileFailResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response error: %v", err)
	}
	if resp.Code != 10003 {
		t.Fatalf("expected code 10003, got %d, msg: %s", resp.Code, resp.Msg)
	}
	if resp.Msg != "头像必须通过上传接口设置" {
		t.Fatalf("expected avatar validation msg, got %q", resp.Msg)
	}
}
