package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/inboxseq"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/textutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/gorm"
)

const (
	eggInstallStepPendingMainAgentChat = "pending_main_agent_chat"
	eggInstallStepChatReady            = "chat_ready"
	eggInstallErrorDispatchFailed      = "main_agent_dispatch_failed"
	eggInstallSeedSummaryMaxRunes      = 60
	eggInstallSuggestedNameMaxRunes    = 32
	eggInstallLocalAdminTool           = "grix_admin"

	eggInstallRouteCreateNew = "create_new"
	eggInstallRouteExisting  = "existing"
)

type eggInstallTargetSnapshot struct {
	ID              int64
	AgentName       string
	AgentClientType string
}

type eggInstallResolvedRoute struct {
	ID               string
	TargetClientType string
	ArtifactPackage  string
}

type eggInstallVisibleMessagePersisted struct {
	MsgID    int64
	InboxSeq int64
}

func startEggInstallViaMainAgent(userID int64, req EggInstallReq, egg model.Egg, version model.EggVersion) (*EggInstallAcceptResp, *errcode.ErrCode) {
	installMode := normalizeEggInstallMode(req.InstallMode)
	if installMode == "" {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "install_mode 不支持"}
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "idempotency_key 不能为空"}
	}
	if installMode == eggInstallModeExistingAgent && (req.TargetAgentID == nil || *req.TargetAgentID <= 0) {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "existing_agent 模式必须提供 target_agent_id"}
	}

	var existing model.EggInstall
	existingErr := store.DB.Where("user_id = ? AND idempotency_key = ?", userID, idempotencyKey).First(&existing).Error
	if existingErr == nil {
		return buildEggInstallAcceptResp(existing), nil
	}
	if existingErr != nil && !errors.Is(existingErr, gorm.ErrRecordNotFound) {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询安装任务失败"}
	}

	resolvedTargetID, ec := prepareEggInstallTarget(userID, installMode, req.TargetAgentID, version)
	if ec != nil {
		return nil, ec
	}
	targetSnapshot, ec := loadEggInstallTargetSnapshot(userID, resolvedTargetID)
	if ec != nil {
		return nil, ec
	}

	// 按路线决定执行者：
	// 技能包路（claude/codex/gemini/qwen 等 CLI 类）的技能要落到目标 agent 本机的技能目录，
	// 主 OpenClaw/Hermes agent 跨机器写不进去，因此让目标 agent 自己当执行者，自装技能包；
	// 人格包路（openclaw/hermes 或新建）仍由具备创建权限的在线主 agent 执行。
	route := resolveEggInstallRoute(installMode, targetSnapshot)
	var executor model.Agent
	if route.ArtifactPackage == model.EggPackageTypeSkillZip {
		selfExecutor, ec := loadEggInstallSelfExecutorAgent(userID, resolvedTargetID)
		if ec != nil {
			return nil, ec
		}
		executor = *selfExecutor
	} else {
		resolvedExecutor, candidates, ec := resolveEggInstallExecutorAgent(userID, req.ExecutorAgentID, resolvedTargetID)
		if ec != nil {
			if candidates != nil {
				return &EggInstallAcceptResp{
					Status:     "choose_executor",
					Candidates: candidates,
				}, nil
			}
			return nil, ec
		}
		executor = resolvedExecutor
	}

	sessionResp, err := SessionCreate(userID, executor.ID, 2)
	if err != nil {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "创建主 agent 私聊失败"}
	}
	sessionID := strings.TrimSpace(sessionResp.SessionID)
	if sessionID == "" {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "创建主 agent 私聊失败"}
	}

	installID := fmt.Sprintf("eggins_%d", snowflake.GenID())
	install := model.EggInstall{
		InstallID:       installID,
		UserID:          userID,
		EggID:           egg.ID,
		Version:         version.Version,
		Status:          model.EggInstallStatusPending,
		Step:            eggInstallStepPendingMainAgentChat,
		ExecutorAgentID: &executor.ID,
		TargetAgentID:   resolvedTargetID,
		SessionID:       sessionID,
		ErrorCode:       "",
		ErrorMsg:        "",
		IdempotencyKey:  idempotencyKey,
		CounterApplied:  false,
	}

	visibleLocale := resolveEggRequestLocale(userID, req.Locale)
	visibleLang := normalizePreferredLanguage(visibleLocale)
	eggDisplayName := resolveEggInstallDisplayName(userID, visibleLocale, egg)
	visibleContent := buildEggInstallVisibleRequestMessage(eggDisplayName, installMode, targetSnapshot, visibleLang)
	seedContent := buildEggInstallSeedMessage(install.InstallID, egg, eggDisplayName, version, installMode, targetSnapshot, userID, visibleLang)
	seedCreatedAt := time.Now().UTC()
	visiblePersisted := eggInstallVisibleMessagePersisted{}
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&install).Error; err != nil {
			return err
		}
		var createErr error
		visiblePersisted, createErr = createEggInstallVisibleMessageTx(tx, userID, sessionID, visibleContent, seedCreatedAt)
		return createErr
	}); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			var hit model.EggInstall
			if qErr := store.DB.Where("user_id = ? AND idempotency_key = ?", userID, idempotencyKey).First(&hit).Error; qErr == nil {
				return buildEggInstallAcceptResp(hit), nil
			}
		}
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "创建安装会话失败"}
	}

	pushEggInstallVisibleMessageRealtime(
		userID,
		sessionID,
		visiblePersisted,
		visibleContent,
		seedCreatedAt,
	)

	event := AgentDelegateEvent{
		EventID:     fmt.Sprintf("%s:%d:%d:%d", sessionID, userID, executor.ID, visiblePersisted.MsgID),
		EventType:   "user_chat",
		AgentID:     executor.ID,
		OwnerID:     userID,
		SessionID:   sessionID,
		SessionType: model.SessionTypeDirect,
		MsgID:       visiblePersisted.MsgID,
		SenderID:    userID,
		MsgType:     1,
		Content:     seedContent,
		CreatedAt:   seedCreatedAt.UnixMilli(),
	}
	if ok := pushDelegateAgentEvent(event); !ok {
		markEggInstallDispatchFailed(install.InstallID)
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "主 agent 安装消息派发失败"}
	}

	setEggInstallChatReady(install.InstallID)
	install.Status = model.EggInstallStatusRunning
	install.Step = eggInstallStepChatReady
	return buildEggInstallAcceptResp(install), nil
}

