package admin

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/askie/grix/backend/internal/gateway/credential"
	"github.com/askie/grix/backend/internal/gateway/pricing"
	"github.com/askie/grix/backend/internal/gateway/reconcile"
	"github.com/askie/grix/backend/internal/gateway/wallet"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/askie/grix/backend/internal/store"
)

// registerGatewayAPIRoutes 注册"大模型计费网关"管理接口：钱包、虚拟Key、价目表、流水、对账报告。
// 底层直接复用 internal/gateway/{wallet,pricing,reconcile} 里已经用真实厂商验证过的业务逻辑，
// 这里只是包一层请求解析/响应格式，不重复实现一遍计费规则。
func registerGatewayAPIRoutes(g *gin.RouterGroup) {
	g.GET("/gateway/wallets", apiListGatewayWallets)
	g.POST("/gateway/wallets", apiCreateGatewayWallet)
	g.GET("/gateway/wallets/:id", apiGetGatewayWallet)
	g.POST("/gateway/wallets/:id/topup", apiTopupGatewayWallet)
	g.GET("/gateway/wallets/:id/ledger", apiListGatewayLedger)
	g.GET("/gateway/wallets/:id/topups", apiListGatewayTopups)
	g.POST("/gateway/wallets/:id/keys", apiIssueGatewayVirtualKey)
	g.POST("/gateway/keys/:id/revoke", apiRevokeGatewayVirtualKey)
	g.GET("/gateway/pricing-rules", apiListGatewayPricingRules)
	g.POST("/gateway/pricing-rules", apiCreateGatewayPricingRule)
	// 退休一条价目规则（置 effective_to=now）。价目表现在同时是"用户可选模型清单"的数据源，
	// 历史探测留下的废规则（上游不认的模型别名）必须能被清掉，否则会出现在用户的模型下拉里。
	g.POST("/gateway/pricing-rules/:id/retire", apiRetireGatewayPricingRule)
	g.GET("/gateway/reconciliation-reports", apiListGatewayReconciliationReports)
	g.GET("/gateway/upstream-credentials", apiListGatewayUpstreamCredentials)
	g.POST("/gateway/upstream-credentials", apiCreateGatewayUpstreamCredential)
	g.POST("/gateway/upstream-credentials/:id/enabled", apiSetGatewayUpstreamCredentialEnabled)
	g.DELETE("/gateway/upstream-credentials/:id", apiDeleteGatewayUpstreamCredential)
}

func gatewayPageParams(c *gin.Context) (page, pageSize int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ = strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	return page, pageSize
}

func parseGatewayIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return 0, false
	}
	return id, true
}

// --- 钱包 ---

func apiListGatewayWallets(c *gin.Context) {
	ownerID, _ := strconv.ParseInt(c.Query("owner_id"), 10, 64)
	page, pageSize := gatewayPageParams(c)

	wallets, total, err := wallet.New(store.DB).ListWallets(ownerID, page, pageSize)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, gin.H{"items": wallets, "total": total, "page": page, "page_size": pageSize})
}

type createGatewayWalletRequest struct {
	OwnerID int64 `json:"owner_id,string"`
}

func apiCreateGatewayWallet(c *gin.Context) {
	var req createGatewayWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.OwnerID <= 0 {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	w, err := wallet.New(store.DB).CreateWallet(req.OwnerID)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"wallet": w})
}

func apiGetGatewayWallet(c *gin.Context) {
	id, ok := parseGatewayIDParam(c)
	if !ok {
		return
	}
	w, err := wallet.New(store.DB).GetWallet(id)
	if err != nil {
		response.Fail(c, http.StatusNotFound, 10005, "钱包不存在")
		return
	}
	keys, err := wallet.New(store.DB).ListVirtualKeys(id)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, gin.H{"wallet": w, "virtual_keys": keys})
}

type topupGatewayWalletRequest struct {
	SourceCurrency string `json:"source_currency"`
	SourceAmount   string `json:"source_amount"`
	FxRate         string `json:"fx_rate"`
	Channel        string `json:"channel"`
	Reference      string `json:"reference"`
}

func apiTopupGatewayWallet(c *gin.Context) {
	id, ok := parseGatewayIDParam(c)
	if !ok {
		return
	}
	var req topupGatewayWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	amount, err := decimal.NewFromString(req.SourceAmount)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "source_amount 不是合法数字")
		return
	}
	fxRate, err := decimal.NewFromString(req.FxRate)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "fx_rate 不是合法数字")
		return
	}
	w, err := wallet.New(store.DB).TopUp(id, req.SourceCurrency, amount, fxRate, req.Channel, req.Reference)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"wallet": w})
}

func apiListGatewayLedger(c *gin.Context) {
	id, ok := parseGatewayIDParam(c)
	if !ok {
		return
	}
	page, pageSize := gatewayPageParams(c)
	entries, total, err := wallet.New(store.DB).ListLedgerEntries(id, page, pageSize)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, gin.H{"items": entries, "total": total, "page": page, "page_size": pageSize})
}

func apiListGatewayTopups(c *gin.Context) {
	id, ok := parseGatewayIDParam(c)
	if !ok {
		return
	}
	page, pageSize := gatewayPageParams(c)
	records, total, err := wallet.New(store.DB).ListTopupRecords(id, page, pageSize)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, gin.H{"items": records, "total": total, "page": page, "page_size": pageSize})
}

// --- 虚拟Key ---

type issueGatewayVirtualKeyRequest struct {
	Label string `json:"label"`
}

