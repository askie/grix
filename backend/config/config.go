package config

import (
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Postgres  PostgresConfig  `mapstructure:"postgres"`
	Redis     RedisConfig     `mapstructure:"redis"`
	NATS      NATSConfig      `mapstructure:"nats"`
	JWT       JWTConfig       `mapstructure:"jwt"`
	Snowflake SnowflakeConfig `mapstructure:"snowflake"`
	OSS       OSSGroupConfig  `mapstructure:"oss"`
	Migration MigrationConfig `mapstructure:"migration"`
	LLM       LLMConfig       `mapstructure:"llm"`
	Push      PushConfig      `mapstructure:"push"`
	AliEmail  AliEmailConfig  `mapstructure:"ali_email"`
	OAuth     OAuthConfig     `mapstructure:"oauth"`
	LiveKit   LiveKitConfig   `mapstructure:"livekit"`
	AppUpdate AppUpdateConfig `mapstructure:"app_update"`
	Gateway   GatewayConfig   `mapstructure:"gateway"`
	Pay       PayConfig       `mapstructure:"pay"`
}

// GatewayConfig 是大模型计费网关(cmd/gateway)的配置：独立端口 + 各厂商真实官方Key。
// 官方Key只在这里配置一次，绝不下发给用户——用户拿到的虚拟Key只用于鉴权和记账。
// GatewayConfig 只保留网关服务自身的端口；上游厂商官方Key/对账凭据不再固化在 env，
// 改由塘主后台动态管理、密文落库（gateway_upstream_credentials 表）。
type GatewayConfig struct {
	Port   int                 `mapstructure:"port"`
	FxSync GatewayFxSyncConfig `mapstructure:"fxsync"`
	// RelayStateEnabled 是"中转开关服务端化"（migration 111，gateway_agent_relay_state）的
	// feature flag。置 false 时：POST /v1/gateway/agents/:id/relay 返回 503，
	// GET /v1/gateway/agents 不读 state 表、响应回落到 configured 旧语义（回滚用）。
	RelayStateEnabled bool `mapstructure:"relay_state_enabled"`
	// DirectRelayEnabled 是"Claude/Codex 原生直连"（direct_relay capability）的总开关：
	// 置 false 时凭证签发响应不带 direct_relay 对象，连接器保持旧 MITM 路径（回滚用，
	// 见 plan-direct-relay-first-refactor §12）。新路由不随开关下线，避免已直连的 agent 断流。
	DirectRelayEnabled bool `mapstructure:"direct_relay_enabled"`
}

// GatewayFxSyncConfig 是汇率自动同步（cmd/gateway 内的一个协程）的配置。
// 汇率决定充值到账金额，故 currencies 里每个币种都必须在 fxsync 的合理区间表里有对应条目。
type GatewayFxSyncConfig struct {
	Enabled    bool          `mapstructure:"enabled"`
	Currencies []string      `mapstructure:"currencies"` // 需要同步的源币种，兑 USD；USD 自身恒为 1 不需同步
	Interval   time.Duration `mapstructure:"interval"`
	APIURL     string        `mapstructure:"api_url"` // 留空用免 Key 公开端点
	APIKey     string        `mapstructure:"api_key"` // 留空即免 Key 模式
}

// PayConfig 是独立支付服务(cmd/pay)的配置：端口与回调基址。
// 各通道商户凭证（AppID/私钥/Client Secret 等）不在这里静态配置，
// 改由塘主后台加密录入，运行时经 internal/systemsetting.GetPayChannelSettings 动态读取，
// 免重启即可生效（见 docs/payment）。
type PayConfig struct {
	Port            int    `mapstructure:"port"`
	NotifyURLBase   string `mapstructure:"notify_url_base"`   // 第三方回调可达的对外基址，如 https://pay.grix.dhf.pub
	InternalBaseURL string `mapstructure:"internal_base_url"` // 其它服务(api)内部调支付系统的可达地址，如 http://pay:27185
}

// LiveKitConfig LiveKit Server 连接配置
type LiveKitConfig struct {
	URL       string     `mapstructure:"url"`
	PublicURL string     `mapstructure:"public_url"`
	APIKey    string     `mapstructure:"api_key"`
	APISecret string     `mapstructure:"api_secret"`
	Turn      TurnConfig `mapstructure:"turn"`
}

// TurnConfig coturn TURN relay 配置
type TurnConfig struct {
	Enabled    bool     `mapstructure:"enabled"`
	URLs       []string `mapstructure:"urls"`
	Username   string   `mapstructure:"username"`
	Credential string   `mapstructure:"credential"`
}