func prepareEggInstallTarget(userID int64, installMode string, targetAgentID *int64, version model.EggVersion) (*int64, *errcode.ErrCode) {
	switch installMode {
	case eggInstallModeCreateNew:
		capabilities := buildEggInstallCapabilities(version)
		if !capabilities.CanCreateAgent {
			return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "当前 egg 不支持新建 agent 安装"}
		}
		return nil, nil
	case eggInstallModeExistingAgent:
		if targetAgentID == nil || *targetAgentID <= 0 {
			return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "target_agent_id 无效"}
		}
		if ec := verifyEggInstallTargetAgent(userID, *targetAgentID, version); ec != nil {
			return nil, ec
		}
		resolved := *targetAgentID
		return &resolved, nil
	default:
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "install_mode 不支持"}
	}
}

func verifyEggInstallTargetAgent(userID, targetAgentID int64, version model.EggVersion) *errcode.ErrCode {
	var agent model.Agent
	if err := store.DB.First(&agent, "id = ?", targetAgentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "目标 agent 不存在"}
		}
		return &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询目标 agent 失败"}
	}
	if agent.OwnerID != userID {
		return &errcode.ErrCode{HTTPStatus: 403, BizCode: 10002, Msg: "目标 agent 不属于当前用户"}
	}
	if agent.ProviderType != model.AgentProviderAPI || agent.Status != model.AgentStatusActive {
		return &errcode.ErrCode{HTTPStatus: 403, BizCode: 10002, Msg: "目标 agent 不是可用的 API Agent"}
	}
	if !supportsEggInstallClient(version, agent.AgentClientType) {
		return &errcode.ErrCode{HTTPStatus: 403, BizCode: 10002, Msg: "目标 agent 类型不匹配"}
	}
	return nil
}

