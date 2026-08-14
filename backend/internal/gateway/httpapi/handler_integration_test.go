package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/askie/grix/backend/internal/gateway/credential"
	"github.com/askie/grix/backend/internal/gateway/pricing"
	"github.com/askie/grix/backend/internal/gateway/wallet"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
)

// fixture 一次性搭出一整套可通过 serve() 主链路的依赖：
// SQLite in-memory DB + wallets + credentials + pricing + gin router，
// 上游 URL 用 httptest 桩替换，测试可自由控制上游返回。
type fixture struct {
	t         *testing.T
	handler   *Handler
	router    *gin.Engine
	upstream  *httptest.Server
	plainKey  string
	walletID  int64
	credID    int64
	pricingID int64
}

// newFixture 建整套依赖。缺省创建 deepseek 一把可用 credential + deepseek-chat 全天兜底价 + 有余额钱包。
// upstreamRoundTrip 是所有到"厂商"的请求实际打到的 handler；测试用来伪装 DeepSeek。
func newFixture(t *testing.T, upstreamRoundTrip http.HandlerFunc) *fixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	logger.Init()
	_ = snowflake.Init(1)

	tdb := testutil.NewTestDB()
	t.Cleanup(tdb.Close)

	upstream := httptest.NewServer(upstreamRoundTrip)
	t.Cleanup(upstream.Close)

	wallets := wallet.New(tdb.DB)
	creds := credential.New(tdb.DB)
	prices := pricing.New(tdb.DB)

	w, err := wallets.CreateWallet(1234567890)
	require.NoError(t, err)
	// 直接改余额到 100 USD,免走 topup 事务(SQLite 对 clause.Locking 支持有限,但 topup 已被 wallet 层 tx 处理)
	require.NoError(t, tdb.DB.Model(&model.GatewayWallet{}).Where("id = ?", w.ID).Update("balance", decimal.NewFromInt(100)).Error)

	plain, _, err := wallets.IssueVirtualKey(w.ID, "test")
	require.NoError(t, err)

	// upstream 桩的 URL 作为 BaseURL —— credential.Resolved.BaseURL 会直接被适配器采用
	cred, err := creds.Create(credential.CreateInput{
		Provider: "deepseek",
		APIKey:   "sk-upstream-official-key",
		BaseURL:  upstream.URL, // 让 NewDeepSeek 和 Anthropic buildUpstream 都指向本地桩
		Label:    "deepseek-main",
	})
	require.NoError(t, err)

	rule, err := prices.CreateRule(pricing.CreateRuleInput{
		Provider: "deepseek", Model: "deepseek-v4-flash",
		Cached: "0.5", Uncached: "1.0", Output: "2.0",
		SourceCurrency: "USD", FxRate: decimal.NewFromInt(1),
	})
	require.NoError(t, err)

	h := &Handler{Wallets: wallets, Pricing: prices, Credentials: creds}
	r := gin.New()
	r.POST("/openai/v1/chat/completions", h.ChatCompletions)
	r.POST("/openai/v1/responses", h.Responses)
	r.POST("/anthropic/v1/messages", h.Messages)
	r.POST("/anthropic/v1/responses", h.Responses)

	return &fixture{
		t:         t,
		handler:   h,
		router:    r,
		upstream:  upstream,
		plainKey:  plain,
		walletID:  w.ID,
		credID:    cred.ID,
		pricingID: rule.ID,
	}
}

func (f *fixture) balance() decimal.Decimal {
	got, err := f.handler.Wallets.GetWallet(f.walletID)
	require.NoError(f.t, err)
	return got.Balance
}

// --- Anthropic 入口:完整链路 SSE 通过 + 计费扣款 ---

