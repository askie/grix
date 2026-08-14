package agentapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testTailnetCap = "tailnet_file_v1"

func newTailnetTestConn(agentID int64, clientID, tnIP string, caps []string) *agentConn {
	return &agentConn{
		agentID:      agentID,
		clientID:     clientID,
		tailnetIP:    tnIP,
		capabilities: caps,
		send:         make(chan []byte, 8),
	}
}

func addTailnetConn(m *Manager, c *agentConn) {
	m.putConnForTest(c)
}

// readSentPacket 从连接的 send channel 取出一个包并解析。
func readSentPacket(t *testing.T, c *agentConn, timeout time.Duration) protocol.Packet {
	t.Helper()
	select {
	case data := <-c.send:
		var pkt protocol.Packet
		require.NoError(t, json.Unmarshal(data, &pkt))
		return pkt
	case <-time.After(timeout):
		t.Fatal("timeout waiting for sent packet")
		return protocol.Packet{}
	}
}

func newTailnetTestManager(t *testing.T) *Manager {
	t.Helper()
	mgr := NewManager("", time.Second, nil, nil, nil, nil)
	t.Cleanup(mgr.Shutdown)
	return mgr
}

// ---- ResolveTransferMode 四分支 ----

func TestResolveTransferMode_AllBranches(t *testing.T) {
	m := newTailnetTestManager(t)
	caps := []string{testTailnetCap}
	const ownerID int64 = 10
	a := newTailnetTestConn(1, "node-a", "100.64.0.1", caps)
	a.ownerID = ownerID
	b := newTailnetTestConn(2, "node-b", "100.64.0.2", caps) // 同 tailnet，可直传
	b.ownerID = ownerID
	nonTailnet := newTailnetTestConn(3, "node-c", "192.168.1.1", caps)
	nonTailnet.ownerID = ownerID
	addTailnetConn(m, a)
	addTailnetConn(m, b)
	addTailnetConn(m, nonTailnet)

	c := m.tailnetCoordinator
	// 按发起 owner 精确路由（两端连接同属 owner=10）
	// <1MB → relay
	assert.Equal(t, TransferModeRelay, c.ResolveTransferMode(1, 2, ownerID, 512*1024))
	// 1~16MB 可直传 → tailnet
	assert.Equal(t, TransferModeTailnet, c.ResolveTransferMode(1, 2, ownerID, 5*1024*1024))
	// >16MB 可直传 → tailnet
	assert.Equal(t, TransferModeTailnet, c.ResolveTransferMode(1, 2, ownerID, 50*1024*1024))
	// 1~16MB 不可直传 → relay
	assert.Equal(t, TransferModeRelay, c.ResolveTransferMode(1, 3, ownerID, 5*1024*1024))
	// >16MB 不可直传 → unavailable
	assert.Equal(t, TransferModeUnavailable, c.ResolveTransferMode(1, 3, ownerID, 50*1024*1024))
	// owner=0 非法 → 找不到连接，按不可直传处理（fail-closed）
	assert.Equal(t, TransferModeUnavailable, c.ResolveTransferMode(1, 2, 0, 50*1024*1024))
}

// ---- StartTransfer 双向角色映射 ----

func runStartTransfer(t *testing.T, direction string) (TailnetFileServePayload, TailnetFileFetchPayload) {
	t.Helper()
	config.C.JWT.Secret = "test-tailnet-secret"
	m := newTailnetTestManager(t)
	caps := []string{testTailnetCap}
	const ownerID int64 = 10
	initiator := newTailnetTestConn(1, "node-initiator", "100.64.0.1", caps)
	initiator.ownerID = ownerID
	peer := newTailnetTestConn(2, "node-peer", "100.64.0.2", caps)
	peer.ownerID = ownerID
	addTailnetConn(m, initiator)
	addTailnetConn(m, peer)

	type res struct {
		actionID string
		err      error
	}
	resCh := make(chan res, 1)
	go func() {
		aid, err := m.tailnetCoordinator.StartTransfer(1, 2, ownerID, direction, "/local/path", "/remote/path")
		resCh <- res{aid, err}
	}()

	// server 端恒为 peer：读取 serve
	servePkt := readSentPacket(t, peer, 2*time.Second)
	require.Equal(t, "tailnet_file_serve", servePkt.Cmd)
	var serve TailnetFileServePayload
	require.NoError(t, json.Unmarshal(servePkt.Payload, &serve))

	// 注入 ready（来源必须是 server 端 = peer，agentID=2）
	m.tailnetCoordinator.HandleFileReady(2, TailnetFileReadyPayload{
		ActionID: serve.ActionID,
		URL:      "http://100.64.0.2:40001/file",
		FileSize: 1234,
	})

	// client 端恒为 initiator：读取 fetch
	fetchPkt := readSentPacket(t, initiator, 2*time.Second)
	require.Equal(t, "tailnet_file_fetch", fetchPkt.Cmd)
	var fetch TailnetFileFetchPayload
	require.NoError(t, json.Unmarshal(fetchPkt.Payload, &fetch))

	r := <-resCh
	require.NoError(t, r.err)
	require.Equal(t, serve.ActionID, r.actionID)
	return serve, fetch
}