func loadEggInstallTargetSnapshot(userID int64, targetAgentID *int64) (*eggInstallTargetSnapshot, *errcode.ErrCode) {
	if targetAgentID == nil || *targetAgentID <= 0 {
		return nil, nil
	}

	var agent model.Agent
	if err := store.DB.Select("id", "owner_id", "agent_name", "agent_client_type").
		Where("id = ?", *targetAgentID).
		First(&agent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "目标 agent 不存在"}
		}
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询目标 agent 失败"}
	}
	if agent.OwnerID != userID {
		return nil, &errcode.ErrCode{HTTPStatus: 403, BizCode: 10002, Msg: "目标 agent 不属于当前用户"}
	}
	return &eggInstallTargetSnapshot{
		ID:              agent.ID,
		AgentName:       strings.TrimSpace(agent.AgentName),
		AgentClientType: strings.TrimSpace(agent.AgentClientType),
	}, nil
}

// loadEggInstallSelfExecutorAgent 用于技能包路：目标 agent 自己安装技能包，执行者即目标自身。
// 目标在 prepareEggInstallTarget 已校验过归属/类型/可用，这里只需补在线判断。
func loadEggInstallSelfExecutorAgent(userID int64, targetAgentID *int64) (*model.Agent, *errcode.ErrCode) {
	if targetAgentID == nil || *targetAgentID <= 0 {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "技能安装必须提供目标 agent"}
	}
	var agent model.Agent
	if err := store.DB.First(&agent, "id = ?", *targetAgentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "目标 agent 不存在"}
		}
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询目标 agent 失败"}
	}
	if agent.OwnerID != userID {
		return nil, &errcode.ErrCode{HTTPStatus: 403, BizCode: 10002, Msg: "目标 agent 不属于当前用户"}
	}
	if !isAgentChannelAvailable(agent.ID) {
		return nil, &errcode.ErrCode{HTTPStatus: 409, BizCode: 10005, Msg: "目标 agent 当前不在线"}
	}
	return &agent, nil
}

