package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	pkgagentapi "github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	delKickOwner  = int64(88001)
	delKickShared = int64(88002)
	delKickAgent  = int64(97001)
)

func setupAgentDeleteKickTest(t *testing.T) {
	t.Helper()
	logger.Init()
	testDB := testutil.NewTestDB()
	origDB, origRDB := store.DB, store.RDB
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		store.DB = origDB
		store.RDB = origRDB
		testDB.Close()
	})
	require.NoError(t, store.DB.Create(&model.Agent{
		ID:           delKickAgent,
		AgentName:    "del-kick-agent",
		OwnerID:      delKickOwner,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
	}).Error)
}

// waitKickMessage 在订阅通道上等待 kick_agent 指令（跳过 share_sync 等无关消息）。
func waitKickMessage(t *testing.T, ch <-chan *redis.Message) map[string]any {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case msg := <-ch:
			var envelope struct {
				Cmd     string          `json:"cmd"`
				Payload json.RawMessage `json:"payload"`
			}
			require.NoError(t, json.Unmarshal([]byte(msg.Payload), &envelope))
			if envelope.Cmd != "kick_agent" {
				continue
			}
			var payload map[string]any
			require.NoError(t, json.Unmarshal(envelope.Payload, &payload))
			return payload
		case <-deadline:
			t.Fatal("未收到 kick_agent 发布")
		}
	}
}

// 删除 agent 时：向主路由节点与所有 owner 路由节点广播 kick_agent，reason=agent_deleted。
func TestAgentDeleteKicksOnlineConnsAllNodes(t *testing.T) {
	setupAgentDeleteKickTest(t)
	ctx := context.Background()

	// 主连接在 node-a，被共享者连接在 node-b（跨节点场景）
	require.NoError(t, store.RDB.Set(ctx, pkgagentapi.RouteKey(delKickAgent), "node-a", time.Minute).Err())
	require.NoError(t, store.RDB.SAdd(ctx, pkgagentapi.RouteOwnerSetKey(delKickAgent), delKickShared).Err())
	require.NoError(t, store.RDB.Set(ctx, pkgagentapi.RouteKeyForOwner(delKickAgent, delKickShared), "node-b", time.Minute).Err())

	subA := store.RDB.Subscribe(ctx, "chan:node-a")
	defer subA.Close()
	_, err := subA.Receive(ctx)
	require.NoError(t, err)
	subB := store.RDB.Subscribe(ctx, "chan:node-b")
	defer subB.Close()
	_, err = subB.Receive(ctx)
	require.NoError(t, err)

	ec := AgentDelete(delKickOwner, delKickAgent)
	require.Nil(t, ec)

	// 两个节点都收到 kick_agent，reason=agent_deleted，全连接踢（无 owner_id）
	for name, ch := range map[string]<-chan *redis.Message{"node-a": subA.Channel(), "node-b": subB.Channel()} {
		payload := waitKickMessage(t, ch)
		assert.Equal(t, "97001", payload["agent_id"], "节点 %s agent_id", name)
		assert.Equal(t, "agent_deleted", payload["reason"], "节点 %s reason", name)
		assert.NotContains(t, payload, "owner_id", "节点 %s 应为全连接踢线", name)
	}

	// agent 已标记删除
	var agent model.Agent
	require.NoError(t, store.DB.First(&agent, delKickAgent).Error)
	assert.Equal(t, model.AgentStatusDeleted, agent.Status)
}

// 无在线连接（无任何路由 key）时删除照常成功，不发布踢线指令。
func TestAgentDeleteWithoutOnlineConns(t *testing.T) {
	setupAgentDeleteKickTest(t)

	ec := AgentDelete(delKickOwner, delKickAgent)
	require.Nil(t, ec)

	var agent model.Agent
	require.NoError(t, store.DB.First(&agent, delKickAgent).Error)
	assert.Equal(t, model.AgentStatusDeleted, agent.Status)
}