func TestStartTransfer_Download(t *testing.T) {
	serve, fetch := runStartTransfer(t, "download")
	// serve 发给 server 端(peer)，读取 remote 路径，只允许 initiator(client) IP 访问
	assert.Equal(t, "download", serve.Direction)
	assert.Equal(t, "/remote/path", serve.FilePath)
	assert.Equal(t, "100.64.0.1", serve.AllowedIP)
	// fetch 发给 client 端(initiator)，写入 local 路径
	assert.Equal(t, "download", fetch.Direction)
	assert.Equal(t, "/local/path", fetch.FilePath)
	assert.Equal(t, "http://100.64.0.2:40001/file", fetch.URL)
}

func TestStartTransfer_Upload(t *testing.T) {
	serve, fetch := runStartTransfer(t, "upload")
	// upload：server 端(peer)接收 PUT 写入 remote，仍只允许 initiator IP
	assert.Equal(t, "upload", serve.Direction)
	assert.Equal(t, "/remote/path", serve.FilePath)
	assert.Equal(t, "100.64.0.1", serve.AllowedIP)
	// client 端(initiator)读取 local 并 PUT
	assert.Equal(t, "upload", fetch.Direction)
	assert.Equal(t, "/local/path", fetch.FilePath)
}

// ---- 入口命令 handleTailnetTransferRequest ----

func TestHandleTailnetTransferRequest_NonDirectReturnsMode(t *testing.T) {
	m := newTailnetTestManager(t)
	caps := []string{testTailnetCap}
	const ownerID int64 = 10
	initiator := newTailnetTestConn(1, "node-initiator", "100.64.0.1", caps)
	initiator.ownerID = ownerID
	peerNonTailnet := newTailnetTestConn(2, "node-peer", "192.168.1.1", caps)
	peerNonTailnet.ownerID = ownerID
	addTailnetConn(m, initiator)
	addTailnetConn(m, peerNonTailnet)

	// 1~16MB 不可直传 → relay
	payload, _ := json.Marshal(TailnetTransferRequestPayload{
		PeerAgentID: 2, Direction: "download", LocalPath: "/l", RemotePath: "/r", FileSize: 5 * 1024 * 1024,
	})
	m.handleTailnetTransferRequest(initiator, 100, payload)
	pkt := readSentPacket(t, initiator, time.Second)
	assert.Equal(t, "tailnet_transfer_result", pkt.Cmd)
	var result TailnetTransferResultPayload
	require.NoError(t, json.Unmarshal(pkt.Payload, &result))
	assert.Equal(t, string(TransferModeRelay), result.Mode)

	// >16MB 不可直传 → unavailable
	payload2, _ := json.Marshal(TailnetTransferRequestPayload{
		PeerAgentID: 2, Direction: "download", LocalPath: "/l", RemotePath: "/r", FileSize: 50 * 1024 * 1024,
	})
	m.handleTailnetTransferRequest(initiator, 101, payload2)
	pkt2 := readSentPacket(t, initiator, time.Second)
	var result2 TailnetTransferResultPayload
	require.NoError(t, json.Unmarshal(pkt2.Payload, &result2))
	assert.Equal(t, string(TransferModeUnavailable), result2.Mode)
}

func TestHandleTailnetTransferRequest_DirectDrivesFullFlow(t *testing.T) {
	config.C.JWT.Secret = "test-tailnet-secret"
	m := newTailnetTestManager(t)
	caps := []string{testTailnetCap}
	const ownerID int64 = 10
	initiator := newTailnetTestConn(1, "node-initiator", "100.64.0.1", caps)
	initiator.ownerID = ownerID
	peer := newTailnetTestConn(2, "node-peer", "100.64.0.2", caps)
	peer.ownerID = ownerID
	addTailnetConn(m, initiator)
	addTailnetConn(m, peer)

	payload, _ := json.Marshal(TailnetTransferRequestPayload{
		PeerAgentID: 2, Direction: "download", LocalPath: "/l", RemotePath: "/r", FileSize: 5 * 1024 * 1024,
	})
	m.handleTailnetTransferRequest(initiator, 100, payload)

	// 后台协程应向 peer 发 serve
	servePkt := readSentPacket(t, peer, 2*time.Second)
	require.Equal(t, "tailnet_file_serve", servePkt.Cmd)
	var serve TailnetFileServePayload
	require.NoError(t, json.Unmarshal(servePkt.Payload, &serve))

	m.tailnetCoordinator.HandleFileReady(2, TailnetFileReadyPayload{ActionID: serve.ActionID, URL: "http://100.64.0.2:40002/file"})

	// initiator 先收到 fetch
	fetchPkt := readSentPacket(t, initiator, 2*time.Second)
	require.Equal(t, "tailnet_file_fetch", fetchPkt.Cmd)

	// 注入 done（来源必须是 client 端 = initiator，agentID=1）
	m.tailnetCoordinator.HandleFileDone(1, TailnetFileDonePayload{ActionID: serve.ActionID, Status: "ok", BytesTransferred: 999})

	// initiator 再收到 transfer_result
	resultPkt := readSentPacket(t, initiator, 2*time.Second)
	require.Equal(t, "tailnet_transfer_result", resultPkt.Cmd)
	var result TailnetTransferResultPayload
	require.NoError(t, json.Unmarshal(resultPkt.Payload, &result))
	assert.Equal(t, string(TransferModeTailnet), result.Mode)
	assert.Equal(t, "ok", result.Status)
	assert.Equal(t, int64(999), result.BytesTransferred)
}