func resolveEggInstallExecutorAgent(userID int64, preferredExecutorID *int64, targetAgentID *int64) (model.Agent, []EggInstallCandidateAgent, *errcode.ErrCode) {
	// If the caller specified an executor, validate and use it directly.
	if preferredExecutorID != nil && *preferredExecutorID > 0 {
		var agent model.Agent
		if err := store.DB.
			Where("id = ? AND owner_id = ? AND provider_type = ? AND status = ?",
				*preferredExecutorID, userID, model.AgentProviderAPI, model.AgentStatusActive).
			First(&agent).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return model.Agent{}, nil, &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "指定的 executor agent 不存在或不属于当前用户"}
			}
			return model.Agent{}, nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询指定 executor agent 失败"}
		}
		if !isAgentChannelAvailable(agent.ID) {
			return model.Agent{}, nil, &errcode.ErrCode{HTTPStatus: 409, BizCode: 10005, Msg: "指定的 executor agent 当前不在线"}
		}
		var scopeCount int64
		store.DB.Model(&model.AgentAPIScope{}).
			Where("agent_id = ? AND scope = ?", agent.ID, agentscope.ScopeAgentAPICreate).
			Count(&scopeCount)
		if scopeCount == 0 {
			return model.Agent{}, nil, &errcode.ErrCode{HTTPStatus: 403, BizCode: 10002, Msg: "指定的 executor agent 不具备创建 Agent 权限"}
		}
		return agent, nil, nil
	}

	// Auto-resolve: find all active API agents for this user.
	var agents []model.Agent
	if err := store.DB.
		Where("owner_id = ? AND provider_type = ? AND status = ?", userID, model.AgentProviderAPI, model.AgentStatusActive).
		Order("updated_at DESC").
		Order("id DESC").
		Find(&agents).Error; err != nil {
		return model.Agent{}, nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询主 Agent 失败"}
	}

	// Filter for agents that have the agent.api.create scope.
	var candidates []model.Agent
	for _, agent := range agents {
		if !isAgentChannelAvailable(agent.ID) {
			continue
		}
		var count int64
		store.DB.Model(&model.AgentAPIScope{}).
			Where("agent_id = ? AND scope = ?", agent.ID, agentscope.ScopeAgentAPICreate).
			Count(&count)
		if count > 0 {
			candidates = append(candidates, agent)
		}
	}

	// 目标 agent 自己就具备执行条件时优先用它：用户在界面上选的是目标，
	// 让别的 agent 代劳会让用户以为选错了对象。
	if targetAgentID != nil && *targetAgentID > 0 {
		for _, agent := range candidates {
			if agent.ID == *targetAgentID {
				return agent, nil, nil
			}
		}
		// 目标在线却没进候选，只能是缺创建权限（归属、类型、状态在
		// verifyEggInstallTargetAgent 已校验过）。这时静默改派别的 agent，
		// 用户看到的还是"我选的它没动、别人在动"，所以直接把配置问题摆出来。
		if isAgentChannelAvailable(*targetAgentID) {
			return model.Agent{}, nil, &errcode.ErrCode{HTTPStatus: 403, BizCode: 10002, Msg: "目标 agent 缺少创建 Agent 权限，请先为它授权 agent.api.create"}
		}
	}

	switch len(candidates) {
	case 0:
		return model.Agent{}, nil, &errcode.ErrCode{HTTPStatus: 409, BizCode: 10005, Msg: "当前没有具备创建 Agent 权限的在线主 Agent"}
	default:
		items := make([]EggInstallCandidateAgent, 0, len(candidates))
		for _, a := range candidates {
			clientType := a.AgentClientType
			if clientType == "" {
				clientType = getAgentClientType(a.ID)
			}
			items = append(items, EggInstallCandidateAgent{
				AgentID:         fmt.Sprintf("%d", a.ID),
				AgentName:       strings.TrimSpace(a.AgentName),
				AgentClientType: clientType,
			})
		}
		return model.Agent{}, items, &errcode.ErrCode{HTTPStatus: 409, BizCode: 10006, Msg: "找到具备创建 Agent 权限的在线主 Agent，请确认"}
	}
}

func buildEggInstallAcceptResp(install model.EggInstall) *EggInstallAcceptResp {
	resp := &EggInstallAcceptResp{
		InstallID: install.InstallID,
		Status:    install.Status,
		SessionID: strings.TrimSpace(install.SessionID),
	}
	if install.ExecutorAgentID != nil && *install.ExecutorAgentID > 0 {
		resp.ExecutorAgentID = fmt.Sprintf("%d", *install.ExecutorAgentID)
	}
	return resp
}

func resolveArtifactPackage(clientType string) string {
	if model.IsProprietaryAgentClientType(model.NormalizeAgentClientType(clientType)) {
		return model.EggPackageTypeSkillZip
	}
	return model.EggPackageTypePersonaZip
}

func resolveEggInstallRoute(
	installMode string,
	target *eggInstallTargetSnapshot,
) eggInstallResolvedRoute {
	switch {
	case installMode == eggInstallModeCreateNew:
		return eggInstallResolvedRoute{
			ID:              eggInstallRouteCreateNew,
			ArtifactPackage: model.EggPackageTypePersonaZip,
		}
	default:
		targetClientType := ""
		if target != nil {
			targetClientType = model.NormalizeAgentClientType(target.AgentClientType)
		}
		return eggInstallResolvedRoute{
			ID:               eggInstallRouteExisting,
			TargetClientType: targetClientType,
			ArtifactPackage:  resolveArtifactPackage(targetClientType),
		}
	}
}