type ServerConfig struct {
	APIPort                    int    `mapstructure:"api_port"`
	WSHost                     string `mapstructure:"ws_host"`
	WSPort                     int    `mapstructure:"ws_port"`
	LLMPort                    int    `mapstructure:"llm_port"`
	PushPort                   int    `mapstructure:"push_port"`
	WidgetEnabled              bool   `mapstructure:"widget_enabled"`
	NodeID                     string `mapstructure:"node_id"`
	AllowedWebOrigins          string `mapstructure:"allowed_web_origins"`
	FriendQRBaseURL            string `mapstructure:"friend_qr_base_url"`
	GroupQRBaseURL             string `mapstructure:"group_qr_base_url"`
	DeepLinkIOSAppID           string `mapstructure:"deep_link_ios_app_id"`
	DeepLinkAndroidPackage     string `mapstructure:"deep_link_android_package"`
	DeepLinkAndroidSHA256Certs string `mapstructure:"deep_link_android_sha256_certs"`
	AgentAPIDomain             string `mapstructure:"agent_api_domain"`
	AgentAPIPath               string `mapstructure:"agent_api_path"`
	AgentAPIWSPath             string `mapstructure:"agent_api_ws_path"`
	AgentAPIHeartbeat          int    `mapstructure:"agent_api_heartbeat_sec"`
	WebhookTokenSecret         string `mapstructure:"webhook_token_secret"`
	PprofSecret                string `mapstructure:"pprof_secret"`
	VoiceCryptoSecret          string `mapstructure:"voice_crypto_secret"`       // 语音 BYOK API key 加密密钥（空时回退 jwt.secret）
	Region                     string `mapstructure:"region"`                    // 部署区域标识：cn 或 global
	PublicWsURL                string `mapstructure:"public_ws_url"`             // 返回给客户端的 WebSocket 公网地址
	NotificationHmacSecret     string `mapstructure:"notification_hmac_secret"`  // Agent 通知离线回调 token 签名密钥（空时回退 jwt.secret）
	AgentIPRuleHmacSecret      string `mapstructure:"agent_ip_rule_hmac_secret"` // Agent IP 规则防篡改签名密钥（空时回退 jwt.secret）
	PayCryptoSecret            string `mapstructure:"pay_crypto_secret"`         // 支付通道商户凭证加密密钥（空时回退 jwt.secret），与语音 BYOK 密钥域隔离
}

