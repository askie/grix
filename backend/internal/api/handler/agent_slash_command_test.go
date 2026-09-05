package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
)

const (
	slashCmdHandlerOwner    = int64(92101)
	slashCmdHandlerStranger = int64(92102)
	slashCmdHandlerAgent    = int64(93101)
)

type slashCmdHandlerResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	} `json:"data"`
}

// newSlashCmdRouter 装上三条路由，userID 由测试指定，模拟已鉴权的调用者。
func newSlashCmdRouter(t *testing.T, userID int64) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	withUser := func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			c.Set("user_id", userID)
			next(c)
		}
	}
	r.GET("/agents/:id/slash-commands", withUser(AgentSlashCommandList))
	r.POST("/agents/:id/slash-commands", withUser(AgentSlashCommandCreate))
	r.DELETE("/agents/:id/slash-commands/:cmd_id", withUser(AgentSlashCommandDelete))
	return r
}

func setupSlashCmdHandlerTest(t *testing.T) {
	t.Helper()
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("init snowflake: %v", err)
	}
	testDB := testutil.NewTestDB()
	prevDB, prevRDB := store.DB, store.RDB
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		store.DB = prevDB
		store.RDB = prevRDB
		testDB.Close()
	})
	if err := store.DB.Create(&model.Agent{
		ID:              slashCmdHandlerAgent,
		AgentName:       "slash_cmd_handler_agent",
		OwnerID:         slashCmdHandlerOwner,
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeClaude,
		Status:          model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed agent error: %v", err)
	}
}

func doSlashCmdRequest(t *testing.T, r *gin.Engine, method, path, body string) (int, slashCmdHandlerResp) {
	t.Helper()
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var resp slashCmdHandlerResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode %s %s: %v body=%s", method, path, err, w.Body.String())
	}
	return w.Code, resp
}

func slashCmdPath() string {
	return "/agents/" + strconv.FormatInt(slashCmdHandlerAgent, 10) + "/slash-commands"
}

// 主人增删走通：新增后能在列表里查到，删除后列表清空。
func TestAgentSlashCommandHandlerCreateListDelete(t *testing.T) {
	setupSlashCmdHandlerTest(t)
	r := newSlashCmdRouter(t, slashCmdHandlerOwner)

	status, resp := doSlashCmdRequest(t, r, http.MethodPost, slashCmdPath(),
		`{"name":"/Deploy","description":"发布到预发环境"}`)
	if status != http.StatusOK || resp.Code != 0 {
		t.Fatalf("create status=%d code=%d msg=%s", status, resp.Code, resp.Msg)
	}
	if resp.Data.Name != "/deploy" {
		t.Fatalf("create name=%q want /deploy", resp.Data.Name)
	}
	commandID := resp.Data.ID

	status, resp = doSlashCmdRequest(t, r, http.MethodGet, slashCmdPath(), "")
	if status != http.StatusOK || len(resp.Data.Items) != 1 || resp.Data.Items[0].Name != "/deploy" {
		t.Fatalf("list status=%d items=%+v", status, resp.Data.Items)
	}

	status, resp = doSlashCmdRequest(t, r, http.MethodDelete, slashCmdPath()+"/"+commandID, "")
	if status != http.StatusOK || resp.Code != 0 {
		t.Fatalf("delete status=%d code=%d msg=%s", status, resp.Code, resp.Msg)
	}

	status, resp = doSlashCmdRequest(t, r, http.MethodGet, slashCmdPath(), "")
	if status != http.StatusOK || len(resp.Data.Items) != 0 {
		t.Fatalf("list after delete status=%d items=%+v", status, resp.Data.Items)
	}
}

// 错误码透传：非主人 403、同名 409、格式非法 400、命令 ID 非法 400。
func TestAgentSlashCommandHandlerErrors(t *testing.T) {
	setupSlashCmdHandlerTest(t)
	owner := newSlashCmdRouter(t, slashCmdHandlerOwner)
	stranger := newSlashCmdRouter(t, slashCmdHandlerStranger)

	status, resp := doSlashCmdRequest(t, stranger, http.MethodPost, slashCmdPath(), `{"name":"/deploy"}`)
	if status != http.StatusForbidden || resp.Code != errcode.ErrAgentForbidden.BizCode {
		t.Fatalf("non-owner create status=%d code=%d", status, resp.Code)
	}

	if status, resp = doSlashCmdRequest(t, owner, http.MethodPost, slashCmdPath(), `{"name":"/deploy"}`); status != http.StatusOK {
		t.Fatalf("seed create status=%d code=%d", status, resp.Code)
	}
	commandID := resp.Data.ID

	status, resp = doSlashCmdRequest(t, owner, http.MethodPost, slashCmdPath(), `{"name":"/deploy"}`)
	if status != http.StatusConflict || resp.Code != errcode.ErrSlashCommandExists.BizCode {
		t.Fatalf("duplicate status=%d code=%d", status, resp.Code)
	}

	status, resp = doSlashCmdRequest(t, owner, http.MethodPost, slashCmdPath(), `{"name":"deploy"}`)
	if status != http.StatusBadRequest || resp.Code != errcode.ErrSlashCommandNameInvalid.BizCode {
		t.Fatalf("invalid name status=%d code=%d", status, resp.Code)
	}

	status, resp = doSlashCmdRequest(t, owner, http.MethodPost, slashCmdPath(),
		`{"name":"/notes","description":"`+strings.Repeat("a", 201)+`"}`)
	if status != http.StatusBadRequest || resp.Code != errcode.ErrSlashCommandDescTooLong.BizCode {
		t.Fatalf("long description status=%d code=%d", status, resp.Code)
	}

	status, resp = doSlashCmdRequest(t, owner, http.MethodDelete, slashCmdPath()+"/not-a-number", "")
	if status != http.StatusBadRequest {
		t.Fatalf("bad command id status=%d code=%d", status, resp.Code)
	}

	status, resp = doSlashCmdRequest(t, stranger, http.MethodDelete, slashCmdPath()+"/"+commandID, "")
	if status != http.StatusForbidden || resp.Code != errcode.ErrAgentForbidden.BizCode {
		t.Fatalf("non-owner delete status=%d code=%d", status, resp.Code)
	}
}