func buildEggInstallSeedMessage(
	installID string,
	egg model.Egg,
	eggDisplayName string,
	version model.EggVersion,
	installMode string,
	target *eggInstallTargetSnapshot,
	userID int64,
	lang string,
) string {
	route := resolveEggInstallRoute(installMode, target)

	var packageURL string
	switch route.ArtifactPackage {
	case model.EggPackageTypeSkillZip:
		packageURL = strings.TrimSpace(version.SkillZipURL)
	default:
		packageURL = strings.TrimSpace(version.PersonaZipURL)
	}

	var sb strings.Builder

	// 技能包路：消息直接发给目标 agent 自身，由它把技能包装进目标客户端可扫描的技能目录、自检。
	if route.ArtifactPackage == model.EggPackageTypeSkillZip {
		skillDir := resolveSkillDirHint(target)
		skillScope := resolveSkillInstallScope(target)
		skillScopeEN := "project-level"
		skillScopeZH := "项目级"
		if skillScope == "user" {
			skillScopeEN = "user-level"
			skillScopeZH = "用户级"
		}
		isEnglish := normalizePreferredLanguage(lang) == preferredLanguageEN
		if isEnglish {
			sb.WriteString(fmt.Sprintf("Do not repeat this message to the user. Install the skill package below as a %s skill for the current workspace.\n\n", skillScopeEN))
			sb.WriteString("Steps:\n")
			sb.WriteString("1. Download the zip file directly from the download_url below — do not search or use any other method\n")
			if skillDir != "" {
				sb.WriteString(fmt.Sprintf("2. Extract to the %s skill root: %s\n", skillScopeEN, skillDir))
			} else {
				sb.WriteString(fmt.Sprintf("2. Extract to the %s skill root for your current agent/client\n", skillScopeEN))
			}
			sb.WriteString("3. Ensure the final layout is <skill_root>/<skill_directory>/SKILL.md. Use an uppercase SKILL.md filename; if the package contains skill.md, rename it to SKILL.md. Do not leave an extra nested wrapper directory.\n")
			sb.WriteString("4. Set <skill_directory> to the exact skill name from SKILL.md frontmatter after trimming whitespace. Spaces in the skill name are allowed; quote paths in shell commands.\n")
			sb.WriteString(fmt.Sprintf("5. After installation, run a self-check to confirm the skill is discoverable from that %s directory\n", skillScopeEN))
			sb.WriteString("6. Reply to the user with the installation result\n\n")
		} else {
			sb.WriteString(fmt.Sprintf("不要复述，不要把原始上下文贴给用户。请把下面的技能包安装为当前工作区的%s技能。\n\n", skillScopeZH))
			sb.WriteString("操作步骤：\n")
			sb.WriteString("1. 直接从下面的 download_url 下载技能包 zip 文件，不要搜索，不要用其他方式获取\n")
			if skillDir != "" {
				sb.WriteString(fmt.Sprintf("2. 解压到%s技能根目录：%s\n", skillScopeZH, skillDir))
			} else {
				sb.WriteString(fmt.Sprintf("2. 解压到当前 agent/client 对应的%s技能根目录\n", skillScopeZH))
			}
			sb.WriteString("3. 确保最终目录结构是 <skill_root>/<skill_directory>/SKILL.md。文件名必须是大写 SKILL.md；如果压缩包里是 skill.md，安装时重命名为 SKILL.md。不要多留一层无效外壳目录。\n")
			sb.WriteString("4. <skill_directory> 必须等于 SKILL.md frontmatter 里的技能 name，去掉首尾空白即可。技能名里有空格是允许的，shell 命令里要给路径加引号。\n")
			sb.WriteString(fmt.Sprintf("5. 安装完成后做一次自检，确认技能能从该%s目录被扫描到\n", skillScopeZH))
			sb.WriteString("6. 向用户回复安装结果\n\n")
		}
		sb.WriteString(fmt.Sprintf("install_scope: %s\n", skillScope))
		if skillDir != "" {
			sb.WriteString(fmt.Sprintf("skill_root_hint: %s\n", skillDir))
		}
		sb.WriteString("skill_layout: <skill_root>/<skill_directory>/SKILL.md\n")
		sb.WriteString("skill_directory_rule: equals SKILL.md frontmatter name\n")
		sb.WriteString(fmt.Sprintf("egg_id: %s\n", strings.TrimSpace(egg.ID)))
		sb.WriteString(fmt.Sprintf("egg_name: %s\n", eggDisplayName))
		sb.WriteString(fmt.Sprintf("download_url: %s\n", packageURL))
		sb.WriteString(fmt.Sprintf("install_id: %s\n", installID))
		if target != nil && target.ID > 0 {
			sb.WriteString(fmt.Sprintf("grix_agent_id: %d\n", target.ID))
		}
		return sb.String()
	}

	// 人格包路：由主 OpenClaw/Hermes agent 执行孵化或人格安装。
	if installMode == eggInstallModeCreateNew {
		sb.WriteString("不要复述，不要把原始上下文贴给用户。请使用 grix-egg 技能为用户孵化一个新的智能体。\n\n")
	} else {
		sb.WriteString("不要复述，不要把原始上下文贴给用户。请使用 grix-egg 技能将人格安装到目标 agent。\n\n")
	}

	sb.WriteString(fmt.Sprintf("人格包: %s\n", packageURL))
	sb.WriteString(fmt.Sprintf("install_id: %s\n", installID))

	if target != nil && target.ID > 0 {
		sb.WriteString(fmt.Sprintf("grix agent id: %d\n", target.ID))
		if resp, ec := AgentRotateAPIKey(userID, target.ID); ec == nil && resp != nil {
			sb.WriteString(fmt.Sprintf("grix wsurl or endpoint: %s\n", resp.APIEndpoint))
			sb.WriteString(fmt.Sprintf("grix api key: %s\n", resp.APIKey))
		}
	} else if installMode == eggInstallModeCreateNew {
		sb.WriteString(fmt.Sprintf("agent名字: %s\n", eggDisplayName))
		suggested := buildEggInstallSuggestedName(eggDisplayName, egg.ID)
		if suggested != "" {
			sb.WriteString(fmt.Sprintf("新agent名字: %s\n", suggested))
		}
	}
	sb.WriteString("\n完成或失败后必须单独发送一条状态卡消息，消息内容只能包含单行 grix://card/egg_install_status 链接，不能混入其它文字。\n")
	sb.WriteString("状态卡里的 install_id 必须使用上面的 install_id 原值；target_agent_id 必须使用实际数字 ID，不要原样填写占位符。\n")
	if installMode == eggInstallModeCreateNew {
		sb.WriteString("成功格式: grix://card/egg_install_status?status=success&install_id=<install_id>&target_agent_id=<新建agent id>&summary=<URL编码的一句话结果>\n")
	} else {
		sb.WriteString("成功格式: grix://card/egg_install_status?status=success&install_id=<install_id>&target_agent_id=<grix agent id>&summary=<URL编码的一句话结果>\n")
	}
	sb.WriteString("失败格式: grix://card/egg_install_status?status=failed&install_id=<install_id>&error_msg=<URL编码的失败原因>\n")

	return sb.String()
}

