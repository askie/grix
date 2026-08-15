package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
)

// /v1/skills（用户 JWT）与 /v1/agent-api/skills（connector api_key）的 HTTP 契约用例
// 自定义技能多机器同步。

// seedAPIAgent 给指定 owner 种一个 agent-api 类型 agent，返回其明文 api_key。
func seedAPIAgent(t *testing.T, ownerID int64) string {
	t.Helper()
	apiKey := fmt.Sprintf("e2e-skill-key-%d", ownerID)
	agent := model.Agent{
		ID:           snowflake.GenID(),
		AgentName:    "skill-e2e-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   agentapi.HashAPIKey(apiKey),
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("seed api agent: %v", err)
	}
	return apiKey
}

func TestSkillRESTCRUD(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()
	token, _ := ctx.loginHelper(t, "skill-crud@example.com", "Passw0rd123")

	// 新建。
	w := ctx.doReq(t, "POST", "/v1/skills", token, map[string]string{
		"name": "报告规范", "content": "# 规范\n结论先行",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create: want 200 got %d body=%s", w.Code, w.Body.String())
	}
	created := parseResp(t, w)["data"].(map[string]interface{})
	id := created["id"].(string)

	// 同名再建 → 409 / 27003。
	w = ctx.doReq(t, "POST", "/v1/skills", token, map[string]string{
		"name": "报告规范", "content": "别的",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("dup create: want 409 got %d body=%s", w.Code, w.Body.String())
	}

	// 非法名 → 400 / 27008。
	w = ctx.doReq(t, "POST", "/v1/skills", token, map[string]string{
		"name": "../evil", "content": "x",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad name: want 400 got %d body=%s", w.Code, w.Body.String())
	}

	// 列表含摘要、不含正文。
	w = ctx.doReq(t, "GET", "/v1/skills", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: want 200 got %d", w.Code)
	}
	items := parseResp(t, w)["data"].(map[string]interface{})["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("list items want 1 got %d", len(items))
	}
	if _, has := items[0].(map[string]interface{})["content"]; has {
		t.Fatalf("list summary should not carry content")
	}

	// 详情带正文。
	w = ctx.doReq(t, "GET", "/v1/skills/"+id+"/content", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("content: want 200 got %d", w.Code)
	}
	full := parseResp(t, w)["data"].(map[string]interface{})
	if full["content"] != "# 规范\n结论先行" {
		t.Fatalf("content mismatch: %v", full["content"])
	}

	// 更新 → 版本自增。
	w = ctx.doReq(t, "PUT", "/v1/skills/"+id, token, map[string]string{
		"name": "报告规范", "content": "# 规范 v2",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("update: want 200 got %d body=%s", w.Code, w.Body.String())
	}
	updated := parseResp(t, w)["data"].(map[string]interface{})
	if updated["version"] != "2" {
		t.Fatalf("version want \"2\" got %v", updated["version"])
	}

	// 删除后列表为空。
	w = ctx.doReq(t, "DELETE", "/v1/skills/"+id, token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: want 200 got %d", w.Code)
	}
	w = ctx.doReq(t, "GET", "/v1/skills", token, nil)
	items = parseResp(t, w)["data"].(map[string]interface{})["items"].([]interface{})
	if len(items) != 0 {
		t.Fatalf("after delete want 0 got %d", len(items))
	}
}

func TestSkillRESTOwnerIsolationAndUpload(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()
	tokenA, _ := ctx.loginHelper(t, "skill-owner-a@example.com", "Passw0rd123")
	tokenB, _ := ctx.loginHelper(t, "skill-owner-b@example.com", "Passw0rd123")

	// A 上载（按名 upsert，重复上载同内容不膨胀版本）。
	w := ctx.doReq(t, "POST", "/v1/skills/upload", tokenA, map[string]string{
		"name": "同步技能", "content": "内容一",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("upload: want 200 got %d body=%s", w.Code, w.Body.String())
	}
	first := parseResp(t, w)["data"].(map[string]interface{})
	w = ctx.doReq(t, "POST", "/v1/skills/upload", tokenA, map[string]string{
		"name": "同步技能", "content": "内容一",
	})
	again := parseResp(t, w)["data"].(map[string]interface{})
	if first["version"] != again["version"] {
		t.Fatalf("idempotent upload bumped version: %v -> %v", first["version"], again["version"])
	}

	// B 看不到 A 的技能，也读不到其正文。
	w = ctx.doReq(t, "GET", "/v1/skills", tokenB, nil)
	items := parseResp(t, w)["data"].(map[string]interface{})["items"].([]interface{})
	if len(items) != 0 {
		t.Fatalf("owner B should see 0 skills, got %d", len(items))
	}
	w = ctx.doReq(t, "GET", "/v1/skills/"+first["id"].(string)+"/content", tokenB, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-owner content read: want 404 got %d", w.Code)
	}
}

func TestSkillAgentAPISync(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()
	token, userID := ctx.loginHelper(t, "skill-agent@example.com", "Passw0rd123")
	apiKey := seedAPIAgent(t, userID)

	// 用户侧建一条技能。
	w := ctx.doReq(t, "POST", "/v1/skills", token, map[string]string{
		"name": "机器同步", "content": "sync me",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create: want 200 got %d", w.Code)
	}

	// connector 以 api_key 拉清单（与用户侧同一份库）。
	w = ctx.doReq(t, "GET", "/v1/agent-api/skills", apiKey, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("agent list: want 200 got %d body=%s", w.Code, w.Body.String())
	}
	items := parseResp(t, w)["data"].(map[string]interface{})["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("agent list want 1 got %d", len(items))
	}
	item := items[0].(map[string]interface{})
	if item["name"] != "机器同步" {
		t.Fatalf("unexpected item: %v", item)
	}

	// connector 拉全文。
	w = ctx.doReq(t, "GET", "/v1/agent-api/skills/"+item["id"].(string)+"/content", apiKey, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("agent content: want 200 got %d", w.Code)
	}
	if got := parseResp(t, w)["data"].(map[string]interface{})["content"]; got != "sync me" {
		t.Fatalf("agent content mismatch: %v", got)
	}

	// connector 上载本地技能 → 用户列表可见。
	w = ctx.doReq(t, "POST", "/v1/agent-api/skills/upload", apiKey, map[string]string{
		"name": "本地技能", "content": "from machine",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("agent upload: want 200 got %d body=%s", w.Code, w.Body.String())
	}
	w = ctx.doReq(t, "GET", "/v1/skills", token, nil)
	items = parseResp(t, w)["data"].(map[string]interface{})["items"].([]interface{})
	names := make([]string, 0, len(items))
	for _, it := range items {
		names = append(names, it.(map[string]interface{})["name"].(string))
	}
	if len(items) != 2 || !strings.Contains(strings.Join(names, ","), "本地技能") {
		t.Fatalf("user should see uploaded skill, got %v", names)
	}

	// connector 按名删除（幂等）。
	w = ctx.doReq(t, "POST", "/v1/agent-api/skills/delete", apiKey, map[string]string{"name": "本地技能"})
	if w.Code != http.StatusOK {
		t.Fatalf("agent delete: want 200 got %d", w.Code)
	}
	w = ctx.doReq(t, "POST", "/v1/agent-api/skills/delete", apiKey, map[string]string{"name": "本地技能"})
	if w.Code != http.StatusOK {
		t.Fatalf("agent delete idempotent: want 200 got %d", w.Code)
	}
}