// ---- P0: 跨 owner 拒绝 ----

func TestHandleTailnetTransferRequest_RejectsCrossOwner(t *testing.T) {
	m := newTailnetTestManager(t)
	caps := []string{testTailnetCap}
	initiator := newTailnetTestConn(1, "node-i", "100.64.0.1", caps)
	initiator.ownerID = 10
	peer := newTailnetTestConn(2, "node-p", "100.64.0.2", caps)
	peer.ownerID = 20 // 不同 owner
	addTailnetConn(m, initiator)
	addTailnetConn(m, peer)

	payload, _ := json.Marshal(TailnetTransferRequestPayload{
		PeerAgentID: 2, Direction: "download", LocalPath: "/l", RemotePath: "/r", FileSize: 5 * 1024 * 1024,
	})
	m.handleTailnetTransferRequest(initiator, 100, payload)
	pkt := readSentPacket(t, initiator, time.Second)
	var result TailnetTransferResultPayload
	require.NoError(t, json.Unmarshal(pkt.Payload, &result))
	assert.Equal(t, string(TransferModeUnavailable), result.Mode)
	assert.Equal(t, "peer not accessible", result.ErrorMsg)
}

// ---- P1: done 早于 WaitDone 到达仍返回成功 ----

func TestWaitDone_DoneBeforeWaitStillSucceeds(t *testing.T) {
	m := newTailnetTestManager(t)
	c := m.tailnetCoordinator
	aid := "tf:test:done-early"
	c.mu.Lock()
	c.pending[aid] = &pendingTransfer{
		actionID:      aid,
		serverAgentID: 2,
		clientAgentID: 1,
		readyCh:       make(chan TailnetFileReadyPayload, 1),
		doneCh:        make(chan TailnetFileDonePayload, 1),
	}
	c.mu.Unlock()

	// done 先到（HandleFileDone 不再删 pending），来源 = client 端 agentID=1
	c.HandleFileDone(1, TailnetFileDonePayload{ActionID: aid, Status: "ok", BytesTransferred: 555})
	// 之后 WaitDone 仍能读到结果
	done, err := c.WaitDone(aid, time.Second)
	require.NoError(t, err)
	assert.Equal(t, "ok", done.Status)
	assert.Equal(t, int64(555), done.BytesTransferred)

	// WaitDone 后 pending 已清理
	c.mu.Lock()
	_, exists := c.pending[aid]
	c.mu.Unlock()
	assert.False(t, exists)
}

// ---- A: ready/done 来源校验 ----

func TestHandleFileReadyDone_RejectsWrongSource(t *testing.T) {
	m := newTailnetTestManager(t)
	c := m.tailnetCoordinator
	aid := "tf:test:src"
	pt := &pendingTransfer{
		actionID:      aid,
		serverAgentID: 2,
		clientAgentID: 1,
		readyCh:       make(chan TailnetFileReadyPayload, 1),
		doneCh:        make(chan TailnetFileDonePayload, 1),
	}
	c.mu.Lock()
	c.pending[aid] = pt
	c.mu.Unlock()

	// 错误来源(agent=99)伪造 ready → 丢弃
	c.HandleFileReady(99, TailnetFileReadyPayload{ActionID: aid, URL: "http://evil"})
	select {
	case <-pt.readyCh:
		t.Fatal("ready from wrong source must be dropped")
	default:
	}
	// 正确来源(server=2) → 投递
	c.HandleFileReady(2, TailnetFileReadyPayload{ActionID: aid, URL: "http://ok"})
	select {
	case r := <-pt.readyCh:
		assert.Equal(t, "http://ok", r.URL)
	default:
		t.Fatal("ready from correct source must be delivered")
	}

	// 错误来源(agent=99)伪造 done → 丢弃
	c.HandleFileDone(99, TailnetFileDonePayload{ActionID: aid, Status: "ok"})
	select {
	case <-pt.doneCh:
		t.Fatal("done from wrong source must be dropped")
	default:
	}
	// 正确来源(client=1) → 投递
	c.HandleFileDone(1, TailnetFileDonePayload{ActionID: aid, Status: "ok"})
	select {
	case d := <-pt.doneCh:
		assert.Equal(t, "ok", d.Status)
	default:
		t.Fatal("done from correct source must be delivered")
	}
}