func resolveEggInstallDisplayName(userID int64, reqLocale string, egg model.Egg) string {
	locale := resolveEggRequestLocale(userID, reqLocale)
	localeChain := buildEggLocaleChain(locale)
	if text, _, err := pickEggI18nByLocale(egg.ID, localeChain); err == nil {
		if name := strings.TrimSpace(text.Name); name != "" {
			return name
		}
	}
	if fallback := humanizeEggInstallIdentifier(egg.ID); fallback != "" {
		return fallback
	}
	return strings.TrimSpace(egg.ID)
}

func buildEggInstallVisibleRequestMessage(
	eggDisplayName string,
	installMode string,
	target *eggInstallTargetSnapshot,
	lang string,
) string {
	name := strings.TrimSpace(eggDisplayName)
	if name == "" {
		if normalizePreferredLanguage(lang) == preferredLanguageEN {
			name = "Unnamed Egg"
		} else {
			name = "未命名虾蛋"
		}
	}

	isEnglish := normalizePreferredLanguage(lang) == preferredLanguageEN
	switch installMode {
	case eggInstallModeCreateNew:
		if isEnglish {
			return fmt.Sprintf("Request to incubate Grix egg \"%s\".", name)
		}
		return fmt.Sprintf("请求孵化Grix虾蛋《%s》", name)
	default:
		if targetName := strings.TrimSpace(targetNameOrEmpty(target)); targetName != "" {
			if isEnglish {
				return fmt.Sprintf("Request to install Grix egg \"%s\" to agent \"%s\".", name, targetName)
			}
			return fmt.Sprintf("请求安装Grix虾蛋《%s》到 Agent「%s」。", name, targetName)
		}
		if isEnglish {
			return fmt.Sprintf("Request to install Grix egg \"%s\".", name)
		}
		return fmt.Sprintf("请求安装Grix虾蛋《%s》。", name)
	}
}

