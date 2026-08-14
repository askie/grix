// cmd/gatewayadmin 是网关的一期临时管理CLI：录价目表、开钱包、充值、发虚拟Key。
// 一期没有 aibot-admin 页面，先用这个把闭环跑起来；二期/三期迁到正式后台页面后这个工具可以退休。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/gateway/credential"
	"github.com/askie/grix/backend/internal/gateway/pricing"
	"github.com/askie/grix/backend/internal/gateway/reconcile"
	"github.com/askie/grix/backend/internal/gateway/wallet"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: gatewayadmin <seed-pricing|create-wallet|topup|issue-key|add-credential|list-credentials> [flags] [config.yaml]")
		os.Exit(1)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	logger.Init()
	config.Load("config.yaml")
	if err := snowflake.Init(config.C.Snowflake.MachineID); err != nil {
		logger.L.Fatalf("snowflake init: %v", err)
	}
	store.InitPostgres(config.C.Postgres)
	store.MaybeInitSchema()

	switch cmd {
	case "seed-pricing":
		seedPricing(args)
	case "create-wallet":
		createWallet(args)
	case "topup":
		topup(args)
	case "issue-key":
		issueKey(args)
	case "revoke-key":
		revokeKey(args)
	case "reconcile":
		runReconcile(args)
	case "add-credential":
		addCredential(args)
	case "list-credentials":
		listCredentials(args)
	default:
		fmt.Printf("unknown subcommand %q\n", cmd)
		os.Exit(1)
	}
}

func mustDecimal(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		logger.L.Fatalf("invalid decimal %q: %v", s, err)
	}
	return d
}

// parseBeijingHHMM 把 "HH:MM"(北京时间) 解析成当日分钟数[0,1440)。空串返回 nil。
func parseBeijingHHMM(s string) *int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		logger.L.Fatalf("invalid HH:MM time %q", s)
	}
	v := h*60 + m
	return &v
}

// seedPricing 录一条价目规则：传入的价格是厂商官方原始币种下的数值，工具按 fx-rate 换算成USD存库。
// 可选 --window-start/--window-end (北京时间HH:MM) 录一条分时价；不填则录全天兜底价。
func seedPricing(args []string) {
	fs := flag.NewFlagSet("seed-pricing", flag.ExitOnError)
	provider := fs.String("provider", "", "厂商，如 deepseek")
	model_ := fs.String("model", "", "计价用的规范模型名，如 deepseek-v4-flash")
	cached := fs.String("cached", "", "官方原始币种下缓存命中单价(每百万token)")
	uncached := fs.String("uncached", "", "官方原始币种下缓存未命中单价(每百万token)")
	output := fs.String("output", "", "官方原始币种下输出单价(每百万token)")
	sourceCurrency := fs.String("source-currency", "USD", "官方原始报价币种")
	fxRate := fs.String("fx-rate", "1", "换算成USD用的汇率(source_currency -> USD)")
	windowStart := fs.String("window-start", "", "分时价起始(北京时间HH:MM,如00:30);留空为全天兜底价")
	windowEnd := fs.String("window-end", "", "分时价结束(北京时间HH:MM,如08:30)")
	_ = fs.Parse(args)

	if *provider == "" || *model_ == "" || *cached == "" || *uncached == "" || *output == "" {
		logger.L.Fatal("seed-pricing requires --provider --model --cached --uncached --output")
	}

	rule, err := pricing.New(store.DB).CreateRule(pricing.CreateRuleInput{
		Provider:       *provider,
		Model:          *model_,
		Cached:         *cached,
		Uncached:       *uncached,
		Output:         *output,
		SourceCurrency: *sourceCurrency,
		FxRate:         mustDecimal(*fxRate),
		WindowStartMin: parseBeijingHHMM(*windowStart),
		WindowEndMin:   parseBeijingHHMM(*windowEnd),
	})
	if err != nil {
		logger.L.Fatalf("insert pricing rule: %v", err)
	}
	window := "全天兜底"
	if rule.DailyWindowStartMin != nil {
		window = fmt.Sprintf("分时[北京%02d:%02d-%02d:%02d)", *rule.DailyWindowStartMin/60, *rule.DailyWindowStartMin%60, *rule.DailyWindowEndMin/60, *rule.DailyWindowEndMin%60)
	}
	fmt.Printf("pricing rule created: id=%d provider=%s model=%s %s cached=%s uncached=%s output=%s (USD/M)\n",
		rule.ID, rule.Provider, rule.Model, window,
		rule.CachedInputPricePerM.String(), rule.UncachedInputPricePerM.String(), rule.OutputPricePerM.String())
}

