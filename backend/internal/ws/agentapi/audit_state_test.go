package agentapi

import "testing"

func TestCanServeConversationAuditRequiresCompleteDeclaredSurface(t *testing.T) {
	mgr := NewManager("", 0, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := auditConn(91001, 81001, false)
	conn.capabilities = []string{"local_action_v1", "audit_replay_v2"}
	conn.localActions = append([]string(nil), auditReplayLocalActions...)
	mgr.putConnForTest(conn)

	if !mgr.CanServeConversationAudit(conn.agentID, conn.ownerID) {
		t.Fatal("complete audit replay declaration must be accepted")
	}
	conn.localActions = conn.localActions[:2]
	if mgr.CanServeConversationAudit(conn.agentID, conn.ownerID) {
		t.Fatal("partial local action declaration must be rejected")
	}
	conn.localActions = append([]string(nil), auditReplayLocalActions...)
	conn.capabilities = []string{"local_action_v1"}
	if mgr.CanServeConversationAudit(conn.agentID, conn.ownerID) {
		t.Fatal("missing audit_replay_v2 capability must be rejected")
	}
}