func TestIntegrationAnthropicSSESettlesCorrectly(t *testing.T) {
	// 桩上游:模拟 DeepSeek Anthropic 端点,SSE 流带 message_start + message_delta
	var gotPath, gotKey, gotVer string
	fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		gotVer = r.Header.Get("anthropic-version")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `event: message_start
data: {"type":"message_start","message":{"id":"a","type":"message","role":"assistant","model":"deepseek-v4-flash","content":[],"usage":{"input_tokens":1000000,"cache_creation_input_tokens":0,"cache_read_input_tokens":500000,"output_tokens":0}}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":250000}}

event: message_stop
data: {"type":"message_stop"}

`)
	})
	beforeBalance := fx.balance()

	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages",
		strings.NewReader(`{"model":"deepseek-chat","stream":true,"max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("x-api-key", fx.plainKey) // Anthropic 原生 x-api-key 头
	rec := httptest.NewRecorder()
	fx.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	// SSE 已透传给客户端
	assert.Contains(t, rec.Body.String(), "event: message_start")
	assert.Contains(t, rec.Body.String(), "event: message_stop")
	// 上游转发头/路径正确:BaseURL 是 upstream 桩根,buildAnthropicUpstream 补 /anthropic,adapter 补 /v1/messages
	assert.Equal(t, "/anthropic/v1/messages", gotPath)
	assert.Equal(t, "sk-upstream-official-key", gotKey)
	assert.Equal(t, "2023-06-01", gotVer)

	// 计费:CachedTokens=500k,UncachedTokens=1M,Output=250k
	// 单价:cached 0.5/M, uncached 1.0/M, output 2.0/M -> 0.5*0.5 + 1.0*1 + 2.0*0.25 = 0.25+1+0.5 = 1.75 USD
	afterBalance := fx.balance()
	cost := beforeBalance.Sub(afterBalance)
	assert.True(t, cost.Equal(decimal.NewFromFloat(1.75)),
		"结算金额应为 1.75 USD,实际 %s", cost.String())
}

// --- OpenAI Responses: DeepSeek 走官方 Responses，计费照常 ---

func TestIntegrationOpenAIResponsesSettlesWithServableModel(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp-up","object":"response","created_at":1800000000,"status":"completed","model":"deepseek-v4-flash","output":[{"type":"message","id":"msg-up","role":"assistant","status":"completed","content":[{"type":"output_text","text":"pong","annotations":[]}]}],"usage":{"input_tokens":1000,"input_tokens_details":{"cached_tokens":300},"output_tokens":500,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":1500}}`)
	})
	beforeBalance := fx.balance()

	req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses",
		strings.NewReader(`{"model":"deepseek-chat","instructions":"be brief","input":"ping"}`))
	req.Header.Set("Authorization", "Bearer "+fx.plainKey)
	rec := httptest.NewRecorder()
	fx.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "/responses", gotPath)
	assert.Equal(t, "deepseek-v4-flash", gotBody["model"])
	assert.Equal(t, false, gotBody["store"])
	assert.Equal(t, "be brief", gotBody["instructions"])
	assert.Equal(t, "ping", gotBody["input"])

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "response", resp["object"])
	assert.Equal(t, "completed", resp["status"])
	output := resp["output"].([]any)
	require.NotEmpty(t, output)
	assert.Equal(t, "message", output[0].(map[string]any)["type"])

	afterBalance := fx.balance()
	cost := beforeBalance.Sub(afterBalance)
	diff := cost.Sub(decimal.NewFromFloat(0.00185)).Abs()
	assert.True(t, diff.LessThanOrEqual(decimal.New(1, -9)),
		"Responses 结算应为 0.00185 USD,实际 %s", cost.String())
}

// failAfterWriter 模拟客户端中途断开：写过 failAfter 字节后 Write 失败。
type failAfterWriter struct {
	http.ResponseWriter
	failAfter int
	written   int
}

func (f *failAfterWriter) Write(p []byte) (int, error) {
	if f.written >= f.failAfter {
		return 0, io.ErrClosedPipe
	}
	n, err := f.ResponseWriter.Write(p)
	f.written += n
	return n, err
}

func (f *failAfterWriter) Flush() {
	if fl, ok := f.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
}