func createWallet(args []string) {
	fs := flag.NewFlagSet("create-wallet", flag.ExitOnError)
	ownerID := fs.Int64("owner-id", 0, "Grix 用户ID")
	_ = fs.Parse(args)
	if *ownerID == 0 {
		logger.L.Fatal("create-wallet requires --owner-id")
	}

	w, err := wallet.New(store.DB).CreateWallet(*ownerID)
	if err != nil {
		logger.L.Fatalf("create wallet: %v", err)
	}
	fmt.Printf("wallet: id=%d owner_id=%d balance=%s USD\n", w.ID, w.OwnerID, w.Balance.String())
}

func topup(args []string) {
	fs := flag.NewFlagSet("topup", flag.ExitOnError)
	walletID := fs.Int64("wallet-id", 0, "钱包ID")
	sourceCurrency := fs.String("source-currency", "CNY", "用户实际支付币种")
	sourceAmount := fs.String("source-amount", "", "用户实际支付金额")
	fxRate := fs.String("fx-rate", "", "换算成USD用的汇率(source_currency -> USD)")
	channel := fs.String("channel", "manual", "支付渠道")
	reference := fs.String("reference", "", "支付渠道流水号")
	_ = fs.Parse(args)
	if *walletID == 0 || *sourceAmount == "" || *fxRate == "" {
		logger.L.Fatal("topup requires --wallet-id --source-amount --fx-rate")
	}

	w, err := wallet.New(store.DB).TopUp(*walletID, *sourceCurrency, mustDecimal(*sourceAmount), mustDecimal(*fxRate), *channel, *reference)
	if err != nil {
		logger.L.Fatalf("topup: %v", err)
	}
	fmt.Printf("wallet %d new balance: %s USD\n", w.ID, w.Balance.String())
}

func issueKey(args []string) {
	fs := flag.NewFlagSet("issue-key", flag.ExitOnError)
	walletID := fs.Int64("wallet-id", 0, "钱包ID")
	label := fs.String("label", "", "备注，如 Claude Code")
	_ = fs.Parse(args)
	if *walletID == 0 {
		logger.L.Fatal("issue-key requires --wallet-id")
	}

	plain, key, err := wallet.New(store.DB).IssueVirtualKey(*walletID, *label)
	if err != nil {
		logger.L.Fatalf("issue key: %v", err)
	}
	fmt.Printf("virtual key (仅显示这一次，请立刻保存): %s\nkey_id=%d hint=%s\n", plain, key.ID, key.KeyHint)
}

func revokeKey(args []string) {
	fs := flag.NewFlagSet("revoke-key", flag.ExitOnError)
	keyID := fs.Int64("key-id", 0, "虚拟Key ID")
	_ = fs.Parse(args)
	if *keyID == 0 {
		logger.L.Fatal("revoke-key requires --key-id")
	}
	if err := wallet.New(store.DB).RevokeVirtualKey(*keyID); err != nil {
		logger.L.Fatalf("revoke key: %v", err)
	}
	fmt.Printf("key %d revoked\n", *keyID)
}

