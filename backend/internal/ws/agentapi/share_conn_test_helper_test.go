package agentapi

// putConnForTest 在测试中把一条连接登记进 (agentID -> ownerID -> conn) 连接表。
// 替代旧的 m.conns[agentID] = conn 直接赋值（连接表已改为按 owner 分层以支持 agent 共享）。
func (m *Manager) putConnForTest(c *agentConn) {
	if c == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	owners := m.conns[c.agentID]
	if owners == nil {
		owners = make(map[int64]*agentConn)
		m.conns[c.agentID] = owners
	}
	owners[c.ownerID] = c
}
