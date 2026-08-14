package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
)

// isFileBasedPersonaAgent 判定 agent 是否使用「本地文件」承载身份/记忆
// （SOUL.md / IDENTITY.md / MEMORY.md 等），这类 agent 不能通过
// agent_profile_push 协议级同步——它们的 CLI 进程只在启动时加载一次文件。
// 解决方式：以一条带外用户消息投递给 agent，由其调用本地工具自更新文件。
//
// 目前仅 openclaw / hermes 走这条路径。其它 agent（claude/codex/ACP 等）
// 通过 connector 注入到 prompt/系统提示词，由 connector 端的 push 通道处理。
func isFileBasedPersonaAgent(clientType string) bool {
	switch model.NormalizeAgentClientType(clientType) {
	case model.AgentClientTypeOpenClaw, model.AgentClientTypeHermes:
		return true
	}
	return false
}

// notifyFileBasedAgentProfileChange 给文件型 agent 投递一条带外指令消息，
// 内容是「请用本地工具更新身份文件并保持静默」。消息不入库（直接走
// PushDelegateEvent + 伪 MsgID），prompt 显式要求 agent 不要回复用户，
// 避免在前端出现凭空冒出的对话。agent 不在线时静默跳过。
//
// 调用方需在 DB 更新完成后传入旧值（用于在 prompt 中展示 diff），
// agent 用 AgentClientType/OwnerID/ID 等不变字段，可直接传更新前的快照。
func notifyFileBasedAgentProfileChange(agent *model.Agent, oldName, newName, oldIntro, newIntro string) {
	if agent == nil {
		return
	}
	if !isFileBasedPersonaAgent(agent.AgentClientType) {
		return
	}
	nameTrimmedOld := strings.TrimSpace(oldName)
	nameTrimmedNew := strings.TrimSpace(newName)
	introTrimmedOld := strings.TrimSpace(oldIntro)
	introTrimmedNew := strings.TrimSpace(newIntro)
	if nameTrimmedOld == nameTrimmedNew && introTrimmedOld == introTrimmedNew {
		return
	}

	// 用固定 directKey suffix 复用「profile-update 专用」会话：
	// - 避免污染用户与 agent 的日常对话历史
	// - 同一 owner+agent 只有一个 thread，agent 端的 file-update 状态自然收敛
	// - SessionCreateForAgentBinding 不要求好友关系（agent 主人/共享者均可）
	sessionResp, err := SessionCreateForAgentBinding(agent.OwnerID, agent.ID, "system-profile-update", "")
	if err != nil || sessionResp == nil || strings.TrimSpace(sessionResp.SessionID) == "" {
		logger.L.Warnf("[agent_profile_self_update] resolve owner-agent session failed agent=%d owner=%d err=%v",
			agent.ID, agent.OwnerID, err)
		return
	}
	sessionID := strings.TrimSpace(sessionResp.SessionID)

	content := buildProfileUpdateInstruction(nameTrimmedOld, nameTrimmedNew, introTrimmedOld, introTrimmedNew)
	pseudoMsgID := snowflake.GenID()
	event := AgentDelegateEvent{
		EventID:     fmt.Sprintf("profile-update:%d:%d:%d", agent.OwnerID, agent.ID, pseudoMsgID),
		EventType:   "user_chat",
		AgentID:     agent.ID,
		OwnerID:     agent.OwnerID,
		SessionID:   sessionID,
		SessionType: model.SessionTypeDirect,
		MsgID:       pseudoMsgID,
		SenderID:    agent.OwnerID,
		MsgType:     1,
		Content:     content,
		CreatedAt:   time.Now().UnixMilli(),
	}
	if ok := pushDelegateAgentEvent(event); !ok {
		logger.L.Warnf("[agent_profile_self_update] push delegate failed agent=%d session=%s", agent.ID, sessionID)
		return
	}
	logger.L.Infof("[agent_profile_self_update] dispatched profile-update instruction agent=%d session=%s name_changed=%v intro_changed=%v",
		agent.ID, sessionID,
		nameTrimmedOld != nameTrimmedNew,
		introTrimmedOld != introTrimmedNew)
}

// buildProfileUpdateInstruction 拼装给 agent 看的自然语言指令。
// - 仅包含真正变化的字段（避免噪音）
// - 用 [system-profile-update] 标签包裹，便于 agent 识别
// - 明确禁止回复用户（消息不入库，agent 若回复会显得凭空冒出）
func buildProfileUpdateInstruction(oldName, newName, oldIntro, newIntro string) string {
	lines := []string{
		"[system-profile-update]",
		"你的 agent 资料已通过管理后台更新。请使用本地工具（如 Read/Edit/Write）静默更新本工作区的身份/记忆文件（典型路径：SOUL.md / IDENTITY.md / MEMORY.md）：",
	}
	if oldName != newName {
		lines = append(lines, fmt.Sprintf("- 名字：旧值=%q → 新值=%q", oldName, newName))
	}
	if oldIntro != newIntro {
		lines = append(lines, fmt.Sprintf("- 介绍：旧值=%q → 新值=%q", oldIntro, newIntro))
	}
	lines = append(lines,
		"",
		"严格要求：",
		"1. 仅更新身份/介绍相关字段，保留其它内容（人物设定、风格规则、记忆条目等）",
		"2. 不要向用户发送任何文本回复——如果必须给上游返回结果，只能返回固定命令 /no_reply，不要解释“选择沉默”或“无需回复”",
		"3. 只做文件读写工具调用即可，工具调用结束就停止",
		"[/system-profile-update]",
	)
	return strings.Join(lines, "\n")
}