// addCredential 录入一把上游厂商官方凭据（密文落库，供网关动态取用）。塘主后台上线前可用它做首次种子。
func addCredential(args []string) {
	fs := flag.NewFlagSet("add-credential", flag.ExitOnError)
	provider := fs.String("provider", "", "厂商: deepseek | volcano_ark")
	purpose := fs.String("purpose", "inference", "用途: inference(推理转发) | reconcile(对账)")
	apiKey := fs.String("api-key", "", "推理Key，或对账的 AccessKey")
	apiSecret := fs.String("api-secret", "", "对账的 SecretKey(推理场景留空)")
	baseURL := fs.String("base-url", "", "端点覆盖(留空用默认)")
	region := fs.String("region", "", "火山对账region(留空默认 cn-beijing)")
	label := fs.String("label", "", "备注")
	_ = fs.Parse(args)
	if *provider == "" || *apiKey == "" {
		logger.L.Fatal("add-credential requires --provider and --api-key")
	}
	cred, err := credential.New(store.DB).Create(credential.CreateInput{
		Provider: *provider, Purpose: *purpose, APIKey: *apiKey, APISecret: *apiSecret,
		BaseURL: *baseURL, Region: *region, Label: *label,
	})
	if err != nil {
		logger.L.Fatalf("add credential: %v", err)
	}
	fmt.Printf("credential added: id=%d provider=%s purpose=%s hint=%s enabled=%v\n",
		cred.ID, cred.Provider, cred.Purpose, cred.KeyHint, cred.Enabled)
}

// listCredentials 列出已录入的上游凭据（只显示末4位，不回明文）。
func listCredentials(args []string) {
	fs := flag.NewFlagSet("list-credentials", flag.ExitOnError)
	provider := fs.String("provider", "", "按厂商过滤(留空列全部)")
	_ = fs.Parse(args)
	rows, err := credential.New(store.DB).List(*provider)
	if err != nil {
		logger.L.Fatalf("list credentials: %v", err)
	}
	if len(rows) == 0 {
		fmt.Println("(no credentials)")
		return
	}
	for _, r := range rows {
		fmt.Printf("id=%d provider=%s purpose=%s hint=%s enabled=%v label=%q\n",
			r.ID, r.Provider, r.Purpose, r.KeyHint, r.Enabled, r.Label)
	}
}

func runReconcile(args []string) {
	fs := flag.NewFlagSet("reconcile", flag.ExitOnError)
	provider := fs.String("provider", "deepseek", "厂商: deepseek | volcano_ark")
	model_ := fs.String("model", "deepseek-v4-flash", "计价模型名")
	apiKey := fs.String("api-key", "", "DeepSeek: 官方Key(推理Key本身能查余额)")
	ak := fs.String("ak", "", "火山: 费用中心用的Access Key(不是ARK推理Key)")
	sk := fs.String("sk", "", "火山: 费用中心用的Secret Key")
	region := fs.String("region", "cn-beijing", "火山: 费用中心API所在region")
	_ = fs.Parse(args)

	var checker reconcile.BalanceChecker
	switch *provider {
	case "deepseek":
		if *apiKey == "" {
			logger.L.Fatal("deepseek reconcile requires --api-key")
		}
		checker = reconcile.NewDeepSeekBalanceChecker(*apiKey)
	case "volcano_ark":
		if *ak == "" || *sk == "" {
			logger.L.Fatal("volcano_ark reconcile requires --ak --sk (费用中心密钥，不是ARK推理Key)")
		}
		c, err := reconcile.NewVolcanoArkBalanceChecker(*ak, *sk, *region)
		if err != nil {
			logger.L.Fatalf("init volcano balance checker: %v", err)
		}
		checker = c
	default:
		logger.L.Fatalf("unsupported provider %q", *provider)
	}

	report, err := reconcile.New(store.DB).RunWindow(context.Background(), *provider, *model_, checker)
	if err != nil {
		logger.L.Fatalf("reconcile: %v", err)
	}
	diffRatioStr := "n/a"
	if report.DiffRatio != nil {
		diffRatioStr = report.DiffRatio.String()
	}
	fmt.Printf("report id=%d status=%s vendor_actual=%s ledger_expected=%s diff=%s diff_ratio=%s auto_adjusted=%v\n",
		report.ID, report.Status, report.VendorActualCost.String(), report.LedgerExpectedCost.String(),
		report.Diff.String(), diffRatioStr, report.AutoAdjusted)
}
