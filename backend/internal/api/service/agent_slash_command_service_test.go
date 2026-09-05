package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/askie/grix/backend/internal/agentslashcmd"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	slashCmdOwner    = int64(88001)
	slashCmdStranger = int64(88002)
	slashCmdAgent    = int64(96001)
)

type recordingToolbarRefresher struct {
	mu    sync.Mutex
	calls []string
}

func (r *recordingToolbarRefresher) RefreshByAgent(_ context.Context, ownerID, agentID int64, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, refreshCallKey(ownerID, agentID, reason))
	return nil
}

func (r *recordingToolbarRefresher) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func refreshCallKey(ownerID, agentID int64, reason string) string {
	return fmt.Sprintf("owner:%d agent:%d reason:%s", ownerID, agentID, reason)
}

func setupSlashCmdSvcTest(t *testing.T) *recordingToolbarRefresher {
	t.Helper()
	logger.Init()
	require.NoError(t, snowflake.Init(1))
	testDB := testutil.NewTestDB()
	origDB := store.DB
	store.DB = testDB.DB
	refresher := &recordingToolbarRefresher{}
	SetAgentToolbarRefresher(refresher)
	t.Cleanup(func() {
		SetAgentToolbarRefresher(nil)
		store.DB = origDB
		testDB.Close()
	})
	require.NoError(t, store.DB.Create(&model.Agent{
		ID:              slashCmdAgent,
		OwnerID:         slashCmdOwner,
		AgentClientType: model.AgentClientTypeClaude,
		Status:          model.AgentStatusActive,
	}).Error)
	return refresher
}

// 主人新增：入库、归一化为小写、并触发一次工具栏重建。
func TestAgentSlashCommandCreateAndRefresh(t *testing.T) {
	refresher := setupSlashCmdSvcTest(t)

	created, ec := AgentSlashCommandCreate(slashCmdOwner, slashCmdAgent, AgentSlashCommandCreateReq{
		Name:        "  /Deploy  ",
		Description: "  发布到预发环境  ",
	})
	require.Nil(t, ec)
	require.NotNil(t, created)
	assert.Equal(t, "/deploy", created.Name)
	assert.Equal(t, "发布到预发环境", created.Description)
	assert.Equal(t, slashCmdOwner, created.OwnerID)
	assert.Equal(t, []string{refreshCallKey(slashCmdOwner, slashCmdAgent, "slash_command_update")}, refresher.snapshot())

	items, ec := AgentSlashCommandList(slashCmdOwner, slashCmdAgent)
	require.Nil(t, ec)
	require.Len(t, items, 1)
	assert.Equal(t, "/deploy", items[0].Name)
}

// 非主人调写接口一律 403；读接口对可用者开放，陌生人同样 403。
func TestAgentSlashCommandWriteForbiddenForNonOwner(t *testing.T) {
	setupSlashCmdSvcTest(t)

	_, ec := AgentSlashCommandCreate(slashCmdStranger, slashCmdAgent, AgentSlashCommandCreateReq{Name: "/deploy"})
	require.NotNil(t, ec)
	assert.Equal(t, errcode.ErrAgentForbidden.BizCode, ec.BizCode)
	assert.Equal(t, 403, ec.HTTPStatus)

	created, ec := AgentSlashCommandCreate(slashCmdOwner, slashCmdAgent, AgentSlashCommandCreateReq{Name: "/deploy"})
	require.Nil(t, ec)

	ec = AgentSlashCommandDelete(slashCmdStranger, slashCmdAgent, created.ID)
	require.NotNil(t, ec)
	assert.Equal(t, errcode.ErrAgentForbidden.BizCode, ec.BizCode)
	assert.Equal(t, 403, ec.HTTPStatus)

	_, ec = AgentSlashCommandList(slashCmdStranger, slashCmdAgent)
	require.NotNil(t, ec)
	assert.Equal(t, errcode.ErrAgentForbidden.BizCode, ec.BizCode)
}

// 同名冲突：与已有自定义命令重名、与该 client_type 的内置命令重名，都是 409。
func TestAgentSlashCommandDuplicateConflicts(t *testing.T) {
	setupSlashCmdSvcTest(t)

	_, ec := AgentSlashCommandCreate(slashCmdOwner, slashCmdAgent, AgentSlashCommandCreateReq{Name: "/deploy"})
	require.Nil(t, ec)

	_, ec = AgentSlashCommandCreate(slashCmdOwner, slashCmdAgent, AgentSlashCommandCreateReq{Name: "/DEPLOY"})
	require.NotNil(t, ec)
	assert.Equal(t, errcode.ErrSlashCommandExists.BizCode, ec.BizCode)
	assert.Equal(t, 409, ec.HTTPStatus)

	builtin := agentslashcmd.Commands(model.AgentClientTypeClaude)
	require.NotEmpty(t, builtin)
	_, ec = AgentSlashCommandCreate(slashCmdOwner, slashCmdAgent, AgentSlashCommandCreateReq{Name: builtin[0].Name})
	require.NotNil(t, ec)
	assert.Equal(t, errcode.ErrSlashCommandExists.BizCode, ec.BizCode)
	assert.Equal(t, 409, ec.HTTPStatus)
}