func apiIssueGatewayVirtualKey(c *gin.Context) {
	walletID, ok := parseGatewayIDParam(c)
	if !ok {
		return
	}
	var req issueGatewayVirtualKeyRequest
	_ = c.ShouldBindJSON(&req)

	plain, key, err := wallet.New(store.DB).IssueVirtualKey(walletID, req.Label)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10006, err.Error())
		return
	}
	// 明文只在这一次返回，之后系统只存哈希，界面要提示管理员当场保存。
	response.OK(c, gin.H{"virtual_key": plain, "key": key})
}

func apiRevokeGatewayVirtualKey(c *gin.Context) {
	id, ok := parseGatewayIDParam(c)
	if !ok {
		return
	}
	if err := wallet.New(store.DB).RevokeVirtualKey(id); err != nil {
		response.Fail(c, http.StatusInternalServerError, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

// --- 价目表 ---

func apiListGatewayPricingRules(c *gin.Context) {
	page, pageSize := gatewayPageParams(c)
	rules, total, err := pricing.New(store.DB).ListRules(c.Query("provider"), page, pageSize)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, gin.H{"items": rules, "total": total, "page": page, "page_size": pageSize})
}

type createGatewayPricingRuleRequest struct {
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	Cached         string `json:"cached"`
	Uncached       string `json:"uncached"`
	Output         string `json:"output"`
	SourceCurrency string `json:"source_currency"`
	FxRate         string `json:"fx_rate"`
	// 分时定价时段：北京时间当日分钟数[0,1440)，两者都为空表示不分时。
	WindowStartMin *int `json:"window_start_min"`
	WindowEndMin   *int `json:"window_end_min"`
	// 输入分档：按单次请求输入token数(缓存命中+未命中)分档的档位区间[start,end)，两者都为空表示不分档。
	// 分时/分档都为空 = 全天兜底价。
	TierStartTokens *int `json:"input_tier_start_tokens"`
	TierEndTokens   *int `json:"input_tier_end_tokens"`
}

// apiRetireGatewayPricingRule 退休一条价目规则：此后不再参与计价，也不再出现在用户的可选模型清单里。
// 用来清掉历史探测留下的废规则（上游不认的模型别名）。已退休的重复调用不报错。
func apiRetireGatewayPricingRule(c *gin.Context) {
	id, ok := parseGatewayIDParam(c)
	if !ok {
		return
	}
	if err := pricing.New(store.DB).RetireRule(id); err != nil {
		if errors.Is(err, pricing.ErrRuleNotFound) {
			response.Fail(c, http.StatusNotFound, 10005, "价目规则不存在")
			return
		}
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func apiCreateGatewayPricingRule(c *gin.Context) {
	var req createGatewayPricingRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Provider == "" || req.Model == "" {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	fxRate := decimal.NewFromInt(1)
	if req.FxRate != "" {
		var err error
		fxRate, err = decimal.NewFromString(req.FxRate)
		if err != nil {
			response.Fail(c, http.StatusBadRequest, 10002, "fx_rate 不是合法数字")
			return
		}
	}
	sourceCurrency := req.SourceCurrency
	if sourceCurrency == "" {
		sourceCurrency = "USD"
	}

	rule, err := pricing.New(store.DB).CreateRule(pricing.CreateRuleInput{
		Provider:        req.Provider,
		Model:           req.Model,
		Cached:          req.Cached,
		Uncached:        req.Uncached,
		Output:          req.Output,
		SourceCurrency:  sourceCurrency,
		FxRate:          fxRate,
		WindowStartMin:  req.WindowStartMin,
		WindowEndMin:    req.WindowEndMin,
		TierStartTokens: req.TierStartTokens,
		TierEndTokens:   req.TierEndTokens,
	})
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"rule": rule})
}

// --- 上游厂商凭据（官方Key动态增删；明文只在录入时收，库里只存密文，接口只回末4位） ---

func apiListGatewayUpstreamCredentials(c *gin.Context) {
	rows, err := credential.New(store.DB).List(c.Query("provider"))
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, gin.H{"items": rows})
}

type createGatewayUpstreamCredentialRequest struct {
	Provider  string `json:"provider"`
	Purpose   string `json:"purpose"`
	APIKey    string `json:"api_key"`
	APISecret string `json:"api_secret"`
	BaseURL   string `json:"base_url"`
	Region    string `json:"region"`
	Label     string `json:"label"`
}

func apiCreateGatewayUpstreamCredential(c *gin.Context) {
	var req createGatewayUpstreamCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Provider == "" || req.APIKey == "" {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误：provider 和 api_key 必填")
		return
	}
	cred, err := credential.New(store.DB).Create(credential.CreateInput{
		Provider:  req.Provider,
		Purpose:   req.Purpose,
		APIKey:    req.APIKey,
		APISecret: req.APISecret,
		BaseURL:   req.BaseURL,
		Region:    req.Region,
		Label:     req.Label,
	})
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"credential": cred})
}

type setGatewayUpstreamCredentialEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

func apiSetGatewayUpstreamCredentialEnabled(c *gin.Context) {
	id, ok := parseGatewayIDParam(c)
	if !ok {
		return
	}
	var req setGatewayUpstreamCredentialEnabledRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	if err := credential.New(store.DB).SetEnabled(id, req.Enabled); err != nil {
		response.Fail(c, http.StatusInternalServerError, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func apiDeleteGatewayUpstreamCredential(c *gin.Context) {
	id, ok := parseGatewayIDParam(c)
	if !ok {
		return
	}
	if err := credential.New(store.DB).Delete(id); err != nil {
		response.Fail(c, http.StatusInternalServerError, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

// --- 对账报告 ---

func apiListGatewayReconciliationReports(c *gin.Context) {
	page, pageSize := gatewayPageParams(c)
	reports, total, err := reconcile.New(store.DB).ListReports(c.Query("provider"), page, pageSize)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, gin.H{"items": reports, "total": total, "page": page, "page_size": pageSize})
}
