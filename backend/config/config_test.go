package config

import "testing"

func TestApplyEnvOverrides(t *testing.T) {
	t.Setenv("AIBOT_SERVER_API_PORT", "37180")
	t.Setenv("AIBOT_SERVER_WIDGET_ENABLED", "true")
	t.Setenv("AIBOT_SERVER_NODE_ID", "ws-node-k8s-0")
	t.Setenv("AIBOT_SERVER_REGION", "global")
	t.Setenv("AIBOT_SERVER_PUBLIC_WS_URL", "wss://ws.example.com/ws")
	t.Setenv("AIBOT_JWT_SECRET", "super-secret-value")
	t.Setenv("AIBOT_POSTGRES_PASSWORD", "postgres-secret")
	t.Setenv("AIBOT_SERVER_ALLOWED_WEB_ORIGINS", "https://admin.example.com")
	t.Setenv("AIBOT_SERVER_FRIEND_QR_BASE_URL", "https://dhf.pub/u")
	t.Setenv("AIBOT_SERVER_GROUP_QR_BASE_URL", "https://dhf.pub/g")
	t.Setenv("AIBOT_SERVER_DEEP_LINK_IOS_APP_ID", "MYTEAMID.pub.dhf.grix")
	t.Setenv("AIBOT_SERVER_DEEP_LINK_ANDROID_PACKAGE", "pub.dhf.grix")
	t.Setenv("AIBOT_SERVER_DEEP_LINK_ANDROID_SHA256_CERTS", "AA:BB:CC")
	t.Setenv("AIBOT_SERVER_AGENT_API_DOMAIN", "wss://api.example.com")
	t.Setenv("AIBOT_SERVER_AGENT_API_PATH", "/v2/agent-api")
	t.Setenv("AIBOT_SERVER_AGENT_API_WS_PATH", "/connect")
	t.Setenv("AIBOT_SERVER_AGENT_API_HEARTBEAT_SEC", "45")
	t.Setenv("AIBOT_SERVER_WEBHOOK_TOKEN_SECRET", "webhook-secret-value")
	t.Setenv("AIBOT_SERVER_VOICE_CRYPTO_SECRET", "voice-crypto-secret-value")
	t.Setenv("AIBOT_SNOWFLAKE_MACHINE_ID", "129")
	t.Setenv("AIBOT_OSS_MEDIA_BUCKET", "media-bucket")
	t.Setenv("AIBOT_OSS_AVATAR_PUBLIC_URL", "https://avatar.example.com")
	t.Setenv("AIBOT_OSS_REPORT_STORAGE_DIR", "prod/report")
	t.Setenv("AIBOT_MIGRATION_LEGACY_OSS_BUCKET", "legacy-bucket")
	t.Setenv("AIBOT_ALI_EMAIL_ACCESS_KEY_ID", "ali-ak")
	t.Setenv("AIBOT_ALI_EMAIL_ACCESS_KEY_SECRET", "ali-secret")
	t.Setenv("AIBOT_ALI_EMAIL_FROM_ADDRESS", "no-reply@dhf.pub")
	t.Setenv("AIBOT_OAUTH_GOOGLE_ALLOWED_CLIENT_IDS", "web-client-1,web-client-2")
	t.Setenv("AIBOT_LLM_TRANSLATION_API_KEY", "translation-ak")
	t.Setenv("AIBOT_LLM_TRANSLATION_BASE_URL", "https://ark.cn-beijing.volces.com/api/v3")
	t.Setenv("AIBOT_LLM_TRANSLATION_PROXY_URL", "")
	t.Setenv("AIBOT_LLM_TRANSLATION_API_STYLE", "chat_completions")
	t.Setenv("AIBOT_LLM_TRANSLATION_MODEL", "doubao-seed-2-0-mini-260215")
	t.Setenv("AIBOT_LLM_TRANSLATION_REASONING_EFFORT", "minimal")
	t.Setenv("AIBOT_LLM_TRANSLATION_TEMPERATURE", "0.1")
	t.Setenv("AIBOT_LLM_TRANSLATION_MAX_OUTPUT_TOKENS", "1200")
	t.Setenv("AIBOT_LLM_TRANSLATION_REQUEST_TIMEOUT_SEC", "30")
	t.Setenv("AIBOT_LLM_TRANSLATION_EXTRA_BODY_JSON", "{\"thinking\":{\"enabled\":false}}")
	t.Setenv("AIBOT_LLM_TRANSLATION_EXTRA_HEADERS_JSON", "{\"X-Trace\":\"translation\"}")

	cfg := Config{
		Server: ServerConfig{
			APIPort:                    27180,
			WidgetEnabled:              false,
			NodeID:                     "from-file-node",
			AllowedWebOrigins:          "http://127.0.0.1:27180",
			FriendQRBaseURL:            "https://staging.example.com/u",
			GroupQRBaseURL:             "https://staging.example.com/g",
			DeepLinkIOSAppID:           "OLDTEAM.old.app",
			DeepLinkAndroidPackage:     "old.package",
			DeepLinkAndroidSHA256Certs: "11:22:33",
			AgentAPIDomain:             "ws://127.0.0.1:27189",
			AgentAPIPath:               "/v1/agent-api",
			AgentAPIWSPath:             "/ws",
			AgentAPIHeartbeat:          30,
		},
		Snowflake: SnowflakeConfig{
			MachineID: 1,
		},
		OSS: OSSGroupConfig{
			Media: OSSConfig{
				Bucket: "from-file-media",
			},
			Avatar: OSSConfig{
				PublicURL: "https://avatar-from-file.example.com",
			},
			Report: OSSConfig{
				StorageDir: "from-file/report",
			},
		},
		Migration: MigrationConfig{
			LegacyOSS: OSSConfig{
				Bucket: "from-file-legacy",
			},
		},
		Postgres: PostgresConfig{
			Password: "from-file",
		},
		JWT: JWTConfig{
			Secret: "from-file",
		},
		AliEmail: AliEmailConfig{
			AccessKeyID:     "from-file-ak",
			AccessKeySecret: "from-file-secret",
			FromAddress:     "from-file@example.com",
		},
		OAuth: OAuthConfig{
			GoogleAllowedClientIDs: "old-client",
		},
		LLM: LLMConfig{
			Translation: TranslationLLMConfig{
				APIKey:            "old-translation-ak",
				BaseURL:           "https://old-translation.example.com",
				ProxyURL:          "https://proxy.old.example.com",
				APIStyle:          "responses",
				Model:             "old-model",
				ReasoningEffort:   "medium",
				Temperature:       0.2,
				MaxOutputTokens:   800,
				RequestTimeoutSec: 60,
				ExtraBodyJSON:     "{\"old\":true}",
				ExtraHeadersJSON:  "{\"X-Old\":\"1\"}",
			},
		},
	}

	applyEnvOverrides(&cfg)

	if cfg.Server.APIPort != 37180 {
		t.Fatalf("expected api port override, got %d", cfg.Server.APIPort)
	}
	if !cfg.Server.WidgetEnabled {
		t.Fatalf("expected widget enabled override, got %v", cfg.Server.WidgetEnabled)
	}
	if cfg.Server.NodeID != "ws-node-k8s-0" {
		t.Fatalf("expected node id override, got %q", cfg.Server.NodeID)
	}
	if cfg.Server.Region != "global" {
		t.Fatalf("expected region override, got %q", cfg.Server.Region)
	}
	if cfg.Server.PublicWsURL != "wss://ws.example.com/ws" {
		t.Fatalf("expected public ws url override, got %q", cfg.Server.PublicWsURL)
	}
	if cfg.JWT.Secret != "super-secret-value" {
		t.Fatalf("expected jwt secret override, got %q", cfg.JWT.Secret)
	}
	if cfg.Postgres.Password != "postgres-secret" {
		t.Fatalf("expected postgres password override, got %q", cfg.Postgres.Password)
	}
	if cfg.Server.AllowedWebOrigins != "https://admin.example.com" {
		t.Fatalf("expected allowed origins override, got %q", cfg.Server.AllowedWebOrigins)
	}
	if cfg.Server.FriendQRBaseURL != "https://dhf.pub/u" {
		t.Fatalf("expected friend qr base url override, got %q", cfg.Server.FriendQRBaseURL)
	}
	if cfg.Server.GroupQRBaseURL != "https://dhf.pub/g" {
		t.Fatalf("expected group qr base url override, got %q", cfg.Server.GroupQRBaseURL)
	}
	if cfg.Server.DeepLinkIOSAppID != "MYTEAMID.pub.dhf.grix" {
		t.Fatalf("expected ios app id override, got %q", cfg.Server.DeepLinkIOSAppID)
	}
	if cfg.Server.DeepLinkAndroidPackage != "pub.dhf.grix" {
		t.Fatalf("expected android package override, got %q", cfg.Server.DeepLinkAndroidPackage)
	}
	if cfg.Server.DeepLinkAndroidSHA256Certs != "AA:BB:CC" {
		t.Fatalf("expected android cert override, got %q", cfg.Server.DeepLinkAndroidSHA256Certs)
	}
	if cfg.Server.AgentAPIDomain != "wss://api.example.com" {
		t.Fatalf("expected agent api domain override, got %q", cfg.Server.AgentAPIDomain)
	}
	if cfg.Server.AgentAPIPath != "/v2/agent-api" {
		t.Fatalf("expected agent api path override, got %q", cfg.Server.AgentAPIPath)
	}
	if cfg.Server.AgentAPIWSPath != "/connect" {
		t.Fatalf("expected agent api ws path override, got %q", cfg.Server.AgentAPIWSPath)
	}
	if cfg.Server.AgentAPIHeartbeat != 45 {
		t.Fatalf("expected agent api heartbeat override, got %d", cfg.Server.AgentAPIHeartbeat)
	}
	if cfg.Server.WebhookTokenSecret != "webhook-secret-value" {
		t.Fatalf("expected webhook token secret override, got %q", cfg.Server.WebhookTokenSecret)
	}
	if cfg.Server.VoiceCryptoSecret != "voice-crypto-secret-value" {
		t.Fatalf("expected voice crypto secret override, got %q", cfg.Server.VoiceCryptoSecret)
	}
	if cfg.Snowflake.MachineID != 129 {
		t.Fatalf("expected snowflake machine id override, got %d", cfg.Snowflake.MachineID)
	}
	if cfg.OSS.Media.Bucket != "media-bucket" {
		t.Fatalf("expected media bucket override, got %q", cfg.OSS.Media.Bucket)
	}
	if cfg.OSS.Avatar.PublicURL != "https://avatar.example.com" {
		t.Fatalf("expected avatar public url override, got %q", cfg.OSS.Avatar.PublicURL)
	}
	if cfg.OSS.Report.StorageDir != "prod/report" {
		t.Fatalf("expected report storage dir override, got %q", cfg.OSS.Report.StorageDir)
	}
	if cfg.Migration.LegacyOSS.Bucket != "legacy-bucket" {
		t.Fatalf("expected legacy oss bucket override, got %q", cfg.Migration.LegacyOSS.Bucket)
	}
	if cfg.AliEmail.AccessKeyID != "ali-ak" {
		t.Fatalf("expected ali email access key id override, got %q", cfg.AliEmail.AccessKeyID)
	}
	if cfg.AliEmail.AccessKeySecret != "ali-secret" {
		t.Fatalf("expected ali email access key secret override, got %q", cfg.AliEmail.AccessKeySecret)
	}
	if cfg.AliEmail.FromAddress != "no-reply@dhf.pub" {
		t.Fatalf("expected ali email from address override, got %q", cfg.AliEmail.FromAddress)
	}
	if cfg.OAuth.GoogleAllowedClientIDs != "web-client-1,web-client-2" {
		t.Fatalf("expected google allowed client ids override, got %q", cfg.OAuth.GoogleAllowedClientIDs)
	}
	if cfg.LLM.Translation.APIKey != "translation-ak" {
		t.Fatalf("expected translation api key override, got %q", cfg.LLM.Translation.APIKey)
	}
	if cfg.LLM.Translation.BaseURL != "https://ark.cn-beijing.volces.com/api/v3" {
		t.Fatalf("expected translation base url override, got %q", cfg.LLM.Translation.BaseURL)
	}
	if cfg.LLM.Translation.ProxyURL != "" {
		t.Fatalf("expected translation proxy url override, got %q", cfg.LLM.Translation.ProxyURL)
	}
	if cfg.LLM.Translation.APIStyle != "chat_completions" {
		t.Fatalf("expected translation api style override, got %q", cfg.LLM.Translation.APIStyle)
	}
	if cfg.LLM.Translation.Model != "doubao-seed-2-0-mini-260215" {
		t.Fatalf("expected translation model override, got %q", cfg.LLM.Translation.Model)
	}
	if cfg.LLM.Translation.ReasoningEffort != "minimal" {
		t.Fatalf("expected translation reasoning effort override, got %q", cfg.LLM.Translation.ReasoningEffort)
	}
	if cfg.LLM.Translation.Temperature != 0.1 {
		t.Fatalf("expected translation temperature override, got %v", cfg.LLM.Translation.Temperature)
	}
	if cfg.LLM.Translation.MaxOutputTokens != 1200 {
		t.Fatalf("expected translation max output tokens override, got %d", cfg.LLM.Translation.MaxOutputTokens)
	}
	if cfg.LLM.Translation.RequestTimeoutSec != 30 {
		t.Fatalf("expected translation request timeout override, got %d", cfg.LLM.Translation.RequestTimeoutSec)
	}
	if cfg.LLM.Translation.ExtraBodyJSON != "{\"thinking\":{\"enabled\":false}}" {
		t.Fatalf("expected translation extra body json override, got %q", cfg.LLM.Translation.ExtraBodyJSON)
	}
	if cfg.LLM.Translation.ExtraHeadersJSON != "{\"X-Trace\":\"translation\"}" {
		t.Fatalf("expected translation extra headers json override, got %q", cfg.LLM.Translation.ExtraHeadersJSON)
	}
}

