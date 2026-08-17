package api

import (
	"github.com/askie/grix/backend/internal/admin"
	"github.com/askie/grix/backend/internal/adminweb"
	"github.com/askie/grix/backend/internal/api/handler"
	"github.com/askie/grix/backend/internal/metrics"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/publicsite"
	"github.com/askie/grix/backend/internal/webapp"
	"github.com/askie/grix/backend/internal/wsproxy"
	"github.com/gin-gonic/gin"
)

func SetupRouter() *gin.Engine {
	r := gin.New()
	// 收敛可信代理边界：只信任回环+私网对端的 XFF，防伪造 X-Forwarded-For 绕过 IP 限流与 InternalOnly。
	middleware.ApplyTrustedProxies(r)
	// webhook incoming 路径含长期凭证 token，跳过访问日志避免凭证落盘；
	// /v1/reach/unsubscribe?token= 同理（长期退订凭证走 query）。
	r.Use(middleware.SensitivePathLogger("/v1/webhook/incoming/", "/v1/reach/unsubscribe"), gin.Recovery(), middleware.CORS(), middleware.Metrics())

	r.GET("/health", handler.Health)
	r.HEAD("/health", handler.Health)
	r.GET("/readyz", handler.Ready)
	r.GET("/version", handler.Version)
	r.GET("/metrics", middleware.InternalOnly(), gin.WrapH(metrics.Handler()))

	admin.RegisterRoutes(r)
	publicsite.RegisterRoutes(r)

	v1 := r.Group("/v1")

	// Public routes (no auth required)
	v1.GET("/features", handler.PublicGetFeatures)

	// Appcast XML for Sparkle / WinSparkle（公开，无需认证）
	v1.GET("/app/appcast.xml", middleware.RateLimitByIP("appcast", 30, 0.5), handler.AppcastXML)

	// Reach: public endpoints (no auth)
	v1.GET("/reach/unsubscribe", middleware.RateLimitByIP("reach-unsub", 10, 1.0/6), handler.ReachUnsubscribe)
	v1.GET("/reach/t/o/:id", handler.ReachTrackOpen)
	v1.GET("/reach/t/c/:id", handler.ReachTrackClick)

	// Auth routes (no auth required)
	auth := v1.Group("/auth")
	{
		auth.GET("/methods", handler.GetAuthMethods)
		auth.GET("/captcha", handler.GenerateCaptcha)
		auth.POST("/send-code", middleware.RateLimitByIP("send-code", 5, 1.0/60), handler.SendEmailCode)
		auth.POST("/sms/send", middleware.RateLimitByIP("sms-send", 10, 1.0/60), handler.SendSmsCode)
		auth.POST("/phone/login-code", middleware.RateLimitByIP("phone-login-code", 10, 5.0/60), handler.PhoneLoginWithCode)
		auth.POST("/register", middleware.RateLimitByIP("register", 10, 5.0/60), handler.Register)
		auth.POST("/login", middleware.RateLimitByIP("login", 10, 5.0/60), handler.Login)
		auth.POST("/oauth2/google", middleware.RateLimitByIP("oauth2-google", 5, 1.0/60), handler.LoginWithGoogle)
		auth.POST("/oauth2/apple", middleware.RateLimitByIP("oauth2-apple", 5, 1.0/60), handler.LoginWithApple)
		auth.POST("/reset-password", middleware.RateLimitByIP("reset-password", 10, 5.0/60), handler.ResetPassword)
		auth.POST("/refresh", middleware.RateLimitByIP("refresh", 10, 5.0/60), handler.Refresh)
		auth.POST("/logout", middleware.Auth(), handler.Logout)
		auth.POST("/qr/create", middleware.RateLimitByIP("auth-qr-create", 10, 1.0/6), handler.QRLoginCreate)
		auth.GET("/qr/status", middleware.RateLimitByIP("auth-qr-status", 120, 2.0), handler.QRLoginStatus)
		auth.POST("/qr/exchange", middleware.RateLimitByIP("auth-qr-exchange", 20, 1.0/3), handler.QRLoginExchange)
		auth.POST("/qr/scan", middleware.Auth(), middleware.RateLimitByUser("auth-qr-scan", 30, 1.0), handler.QRLoginScan)
		auth.POST("/qr/confirm", middleware.Auth(), middleware.RateLimitByUser("auth-qr-confirm", 30, 1.0), handler.QRLoginConfirm)
	}

	// Authenticated routes
	authed := v1.Group("")
	authed.Use(middleware.Auth())
	{
		// Sessions
		sessions := authed.Group("/sessions")
		{
			sessionsRead := sessions.Group("")
			sessionsRead.Use(middleware.RateLimitByUser("sessions-read", 200, 5.0))
			{
				sessionsRead.GET("/list", handler.SessionList)
				sessionsRead.GET("/sync", handler.SessionSync)
				sessionsRead.GET("/conversations", handler.SessionConversations)
				sessionsRead.GET("/conversation_threads", handler.SessionConversationThreads)
				sessionsRead.GET("/favorites", handler.ListFavoriteSessions)
				sessionsRead.GET("/favorites/ids", handler.GetFavoriteSessionIDs)
			}

			sessionsDetail := sessions.Group("")
			sessionsDetail.Use(middleware.RateLimitByUser("sessions-detail", 120, 2.0))
			{
				sessionsDetail.GET("/detail", handler.SessionDetail)
				sessionsDetail.GET("/group/qr", handler.SessionGroupQRCodeGet)
				sessionsDetail.GET("/group/qr/resolve/:code", handler.SessionGroupQRCodeResolve)
				sessionsDetail.GET("/:id/favorite", handler.GetSessionFavoriteStatus)
			}

			sessionsWrite := sessions.Group("")
			sessionsWrite.Use(middleware.RateLimitByUser("sessions-write", 40, 0.5))
			{
				sessionsWrite.POST("/create", handler.SessionCreate)
				sessionsWrite.POST("/open_latest", handler.SessionOpenLatest)
				sessionsWrite.POST("/rename", handler.SessionRename)
				sessionsWrite.POST("/pin", handler.SessionSetPinned)
				sessionsWrite.POST("/mute", handler.SessionSetMuted)
				sessionsWrite.POST("/create_group", handler.SessionCreateGroup)
				sessionsWrite.POST("/convert_to_group", handler.SessionConvertToGroup)
				sessionsWrite.POST("/group/join_by_qr", handler.SessionJoinGroupByQRCode)
				sessionsWrite.POST("/members/add", handler.SessionAddMembers)
				sessionsWrite.POST("/members/invite_setting", handler.SessionUpdateInviteSetting)
				sessionsWrite.POST("/speaking/all_muted", handler.SessionUpdateAllMembersMuted)
				sessionsWrite.POST("/leave", handler.SessionLeave)
				sessionsWrite.POST("/members/remove", handler.SessionRemoveMembers)
				sessionsWrite.POST("/members/nickname", handler.SessionSetGroupNickname)
				sessionsWrite.POST("/members/speaking", handler.SessionUpdateMemberSpeaking)
				sessionsWrite.POST("/members/agent_receive", handler.SessionUpdateMemberAgentReceive)
				sessionsWrite.POST("/members/role", handler.SessionUpdateMemberRole)
				sessionsWrite.POST("/owner/transfer", handler.SessionTransferOwner)
				sessionsWrite.POST("/dissolve", handler.SessionDissolve)
				sessionsWrite.POST("/:id/favorite", handler.AddSessionFavorite)
				sessionsWrite.DELETE("/:id/favorite", handler.RemoveSessionFavorite)
			}
		}

		// App update check
		authed.GET("/app/check-update", handler.CheckAppUpdate)
		authed.POST("/app/report-download", handler.ReportAppDownload)

		// Messages
		messages := authed.Group("/messages")
		{
			messages.GET("/history", handler.MessageHistory)
			messages.POST("/delete", handler.MessageDelete)
			messages.POST("/edit", handler.MessageEdit)
		}

		webhooks := authed.Group("/api/webhooks")
		{
			webhooks.POST("", handler.WebhookCreate)
			webhooks.GET("", handler.WebhookListAll)
			webhooks.DELETE("/:id", handler.WebhookDelete)
		}
		authed.GET("/api/sessions/:session_id/webhooks", handler.WebhookList)

		// OSS
		oss := authed.Group("/oss")
		oss.Use(middleware.RateLimitByUser("oss", 20, 10.0/60))
		{
			oss.POST("/presign", handler.OSSPresign)
			oss.POST("/delete", handler.OSSDeleteObjects)
		}

		reports := authed.Group("/reports")
		{
			reports.POST(
				"/assets/presign",
				middleware.RateLimitByUser("reports-assets-presign", 30, 10.0/60),
				handler.ReportAssetPresign,
			)
			reports.POST(
				"",
				middleware.RateLimitByUser("reports-create", 10, 1.0/60),
				handler.ReportCreate,
			)
		}

		// Devices
		devices := authed.Group("/devices")
		{
			devices.POST("/bind", handler.DeviceBind)
			devices.GET("/sessions", handler.DeviceSessionList)
			devices.DELETE("/sessions/:session_id", handler.DeviceSessionRemove)
		}

		// Agent 通知偏好
		authed.GET("/notification-prefs", handler.GetNotificationPrefs)
		authed.PUT("/notification-prefs", handler.UpdateNotificationPrefs)

		// Users
		users := authed.Group("/users")
		{
			users.GET("/profile", handler.GetProfile)
			users.PUT("/profile", handler.UpdateProfile)
			users.GET("/settings", handler.GetUserSettings)
			users.PUT("/settings", handler.UpdateUserSettings)
			users.POST(
				"/password/email-code",
				middleware.RateLimitByUser("user-password-email-code", 5, 1.0/60),
				handler.SendChangePasswordEmailCode,
			)
			users.POST(
				"/password",
				middleware.RateLimitByUser("user-password-change", 8, 2.0/60),
				handler.ChangeOwnPassword,
			)
			users.DELETE("/me", handler.DeleteAccount)
			users.POST("/avatar", handler.UploadAvatar)
			users.PUT("/username", handler.UpdateUsername)
			users.POST(
				"/bind-phone",
				middleware.RateLimitByUser("user-bind-phone", 8, 2.0/60),
				handler.BindPhone,
			)
			users.GET("/search", handler.UserSearch)
			users.GET("/features", handler.UserGetFeatures)
			users.GET("/:id/profile", handler.GetUserProfile)

			userFavorites := users.Group("/favorites/paths")
			{
				userFavorites.GET("/list", handler.ListFavoritePaths)
				userFavorites.POST("/add", handler.AddFavoritePath)
				userFavorites.DELETE("/:id", handler.DeleteFavoritePath)
			}
		}

		// Friends
		friends := authed.Group("/friends")
		{
			friends.POST("/request", handler.FriendRequestSend)
			friends.GET("/requests", handler.FriendRequestList)
			friends.POST("/handle", handler.FriendRequestHandle)
			friends.GET("/list", handler.FriendList)
			friends.GET("/qr", handler.FriendQRCodeGet)
			friends.GET("/qr/resolve/:code", handler.FriendQRCodeResolve)
			friends.POST("/remark", handler.FriendRemarkUpdate)
			friends.POST("/pin", handler.FriendSetPinned)
			friends.POST("/mute", handler.FriendSetMuted)
			friends.POST("/block", handler.FriendBlock)
			friends.DELETE("/:id", handler.FriendDelete)
		}

		// Agents
		agents := authed.Group("/agents")
		{
			agentCategories := agents.Group("/categories")
			{
				agentCategories.GET("/list", handler.AgentCategoryList)
				agentCategories.POST("/create", handler.AgentCategoryCreate)
				agentCategories.PUT("/:id", handler.AgentCategoryUpdate)
				agentCategories.DELETE("/:id", handler.AgentCategoryDelete)
			}

			agents.POST("/create", handler.AgentCreate)
			agents.GET("/list", handler.AgentList)
			agents.GET("/agent-api/install-guides", handler.AgentAPIInstallGuideList)
			agents.GET("/voice-models", handler.AgentVoiceModelList)
			agents.PUT("/batch-sort", handler.AgentBatchSort)
			// agent 共享：分享给我的（静态路由需在 /:id 之前）。
			agents.GET("/shared-with-me", handler.AgentSharedWithMe)
			agents.GET("/:id", handler.AgentGet)
			agents.GET("/:id/scopes", handler.AgentScopeGet)
			agents.PUT("/:id", handler.AgentUpdate)
			agents.PUT("/:id/scopes", handler.AgentScopeReplace)
			agents.POST("/:id/avatar", handler.AgentUploadAvatar)
			agents.PUT("/:id/context", handler.AgentUpdateContext)
			agents.POST("/:id/api/key/rotate", handler.AgentRotateAPIKey)
			// agent WS 连接安全（阶段0）：在线连接/日志/踢线/IP 黑白名单。
			agents.GET("/:id/connections", handler.AgentConnectionsOnline)
			agents.GET("/:id/connection-logs", handler.AgentConnectionLogs)
			agents.POST("/:id/connections/kick", handler.AgentConnectionKick)
			agents.GET("/:id/ip-rules", handler.AgentIPRuleList)
			agents.POST("/:id/ip-rules", handler.AgentIPRuleCreate)
			agents.DELETE("/:id/ip-rules/:rule_id", handler.AgentIPRuleDelete)
			// agent 共享：主人管理共享对象。
			agents.POST("/:id/shares", handler.AgentShareCreate)
			agents.GET("/:id/shares", handler.AgentShareList)
			agents.DELETE("/:id/shares/:uid", handler.AgentShareRevoke)
			agents.DELETE("/:id", handler.AgentDelete)
		}

		// Eggs market (user side)
		eggs := authed.Group("/eggs")
		{
			eggsRead := eggs.Group("")
			eggsRead.Use(middleware.RateLimitByUser("eggs-read", 120, 2.0))
			{
				eggsRead.GET("/categories", handler.EggCategoryList)
				eggsRead.GET("/search", handler.EggSearch)
				eggsRead.GET("/get", handler.EggGet)
				eggsRead.GET("/install/:install_id", handler.EggInstallStatus)
			}

			eggsWrite := eggs.Group("")
			eggsWrite.Use(middleware.RateLimitByUser("eggs-install", 20, 1.0/3))
			{
				eggsWrite.POST("/install", handler.EggInstall)
			}
		}

		// 自定义技能多机器同步：技能库 CRUD + 上载 + connector 拉取
		skills := authed.Group("/skills")
		skills.Use(middleware.RateLimitByUser("skills", 120, 2.0))
		{
			skills.GET("", handler.SkillList)
			skills.POST("", handler.SkillCreate)
			skills.POST("/upload", handler.SkillUpload)
			skills.GET("/:id/content", handler.SkillGetContent)
			skills.PUT("/:id", handler.SkillUpdate)
			skills.DELETE("/:id", handler.SkillDelete)
		}

		// 大模型计费网关 C端自助（建/查/吊销自己的虚拟Key、查自己的钱包余额与流水）
		gateway := authed.Group("/gateway")
		{
			gatewayRead := gateway.Group("")
			gatewayRead.Use(middleware.RateLimitByUser("gateway-read", 120, 2.0))
			{
				gatewayRead.GET("/keys", handler.GatewayListKeys)
				gatewayRead.GET("/wallet", handler.GatewayGetWallet)
				gatewayRead.GET("/wallet/ledger", handler.GatewayListLedger)
				gatewayRead.GET("/wallet/topups", handler.GatewayListTopups)
				// "Grix中转"：可用模型清单（含单价）、兜底模型与映射表、我名下Agent的接入状态
				gatewayRead.GET("/models", handler.GatewayListModels)
				gatewayRead.GET("/relay-settings", handler.GatewayGetRelaySettings)
				gatewayRead.GET("/agents", handler.GatewayListAgents)
			}

			gatewayWrite := gateway.Group("")
			gatewayWrite.Use(middleware.RateLimitByUser("gateway-write", 20, 1.0/3))
			{
				gatewayWrite.POST("/keys", handler.GatewayCreateKey)
				gatewayWrite.POST("/keys/:id/revoke", handler.GatewayRevokeKey)
				gatewayWrite.POST("/agents/:agent_id/provider", handler.GatewayConfigureAgentProvider)
				gatewayWrite.POST("/agents/:agent_id/relay-credential", handler.GatewayIssueAgentRelayCredential)
				// 中转开关服务端化（migration 111）：设置 Agent 中转开关的 desired 期望态
				gatewayWrite.POST("/agents/:agent_id/relay", handler.GatewaySetAgentRelay)
				gatewayWrite.POST("/wallet/topup", handler.GatewayCreateTopup)
				gatewayWrite.PUT("/relay-settings", handler.GatewayPutRelaySettings)
			}
		}

		authed.GET("/reach/subscription", handler.ReachGetSubscription)
		authed.PUT("/reach/subscription", handler.ReachUpdateSubscription)
	}

	// Agent API routes (authenticated via api_key, not JWT)
	agentAPI := v1.Group("/agent-api")
	agentAPI.Use(middleware.AgentAPIAuth())
	{
		agentAPIRead := agentAPI.Group("")
		agentAPIRead.Use(
			middleware.RateLimitByOwner("agent-api-read-owner", 120, 2.0),
			middleware.RateLimitByAgent("agent-api-read-agent", 120, 2.0),
		)
		{
			agentAPIRead.GET("/messages/history", handler.AgentMessageHistory)
			agentAPIRead.GET("/messages/search", handler.AgentMessageSearch)
			agentAPIRead.GET(
				"/agents/categories/list",
				middleware.AgentAPIScope(agentscope.ScopeAgentCategoryList),
				handler.AgentAPICategoryList,
			)
			agentAPIRead.GET(
				"/contacts/search",
				middleware.AgentAPIScope(agentscope.ScopeContactSearch),
				handler.AgentContactSearch,
			)
			agentAPIRead.GET(
				"/sessions/search",
				middleware.AgentAPIScope(agentscope.ScopeSessionSearch),
				handler.AgentSessionSearch,
			)
			agentAPIRead.GET(
				"/sessions/group/detail",
				handler.AgentSessionGroupDetail,
			)
			agentAPIRead.GET("/upgrade/check", handler.AgentCheckUpgrade)

			// 自定义技能多机器同步：connector 拉取本 owner 技能库
			agentAPIRead.GET("/skills", handler.AgentSkillList)
			agentAPIRead.GET("/skills/:id/content", handler.AgentSkillGetContent)
		}

		agentAPIWrite := agentAPI.Group("")
		agentAPIWrite.Use(
			middleware.RateLimitByOwner("agent-api-write-owner", 40, 0.5),
			middleware.RateLimitByAgent("agent-api-write-agent", 40, 0.5),
		)
		{
			agentAPIWrite.POST(
				"/agents/create",
				middleware.RateLimitByOwner("agent-api-agents-create-owner", 4, 1.0/60),
				middleware.RateLimitByAgent("agent-api-agents-create-agent", 2, 1.0/60),
				middleware.AgentAPIScope(agentscope.ScopeAgentAPICreate),
				handler.AgentAPIAgentCreate,
			)
			agentAPIWrite.POST(
				"/agents/:id/api/key/rotate",
				middleware.AgentAPIScope(agentscope.ScopeAgentAPICreate),
				handler.AgentAPIAgentRotateAPIKey,
			)
			agentAPIWrite.POST(
				"/agents/categories/create",
				middleware.AgentAPIScope(agentscope.ScopeAgentCategoryCreate),
				handler.AgentAPICategoryCreate,
			)
			agentAPIWrite.PUT(
				"/agents/categories/:id",
				middleware.AgentAPIScope(agentscope.ScopeAgentCategoryUpdate),
				handler.AgentAPICategoryUpdate,
			)
			agentAPIWrite.PUT(
				"/agents/:id/category",
				middleware.AgentAPIScope(agentscope.ScopeAgentCategoryAssign),
				handler.AgentAPIAgentAssignCategory,
			)
			agentAPIWrite.POST("/messages/delete", handler.AgentMessageDelete)
			agentAPIWrite.POST("/messages/edit", handler.AgentMessageEdit)
			agentAPIWrite.POST(
				"/oss/presign",
				middleware.AgentAPIScope(agentscope.ScopeMediaUpload),
				handler.AgentOSSPresign,
			)
			agentAPIWrite.POST("/sessions/create", handler.AgentSessionCreate)
			agentAPIWrite.POST("/sessions/open_latest", handler.AgentSessionOpenLatest)
			agentAPIWrite.POST("/sessions/leave", handler.AgentSessionLeave)

			// Phase 4: group governance
			agentAPIWrite.POST(
				"/sessions/create_group",
				middleware.AgentAPIScope(agentscope.ScopeGroupCreate),
				handler.AgentSessionCreateGroup,
			)
			agentAPIWrite.POST(
				"/sessions/members/add",
				middleware.AgentAPIScope(agentscope.ScopeGroupMemberAdd),
				handler.AgentSessionAddMembers,
			)
			agentAPIWrite.POST(
				"/sessions/members/remove",
				middleware.AgentAPIScope(agentscope.ScopeGroupMemberRemove),
				handler.AgentSessionRemoveMembers,
			)
			agentAPIWrite.POST(
				"/sessions/members/role",
				middleware.AgentAPIScope(agentscope.ScopeGroupMemberRoleUpdate),
				handler.AgentSessionUpdateMemberRole,
			)
			agentAPIWrite.POST(
				"/sessions/speaking/all_muted",
				middleware.AgentAPIScope(agentscope.ScopeGroupSpeakingUpdate),
				handler.AgentSessionUpdateAllMembersMuted,
			)
			agentAPIWrite.POST(
				"/sessions/members/speaking",
				middleware.AgentAPIScope(agentscope.ScopeGroupSpeakingUpdate),
				handler.AgentSessionUpdateMemberSpeaking,
			)
			agentAPIWrite.POST(
				"/sessions/dissolve",
				middleware.AgentAPIScope(agentscope.ScopeGroupDissolve),
				handler.AgentSessionDissolve,
			)
			agentAPIWrite.POST("/upgrade/report", handler.AgentReportUpgrade)

			// 自定义技能多机器同步：connector 上载/删除本地技能
			agentAPIWrite.POST("/skills/upload", handler.AgentSkillUpload)
			agentAPIWrite.POST("/skills/delete", handler.AgentSkillDelete)
		}
	}

	// Widget APIs are always available.
	handler.RegisterWidgetManagementRoutes(authed)
	handler.RegisterWidgetRoutes(v1, authed)

	// 通话历史（Phase 1）
	authed.GET("/call-records", handler.CallRecordList)

	// 链接安全（打开时校验）
	authed.POST("/link/check",
		middleware.RateLimitByUser("link-check", 60, 1.0),
		handler.LinkCheck,
	)

	wsproxy.RegisterRoutes(r)
	// Admin console (塘主) is served under /admin from its own embedded bundle.
	// Registered before webapp so its explicit /admin routes take precedence;
	// webapp's NoRoute fallback already excludes /admin via reservedPrefixes.
	adminweb.RegisterRoutes(r)
	webapp.RegisterRoutes(r)

	return r
}
