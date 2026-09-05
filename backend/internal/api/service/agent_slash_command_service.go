package service

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/askie/grix/backend/internal/agentslashcmd"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

// agent 自定义斜杠命令：主人在命令面板里自己加的命令，与 internal/agentslashcmd 的
// 内置命令合并后随工具栏快照下发。读接口对「能用这个 agent 的人」开放（含被共享者），
// 写接口只有主人可用，与 AgentUpdate 同门槛。

const (
	// agentSlashCommandMaxPerAgent 单个 agent 的自定义命令条数上限。
	agentSlashCommandMaxPerAgent = 50
	// agentSlashCommandDescMaxLen 对应 migration 124 中 description VARCHAR(200)。
	agentSlashCommandDescMaxLen = 200
	// agentSlashCommandRefreshReason 增删后重建工具栏快照的原因标记。
	agentSlashCommandRefreshReason = "slash_command_update"
)

// agentSlashCommandNamePattern 命令名：前导斜杠 + 小写字母/数字开头，其后可跟
// 小写字母、数字、下划线、冒号、连字符，总长（不含斜杠）1-32。
var agentSlashCommandNamePattern = regexp.MustCompile(`^/[a-z0-9][a-z0-9_:-]{0,31}$`)

type AgentSlashCommandCreateReq struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// loadOwnedAgentForSlashCommand 加载 agent 并校验请求者是主人（写接口门槛）。
func loadOwnedAgentForSlashCommand(userID, agentID int64) (*model.Agent, *errcode.ErrCode) {
	if agentID <= 0 || userID <= 0 {
		return nil, &errcode.ErrBadRequest
	}
	var agent model.Agent
	if err := store.DB.Select("id", "owner_id", "agent_client_type").First(&agent, agentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &errcode.ErrAgentNotFound
		}
		return nil, &errcode.ErrInternal
	}
	if agent.OwnerID != userID {
		return nil, &errcode.ErrAgentForbidden
	}
	return &agent, nil
}

// AgentSlashCommandList 列出该 agent 的自定义命令，按创建顺序返回
// （与工具栏快照里的追加顺序一致）。可用该 agent 的人都能读。
func AgentSlashCommandList(userID, agentID int64) ([]model.AgentSlashCommand, *errcode.ErrCode) {
	if agentID <= 0 || userID <= 0 {
		return nil, &errcode.ErrBadRequest
	}
	ok, err := CanUseAgent(userID, agentID)
	if err != nil {
		return nil, &errcode.ErrInternal
	}
	if !ok {
		return nil, &errcode.ErrAgentForbidden
	}
	return listAgentSlashCommands(agentID)
}

func listAgentSlashCommands(agentID int64) ([]model.AgentSlashCommand, *errcode.ErrCode) {
	commands := make([]model.AgentSlashCommand, 0, 8)
	if err := store.DB.Where("agent_id = ?", agentID).
		Order("created_at ASC, id ASC").
		Limit(agentSlashCommandMaxPerAgent).
		Find(&commands).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	return commands, nil
}

// AgentSlashCommandCreate 主人为自己的 agent 新增一条自定义命令。
func AgentSlashCommandCreate(userID, agentID int64, req AgentSlashCommandCreateReq) (*model.AgentSlashCommand, *errcode.ErrCode) {
	agent, ec := loadOwnedAgentForSlashCommand(userID, agentID)
	if ec != nil {
		return nil, ec
	}
	name := NormalizeAgentSlashCommandName(req.Name)
	if !agentSlashCommandNamePattern.MatchString(name) {
		return nil, &errcode.ErrSlashCommandNameInvalid
	}
	description := strings.TrimSpace(req.Description)
	if len([]rune(description)) > agentSlashCommandDescMaxLen {
		return nil, &errcode.ErrSlashCommandDescTooLong
	}
	// 与该 agent client_type 的内置命令重名：内置命令不落库，只能在这里拦。
	for _, builtin := range agentslashcmd.Commands(model.NormalizeAgentClientType(agent.AgentClientType)) {
		if strings.EqualFold(strings.TrimSpace(builtin.Name), name) {
			return nil, &errcode.ErrSlashCommandExists
		}
	}
	var count int64
	if err := store.DB.Model(&model.AgentSlashCommand{}).
		Where("agent_id = ?", agentID).Count(&count).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	if count >= agentSlashCommandMaxPerAgent {
		return nil, &errcode.ErrSlashCommandLimitExceed
	}

	command := &model.AgentSlashCommand{
		ID:          snowflake.GenID(),
		AgentID:     agentID,
		OwnerID:     agent.OwnerID,
		Name:        name,
		Description: description,
	}
	if err := store.DB.Create(command).Error; err != nil {
		// 唯一键 (agent_id, name) 冲突：并发下也只可能是同名。
		var existing model.AgentSlashCommand
		if lookupErr := store.DB.Where("agent_id = ? AND name = ?", agentID, name).
			First(&existing).Error; lookupErr == nil {
			return nil, &errcode.ErrSlashCommandExists
		}
		return nil, &errcode.ErrInternal
	}
	refreshAgentToolbar(agent.OwnerID, agentID, agentSlashCommandRefreshReason)
	return command, nil
}

// AgentSlashCommandDelete 主人删除自己 agent 的一条自定义命令。
func AgentSlashCommandDelete(userID, agentID, commandID int64) *errcode.ErrCode {
	agent, ec := loadOwnedAgentForSlashCommand(userID, agentID)
	if ec != nil {
		return ec
	}
	if commandID <= 0 {
		return &errcode.ErrBadRequest
	}
	result := store.DB.Where("id = ? AND agent_id = ?", commandID, agentID).
		Delete(&model.AgentSlashCommand{})
	if result.Error != nil {
		return &errcode.ErrInternal
	}
	if result.RowsAffected == 0 {
		return &errcode.ErrSlashCommandNotFound
	}
	refreshAgentToolbar(agent.OwnerID, agentID, agentSlashCommandRefreshReason)
	return nil
}

// AgentSlashCommandsForToolbar 供工具栏快照合并使用：按创建顺序返回该 agent 的自定义命令。
// 读失败时返回 nil，只丢掉自定义部分，不影响内置命令照常下发。
func AgentSlashCommandsForToolbar(ctx context.Context, agentID int64, clientType string) []agentslashcmd.SlashCommand {
	// 只有注册了内置命令的 client_type 才会有斜杠命令面板（agents/shared.BuildSlashCommandsItem），
	// 没有面板就没有合并目标；这里直接跳过查询，避免每次工具栏刷新都白跑一次 DB。
	if store.DB == nil || agentID <= 0 || len(agentslashcmd.Commands(clientType)) == 0 {
		return nil
	}
	var rows []model.AgentSlashCommand
	if err := store.DB.WithContext(ctx).
		Select("name", "description").
		Where("agent_id = ?", agentID).
		Order("created_at ASC, id ASC").
		Limit(agentSlashCommandMaxPerAgent).
		Find(&rows).Error; err != nil {
		return nil
	}
	commands := make([]agentslashcmd.SlashCommand, 0, len(rows))
	for _, row := range rows {
		commands = append(commands, agentslashcmd.SlashCommand{
			Name:        row.Name,
			Description: row.Description,
		})
	}
	return commands
}

// NormalizeAgentSlashCommandName 统一命令名写法：去空白 + 转小写。
// 入库前和查重前都走这里，保证「/Foo」与「/foo」是同一条命令。
func NormalizeAgentSlashCommandName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