// 客户端断流时上游仍返回 usage：必须 Settle，不能因 Finish/写失败白嫖。
func TestIntegrationOpenAIResponsesStreamClientDisconnectStillSettles(t *testing.T) {
	fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.created\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-up\",\"object\":\"response\",\"status\":\"in_progress\",\"model\":\"deepseek-v4-flash\"}}\n\n")
		_, _ = io.WriteString(w, "event: response.output_text.delta\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hi\",\"output_index\":0,\"content_index\":0,\"item_id\":\"msg-up\"}\n\n")
		_, _ = io.WriteString(w, "event: response.completed\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-up\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"deepseek-v4-flash\",\"usage\":{\"input_tokens\":1000,\"input_tokens_details\":{\"cached_tokens\":300},\"output_tokens\":500,\"output_tokens_details\":{\"reasoning_tokens\":0},\"total_tokens\":1500}}}\n\n")
	})
	beforeBalance := fx.balance()

	req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses",
		strings.NewReader(`{"model":"deepseek-chat","input":"ping","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+fx.plainKey)
	rec := httptest.NewRecorder()
	failW := &failAfterWriter{ResponseWriter: rec, failAfter: 8} // 第一个 event 就断
	fx.router.ServeHTTP(failW, req)

	afterBalance := fx.balance()
	cost := beforeBalance.Sub(afterBalance)
	diff := cost.Sub(decimal.NewFromFloat(0.00185)).Abs()
	assert.True(t, diff.LessThanOrEqual(decimal.New(1, -9)),
		"客户端断流仍应结算 0.00185 USD,实际 %s (before=%s after=%s)", cost.String(), beforeBalance, afterBalance)
}

// --- OpenAI 回归:完整链路仍走通 + 计费不受 serve() 抽取影响 ---

func TestIntegrationOpenAINonStreamStillWorks(t *testing.T) {
	fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chat-1","object":"chat.completion","model":"deepseek-v4-flash","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1000,"prompt_cache_hit_tokens":300,"prompt_cache_miss_tokens":700,"completion_tokens":500,"total_tokens":1500}}`)
	})
	beforeBalance := fx.balance()

	req := httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions",
		strings.NewReader(`{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer "+fx.plainKey)
	rec := httptest.NewRecorder()
	fx.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	// 上游响应原样透传
	assert.Contains(t, rec.Body.String(), `"id":"chat-1"`)

	// cache_hit=300, cache_miss=700, completion=500;单价 /M tokens
	// cost = 300/1e6 * 0.5 + 700/1e6 * 1.0 + 500/1e6 * 2.0 = 0.00015+0.0007+0.001 = 0.00185 USD
	afterBalance := fx.balance()
	cost := beforeBalance.Sub(afterBalance)
	// SQLite NUMERIC 列以 REAL 存储，Windows 下余额回读带 ~1e-14 浮点误差（0.00185 非二进制精确值），
	// 用 1e-9 容差断言；真实计费错误的量级远大于该容差，不影响测试意图。
	diff := cost.Sub(decimal.NewFromFloat(0.00185)).Abs()
	assert.True(t, diff.LessThanOrEqual(decimal.New(1, -9)),
		"OpenAI 结算应为 0.00185 USD,实际 %s", cost.String())
}

// --- 缺 Key:两种协议错误体格式各自正确 ---

func TestIntegrationAnthropicMissingXApiKey(t *testing.T) {
	fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("上游不应被调用")
	})
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages",
		strings.NewReader(`{"model":"deepseek-chat"}`))
	rec := httptest.NewRecorder()
	fx.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "error", resp["type"], "Anthropic 错误体顶层 type=error")
	errObj := resp["error"].(map[string]any)
	assert.Equal(t, "missing_api_key", errObj["type"])
}

func TestIntegrationOpenAIMissingBearer(t *testing.T) {
	fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("上游不应被调用")
	})
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions",
		strings.NewReader(`{"model":"deepseek-chat"}`))
	rec := httptest.NewRecorder()
	fx.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	// OpenAI 格式:无顶层 type,error 对象含 code+message
	assert.Nil(t, resp["type"])
	errObj := resp["error"].(map[string]any)
	assert.Equal(t, "missing_api_key", errObj["type"])
}

// --- 非 deepseek 厂商走 Anthropic 入口:因适配器不支持而报清晰错 ---

func TestIntegrationAnthropicVolcanoNotSupported(t *testing.T) {
	fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("上游不应被调用")
	})
	// 补一条 volcano_ark 的 credential + pricing,让请求走到 buildUpstream 才失败
	// (否则会在"取不到 credential"或"没定价规则"提前失败)
	_, err := fx.handler.Credentials.Create(credential.CreateInput{
		Provider: "volcano_ark", APIKey: "vk-doubao", Label: "doubao",
	})
	require.NoError(t, err)
	_, err = fx.handler.Pricing.CreateRule(pricing.CreateRuleInput{
		Provider: "volcano_ark", Model: "doubao-1.5-pro",
		Cached: "0.5", Uncached: "1.0", Output: "2.0",
		SourceCurrency: "USD", FxRate: decimal.NewFromInt(1),
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages",
		strings.NewReader(`{"model":"doubao-1.5-pro","messages":[]}`))
	req.Header.Set("x-api-key", fx.plainKey)
	rec := httptest.NewRecorder()
	fx.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "error", resp["type"])
	errObj := resp["error"].(map[string]any)
	assert.Equal(t, "unsupported_model", errObj["type"])
	msg := errObj["message"].(string)
	assert.Contains(t, msg, "volcano_ark", "报错文案应带厂商名")
	assert.Contains(t, msg, "anthropic", "报错文案应说明是在 anthropic 协议上不支持")
}

// --- 断线不白嫖:客户端在 SSE 中途断开,gateway 仍读完上游拿 usage 并结算 ---

func TestIntegrationClientCancelStillSettles(t *testing.T) {
	// 桩上游:每帧间隔 30ms,共 3 帧,末帧带最终 usage
	var upstreamDone int32
	frames := []string{
		`event: message_start
data: {"type":"message_start","message":{"id":"a","type":"message","role":"assistant","model":"deepseek-v4-flash","content":[],"usage":{"input_tokens":1000000,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":0}}}

`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"streaming"}}

`,
		`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":500000}}

event: message_stop
data: {"type":"message_stop"}

`,
	}
	fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		for _, chunk := range frames {
			_, _ = io.WriteString(w, chunk)
			f.Flush()
		}
		atomic.StoreInt32(&upstreamDone, 1)
	})
	beforeBalance := fx.balance()

	// 用普通 http.Client + 真监听端口跑,这样才能模拟"客户端断线"这个真实网络场景
	realSrv := httptest.NewServer(fx.router)
	defer realSrv.Close()

	body := `{"model":"deepseek-chat","stream":true,"max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`
	req, err := http.NewRequest(http.MethodPost, realSrv.URL+"/anthropic/v1/messages", bytes.NewBufferString(body))
	require.NoError(t, err)
	req.Header.Set("x-api-key", fx.plainKey)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	// 立刻关闭 body -> 客户端断线
	require.NoError(t, resp.Body.Close())

	// 等 gateway 后台继续读完上游流并结算(WithoutCancel 让厂商 ctx 不被 cancel)
	// 拉长等待时间足够走完 SSE + 结算 SQL 事务
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&upstreamDone) == 1 &&
			!fx.balance().Equal(beforeBalance)
	}, 3*time.Second, 20*time.Millisecond, "gateway 应在客户端断开后仍拉完上游 SSE 并结算")

	// uncached=1M, output=500k -> 1.0*1 + 2.0*0.5 = 2.0 USD
	cost := beforeBalance.Sub(fx.balance())
	assert.True(t, cost.Equal(decimal.NewFromFloat(2.0)),
		"断线场景应正常结算 2.0 USD,实际 %s", cost.String())
}

