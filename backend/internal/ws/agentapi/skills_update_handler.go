package agentapi

import (
	"encoding/json"
	"strings"

	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// SkillsUpdatePayload 让 plugin 在 auth 之后追报最新的 skills 列表。
// 后端会用 payload.Skills 整体覆盖 conn.skills，并立刻刷新 redis runtime profile，
// 工具栏据此动态判断是否渲染技能按钮。
// LibrarySkills 与 Skills 并列上报技能库全集 + 各作用域启用状态
// （技能库启用，方案 v2），语义见 toolruntime.LibrarySkillEntry。
// OwnerLibrarySync 为 true 时表示本次是 owner/machine 技能库台账同步：
// 可将 library_skills 扇入同 owner + 同 client_type 的其它在线连接，
// 避免连接器按 agent 连接重复推送大包。会话级/重连上报勿置 true。
type SkillsUpdatePayload struct {
	Skills           json.RawMessage `json:"skills"`
	LibrarySkills    json.RawMessage `json:"library_skills"`
	OwnerLibrarySync bool            `json:"owner_library_sync,omitempty"`
}

// handleSkillsUpdate 处理 plugin 主动上报的 skills 刷新。
// 当前主要用于 Kiro 模式：plugin 在 ACP session 拿到工作区 cwd 后，
// 重新扫描 ~/.kiro/skills 与 <cwd>/.kiro/skills，再通过该 cmd 把全集推上来。
func (m *Manager) handleSkillsUpdate(conn *agentConn, pkt *protocol.Packet) {
	if conn == nil || pkt == nil {
		return
	}

	var payload SkillsUpdatePayload
	if len(pkt.Payload) > 0 {
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			logger.L.Warnf("agent_skills_update payload invalid agent=%d err=%v", conn.agentID, err)
			conn.recordViolation()
			conn.sendPayload("error", pkt.Seq, SendNackPayload{
				Code: protocol.CodeInvalidPayload,
				Msg:  "invalid payload",
			})
			return
		}
	}

	skills := parseAuthSkills(payload.Skills)
	conn.skills = append([]toolruntime.SkillEntry(nil), skills...)
	librarySkills := parseLibrarySkills(payload.LibrarySkills)
	conn.librarySkills = append([]toolruntime.LibrarySkillEntry(nil), librarySkills...)
	// 更新完连接内快照后只刷新一次，使 Redis runtime profile 原子地看到新清单。
	// 控制帧心跳已独立续租；解析前再刷新一次只会写入旧 profile 并放大 Redis 流量。
	m.refreshAgentLease(conn)

	if payload.OwnerLibrarySync {
		m.propagateOwnerLibrarySkills(conn)
	}
}

// propagateOwnerLibrarySkills 把 reporter 的 library_skills 台账扇入本节点上
// 同 owner + 同 client_type 的其它 agent 连接。保留各 peer 已有 enable_scopes，
// 避免用 reporter 的 project cwd 覆盖其它会话的启用状态；active skills 不扇入
//（仍由各连接自身会话扫描决定）。
func (m *Manager) propagateOwnerLibrarySkills(source *agentConn) {
	if m == nil || source == nil || source.ownerID <= 0 {
		return
	}
	srcType := normalizeClientType(source.clientType)
	if srcType == "" {
		return
	}
	catalog := append([]toolruntime.LibrarySkillEntry(nil), source.librarySkills...)
	peers := 0
	m.ForEachLocalAgentConn(func(peer *agentConn) bool {
		if peer == nil || peer == source {
			return true
		}
		if peer.ownerID != source.ownerID {
			return true
		}
		if normalizeClientType(peer.clientType) != srcType {
			return true
		}
		peer.librarySkills = mergeLibrarySkillsCatalog(peer.librarySkills, catalog)
		m.refreshAgentLease(peer)
		peers++
		return true
	})
	if peers > 0 {
		logger.L.Infof(
			"owner_library_sync propagated agent=%d owner=%d client_type=%s peers=%d library=%d",
			source.agentID,
			source.ownerID,
			srcType,
			peers,
			len(catalog),
		)
	}
}

func normalizeClientType(clientType string) string {
	return strings.ToLower(strings.TrimSpace(clientType))
}

// mergeLibrarySkillsCatalog 以 src 台账为准重建列表；同名技能保留 dst 的 enable_scopes。
// 新技能不得照搬 reporter 的 project scope（那是 reporter 会话 cwd 的判定），
// project 一律标 unavailable，等 peer 自己会话扫描再纠正；global 可跟台账。
func mergeLibrarySkillsCatalog(
	dst []toolruntime.LibrarySkillEntry,
	src []toolruntime.LibrarySkillEntry,
) []toolruntime.LibrarySkillEntry {
	byName := make(map[string]toolruntime.LibrarySkillEntry, len(dst))
	for _, e := range dst {
		name := strings.TrimSpace(e.Name)
		if name == "" {
			continue
		}
		byName[name] = e
	}
	out := make([]toolruntime.LibrarySkillEntry, 0, len(src))
	for _, e := range src {
		name := strings.TrimSpace(e.Name)
		if name == "" {
			continue
		}
		next := e
		if old, ok := byName[name]; ok {
			next.EnableScopes = old.EnableScopes
		} else {
			next.EnableScopes = toolruntime.LibrarySkillEnableScopes{
				Global:  e.EnableScopes.Global,
				Project: "unavailable",
			}
		}
		out = append(out, next)
	}
	return out
}