func targetNameOrEmpty(target *eggInstallTargetSnapshot) string {
	if target == nil {
		return ""
	}
	return target.AgentName
}

func buildEggInstallSuggestedName(eggDisplayName, eggID string) string {
	candidates := []string{
		strings.TrimSpace(eggDisplayName),
		humanizeEggInstallIdentifier(eggID),
		strings.TrimSpace(eggID),
	}
	for _, candidate := range candidates {
		normalized := normalizeEggInstallSuggestedNameCandidate(candidate)
		if normalized != "" {
			return normalized
		}
	}
	return ""
}

func normalizeEggInstallSuggestedNameCandidate(raw string) string {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return ""
	}
	candidate = strings.Trim(candidate, "._- ")
	if candidate == "" {
		return ""
	}
	candidate = strings.TrimSpace(textutil.TruncateRunes(candidate, eggInstallSuggestedNameMaxRunes))
	if candidate == "" {
		return ""
	}
	normalized, ec := normalizeAgentName(candidate)
	if ec != nil {
		return ""
	}
	return normalized
}

func humanizeEggInstallIdentifier(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	for _, separator := range []string{"/", "."} {
		if idx := strings.LastIndex(trimmed, separator); idx >= 0 && idx+1 < len(trimmed) {
			trimmed = trimmed[idx+1:]
		}
	}
	trimmed = strings.Trim(trimmed, "._- ")
	trimmed = trimEggInstallNumericSuffix(trimmed)
	replacer := strings.NewReplacer("_", " ", "-", " ")
	trimmed = strings.Join(strings.Fields(replacer.Replace(trimmed)), " ")
	return strings.TrimSpace(trimmed)
}

func trimEggInstallNumericSuffix(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	cut := len(trimmed)
	for cut > 0 {
		r := trimmed[cut-1]
		if r < '0' || r > '9' {
			break
		}
		cut--
	}
	if cut == len(trimmed) {
		return trimmed
	}
	if len(trimmed)-cut < 6 {
		return trimmed
	}
	base := strings.TrimRight(trimmed[:cut], "_-. ")
	if base == "" {
		return trimmed
	}
	return base
}