// --- 上游回显名查不到价目时必须按发出模型的价目收钱，绝不白嫖 ---
//
// 豆包 Ark 常回显带日期后缀的版本名（发 doubao-seed-evolving 回 doubao-seed-evolving-latest-version）。
// 修复前：Calculate(回显名) 失败 → RecordFailure → 用户白用，我们垫钱。
// 修复后：回落到"我们发出去的模型"的价目结算（计价前置保证那条必有基准价）。
func TestIntegrationEchoedModelWithoutPricingStillSettles(t *testing.T) {
	fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 关键：回显一个价目表里不存在的名字
		_, _ = io.WriteString(w, `{"id":"c1","object":"chat.completion","model":"deepseek-v4-flash-20991231-preview",`+
			`"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":1000000,"prompt_cache_hit_tokens":0,"prompt_cache_miss_tokens":1000000,"completion_tokens":500000,"total_tokens":1500000}}`)
	})
	beforeBalance := fx.balance()

	req := httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions",
		strings.NewReader(`{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer "+fx.plainKey)
	rec := httptest.NewRecorder()
	fx.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	// 必须扣到钱：按 deepseek-v4-flash（deepseek-chat 的规范名）的价目
	// uncached 1M*1.0 + output 0.5M*2.0 = 2.0 USD
	afterBalance := fx.balance()
	cost := beforeBalance.Sub(afterBalance)
	assert.True(t, cost.Equal(decimal.NewFromFloat(2.0)),
		"回显名无价目也必须按发出模型的价目扣款 2.0 USD，实际扣了 %s", cost.String())

	// 流水必须是一条 settled（不是 failed），且记的是回落后的计费模型
	entries, _, err := fx.handler.Wallets.ListLedgerEntries(fx.walletID, 1, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "settled", entries[0].Status)
	assert.Equal(t, "deepseek-v4-flash", entries[0].Model)
}