type PostgresConfig struct {
	Host         string `mapstructure:"host"`
	Port         int    `mapstructure:"port"`
	User         string `mapstructure:"user"`
	Password     string `mapstructure:"password"`
	DBName       string `mapstructure:"dbname"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
	ReadHost     string `mapstructure:"read_host"` // 可选:只读副本主机;为空则读走主库
	SSLMode      string `mapstructure:"sslmode"`   // 生产建议 require 或 verify-full；默认 disable 仅用于本地开发
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
	PoolSize int    `mapstructure:"pool_size"`
}

type NATSConfig struct {
	URL        string `mapstructure:"url"`
	StreamName string `mapstructure:"stream_name"`
}

type JWTConfig struct {
	Secret     string `mapstructure:"secret"`
	AccessTTL  int    `mapstructure:"access_ttl"`
	RefreshTTL int    `mapstructure:"refresh_ttl"`
}

type SnowflakeConfig struct {
	MachineID int64 `mapstructure:"machine_id"`
}

type OSSConfig struct {
	Endpoint   string `mapstructure:"endpoint"`
	AccessKey  string `mapstructure:"access_key"`
	SecretKey  string `mapstructure:"secret_key"`
	Bucket     string `mapstructure:"bucket"`
	Region     string `mapstructure:"region"` // Bucket region, e.g. ap-chengdu
	UseSSL     bool   `mapstructure:"use_ssl"`
	PublicURL  string `mapstructure:"public_url"`  // e.g. https://cdn.example.com — media access base URL
	StorageDir string `mapstructure:"storage_dir"` // storage directory prefix
}

type OSSGroupConfig struct {
	Media  OSSConfig `mapstructure:"media"`
	Avatar OSSConfig `mapstructure:"avatar"`
	Report OSSConfig `mapstructure:"report"`
}

type MigrationConfig struct {
	LegacyOSS OSSConfig `mapstructure:"legacy_oss"`
}

type LLMConfig struct {
	OpenAI             OpenAIConfig         `mapstructure:"openai"`
	Claude             ClaudeConfig         `mapstructure:"claude"`
	Translation        TranslationLLMConfig `mapstructure:"translation"`
	EmbeddingModel     string               `mapstructure:"embedding_model"`
	DailyTokenQuota    int                  `mapstructure:"daily_token_quota"`
	DelegateTimeoutSec int                  `mapstructure:"delegate_timeout_sec"`
	EventResultWaitSec int                  `mapstructure:"event_result_wait_sec"`
	StaleRunReapSec    int                  `mapstructure:"stale_run_reap_sec"`
	// 内置 AI 流式 chunk 广播合并窗口。StreamFlushIntervalMs<=0 退回逐 chunk 广播（紧急关闭）。
	StreamFlushIntervalMs int `mapstructure:"stream_flush_interval_ms"`
	StreamFlushBytes      int `mapstructure:"stream_flush_bytes"`
}

type OpenAIConfig struct {
	APIKey       string `mapstructure:"api_key"`
	BaseURL      string `mapstructure:"base_url"`
	DefaultModel string `mapstructure:"default_model"`
}

type ClaudeConfig struct {
	APIKey       string `mapstructure:"api_key"`
	BaseURL      string `mapstructure:"base_url"`
	DefaultModel string `mapstructure:"default_model"`
}

type TranslationLLMConfig struct {
	APIKey            string  `mapstructure:"api_key"`
	BaseURL           string  `mapstructure:"base_url"`
	ProxyURL          string  `mapstructure:"proxy_url"`
	APIStyle          string  `mapstructure:"api_style"`
	Model             string  `mapstructure:"model"`
	ReasoningEffort   string  `mapstructure:"reasoning_effort"`
	Temperature       float64 `mapstructure:"temperature"`
	MaxOutputTokens   int     `mapstructure:"max_output_tokens"`
	RequestTimeoutSec int     `mapstructure:"request_timeout_sec"`
	ExtraBodyJSON     string  `mapstructure:"extra_body_json"`
	ExtraHeadersJSON  string  `mapstructure:"extra_headers_json"`
}

type PushConfig struct {
	APNs    APNsConfig    `mapstructure:"apns"`
	FCM     FCMConfig     `mapstructure:"fcm"`
	JPush   JPushConfig   `mapstructure:"jpush"`
	WebPush WebPushConfig `mapstructure:"web_push"`
	Huawei  HuaweiConfig  `mapstructure:"huawei"`
	Xiaomi  XiaomiConfig  `mapstructure:"xiaomi"`
}

// HuaweiConfig 为华为 Push Kit 凭据，取自 AGC 控制台。
type HuaweiConfig struct {
	AppID        string `mapstructure:"app_id"`
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
}

// XiaomiConfig 为小米推送凭据，取自小米开放平台。
type XiaomiConfig struct {
	AppSecret   string `mapstructure:"app_secret"`
	PackageName string `mapstructure:"package_name"`
}

type APNsConfig struct {
	KeyPath string `mapstructure:"key_path"`
	KeyID   string `mapstructure:"key_id"`
	TeamID  string `mapstructure:"team_id"`
	Topic   string `mapstructure:"topic"`
}

type FCMConfig struct {
	CredentialsFile string `mapstructure:"credentials_file"`
}

type JPushConfig struct {
	AppKey       string `mapstructure:"app_key"`
	MasterSecret string `mapstructure:"master_secret"`
}

type WebPushConfig struct {
	VAPIDPublicKey  string `mapstructure:"vapid_public_key"`
	VAPIDPrivateKey string `mapstructure:"vapid_private_key"`
	Subscriber      string `mapstructure:"subscriber"`
}

type AliEmailConfig struct {
	AccessKeyID     string `mapstructure:"access_key_id"`
	AccessKeySecret string `mapstructure:"access_key_secret"`
	RegionID        string `mapstructure:"region_id"`
	Endpoint        string `mapstructure:"endpoint"`
	FromAddress     string `mapstructure:"from_address"`
}

type OAuthConfig struct {
	GoogleAllowedClientIDs string `mapstructure:"google_allowed_client_ids"`
	AppleBundleIDs         string `mapstructure:"apple_bundle_ids"`
}

// AppUpdateConfig 客户端自动更新安全配置
type AppUpdateConfig struct {
	// 允许的下载链接域名白名单（逗号分隔），为空则仅校验 https scheme
	AllowedDownloadDomains string `mapstructure:"allowed_download_domains"`
}

var C Config

func Load(path string) {
	viper.SetDefault("server.agent_api_path", "/v1/agent-api")
	viper.SetDefault("server.agent_api_ws_path", "/ws")
	viper.SetDefault("server.agent_api_heartbeat_sec", 30)
	viper.SetDefault("gateway.port", 27184) // 部署环境 configmap 若未含 gateway 段时的兜底端口
	viper.SetDefault("gateway.fxsync.enabled", true)
	viper.SetDefault("gateway.fxsync.currencies", []string{"CNY"})
	viper.SetDefault("gateway.fxsync.interval", 24*time.Hour) // 免 Key 数据源每 24h 才刷新一次报价
	viper.SetDefault("gateway.relay_state_enabled", true)     // 中转开关服务端化 feature flag，回滚时置 false
	viper.SetDefault("gateway.direct_relay_enabled", false)   // 原生直连 capability 默认关闭，灰度就绪后开启
	viper.SetDefault("pay.port", 27185)                       // 支付服务兜底端口（27180-27189 区间空闲位）
	viper.SetDefault("server.widget_enabled", false)
	viper.SetDefault("server.friend_qr_base_url", "https://dhf.pub/u")
	viper.SetDefault("server.group_qr_base_url", "https://dhf.pub/g")
	viper.SetDefault("llm.delegate_timeout_sec", 300)
	viper.SetDefault("llm.event_result_wait_sec", 600)
	viper.SetDefault("llm.stale_run_reap_sec", 600)
	viper.SetDefault("llm.stream_flush_interval_ms", 80)
	viper.SetDefault("llm.stream_flush_bytes", 256)
	viper.SetDefault("llm.translation.api_style", "responses")
	viper.SetDefault("llm.translation.temperature", 0.2)
	viper.SetDefault("llm.translation.max_output_tokens", 800)
	viper.SetDefault("llm.translation.request_timeout_sec", 60)
	viper.SetConfigFile(path)
	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("failed to read config: %v", err)
	}
	if err := viper.Unmarshal(&C); err != nil {
		log.Fatalf("failed to unmarshal config: %v", err)
	}
	applyEnvOverrides(&C)
	if err := validateSecurityCriticalConfig(C); err != nil {
		log.Fatalf("invalid config: %v", err)
	}
}

func applyEnvOverrides(cfg *Config) {
	if cfg == nil {
		return
	}

	overrideInt(&cfg.Server.APIPort, "AIBOT_SERVER_API_PORT")
	overrideString(&cfg.Server.Region, "AIBOT_SERVER_REGION")
	overrideString(&cfg.Server.PublicWsURL, "AIBOT_SERVER_PUBLIC_WS_URL")
	overrideString(&cfg.Server.WSHost, "AIBOT_SERVER_WS_HOST")
	overrideInt(&cfg.Server.WSPort, "AIBOT_SERVER_WS_PORT")
	overrideInt(&cfg.Server.LLMPort, "AIBOT_SERVER_LLM_PORT")
	overrideInt(&cfg.Server.PushPort, "AIBOT_SERVER_PUSH_PORT")
	overrideBool(&cfg.Server.WidgetEnabled, "AIBOT_SERVER_WIDGET_ENABLED")
	overrideString(&cfg.Server.NodeID, "AIBOT_SERVER_NODE_ID")
	overrideString(&cfg.Server.AllowedWebOrigins, "AIBOT_SERVER_ALLOWED_WEB_ORIGINS")
	overrideString(&cfg.Server.FriendQRBaseURL, "AIBOT_SERVER_FRIEND_QR_BASE_URL")
	overrideString(&cfg.Server.GroupQRBaseURL, "AIBOT_SERVER_GROUP_QR_BASE_URL")
	overrideString(&cfg.Server.DeepLinkIOSAppID, "AIBOT_SERVER_DEEP_LINK_IOS_APP_ID")
	overrideString(&cfg.Server.DeepLinkAndroidPackage, "AIBOT_SERVER_DEEP_LINK_ANDROID_PACKAGE")
	overrideString(&cfg.Server.DeepLinkAndroidSHA256Certs, "AIBOT_SERVER_DEEP_LINK_ANDROID_SHA256_CERTS")
	overrideString(&cfg.Server.AgentAPIDomain, "AIBOT_SERVER_AGENT_API_DOMAIN")
	overrideString(&cfg.Server.AgentAPIPath, "AIBOT_SERVER_AGENT_API_PATH")
	overrideString(&cfg.Server.AgentAPIWSPath, "AIBOT_SERVER_AGENT_API_WS_PATH")
	overrideInt(&cfg.Server.AgentAPIHeartbeat, "AIBOT_SERVER_AGENT_API_HEARTBEAT_SEC")
	overrideString(&cfg.Server.WebhookTokenSecret, "AIBOT_SERVER_WEBHOOK_TOKEN_SECRET")
	overrideString(&cfg.Server.PprofSecret, "AIBOT_SERVER_PPROF_SECRET")
	overrideString(&cfg.Server.VoiceCryptoSecret, "AIBOT_SERVER_VOICE_CRYPTO_SECRET")
	overrideString(&cfg.Server.AgentIPRuleHmacSecret, "AIBOT_SERVER_AGENT_IP_RULE_HMAC_SECRET")
	overrideString(&cfg.Server.PayCryptoSecret, "AIBOT_SERVER_PAY_CRYPTO_SECRET")

	overrideString(&cfg.Postgres.Host, "AIBOT_POSTGRES_HOST")
	overrideString(&cfg.Postgres.ReadHost, "AIBOT_POSTGRES_READ_HOST")
	overrideString(&cfg.Postgres.User, "AIBOT_POSTGRES_USER")
	overrideString(&cfg.Postgres.Password, "AIBOT_POSTGRES_PASSWORD")
	overrideString(&cfg.Postgres.DBName, "AIBOT_POSTGRES_DBNAME")
	overrideString(&cfg.Postgres.SSLMode, "AIBOT_POSTGRES_SSLMODE")

	overrideString(&cfg.Redis.Addr, "AIBOT_REDIS_ADDR")
	overrideString(&cfg.Redis.Password, "AIBOT_REDIS_PASSWORD")

	overrideString(&cfg.NATS.URL, "AIBOT_NATS_URL")

	overrideString(&cfg.JWT.Secret, "AIBOT_JWT_SECRET")

	overrideInt64(&cfg.Snowflake.MachineID, "AIBOT_SNOWFLAKE_MACHINE_ID")

	applyOSSEnvOverrides(&cfg.OSS.Media, "AIBOT_OSS_MEDIA")
	applyOSSEnvOverrides(&cfg.OSS.Avatar, "AIBOT_OSS_AVATAR")
	applyOSSEnvOverrides(&cfg.OSS.Report, "AIBOT_OSS_REPORT")
	applyOSSEnvOverrides(&cfg.Migration.LegacyOSS, "AIBOT_MIGRATION_LEGACY_OSS")

	overrideString(&cfg.LLM.OpenAI.APIKey, "AIBOT_OPENAI_API_KEY")
	overrideString(&cfg.LLM.OpenAI.BaseURL, "AIBOT_OPENAI_BASE_URL")
	overrideString(&cfg.LLM.OpenAI.DefaultModel, "AIBOT_OPENAI_DEFAULT_MODEL")
	overrideString(&cfg.LLM.Claude.APIKey, "AIBOT_CLAUDE_API_KEY")
	overrideString(&cfg.LLM.Claude.BaseURL, "AIBOT_CLAUDE_BASE_URL")
	overrideString(&cfg.LLM.Claude.DefaultModel, "AIBOT_CLAUDE_DEFAULT_MODEL")
	overrideString(&cfg.LLM.Translation.APIKey, "AIBOT_LLM_TRANSLATION_API_KEY")
	overrideString(&cfg.LLM.Translation.BaseURL, "AIBOT_LLM_TRANSLATION_BASE_URL")
	overrideString(&cfg.LLM.Translation.ProxyURL, "AIBOT_LLM_TRANSLATION_PROXY_URL")
	overrideString(&cfg.LLM.Translation.APIStyle, "AIBOT_LLM_TRANSLATION_API_STYLE")
	overrideString(&cfg.LLM.Translation.Model, "AIBOT_LLM_TRANSLATION_MODEL")
	overrideString(&cfg.LLM.Translation.ReasoningEffort, "AIBOT_LLM_TRANSLATION_REASONING_EFFORT")
	overrideFloat64(&cfg.LLM.Translation.Temperature, "AIBOT_LLM_TRANSLATION_TEMPERATURE")
	overrideInt(&cfg.LLM.Translation.MaxOutputTokens, "AIBOT_LLM_TRANSLATION_MAX_OUTPUT_TOKENS")
	overrideInt(&cfg.LLM.Translation.RequestTimeoutSec, "AIBOT_LLM_TRANSLATION_REQUEST_TIMEOUT_SEC")
	overrideString(&cfg.LLM.Translation.ExtraBodyJSON, "AIBOT_LLM_TRANSLATION_EXTRA_BODY_JSON")
	overrideString(&cfg.LLM.Translation.ExtraHeadersJSON, "AIBOT_LLM_TRANSLATION_EXTRA_HEADERS_JSON")
	overrideInt(&cfg.LLM.StreamFlushIntervalMs, "AIBOT_LLM_STREAM_FLUSH_INTERVAL_MS")
	overrideInt(&cfg.LLM.StreamFlushBytes, "AIBOT_LLM_STREAM_FLUSH_BYTES")
	overrideString(&cfg.LLM.EmbeddingModel, "AIBOT_LLM_EMBEDDING_MODEL")

	overrideString(&cfg.Push.APNs.KeyPath, "AIBOT_PUSH_APNS_KEY_PATH")
	overrideString(&cfg.Push.APNs.KeyID, "AIBOT_PUSH_APNS_KEY_ID")
	overrideString(&cfg.Push.APNs.TeamID, "AIBOT_PUSH_APNS_TEAM_ID")
	overrideString(&cfg.Push.APNs.Topic, "AIBOT_PUSH_APNS_TOPIC")
	overrideString(&cfg.Push.FCM.CredentialsFile, "AIBOT_PUSH_FCM_CREDENTIALS_FILE")
	overrideString(&cfg.Push.JPush.AppKey, "AIBOT_PUSH_JPUSH_APP_KEY")
	overrideString(&cfg.Push.JPush.MasterSecret, "AIBOT_PUSH_JPUSH_MASTER_SECRET")
	overrideString(&cfg.Push.WebPush.VAPIDPublicKey, "AIBOT_PUSH_WEBPUSH_VAPID_PUBLIC_KEY")
	overrideString(&cfg.Push.WebPush.VAPIDPrivateKey, "AIBOT_PUSH_WEBPUSH_VAPID_PRIVATE_KEY")
	overrideString(&cfg.Push.WebPush.Subscriber, "AIBOT_PUSH_WEBPUSH_SUBSCRIBER")
	overrideString(&cfg.Push.Huawei.AppID, "AIBOT_PUSH_HUAWEI_APP_ID")
	overrideString(&cfg.Push.Huawei.ClientID, "AIBOT_PUSH_HUAWEI_CLIENT_ID")
	overrideString(&cfg.Push.Huawei.ClientSecret, "AIBOT_PUSH_HUAWEI_CLIENT_SECRET")
	overrideString(&cfg.Push.Xiaomi.AppSecret, "AIBOT_PUSH_XIAOMI_APP_SECRET")
	overrideString(&cfg.Push.Xiaomi.PackageName, "AIBOT_PUSH_XIAOMI_PACKAGE_NAME")

	overrideString(&cfg.AliEmail.AccessKeyID, "AIBOT_ALI_EMAIL_ACCESS_KEY_ID")
	overrideString(&cfg.AliEmail.AccessKeySecret, "AIBOT_ALI_EMAIL_ACCESS_KEY_SECRET")
	overrideString(&cfg.AliEmail.RegionID, "AIBOT_ALI_EMAIL_REGION_ID")
	overrideString(&cfg.AliEmail.Endpoint, "AIBOT_ALI_EMAIL_ENDPOINT")
	overrideString(&cfg.AliEmail.FromAddress, "AIBOT_ALI_EMAIL_FROM_ADDRESS")

	overrideString(&cfg.OAuth.GoogleAllowedClientIDs, "AIBOT_OAUTH_GOOGLE_ALLOWED_CLIENT_IDS")
	overrideString(&cfg.OAuth.AppleBundleIDs, "AIBOT_OAUTH_APPLE_BUNDLE_IDS")

	overrideString(&cfg.LiveKit.URL, "AIBOT_LIVEKIT_URL")
	overrideString(&cfg.LiveKit.PublicURL, "AIBOT_LIVEKIT_PUBLIC_URL")
	overrideString(&cfg.LiveKit.APIKey, "AIBOT_LIVEKIT_API_KEY")
	overrideString(&cfg.LiveKit.APISecret, "AIBOT_LIVEKIT_API_SECRET")

	// TURN relay (Coturn)
	overrideBool(&cfg.LiveKit.Turn.Enabled, "AIBOT_LIVEKIT_TURN_ENABLED")
	overrideString(&cfg.LiveKit.Turn.Username, "AIBOT_LIVEKIT_TURN_USERNAME")
	overrideString(&cfg.LiveKit.Turn.Credential, "AIBOT_LIVEKIT_TURN_CREDENTIAL")
	if urls := os.Getenv("AIBOT_LIVEKIT_TURN_URLS"); urls != "" {
		cfg.LiveKit.Turn.URLs = strings.Split(urls, ",")
	}

	overrideInt(&cfg.Gateway.Port, "AIBOT_GATEWAY_PORT")
	overrideBool(&cfg.Gateway.RelayStateEnabled, "AIBOT_GATEWAY_RELAY_STATE_ENABLED")
	overrideBool(&cfg.Gateway.DirectRelayEnabled, "AIBOT_GATEWAY_DIRECT_RELAY_ENABLED")
	overrideBool(&cfg.Gateway.FxSync.Enabled, "AIBOT_GATEWAY_FXSYNC_ENABLED")
	overrideString(&cfg.Gateway.FxSync.APIURL, "AIBOT_GATEWAY_FXSYNC_API_URL")
	overrideString(&cfg.Gateway.FxSync.APIKey, "AIBOT_GATEWAY_FXSYNC_API_KEY")
	if raw := os.Getenv("AIBOT_GATEWAY_FXSYNC_CURRENCIES"); raw != "" {
		// 规整成 "CNY" 这种标准写法：容忍 "cny, eur" 之类的手抖，
		// 否则 " EUR" 会以"未配置合理区间"的名义被拒，报错信息指向错误的方向。
		parts := strings.Split(raw, ",")
		currencies := make([]string, 0, len(parts))
		for _, p := range parts {
			if c := strings.ToUpper(strings.TrimSpace(p)); c != "" {
				currencies = append(currencies, c)
			}
		}
		cfg.Gateway.FxSync.Currencies = currencies
	}
	if raw := os.Getenv("AIBOT_GATEWAY_FXSYNC_INTERVAL"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			log.Fatalf("config: AIBOT_GATEWAY_FXSYNC_INTERVAL %q is not a valid duration: %v", raw, err)
		}
		cfg.Gateway.FxSync.Interval = d
	}

	overrideInt(&cfg.Pay.Port, "AIBOT_PAY_PORT")
	overrideString(&cfg.Pay.NotifyURLBase, "AIBOT_PAY_NOTIFY_URL_BASE")
	overrideString(&cfg.Pay.InternalBaseURL, "AIBOT_PAY_INTERNAL_BASE_URL")
}

func overrideString(target *string, envKey string) {
	if target == nil {
		return
	}
	if value, ok := os.LookupEnv(envKey); ok {
		*target = strings.TrimSpace(value)
	}
}

func overrideInt(target *int, envKey string) {
	if target == nil {
		return
	}
	value, ok := os.LookupEnv(envKey)
	if !ok {
		return
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err == nil {
		*target = parsed
	}
}

func overrideInt64(target *int64, envKey string) {
	if target == nil {
		return
	}
	value, ok := os.LookupEnv(envKey)
	if !ok {
		return
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err == nil {
		*target = parsed
	}
}

func overrideBool(target *bool, envKey string) {
	if target == nil {
		return
	}
	value, ok := os.LookupEnv(envKey)
	if !ok {
		return
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err == nil {
		*target = parsed
	}
}

func overrideFloat64(target *float64, envKey string) {
	if target == nil {
		return
	}
	value, ok := os.LookupEnv(envKey)
	if !ok {
		return
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err == nil {
		*target = parsed
	}
}

func applyOSSEnvOverrides(cfg *OSSConfig, prefix string) {
	if cfg == nil {
		return
	}
	overrideString(&cfg.Endpoint, prefix+"_ENDPOINT")
	overrideString(&cfg.AccessKey, prefix+"_ACCESS_KEY")
	overrideString(&cfg.SecretKey, prefix+"_SECRET_KEY")
	overrideString(&cfg.Bucket, prefix+"_BUCKET")
	overrideString(&cfg.Region, prefix+"_REGION")
	overrideString(&cfg.PublicURL, prefix+"_PUBLIC_URL")
	overrideString(&cfg.StorageDir, prefix+"_STORAGE_DIR")
}

func validateSecurityCriticalConfig(cfg Config) error {
	if isUnsafeSecret(cfg.JWT.Secret) {
		return fmt.Errorf("jwt.secret must be provided via secure value or AIBOT_JWT_SECRET")
	}
	if len(strings.TrimSpace(cfg.JWT.Secret)) < 32 {
		return fmt.Errorf("jwt.secret must be at least 32 characters to resist offline brute force")
	}
	if isUnsafeSecret(cfg.Postgres.Password) {
		return fmt.Errorf("postgres.password must be provided via secure value or AIBOT_POSTGRES_PASSWORD")
	}
	if isUnsafeSecret(cfg.AliEmail.AccessKeyID) {
		return fmt.Errorf("ali_email.access_key_id must be provided via secure value or AIBOT_ALI_EMAIL_ACCESS_KEY_ID")
	}
	if isUnsafeSecret(cfg.AliEmail.AccessKeySecret) {
		return fmt.Errorf("ali_email.access_key_secret must be provided via secure value or AIBOT_ALI_EMAIL_ACCESS_KEY_SECRET")
	}
	if strings.TrimSpace(cfg.AliEmail.FromAddress) == "" {
		return fmt.Errorf("ali_email.from_address must not be empty")
	}
	if strings.TrimSpace(cfg.Server.AllowedWebOrigins) == "" {
		return fmt.Errorf("server.allowed_web_origins must not be empty")
	}
	// 独立加密密钥：webhook token 加密与语音 BYOK 加密不得复用 JWT secret，
	// 必须通过环境变量单独注入，避免 JWT 密钥泄露时连带暴露加密数据。
	if strings.TrimSpace(cfg.Server.WebhookTokenSecret) == "" {
		return fmt.Errorf("server.webhook_token_secret must be set via AIBOT_SERVER_WEBHOOK_TOKEN_SECRET")
	}
	if strings.TrimSpace(cfg.Server.VoiceCryptoSecret) == "" {
		return fmt.Errorf("server.voice_crypto_secret must be set via AIBOT_SERVER_VOICE_CRYPTO_SECRET")
	}
	if err := validateOSSEndpoint(cfg.OSS.Media, "oss.media"); err != nil {
		return err
	}
	if err := validateOSSEndpoint(cfg.OSS.Avatar, "oss.avatar"); err != nil {
		return err
	}
	if err := validateOSSEndpoint(cfg.OSS.Report, "oss.report"); err != nil {
		return err
	}
	if err := validateLiveKitConfig(cfg.LiveKit); err != nil {
		return err
	}
	return nil
}

func validateOSSEndpoint(cfg OSSConfig, configPath string) error {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	bucket := strings.TrimSpace(cfg.Bucket)
	if endpoint == "" || bucket == "" {
		return nil
	}
	if strings.Contains(endpoint, "://") {
		return fmt.Errorf("%s.endpoint must be host only, without scheme", configPath)
	}
	if strings.Contains(endpoint, "/") {
		return fmt.Errorf("%s.endpoint must not contain path segments", configPath)
	}

	host := endpoint
	if parsedHost, _, err := net.SplitHostPort(endpoint); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	labels := strings.Split(strings.ToLower(host), ".")
	if len(labels) < 2 {
		return nil
	}
	if labels[0] != strings.ToLower(bucket) {
		return nil
	}

	serviceLabel := labels[1]
	if serviceLabel == "file" || serviceLabel == "s3" || serviceLabel == "oss" || serviceLabel == "cos" || strings.HasPrefix(serviceLabel, "s3-") || strings.HasPrefix(serviceLabel, "oss-") {
		return fmt.Errorf("%s.endpoint must not include the bucket host prefix when DNS bucket lookup is enabled; expected provider endpoint host, got %q", configPath, endpoint)
	}
	return nil
}

// validateLiveKitConfig 校验 LiveKit 配置。
// Phase 1（真人通话）：URL + APIKey + APISecret 三项必须同时提供或同时为空。
// 三项全空时降级为 NoopRoomManager（通话功能不可用但不阻塞启动）。
// 部分填写视为配置错误，启动拒绝。
func validateLiveKitConfig(cfg LiveKitConfig) error {
	hasURL := strings.TrimSpace(cfg.URL) != ""
	hasKey := strings.TrimSpace(cfg.APIKey) != ""
	hasSecret := strings.TrimSpace(cfg.APISecret) != ""

	if !hasURL && !hasKey && !hasSecret {
		return nil // 未配置，降级为 noop
	}
	if !hasURL {
		return fmt.Errorf("livekit.url is required when livekit.api_key is set")
	}
	if !hasKey {
		return fmt.Errorf("livekit.api_key is required when livekit.url is set")
	}
	if !hasSecret {
		return fmt.Errorf("livekit.api_secret is required when livekit.url is set")
	}
	if !strings.HasPrefix(strings.TrimSpace(cfg.URL), "ws") {
		return fmt.Errorf("livekit.url must start with ws:// or wss://, got %q", cfg.URL)
	}
	return nil
}

func isUnsafeSecret(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return true
	}

	lower := strings.ToLower(trimmed)
	unsafeMarkers := []string{
		"change-in-production",
		"change-me",
		"change_me",
		"your-",
		"example",
		"dummy",
		"placeholder",
	}
	for _, marker := range unsafeMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
