package agentapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSkillSyncTestConn(agentID, ownerID int64, primary bool) *agentConn {
	return &agentConn{
		agentID:   agentID,
		ownerID:   ownerID,
		isPrimary: primary,
		send:      make(chan []byte, 8),
		done:      make(chan struct{}),
	}
}

func recvPacket(t *testing.T, c *agentConn) *protocol.Packet {
	t.Helper()
	select {
	case data := <-c.send:
		var pkt protocol.Packet
		require.NoError(t, json.Unmarshal(data, &pkt))
		return &pkt
	case <-time.After(time.Second):
		t.Fatal("no packet received")
		return nil
	}
}

// skill_sync 推送目标：同 owner 的全部主连接；他人 owner、共享连接一概不发。
func TestPushSkillSyncToOwnerTargetsPrimaryConnsOfOwner(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	const owner, otherOwner = int64(9001), int64(9002)

	// owner 的两台机器（两个 agent 主连接）。
	a1 := newSkillSyncTestConn(101, owner, true)
	a2 := newSkillSyncTestConn(102, owner, true)
	// 同 agent 挂着的共享连接（被共享者不是 owner）——不应收到。
	shared := newSkillSyncTestConn(101, otherOwner, false)
	// 另一 owner 的主连接——不应收到。
	other := newSkillSyncTestConn(201, otherOwner, true)
	for _, c := range []*agentConn{a1, a2, shared, other} {
		mgr.putConnForTest(c)
	}

	mgr.pushSkillSyncToOwner(owner, "报告规范", 3)

	for _, c := range []*agentConn{a1, a2} {
		pkt := recvPacket(t, c)
		assert.Equal(t, protocol.CmdSkillSync, pkt.Cmd)
		var p protocol.SkillSyncPayload
		require.NoError(t, json.Unmarshal(pkt.Payload, &p))
		assert.Equal(t, owner, p.OwnerID)
		assert.Equal(t, "报告规范", p.Name)
		assert.Equal(t, int64(3), p.Version)
	}
	assert.Empty(t, shared.send, "shared conn should not receive skill_sync")
	assert.Empty(t, other.send, "other owner should not receive skill_sync")
}

// Redis 广播分发：cmd 命中即消费；manager 未初始化也不 panic。
func TestHandleRedisDispatchSkillLibraryChanged(t *testing.T) {
	payload, _ := json.Marshal(protocol.SkillLibraryChangedPayload{OwnerID: 9003, Name: "n", Version: 1})
	assert.True(t, HandleRedisDispatch(protocol.RedisCmdSkillLibraryChanged, payload))

	// 挂上带连接的 manager 后，广播应转成对该 owner 的下行推送。
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	c := newSkillSyncTestConn(103, 9003, true)
	mgr.putConnForTest(c)
	globalMu.Lock()
	prev := globalManager
	globalManager = mgr
	globalMu.Unlock()
	defer func() {
		globalMu.Lock()
		globalManager = prev
		globalMu.Unlock()
	}()

	assert.True(t, HandleRedisDispatch(protocol.RedisCmdSkillLibraryChanged, payload))
	pkt := recvPacket(t, c)
	assert.Equal(t, protocol.CmdSkillSync, pkt.Cmd)
}