func createEggInstallVisibleMessageTx(tx *gorm.DB, userID int64, sessionID, content string, createdAt time.Time) (eggInstallVisibleMessagePersisted, error) {
	if tx == nil {
		return eggInstallVisibleMessagePersisted{}, errors.New("install transaction unavailable")
	}
	if userID <= 0 || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(content) == "" {
		return eggInstallVisibleMessagePersisted{}, errors.New("invalid egg install visible message")
	}

	msgID := snowflake.GenID()
	if err := tx.Create(&model.Message{
		MsgID:      msgID,
		SessionID:  sessionID,
		SenderID:   userID,
		SenderType: 1,
		MsgType:    1,
		Content:    content,
		CreatedAt:  createdAt,
	}).Error; err != nil {
		return eggInstallVisibleMessagePersisted{}, err
	}

	summary := textutil.TruncateRunes(content, eggInstallSeedSummaryMaxRunes)
	if err := tx.Model(&model.Session{}).
		Where("session_id = ?", sessionID).
		Updates(map[string]any{
			"last_msg_id":      msgID,
			"last_msg_summary": summary,
			"updated_at":       createdAt,
		}).Error; err != nil {
		return eggInstallVisibleMessagePersisted{}, err
	}

	nextSeq, err := inboxseq.NextTx(context.Background(), tx, userID)
	if err != nil {
		return eggInstallVisibleMessagePersisted{}, err
	}
	if err := tx.Create(&model.UserInbox{
		UserID:    userID,
		InboxSeq:  nextSeq,
		MsgID:     msgID,
		SessionID: sessionID,
		EventKind: model.UserInboxEventKindMessage,
		CreatedAt: createdAt,
	}).Error; err != nil {
		return eggInstallVisibleMessagePersisted{}, err
	}

	if err := tx.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, userID).
		Updates(map[string]any{
			"last_active_at":   createdAt,
			"last_read_msg_id": gorm.Expr("CASE WHEN last_read_msg_id < ? THEN ? ELSE last_read_msg_id END", msgID, msgID),
			"unread_count":     0,
		}).Error; err != nil {
		return eggInstallVisibleMessagePersisted{}, err
	}

	return eggInstallVisibleMessagePersisted{
		MsgID:    msgID,
		InboxSeq: nextSeq,
	}, nil
}

func pushEggInstallVisibleMessageRealtime(
	userID int64,
	sessionID string,
	persisted eggInstallVisibleMessagePersisted,
	content string,
	createdAt time.Time,
) {
	if userID <= 0 || strings.TrimSpace(sessionID) == "" || persisted.MsgID <= 0 || persisted.InboxSeq <= 0 {
		return
	}

	pushRealtimeEvent(userID, protocol.CmdPushMsg, protocol.PushMsgPayload{
		InboxSeq:    persisted.InboxSeq,
		MsgID:       persisted.MsgID,
		SessionID:   sessionID,
		SessionType: model.SessionTypeDirect,
		SenderID:    userID,
		SenderType:  1,
		MsgType:     1,
		Content:     content,
		CreatedAt:   createdAt.UTC().UnixMilli(),
	})
}

func setEggInstallChatReady(installID string) {
	_ = store.DB.Model(&model.EggInstall{}).
		Where("install_id = ?", installID).
		Updates(map[string]any{
			"status":     model.EggInstallStatusRunning,
			"step":       eggInstallStepChatReady,
			"error_code": "",
			"error_msg":  "",
			"updated_at": time.Now().UTC(),
		}).Error
}

func resolveSkillDirHint(target *eggInstallTargetSnapshot) string {
	if target != nil {
		switch model.NormalizeAgentClientType(target.AgentClientType) {
		case model.AgentClientTypeClaude:
			return "<project>/.claude/skills/"
		case model.AgentClientTypeCodex:
			return "<project>/.codex/skills/"
		case model.AgentClientTypeGemini:
			return "<project>/.gemini/skills/"
		case model.AgentClientTypeQwen:
			return "<project>/.qwen/skills/"
		case model.AgentClientTypeKiro:
			return "<project>/.kiro/skills/"
		case model.AgentClientTypeKimi:
			return "~/.kimi-code/skills/"
		case model.AgentClientTypeDeepSeek:
			return "<project>/.agents/skills/"
		}
	}
	return ""
}

func resolveSkillInstallScope(target *eggInstallTargetSnapshot) string {
	if target != nil && model.NormalizeAgentClientType(target.AgentClientType) == model.AgentClientTypeKimi {
		return "user"
	}
	return "project"
}

func markEggInstallDispatchFailed(installID string) {
	_ = store.DB.Model(&model.EggInstall{}).
		Where("install_id = ?", installID).
		Updates(map[string]any{
			"status":     model.EggInstallStatusFailed,
			"step":       model.EggInstallStatusFailed,
			"error_code": eggInstallErrorDispatchFailed,
			"error_msg":  "主 agent 安装消息派发失败",
			"updated_at": time.Now().UTC(),
		}).Error
}