// 格式非法 400：缺斜杠、含大写以外的非法字符、超长、说明超 200 字。
func TestAgentSlashCommandValidation(t *testing.T) {
	setupSlashCmdSvcTest(t)

	for _, name := range []string{"", "deploy", "/", "/-deploy", "/dep loy", "/deploy!", "/" + strings.Repeat("a", 33)} {
		_, ec := AgentSlashCommandCreate(slashCmdOwner, slashCmdAgent, AgentSlashCommandCreateReq{Name: name})
		require.NotNil(t, ec, "name=%q must be rejected", name)
		assert.Equal(t, errcode.ErrSlashCommandNameInvalid.BizCode, ec.BizCode, "name=%q", name)
		assert.Equal(t, 400, ec.HTTPStatus)
	}

	_, ec := AgentSlashCommandCreate(slashCmdOwner, slashCmdAgent, AgentSlashCommandCreateReq{
		Name:        "/deploy",
		Description: strings.Repeat("说", 201),
	})
	require.NotNil(t, ec)
	assert.Equal(t, errcode.ErrSlashCommandDescTooLong.BizCode, ec.BizCode)

	// 边界内的合法值必须放行：32 位命令名 + 200 字说明。
	_, ec = AgentSlashCommandCreate(slashCmdOwner, slashCmdAgent, AgentSlashCommandCreateReq{
		Name:        "/" + strings.Repeat("a", 32),
		Description: strings.Repeat("说", 200),
	})
	require.Nil(t, ec)
}

// 超过 50 条上限报错，且不再入库。
func TestAgentSlashCommandLimit(t *testing.T) {
	setupSlashCmdSvcTest(t)

	for i := 0; i < agentSlashCommandMaxPerAgent; i++ {
		_, ec := AgentSlashCommandCreate(slashCmdOwner, slashCmdAgent, AgentSlashCommandCreateReq{
			Name: "/cmd" + strconv.Itoa(i),
		})
		require.Nil(t, ec, "seed %d", i)
	}

	_, ec := AgentSlashCommandCreate(slashCmdOwner, slashCmdAgent, AgentSlashCommandCreateReq{Name: "/overflow"})
	require.NotNil(t, ec)
	assert.Equal(t, errcode.ErrSlashCommandLimitExceed.BizCode, ec.BizCode)

	var count int64
	require.NoError(t, store.DB.Model(&model.AgentSlashCommand{}).
		Where("agent_id = ?", slashCmdAgent).Count(&count).Error)
	assert.EqualValues(t, agentSlashCommandMaxPerAgent, count)
}

// 删除：命中即删并重建工具栏；不存在的命令 404 且不触发重建。
func TestAgentSlashCommandDelete(t *testing.T) {
	refresher := setupSlashCmdSvcTest(t)

	created, ec := AgentSlashCommandCreate(slashCmdOwner, slashCmdAgent, AgentSlashCommandCreateReq{Name: "/deploy"})
	require.Nil(t, ec)
	require.Len(t, refresher.snapshot(), 1)

	require.Nil(t, AgentSlashCommandDelete(slashCmdOwner, slashCmdAgent, created.ID))
	assert.Len(t, refresher.snapshot(), 2)

	items, ec := AgentSlashCommandList(slashCmdOwner, slashCmdAgent)
	require.Nil(t, ec)
	assert.Empty(t, items)

	ec = AgentSlashCommandDelete(slashCmdOwner, slashCmdAgent, created.ID)
	require.NotNil(t, ec)
	assert.Equal(t, errcode.ErrSlashCommandNotFound.BizCode, ec.BizCode)
	assert.Len(t, refresher.snapshot(), 2)
}

// 工具栏取数：按创建顺序返回；client_type 没有内置命令面板时不查库、直接返回空。
func TestAgentSlashCommandsForToolbar(t *testing.T) {
	setupSlashCmdSvcTest(t)

	for _, name := range []string{"/deploy", "/standup", "/rollback"} {
		_, ec := AgentSlashCommandCreate(slashCmdOwner, slashCmdAgent, AgentSlashCommandCreateReq{Name: name})
		require.Nil(t, ec)
	}

	commands := AgentSlashCommandsForToolbar(context.Background(), slashCmdAgent, model.AgentClientTypeClaude)
	require.Len(t, commands, 3)
	assert.Equal(t, []agentslashcmd.SlashCommand{
		{Name: "/deploy"}, {Name: "/standup"}, {Name: "/rollback"},
	}, commands)

	assert.Empty(t, AgentSlashCommandsForToolbar(context.Background(), slashCmdAgent, "no-such-client-type"))
}