func TestValidateSecurityCriticalConfig(t *testing.T) {
	valid := Config{
		Server: ServerConfig{
			AllowedWebOrigins:  "https://admin.example.com",
			WebhookTokenSecret: "webhook-secret-value",
			VoiceCryptoSecret:  "voice-crypto-secret-value",
		},
		Postgres: PostgresConfig{
			Password: "postgres-secret",
		},
		JWT: JWTConfig{
			Secret: "jwt-secret-value-0123456789abcdef",
		},
		AliEmail: AliEmailConfig{
			AccessKeyID:     "ali-ak",
			AccessKeySecret: "ali-secret",
			FromAddress:     "no-reply@dhf.pub",
		},
		OSS: OSSGroupConfig{
			Media: OSSConfig{
				Endpoint: "cos.ap-chengdu.myqcloud.com",
				Bucket:   "prod-media-bucket",
			},
			Avatar: OSSConfig{
				Endpoint: "cos.ap-chengdu.myqcloud.com",
				Bucket:   "prod-avatar-bucket",
			},
			Report: OSSConfig{
				Endpoint: "cos.ap-chengdu.myqcloud.com",
				Bucket:   "prod-report-bucket",
			},
		},
	}
	if err := validateSecurityCriticalConfig(valid); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}

	invalid := valid
	invalid.JWT.Secret = "CHANGE_ME_USE_ENV_AIBOT_JWT_SECRET"
	if err := validateSecurityCriticalConfig(invalid); err == nil {
		t.Fatal("expected placeholder jwt secret to be rejected")
	}

	invalid = valid
	invalid.AliEmail.AccessKeyID = ""
	if err := validateSecurityCriticalConfig(invalid); err == nil {
		t.Fatal("expected empty ali email access key id to be rejected")
	}

	invalid = valid
	invalid.AliEmail.AccessKeySecret = "dummy_secret"
	if err := validateSecurityCriticalConfig(invalid); err == nil {
		t.Fatal("expected placeholder ali email access key secret to be rejected")
	}

	invalid = valid
	invalid.AliEmail.FromAddress = ""
	if err := validateSecurityCriticalConfig(invalid); err == nil {
		t.Fatal("expected empty ali email from address to be rejected")
	}

	invalid = valid
	invalid.OSS.Media.Endpoint = "prod-media-bucket.file.myqcloud.com"
	if err := validateSecurityCriticalConfig(invalid); err == nil {
		t.Fatal("expected bucket-scoped oss endpoint to be rejected")
	}

	invalid = valid
	invalid.OSS.Media.Endpoint = "https://cos.ap-chengdu.myqcloud.com"
	if err := validateSecurityCriticalConfig(invalid); err == nil {
		t.Fatal("expected oss endpoint with scheme to be rejected")
	}

	invalid = valid
	invalid.Server.WebhookTokenSecret = ""
	if err := validateSecurityCriticalConfig(invalid); err == nil {
		t.Fatal("expected empty webhook_token_secret to be rejected")
	}

	invalid = valid
	invalid.Server.VoiceCryptoSecret = ""
	if err := validateSecurityCriticalConfig(invalid); err == nil {
		t.Fatal("expected empty voice_crypto_secret to be rejected")
	}
}

func TestValidateLiveKitConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     LiveKitConfig
		wantErr bool
	}{
		{"all empty — noop mode", LiveKitConfig{}, false},
		{"fully configured", LiveKitConfig{URL: "wss://lk.example.com", APIKey: "key", APISecret: "secret"}, false},
		{"ws:// scheme ok", LiveKitConfig{URL: "ws://lk.local", APIKey: "key", APISecret: "secret"}, false},
		{"missing URL", LiveKitConfig{APIKey: "key", APISecret: "secret"}, true},
		{"missing APIKey", LiveKitConfig{URL: "wss://lk.example.com", APISecret: "secret"}, true},
		{"missing APISecret", LiveKitConfig{URL: "wss://lk.example.com", APIKey: "key"}, true},
		{"bad scheme", LiveKitConfig{URL: "https://lk.example.com", APIKey: "key", APISecret: "secret"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLiveKitConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateLiveKitConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
