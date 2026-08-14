package e2e

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/agentreceive"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/require"
)

func TestLiveAgentPreflight(t *testing.T) {
	cfg := loadLiveAgentConfig(t)
	if !cfg.Enabled {
		t.Skip("set GRIX_LIVE_E2E=1 to enable live local OpenClaw E2E")
	}
	if strings.TrimSpace(cfg.OpenClawAgent) == "" || strings.TrimSpace(cfg.OpenClawAccount) == "" {
		t.Skip("set GRIX_LIVE_OPENCLAW_AGENT and GRIX_LIVE_GRIX_ACCOUNT to run live preflight")
	}

	harness := newLiveAgentHarness(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	summary := harness.runPreflight(ctx)
	t.Logf("preflight artifacts: %s", harness.artifactDir)
	t.Logf("preflight status: %s", summary.statusLine())
	require.False(t, summary.hasFailures(), "preflight contains hard failures")
}

func TestLiveAgentConversationDMSmoke(t *testing.T) {
	cfg := loadLiveAgentConfig(t)
	if !cfg.Enabled {
		t.Skip("set GRIX_LIVE_E2E=1 to enable live local OpenClaw E2E")
	}
	if strings.TrimSpace(cfg.UserToken) == "" && (strings.TrimSpace(cfg.UserAccount) == "" || strings.TrimSpace(cfg.UserPassword) == "") {
		t.Skip("set GRIX_LIVE_USER_TOKEN or both GRIX_LIVE_USER_ACCOUNT / GRIX_LIVE_USER_PASSWORD to run DM smoke")
	}

	harness := newLiveAgentHarness(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConversationTimeout+30*time.Second)
	defer cancel()

	if strings.TrimSpace(cfg.OpenClawAgent) != "" && strings.TrimSpace(cfg.OpenClawAccount) != "" {
		summary := harness.runPreflight(ctx)
		require.False(t, summary.hasFailures(), "preflight contains hard failures")
	}

	session := harness.bootstrapProjectSession(ctx)
	client := newLiveUserWSClient(t, harness, session)
	client.connect(ctx)
	defer client.close()

	marker := fmt.Sprintf("E2E_DM_SMOKE_%d", time.Now().Unix())
	probe := client.runConversationProbe(ctx, liveConversationProbeOptions{
		Message:              "请严格只回复：" + marker,
		ExpectedSenderID:     session.AgentID,
		ExpectedTextContains: marker,
		Timeout:              cfg.ConversationTimeout,
	})

	require.NotEmpty(t, probe.SendAck, "send_ack should be captured")
	require.NotEmpty(t, probe.AgentPushes, "agent push_msg should be captured")

	harness.writeJSON("dm-smoke-summary.json", map[string]any{
		"generated_at":  time.Now().UTC().Format(time.RFC3339),
		"artifacts_dir": harness.artifactDir,
		"session":       session,
		"probe":         probe,
		"marker":        marker,
	})
	t.Logf("dm smoke artifacts: %s", harness.artifactDir)
}

func TestLiveAgentConversationGroupSmoke(t *testing.T) {
	cfg := loadLiveAgentConfig(t)
	if !cfg.Enabled {
		t.Skip("set GRIX_LIVE_E2E=1 to enable live local OpenClaw E2E")
	}
	if strings.TrimSpace(cfg.UserToken) == "" && (strings.TrimSpace(cfg.UserAccount) == "" || strings.TrimSpace(cfg.UserPassword) == "") {
		t.Skip("set GRIX_LIVE_USER_TOKEN or both GRIX_LIVE_USER_ACCOUNT / GRIX_LIVE_USER_PASSWORD to run group smoke")
	}

	harness := newLiveAgentHarness(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConversationTimeout+45*time.Second)
	defer cancel()

	if strings.TrimSpace(cfg.OpenClawAgent) != "" && strings.TrimSpace(cfg.OpenClawAccount) != "" {
		summary := harness.runPreflight(ctx)
		require.False(t, summary.hasFailures(), "preflight contains hard failures")
	}

	session := harness.bootstrapProjectIdentity(ctx)
	targetAgent := harness.resolveMultiAgentGroupTargets(ctx, session, 1)[0]
	targetAgentID := asString(targetAgent["id"])
	group := harness.createIsolatedGroupSessionWithAgents(
		ctx,
		session,
		fmt.Sprintf("e2e_group_smoke_%d", time.Now().Unix()),
		[]map[string]any{targetAgent},
	)
	groupCleanupMode := harness.configureGroupCleanup(t, session, group)
	receiveSetting := harness.updateGroupAgentReceiveMode(
		ctx,
		session,
		group.SessionID,
		targetAgentID,
		agentreceive.ModeNormal,
	)

	groupSession := session
	groupSession.SessionID = group.SessionID
	groupSession.AgentID = targetAgentID
	groupSession.Agent = cloneMap(targetAgent)
	client := newLiveUserWSClient(t, harness, groupSession)
	client.connect(ctx)
	defer client.close()

	marker := fmt.Sprintf("E2E_GROUP_SMOKE_%d", time.Now().Unix())
	probe := client.runConversationProbe(ctx, liveConversationProbeOptions{
		Message:              fmt.Sprintf("@%s 请严格只回复：%s", targetAgentID, marker),
		ExpectedSenderID:     targetAgentID,
		ExpectedTextContains: marker,
		Timeout:              cfg.ConversationTimeout,
	})

	require.NotEmpty(t, probe.SendAck, "send_ack should be captured")
	require.NotEmpty(t, probe.AgentPushes, "group agent push_msg should be captured")

	harness.writeJSON("group-smoke-summary.json", map[string]any{
		"generated_at":    time.Now().UTC().Format(time.RFC3339),
		"artifacts_dir":   harness.artifactDir,
		"session":         groupSession,
		"group":           group,
		"group_cleanup":   groupCleanupMode,
		"receive_setting": receiveSetting,
		"probe":           probe,
		"marker":          marker,
	})
	t.Logf("group smoke artifacts: %s", harness.artifactDir)
}

func TestLiveAgentMentionGroupTrigger(t *testing.T) {
	cfg := loadLiveAgentConfig(t)
	if !cfg.Enabled {
		t.Skip("set GRIX_LIVE_E2E=1 to enable live local OpenClaw E2E")
	}
	if strings.TrimSpace(cfg.UserToken) == "" && (strings.TrimSpace(cfg.UserAccount) == "" || strings.TrimSpace(cfg.UserPassword) == "") {
		t.Skip("set GRIX_LIVE_USER_TOKEN or both GRIX_LIVE_USER_ACCOUNT / GRIX_LIVE_USER_PASSWORD to run mention smoke")
	}

	harness := newLiveAgentHarness(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConversationTimeout+60*time.Second)
	defer cancel()

	if strings.TrimSpace(cfg.OpenClawAgent) != "" && strings.TrimSpace(cfg.OpenClawAccount) != "" {
		summary := harness.runPreflight(ctx)
		require.False(t, summary.hasFailures(), "preflight contains hard failures")
	}

	session := harness.bootstrapProjectIdentity(ctx)
	targetAgent := harness.resolveMultiAgentGroupTargets(ctx, session, 1)[0]
	targetAgentID := asString(targetAgent["id"])
	group := harness.createIsolatedGroupSessionWithAgents(
		ctx,
		session,
		fmt.Sprintf("e2e_group_mention_%d", time.Now().Unix()),
		[]map[string]any{targetAgent},
	)
	groupCleanupMode := harness.configureGroupCleanup(t, session, group)

	receiveSetting := harness.updateGroupAgentReceiveMode(
		ctx,
		session,
		group.SessionID,
		targetAgentID,
		agentreceive.ModeMentionOnly,
	)

	groupSession := session
	groupSession.SessionID = group.SessionID
	groupSession.AgentID = targetAgentID
	groupSession.Agent = cloneMap(targetAgent)
	client := newLiveUserWSClient(t, harness, groupSession)
	client.connect(ctx)
	defer client.close()

	quietAck := client.sendText("这是一条不带@的群消息，请不要回复。", nil)
	quietTrigger := asString(quietAck["msg_id"])
	quietResult := client.assertNoAgentPushWithin(ctx, quietTrigger, 8*time.Second)

	marker := fmt.Sprintf("E2E_MENTION_SMOKE_%d", time.Now().Unix())
	mentionText := fmt.Sprintf("@%s 请严格只回复：%s", targetAgentID, marker)
	probe := client.runConversationProbe(ctx, liveConversationProbeOptions{
		Message:              mentionText,
		ExpectedSenderID:     targetAgentID,
		ExpectedTextContains: marker,
		Timeout:              cfg.ConversationTimeout,
	})

	require.NotEmpty(t, quietAck, "quiet send_ack should be captured")
	require.NotEmpty(t, probe.SendAck, "mention send_ack should be captured")
	require.NotEmpty(t, probe.AgentPushes, "mention should wake the target agent")

	harness.writeJSON("group-mention-summary.json", map[string]any{
		"generated_at":    time.Now().UTC().Format(time.RFC3339),
		"artifacts_dir":   harness.artifactDir,
		"session":         groupSession,
		"group":           group,
		"group_cleanup":   groupCleanupMode,
		"receive_setting": receiveSetting,
		"quiet_ack":       quietAck,
		"quiet_result":    quietResult,
		"mention_probe":   probe,
		"marker":          marker,
		"mention_text":    mentionText,
	})
	t.Logf("group mention artifacts: %s", harness.artifactDir)
}

func TestLiveAgentCreateAgentByMainAgentDialogue(t *testing.T) {
	cfg := loadLiveAgentConfig(t)
	if !cfg.Enabled {
		t.Skip("set GRIX_LIVE_E2E=1 to enable live local OpenClaw E2E")
	}
	if strings.TrimSpace(cfg.UserToken) == "" && (strings.TrimSpace(cfg.UserAccount) == "" || strings.TrimSpace(cfg.UserPassword) == "") {
		t.Skip("set GRIX_LIVE_USER_TOKEN or both GRIX_LIVE_USER_ACCOUNT / GRIX_LIVE_USER_PASSWORD to run main-agent create-agent dialogue")
	}
	if strings.TrimSpace(cfg.RemoteAgentID) == "" {
		t.Skip("set GRIX_LIVE_REMOTE_AGENT_ID to run main-agent create-agent dialogue")
	}

	harness := newLiveAgentHarness(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConversationTimeout+2*time.Minute)
	defer cancel()

	if strings.TrimSpace(cfg.OpenClawAgent) != "" && strings.TrimSpace(cfg.OpenClawAccount) != "" {
		summary := harness.runPreflight(ctx)
		require.False(t, summary.hasFailures(), "preflight contains hard failures")
	}

	identity := harness.bootstrapProjectIdentity(ctx)
	mainAgent := harness.resolveConfiguredMainAgent(ctx, identity)
	session := harness.openDirectSessionForAgent(ctx, identity, mainAgent)

	client := newLiveUserWSClient(t, harness, session)
	client.connect(ctx)
	defer client.close()

	createdAgentName := fmt.Sprintf("e2e-dialog-agent-%d", time.Now().Unix())
	beforeMatches := harness.listAgentsByExactName(ctx, identity, createdAgentName)
	require.Empty(t, beforeMatches, "randomized temporary agent name unexpectedly already exists")

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cleanupCancel()

		matches := harness.listAgentsByExactName(cleanupCtx, identity, createdAgentName)
		results := make([]map[string]any, 0, len(matches))
		for _, match := range matches {
			agentID := asString(match["id"])
			if strings.TrimSpace(agentID) == "" {
				continue
			}
			result := harness.deleteAgentByID(cleanupCtx, identity, agentID)
			results = append(results, map[string]any{
				"agent":  match,
				"result": result,
			})
		}
		harness.writeJSON("dialog-agent-delete.json", map[string]any{
			"requested_name": createdAgentName,
			"matches":        matches,
			"results":        results,
		})
	})

	probe, createdMatches := client.runCreateAgentDialogueProbe(
		ctx,
		identity,
		session,
		createdAgentName,
		cfg.ConversationTimeout+90*time.Second,
	)

	require.NotEmpty(t, probe.SendAck, "send_ack should be captured")
	require.True(t, liveProbeHasDeliveryStatus(probe.DeliveryStatuses, "received"), "main agent did not confirm receiving the create-agent instruction")
	require.Len(t, createdMatches, 1, "dialogue create should leave exactly one created agent with the requested name")

	createdAgent := createdMatches[0]
	createdAgentID := asString(createdAgent["id"])
	require.NotEmpty(t, createdAgentID, "created agent should have an id")
	require.True(t, isLiveEligibleGroupAgent(createdAgent), "created agent should be an active openclaw agent")

	createdSession := harness.openDirectSessionForAgent(ctx, identity, createdAgent)
	createdClient := newLiveUserWSClient(t, harness, createdSession)
	createdClient.connect(ctx)
	defer createdClient.close()

	liveMarker := fmt.Sprintf("E2E_CREATED_AGENT_ONLINE_%d", time.Now().Unix())
	createdProbe := createdClient.runConversationProbe(ctx, liveConversationProbeOptions{
		Message:              "请严格只回复：" + liveMarker,
		ExpectedSenderID:     createdAgentID,
		ExpectedTextContains: liveMarker,
		Timeout:              cfg.ConversationTimeout + 30*time.Second,
	})

	harness.writeJSON("dialog-agent-create-summary.json", map[string]any{
		"generated_at":                    time.Now().UTC().Format(time.RFC3339),
		"artifacts_dir":                   harness.artifactDir,
		"session":                         session,
		"main_agent":                      mainAgent,
		"probe":                           probe,
		"requested_name":                  createdAgentName,
		"main_agent_confirmation_seen":    len(probe.AgentPushes) > 0,
		"created_agent_matches":           createdMatches,
		"created_agent":                   createdAgent,
		"created_agent_id":                createdAgentID,
		"created_agent_online_probe":      createdProbe,
		"created_agent_delivery_received": liveProbeHasDeliveryStatus(createdProbe.DeliveryStatuses, "received"),
	})
	t.Logf("dialog create-agent artifacts: %s", harness.artifactDir)
}

func TestLiveAgentGroupManagementByMainAgentDialogue(t *testing.T) {
	cfg := loadLiveAgentConfig(t)
	if !cfg.Enabled {
		t.Skip("set GRIX_LIVE_E2E=1 to enable live local OpenClaw E2E")
	}
	if strings.TrimSpace(cfg.UserToken) == "" && (strings.TrimSpace(cfg.UserAccount) == "" || strings.TrimSpace(cfg.UserPassword) == "") {
		t.Skip("set GRIX_LIVE_USER_TOKEN or both GRIX_LIVE_USER_ACCOUNT / GRIX_LIVE_USER_PASSWORD to run main-agent group management dialogue")
	}
	if strings.TrimSpace(cfg.RemoteAgentID) == "" {
		t.Skip("set GRIX_LIVE_REMOTE_AGENT_ID to run main-agent group management dialogue")
	}

	harness := newLiveAgentHarness(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConversationTimeout+5*time.Minute)
	defer cancel()

	if strings.TrimSpace(cfg.OpenClawAgent) != "" && strings.TrimSpace(cfg.OpenClawAccount) != "" {
		summary := harness.runPreflight(ctx)
		require.False(t, summary.hasFailures(), "preflight contains hard failures")
	}

	identity := harness.bootstrapProjectIdentity(ctx)
	mainAgent := harness.resolveConfiguredMainAgent(ctx, identity)
	mainAgentID := asString(mainAgent["id"])
	targetAgent := harness.resolveEligibleGroupTargetsExcluding(
		ctx,
		identity,
		1,
		"group-management-target-agents",
		mainAgentID,
	)[0]
	targetAgentID := asString(targetAgent["id"])
	targetAgentName := asString(targetAgent["agent_name"])

	session := harness.openDirectSessionForAgent(ctx, identity, mainAgent)
	ownerID := strings.TrimSpace(asString(identity.User["id"]))
	groupName := fmt.Sprintf("e2e_dialog_group_manage_%d", time.Now().Unix())

	var (
		groupSessionID string
		groupDissolved bool
	)
	t.Cleanup(func() {
		if strings.TrimSpace(groupSessionID) == "" || groupDissolved || !harness.cfg.CleanupGroups {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		harness.dissolveGroupSession(cleanupCtx, identity, groupSessionID)
	})

	createMessage := fmt.Sprintf(
		"请你现在只做一件事：创建一个新的测试群，群名必须严格为 %s。要求：1. 只创建这一个群；2. 不要额外拉人；3. 使用正规的群管理能力完成；4. 完成后可以简短确认，但不要做别的动作。",
		groupName,
	)
	createProbe, createVerification := runMainAgentDialogueAction(
		ctx,
		t,
		harness,
		session,
		"dialog-group-create",
		createMessage,
		cfg.ConversationTimeout+45*time.Second,
		func(checkCtx context.Context) (map[string]any, bool) {
			groups := harness.listGroupSessionsByExactTitle(
				checkCtx,
				identity,
				groupName,
				"sessions-list-dialog-group-create",
			)
			if len(groups) != 1 {
				return map[string]any{"groups": groups}, false
			}
			detail := harness.fetchSessionDetail(
				checkCtx,
				identity,
				asString(groups[0]["session_id"]),
				"session-detail-dialog-group-create",
			)
			if member := findSessionDetailMember(detail, mainAgentID, 2); member == nil {
				return map[string]any{
					"groups":  groups,
					"detail":  detail,
					"missing": "main_agent_member",
				}, false
			}
			if ownerID != "" {
				if member := findSessionDetailMember(detail, ownerID, 1); member == nil {
					return map[string]any{
						"groups":  groups,
						"detail":  detail,
						"missing": "owner_member",
					}, false
				}
			}
			return map[string]any{
				"groups":  groups,
				"session": groups[0],
				"detail":  detail,
			}, true
		},
	)
	createSession := requireMapField(t, createVerification, "session")
	groupSessionID = asString(createSession["session_id"])
	require.NotEmpty(t, groupSessionID, "created group session_id should not be empty")

	addMessage := fmt.Sprintf(
		"请你现在只做一件事：把这个 OpenClaw agent 拉进群。群 session_id 必须是 %s，群名是 %s，目标 agent id 是 %s，目标 agent 名称是 %s。要求：1. 只添加这一个 agent；2. 不要移人，不要改角色，不要禁言；3. 使用正规的群管理能力完成。",
		groupSessionID,
		groupName,
		targetAgentID,
		targetAgentName,
	)
	addProbe, addVerification := runMainAgentDialogueAction(
		ctx,
		t,
		harness,
		session,
		"dialog-group-add-member",
		addMessage,
		cfg.ConversationTimeout+45*time.Second,
		func(checkCtx context.Context) (map[string]any, bool) {
			detail := harness.fetchSessionDetail(
				checkCtx,
				identity,
				groupSessionID,
				"session-detail-dialog-group-add",
			)
			member := findSessionDetailMember(detail, targetAgentID, 2)
			if member == nil {
				return map[string]any{"detail": detail}, false
			}
			return map[string]any{
				"detail": detail,
				"member": member,
			}, true
		},
	)

	muteMessage := fmt.Sprintf(
		"请你现在只做一件事：把群 %s 里的这个 agent 设为禁言。目标 agent id 是 %s，名称是 %s。要求：1. 只修改这个成员的禁言状态；2. 不要全员禁言；3. 不要移人，不要解散群；4. 使用正规的群管理能力完成。",
		groupSessionID,
		targetAgentID,
		targetAgentName,
	)
	muteProbe, muteVerification := runMainAgentDialogueAction(
		ctx,
		t,
		harness,
		session,
		"dialog-group-mute-member",
		muteMessage,
		cfg.ConversationTimeout+45*time.Second,
		func(checkCtx context.Context) (map[string]any, bool) {
			detail := harness.fetchSessionDetail(
				checkCtx,
				identity,
				groupSessionID,
				"session-detail-dialog-group-mute",
			)
			member := findSessionDetailMember(detail, targetAgentID, 2)
			if member == nil {
				return map[string]any{"detail": detail}, false
			}
			if !asBool(member["is_speak_muted"]) {
				return map[string]any{
					"detail": detail,
					"member": member,
				}, false
			}
			return map[string]any{
				"detail": detail,
				"member": member,
			}, true
		},
	)

	removeMessage := fmt.Sprintf(
		"请你现在只做一件事：把群 %s 里的这个 agent 移出群。目标 agent id 是 %s，名称是 %s。要求：1. 只移除这一个成员；2. 不要解散群；3. 使用正规的群管理能力完成。",
		groupSessionID,
		targetAgentID,
		targetAgentName,
	)
	removeProbe, removeVerification := runMainAgentDialogueAction(
		ctx,
		t,
		harness,
		session,
		"dialog-group-remove-member",
		removeMessage,
		cfg.ConversationTimeout+45*time.Second,
		func(checkCtx context.Context) (map[string]any, bool) {
			detail := harness.fetchSessionDetail(
				checkCtx,
				identity,
				groupSessionID,
				"session-detail-dialog-group-remove",
			)
			member := findSessionDetailMember(detail, targetAgentID, 2)
			if member != nil {
				return map[string]any{
					"detail": detail,
					"member": member,
				}, false
			}
			return map[string]any{"detail": detail}, true
		},
	)

	dissolveMessage := fmt.Sprintf(
		"请你现在只做一件事：解散这个测试群，群 session_id 必须是 %s，群名是 %s。要求：1. 只解散这一个群；2. 不要新建别的群；3. 使用正规的群管理能力完成。",
		groupSessionID,
		groupName,
	)
	dissolveProbe, dissolveVerification := runMainAgentDialogueAction(
		ctx,
		t,
		harness,
		session,
		"dialog-group-dissolve",
		dissolveMessage,
		cfg.ConversationTimeout+45*time.Second,
		func(checkCtx context.Context) (map[string]any, bool) {
			groups := harness.listGroupSessionsByExactTitle(
				checkCtx,
				identity,
				groupName,
				"sessions-list-dialog-group-dissolve",
			)
			if len(groups) > 0 {
				return map[string]any{"groups": groups}, false
			}
			return map[string]any{"groups": []map[string]any{}}, true
		},
	)
	groupDissolved = true

	harness.writeJSON("dialog-group-management-summary.json", map[string]any{
		"generated_at":  time.Now().UTC().Format(time.RFC3339),
		"artifacts_dir": harness.artifactDir,
		"session":       session,
		"main_agent":    mainAgent,
		"target_agent":  targetAgent,
		"group_name":    groupName,
		"group_session": groupSessionID,
		"steps": map[string]any{
			"create_group": map[string]any{
				"message":      createMessage,
				"probe":        createProbe,
				"verification": createVerification,
			},
			"add_member": map[string]any{
				"message":      addMessage,
				"probe":        addProbe,
				"verification": addVerification,
			},
			"mute_member": map[string]any{
				"message":      muteMessage,
				"probe":        muteProbe,
				"verification": muteVerification,
			},
			"remove_member": map[string]any{
				"message":      removeMessage,
				"probe":        removeProbe,
				"verification": removeVerification,
			},
			"dissolve_group": map[string]any{
				"message":      dissolveMessage,
				"probe":        dissolveProbe,
				"verification": dissolveVerification,
			},
		},
	})
	t.Logf("dialog group-management artifacts: %s", harness.artifactDir)
}

func TestLiveAgentContactLookupByMainAgentDialogue(t *testing.T) {
	cfg := loadLiveAgentConfig(t)
	if !cfg.Enabled {
		t.Skip("set GRIX_LIVE_E2E=1 to enable live local OpenClaw E2E")
	}
	if strings.TrimSpace(cfg.UserToken) == "" && (strings.TrimSpace(cfg.UserAccount) == "" || strings.TrimSpace(cfg.UserPassword) == "") {
		t.Skip("set GRIX_LIVE_USER_TOKEN or both GRIX_LIVE_USER_ACCOUNT / GRIX_LIVE_USER_PASSWORD to run main-agent contact lookup dialogue")
	}
	if strings.TrimSpace(cfg.RemoteAgentID) == "" {
		t.Skip("set GRIX_LIVE_REMOTE_AGENT_ID to run main-agent contact lookup dialogue")
	}

	harness := newLiveAgentHarness(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConversationTimeout+90*time.Second)
	defer cancel()

	if strings.TrimSpace(cfg.OpenClawAgent) != "" && strings.TrimSpace(cfg.OpenClawAccount) != "" {
		summary := harness.runPreflight(ctx)
		require.False(t, summary.hasFailures(), "preflight contains hard failures")
	}

	identity := harness.bootstrapProjectIdentity(ctx)
	mainAgent := harness.resolveConfiguredMainAgent(ctx, identity)
	session := harness.openDirectSessionForAgent(ctx, identity, mainAgent)
	target := selectDialogueContactLookupTarget(ctx, t, harness, identity, asString(mainAgent["id"]))

	responseMarker := fmt.Sprintf("CONTACT_LOOKUP_%d", time.Now().Unix())
	prompt := fmt.Sprintf(
		"请你现在查看通讯录，并搜索关键词 %q。只回复一行，格式必须严格是：%s ID=<peer_id> NAME=<display_name> TYPE=<peer_type>。不要解释，不要编造；如果找不到就明确写 NOT_FOUND。",
		target.Keyword,
		responseMarker,
	)

	client := newLiveUserWSClient(t, harness, session)
	client.connect(ctx)
	defer client.close()

	probe := client.runConversationProbe(ctx, liveConversationProbeOptions{
		Message:              prompt,
		ExpectedSenderID:     session.AgentID,
		ExpectedTextContains: responseMarker,
		Timeout:              cfg.ConversationTimeout + 30*time.Second,
	})

	replyText := extractConversationReplyText(t, probe)
	require.Contains(t, replyText, "ID="+target.PeerID, "contact lookup reply should contain the expected peer_id")
	require.Contains(t, replyText, "NAME="+target.DisplayName, "contact lookup reply should contain the expected display name")
	require.Contains(t, replyText, fmt.Sprintf("TYPE=%d", target.PeerType), "contact lookup reply should contain the expected peer_type")

	harness.writeJSON("dialog-contact-lookup-summary.json", map[string]any{
		"generated_at":  time.Now().UTC().Format(time.RFC3339),
		"artifacts_dir": harness.artifactDir,
		"session":       session,
		"main_agent":    mainAgent,
		"target":        target,
		"prompt":        prompt,
		"probe":         probe,
		"reply_text":    replyText,
	})
	t.Logf("dialog contact-lookup artifacts: %s", harness.artifactDir)
}

func TestLiveAgentConversationHistoryReadByMainAgentDialogue(t *testing.T) {
	cfg := loadLiveAgentConfig(t)
	if !cfg.Enabled {
		t.Skip("set GRIX_LIVE_E2E=1 to enable live local OpenClaw E2E")
	}
	if strings.TrimSpace(cfg.UserToken) == "" && (strings.TrimSpace(cfg.UserAccount) == "" || strings.TrimSpace(cfg.UserPassword) == "") {
		t.Skip("set GRIX_LIVE_USER_TOKEN or both GRIX_LIVE_USER_ACCOUNT / GRIX_LIVE_USER_PASSWORD to run main-agent history dialogue")
	}
	if strings.TrimSpace(cfg.RemoteAgentID) == "" {
		t.Skip("set GRIX_LIVE_REMOTE_AGENT_ID to run main-agent history dialogue")
	}

	harness := newLiveAgentHarness(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConversationTimeout+2*time.Minute)
	defer cancel()

	if strings.TrimSpace(cfg.OpenClawAgent) != "" && strings.TrimSpace(cfg.OpenClawAccount) != "" {
		summary := harness.runPreflight(ctx)
		require.False(t, summary.hasFailures(), "preflight contains hard failures")
	}

	identity := harness.bootstrapProjectIdentity(ctx)
	mainAgent := harness.resolveConfiguredMainAgent(ctx, identity)
	targetAgent := harness.resolveEligibleGroupTargetsExcluding(
		ctx,
		identity,
		1,
		"history-read-target-agents",
		asString(mainAgent["id"]),
	)[0]

	fixtureSession := harness.openDirectSessionForAgent(ctx, identity, targetAgent)
	fixtureClient := newLiveUserWSClient(t, harness, fixtureSession)
	fixtureClient.connect(ctx)

	historyMarker := fmt.Sprintf("E2E_HISTORY_LOOKUP_%d", time.Now().Unix())
	historyFixtureText := "历史读取测试消息：" + historyMarker
	historyAck := fixtureClient.sendText(historyFixtureText, nil)
	fixtureClient.close()

	session := harness.openDirectSessionForAgent(ctx, identity, mainAgent)
	client := newLiveUserWSClient(t, harness, session)
	client.connect(ctx)
	defer client.close()

	prompt := fmt.Sprintf(
		"请你现在查看会话历史。目标 session_id 是 %s。请只回复这条会话里最近一条由用户发送的消息原文，不要解释，不要改写。",
		fixtureSession.SessionID,
	)
	probe := client.runConversationProbe(ctx, liveConversationProbeOptions{
		Message:              prompt,
		ExpectedSenderID:     session.AgentID,
		ExpectedTextContains: historyMarker,
		Timeout:              cfg.ConversationTimeout + 30*time.Second,
	})

	replyText := extractConversationReplyText(t, probe)
	require.Contains(t, replyText, historyFixtureText, "history reply should contain the latest human-sent message text")

	harness.writeJSON("dialog-history-read-summary.json", map[string]any{
		"generated_at":         time.Now().UTC().Format(time.RFC3339),
		"artifacts_dir":        harness.artifactDir,
		"session":              session,
		"main_agent":           mainAgent,
		"target_session":       fixtureSession,
		"target_agent":         targetAgent,
		"history_fixture_text": historyFixtureText,
		"history_send_ack":     historyAck,
		"prompt":               prompt,
		"probe":                probe,
		"reply_text":           replyText,
	})
	t.Logf("dialog history-read artifacts: %s", harness.artifactDir)
}

func TestLiveAgentGroupInfoReadByMainAgentDialogue(t *testing.T) {
	cfg := loadLiveAgentConfig(t)
	if !cfg.Enabled {
		t.Skip("set GRIX_LIVE_E2E=1 to enable live local OpenClaw E2E")
	}
	if strings.TrimSpace(cfg.UserToken) == "" && (strings.TrimSpace(cfg.UserAccount) == "" || strings.TrimSpace(cfg.UserPassword) == "") {
		t.Skip("set GRIX_LIVE_USER_TOKEN or both GRIX_LIVE_USER_ACCOUNT / GRIX_LIVE_USER_PASSWORD to run main-agent group-info dialogue")
	}
	if strings.TrimSpace(cfg.RemoteAgentID) == "" {
		t.Skip("set GRIX_LIVE_REMOTE_AGENT_ID to run main-agent group-info dialogue")
	}

	harness := newLiveAgentHarness(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConversationTimeout+2*time.Minute)
	defer cancel()

	if strings.TrimSpace(cfg.OpenClawAgent) != "" && strings.TrimSpace(cfg.OpenClawAccount) != "" {
		summary := harness.runPreflight(ctx)
		require.False(t, summary.hasFailures(), "preflight contains hard failures")
	}

	identity := harness.bootstrapProjectIdentity(ctx)
	mainAgent := harness.resolveConfiguredMainAgent(ctx, identity)
	targetAgent := harness.resolveEligibleGroupTargetsExcluding(
		ctx,
		identity,
		1,
		"group-info-target-agents",
		asString(mainAgent["id"]),
	)[0]

	groupName := fmt.Sprintf("e2e_dialog_group_info_%d", time.Now().Unix())
	group := harness.createIsolatedGroupSessionWithAgents(
		ctx,
		identity,
		groupName,
		[]map[string]any{mainAgent, targetAgent},
	)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		harness.dissolveGroupSession(cleanupCtx, identity, group.SessionID)
	})

	session := harness.openDirectSessionForAgent(ctx, identity, mainAgent)
	client := newLiveUserWSClient(t, harness, session)
	client.connect(ctx)
	defer client.close()

	responseMarker := fmt.Sprintf("GROUP_INFO_%d", time.Now().Unix())
	expectedMemberCount := len(group.AgentIDs) + 1
	prompt := fmt.Sprintf(
		"请你现在查看群信息。目标群 session_id 是 %s。只回复一行，格式必须严格是：%s GROUP=<group_name> COUNT=<member_count> MEMBER=<target_member_name>。其中 target_member_name 指成员 id %s 对应的名称。不要解释，不要编造。",
		group.SessionID,
		responseMarker,
		asString(targetAgent["id"]),
	)

	probe := client.runConversationProbe(ctx, liveConversationProbeOptions{
		Message:              prompt,
		ExpectedSenderID:     session.AgentID,
		ExpectedTextContains: responseMarker,
		Timeout:              cfg.ConversationTimeout + 30*time.Second,
	})

	replyText := extractConversationReplyText(t, probe)
	require.Contains(t, replyText, "GROUP="+groupName, "group info reply should contain the group name")
	require.Contains(t, replyText, fmt.Sprintf("COUNT=%d", expectedMemberCount), "group info reply should contain the member count")
	require.Contains(t, replyText, "MEMBER="+asString(targetAgent["agent_name"]), "group info reply should contain the target member name")

	harness.writeJSON("dialog-group-info-read-summary.json", map[string]any{
		"generated_at":          time.Now().UTC().Format(time.RFC3339),
		"artifacts_dir":         harness.artifactDir,
		"session":               session,
		"main_agent":            mainAgent,
		"group":                 group,
		"target_agent":          targetAgent,
		"expected_member_count": expectedMemberCount,
		"prompt":                prompt,
		"probe":                 probe,
		"reply_text":            replyText,
	})
	t.Logf("dialog group-info artifacts: %s", harness.artifactDir)
}

func TestLiveAgentCategoryCRUDByMainAgentDialogue(t *testing.T) {
	cfg := loadLiveAgentConfig(t)
	if !cfg.Enabled {
		t.Skip("set GRIX_LIVE_E2E=1 to enable live local OpenClaw E2E")
	}
	if strings.TrimSpace(cfg.UserToken) == "" && (strings.TrimSpace(cfg.UserAccount) == "" || strings.TrimSpace(cfg.UserPassword) == "") {
		t.Skip("set GRIX_LIVE_USER_TOKEN or both GRIX_LIVE_USER_ACCOUNT / GRIX_LIVE_USER_PASSWORD to run main-agent category CRUD dialogue")
	}
	if strings.TrimSpace(cfg.RemoteAgentID) == "" {
		t.Skip("set GRIX_LIVE_REMOTE_AGENT_ID to run main-agent category CRUD dialogue")
	}

	harness := newLiveAgentHarness(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConversationTimeout+3*time.Minute)
	defer cancel()

	if strings.TrimSpace(cfg.OpenClawAgent) != "" && strings.TrimSpace(cfg.OpenClawAccount) != "" {
		summary := harness.runPreflight(ctx)
		require.False(t, summary.hasFailures(), "preflight contains hard failures")
	}

	identity := harness.bootstrapProjectIdentity(ctx)
	mainAgent := harness.resolveConfiguredMainAgent(ctx, identity)
	mainAgentID := asString(mainAgent["id"])
	session := harness.openDirectSessionForAgent(ctx, identity, mainAgent)

	scopesPayload := harness.fetchAgentScopes(ctx, identity, mainAgentID, "main-agent-category-scopes")
	require.Empty(
		t,
		missingScopeNames(scopesPayload, "agent.category.list", "agent.category.create", "agent.category.update"),
		"main agent is missing required category scopes",
	)

	categoryName := fmt.Sprintf("e2e_dialog_category_%d", time.Now().Unix())
	updatedCategoryName := categoryName + "_renamed"
	createSortOrder := 31
	updateSortOrder := 73

	var createdCategoryID string
	t.Cleanup(func() {
		if strings.TrimSpace(createdCategoryID) == "" {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		harness.deleteAgentCategory(cleanupCtx, identity, createdCategoryID, "cleanup-category-delete-"+sanitizeFileName(createdCategoryID))
	})

	createProbe, createVerification, createdCategory := createAgentCategoryByMainAgentDialogue(
		ctx,
		t,
		harness,
		identity,
		session,
		categoryName,
		createSortOrder,
		cfg.ConversationTimeout+45*time.Second,
	)
	createdCategoryID = asString(createdCategory["id"])
	require.NotEmpty(t, createdCategoryID, "created category id should not be empty")

	client := newLiveUserWSClient(t, harness, session)
	client.connect(ctx)
	defer client.close()

	createLookupMarker := fmt.Sprintf("CATEGORY_LOOKUP_CREATE_%d", time.Now().Unix())
	createLookupPrompt := fmt.Sprintf(
		"请你现在通过 grix-admin 查看 agent 分类列表，并找到名称严格为 %s 的分类。只回复一行，格式必须严格是：%s ID=<id> NAME=<name> PARENT=<parent_id> SORT=<sort_order>。不要解释，不要编造。",
		categoryName,
		createLookupMarker,
	)
	createLookupProbe := client.runConversationProbe(ctx, liveConversationProbeOptions{
		Message:              createLookupPrompt,
		ExpectedSenderID:     session.AgentID,
		ExpectedTextContains: createLookupMarker,
		Timeout:              cfg.ConversationTimeout + 30*time.Second,
	})
	createLookupReply := extractConversationReplyText(t, createLookupProbe)
	require.Contains(t, createLookupReply, "ID="+createdCategoryID, "category lookup should contain the created category id")
	require.Contains(t, createLookupReply, "NAME="+categoryName, "category lookup should contain the created category name")
	require.Contains(t, createLookupReply, "PARENT=0", "category lookup should report root parent")
	require.Contains(t, createLookupReply, fmt.Sprintf("SORT=%d", createSortOrder), "category lookup should report the create sort order")

	updateMessage := fmt.Sprintf(
		"请你现在只做一件事：通过 grix-admin 修改一个现有 agent 分类。分类 id 必须是 %s；新的名称必须严格为 %s；parent_id 必须保持 0；sort_order 必须改为 %d。要求：1. 使用正规的 agent 分类管理能力完成；2. 不要创建新分类；3. 完成后可以简短确认，但不要做别的动作。",
		createdCategoryID,
		updatedCategoryName,
		updateSortOrder,
	)
	updateProbe, updateVerification := runMainAgentDialogueAction(
		ctx,
		t,
		harness,
		session,
		"dialog-category-update",
		updateMessage,
		cfg.ConversationTimeout+45*time.Second,
		func(checkCtx context.Context) (map[string]any, bool) {
			matches := harness.listAgentCategoriesByExactName(
				checkCtx,
				identity,
				updatedCategoryName,
				"categories-list-dialog-update",
			)
			if len(matches) != 1 {
				return map[string]any{"matches": matches}, false
			}
			if !agentCategoryMatches(matches[0], createdCategoryID, updatedCategoryName, 0, updateSortOrder) {
				return map[string]any{"category": matches[0]}, false
			}
			return map[string]any{
				"category": matches[0],
				"matches":  matches,
			}, true
		},
	)

	updateLookupMarker := fmt.Sprintf("CATEGORY_LOOKUP_UPDATE_%d", time.Now().Unix())
	updateLookupPrompt := fmt.Sprintf(
		"请你现在通过 grix-admin 查看 agent 分类列表，并找到 id 严格为 %s 的分类。只回复一行，格式必须严格是：%s ID=<id> NAME=<name> PARENT=<parent_id> SORT=<sort_order>。不要解释，不要编造。",
		createdCategoryID,
		updateLookupMarker,
	)
	updateLookupProbe := client.runConversationProbe(ctx, liveConversationProbeOptions{
		Message:              updateLookupPrompt,
		ExpectedSenderID:     session.AgentID,
		ExpectedTextContains: updateLookupMarker,
		Timeout:              cfg.ConversationTimeout + 30*time.Second,
	})
	updateLookupReply := extractConversationReplyText(t, updateLookupProbe)
	require.Contains(t, updateLookupReply, "ID="+createdCategoryID, "updated category lookup should contain the category id")
	require.Contains(t, updateLookupReply, "NAME="+updatedCategoryName, "updated category lookup should contain the updated category name")
	require.Contains(t, updateLookupReply, "PARENT=0", "updated category lookup should keep root parent")
	require.Contains(t, updateLookupReply, fmt.Sprintf("SORT=%d", updateSortOrder), "updated category lookup should report the updated sort order")

	harness.writeJSON("dialog-category-crud-summary.json", map[string]any{
		"generated_at":          time.Now().UTC().Format(time.RFC3339),
		"artifacts_dir":         harness.artifactDir,
		"session":               session,
		"main_agent":            mainAgent,
		"main_agent_scopes":     scopesPayload,
		"created_category_id":   createdCategoryID,
		"created_category_name": categoryName,
		"updated_category_name": updatedCategoryName,
		"create_sort_order":     createSortOrder,
		"update_sort_order":     updateSortOrder,
		"create_probe":          createProbe,
		"create_verification":   createVerification,
		"create_lookup_prompt":  createLookupPrompt,
		"create_lookup_probe":   createLookupProbe,
		"create_lookup_reply":   createLookupReply,
		"update_message":        updateMessage,
		"update_probe":          updateProbe,
		"update_verification":   updateVerification,
		"update_lookup_prompt":  updateLookupPrompt,
		"update_lookup_probe":   updateLookupProbe,
		"update_lookup_reply":   updateLookupReply,
	})
	t.Logf("dialog category-crud artifacts: %s", harness.artifactDir)
}

func TestLiveAgentCategoryAssignmentByMainAgentDialogue(t *testing.T) {
	cfg := loadLiveAgentConfig(t)
	if !cfg.Enabled {
		t.Skip("set GRIX_LIVE_E2E=1 to enable live local OpenClaw E2E")
	}
	if strings.TrimSpace(cfg.UserToken) == "" && (strings.TrimSpace(cfg.UserAccount) == "" || strings.TrimSpace(cfg.UserPassword) == "") {
		t.Skip("set GRIX_LIVE_USER_TOKEN or both GRIX_LIVE_USER_ACCOUNT / GRIX_LIVE_USER_PASSWORD to run main-agent category assignment dialogue")
	}
	if strings.TrimSpace(cfg.RemoteAgentID) == "" {
		t.Skip("set GRIX_LIVE_REMOTE_AGENT_ID to run main-agent category assignment dialogue")
	}

	harness := newLiveAgentHarness(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConversationTimeout+4*time.Minute)
	defer cancel()

	if strings.TrimSpace(cfg.OpenClawAgent) != "" && strings.TrimSpace(cfg.OpenClawAccount) != "" {
		summary := harness.runPreflight(ctx)
		require.False(t, summary.hasFailures(), "preflight contains hard failures")
	}

	identity := harness.bootstrapProjectIdentity(ctx)
	mainAgent := harness.resolveConfiguredMainAgent(ctx, identity)
	mainAgentID := asString(mainAgent["id"])
	session := harness.openDirectSessionForAgent(ctx, identity, mainAgent)
	targetAgent := harness.resolveEligibleGroupTargetsExcluding(
		ctx,
		identity,
		1,
		"category-assignment-target-agents",
		mainAgentID,
	)[0]
	targetAgentID := asString(targetAgent["id"])
	targetAgentName := asString(targetAgent["agent_name"])

	scopesPayload := harness.fetchAgentScopes(ctx, identity, mainAgentID, "main-agent-category-assignment-scopes")
	require.Empty(
		t,
		missingScopeNames(scopesPayload, "agent.category.create", "agent.category.assign"),
		"main agent is missing required category assignment scopes",
	)

	targetBefore := harness.fetchAgentByID(ctx, identity, targetAgentID, "category-assignment-target-before")
	originalCategoryID, err := parseFlexibleInt64(targetBefore["category_id"])
	require.NoError(t, err, "target agent category_id should be parseable")

	firstCategoryName := fmt.Sprintf("e2e_assign_category_a_%d", time.Now().Unix())
	secondCategoryName := fmt.Sprintf("e2e_assign_category_b_%d", time.Now().Unix())

	var createdCategoryIDs []string
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cleanupCancel()

		harness.updateAgentCategory(
			cleanupCtx,
			identity,
			targetAgentID,
			originalCategoryID,
			"cleanup-category-restore-"+sanitizeFileName(targetAgentID),
		)
		for i := len(createdCategoryIDs) - 1; i >= 0; i-- {
			categoryID := strings.TrimSpace(createdCategoryIDs[i])
			if categoryID == "" {
				continue
			}
			harness.deleteAgentCategory(
				cleanupCtx,
				identity,
				categoryID,
				"cleanup-category-delete-"+sanitizeFileName(categoryID),
			)
		}
	})

	firstCreateProbe, firstCreateVerification, firstCategory := createAgentCategoryByMainAgentDialogue(
		ctx,
		t,
		harness,
		identity,
		session,
		firstCategoryName,
		41,
		cfg.ConversationTimeout+45*time.Second,
	)
	firstCategoryID := asString(firstCategory["id"])
	createdCategoryIDs = append(createdCategoryIDs, firstCategoryID)

	secondCreateProbe, secondCreateVerification, secondCategory := createAgentCategoryByMainAgentDialogue(
		ctx,
		t,
		harness,
		identity,
		session,
		secondCategoryName,
		42,
		cfg.ConversationTimeout+45*time.Second,
	)
	secondCategoryID := asString(secondCategory["id"])
	createdCategoryIDs = append(createdCategoryIDs, secondCategoryID)

	assignMessage := fmt.Sprintf(
		"请你现在只做一件事：通过 grix-admin 给一个 agent 设置分类。目标 agent id 必须是 %s，目标 agent 名称是 %s，目标 category_id 必须是 %s，目标分类名称是 %s。要求：1. 只修改这个 agent 的分类；2. 使用正规的 agent 分类管理能力完成；3. 不要改别的 agent，不要创建新分类。",
		targetAgentID,
		targetAgentName,
		firstCategoryID,
		firstCategoryName,
	)
	assignProbe, assignVerification := runMainAgentDialogueAction(
		ctx,
		t,
		harness,
		session,
		"dialog-agent-category-assign",
		assignMessage,
		cfg.ConversationTimeout+45*time.Second,
		func(checkCtx context.Context) (map[string]any, bool) {
			agent := harness.fetchAgentByID(
				checkCtx,
				identity,
				targetAgentID,
				"agent-detail-dialog-category-assign",
			)
			if asString(agent["category_id"]) != firstCategoryID {
				return map[string]any{"agent": agent}, false
			}
			filtered := harness.listAgentsByCategoryID(
				checkCtx,
				identity,
				firstCategoryID,
				"agents-list-dialog-category-assign",
			)
			return map[string]any{
				"agent":           agent,
				"filtered_agents": filtered,
			}, true
		},
	)

	reassignMessage := fmt.Sprintf(
		"请你现在只做一件事：通过 grix-admin 把这个 agent 的分类改到另一个分类。目标 agent id 必须是 %s，目标 agent 名称是 %s，新的 category_id 必须是 %s，新的分类名称是 %s。要求：1. 只修改这个 agent 的分类；2. 使用正规的 agent 分类管理能力完成；3. 不要改别的 agent，不要创建新分类。",
		targetAgentID,
		targetAgentName,
		secondCategoryID,
		secondCategoryName,
	)
	reassignProbe, reassignVerification := runMainAgentDialogueAction(
		ctx,
		t,
		harness,
		session,
		"dialog-agent-category-reassign",
		reassignMessage,
		cfg.ConversationTimeout+45*time.Second,
		func(checkCtx context.Context) (map[string]any, bool) {
			agent := harness.fetchAgentByID(
				checkCtx,
				identity,
				targetAgentID,
				"agent-detail-dialog-category-reassign",
			)
			if asString(agent["category_id"]) != secondCategoryID {
				return map[string]any{"agent": agent}, false
			}
			filtered := harness.listAgentsByCategoryID(
				checkCtx,
				identity,
				secondCategoryID,
				"agents-list-dialog-category-reassign",
			)
			return map[string]any{
				"agent":           agent,
				"filtered_agents": filtered,
			}, true
		},
	)

	targetAfter := harness.fetchAgentByID(ctx, identity, targetAgentID, "category-assignment-target-after")
	require.Equal(t, secondCategoryID, asString(targetAfter["category_id"]), "target agent should end up in the reassigned category")

	harness.writeJSON("dialog-category-assignment-summary.json", map[string]any{
		"generated_at":               time.Now().UTC().Format(time.RFC3339),
		"artifacts_dir":              harness.artifactDir,
		"session":                    session,
		"main_agent":                 mainAgent,
		"main_agent_scopes":          scopesPayload,
		"target_agent":               targetAgent,
		"target_agent_before":        targetBefore,
		"target_agent_after":         targetAfter,
		"original_category_id":       originalCategoryID,
		"first_category_name":        firstCategoryName,
		"first_category_id":          firstCategoryID,
		"first_create_probe":         firstCreateProbe,
		"first_create_verification":  firstCreateVerification,
		"second_category_name":       secondCategoryName,
		"second_category_id":         secondCategoryID,
		"second_create_probe":        secondCreateProbe,
		"second_create_verification": secondCreateVerification,
		"assign_message":             assignMessage,
		"assign_probe":               assignProbe,
		"assign_verification":        assignVerification,
		"reassign_message":           reassignMessage,
		"reassign_probe":             reassignProbe,
		"reassign_verification":      reassignVerification,
	})
	t.Logf("dialog category-assignment artifacts: %s", harness.artifactDir)
}

func TestLiveAgentMultiAgentGroupRouting(t *testing.T) {
	cfg := loadLiveAgentConfig(t)
	if !cfg.Enabled {
		t.Skip("set GRIX_LIVE_E2E=1 to enable live local OpenClaw E2E")
	}
	if strings.TrimSpace(cfg.UserToken) == "" && (strings.TrimSpace(cfg.UserAccount) == "" || strings.TrimSpace(cfg.UserPassword) == "") {
		t.Skip("set GRIX_LIVE_USER_TOKEN or both GRIX_LIVE_USER_ACCOUNT / GRIX_LIVE_USER_PASSWORD to run multi-agent group routing")
	}

	harness := newLiveAgentHarness(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConversationTimeout+2*time.Minute)
	defer cancel()

	if strings.TrimSpace(cfg.OpenClawAgent) != "" && strings.TrimSpace(cfg.OpenClawAccount) != "" {
		summary := harness.runPreflight(ctx)
		require.False(t, summary.hasFailures(), "preflight contains hard failures")
	}

	session := harness.bootstrapProjectIdentity(ctx)
	targetAgents := harness.resolveMultiAgentGroupTargets(ctx, session, 3)
	group := harness.createIsolatedGroupSessionWithAgents(
		ctx,
		session,
		fmt.Sprintf("e2e_multi_group_%d", time.Now().Unix()),
		targetAgents,
	)
	groupCleanupMode := harness.configureGroupCleanup(t, session, group)
	receiveSettings := harness.updateGroupAgentReceiveModes(
		ctx,
		session,
		group.SessionID,
		group.AgentIDs,
		agentreceive.ModeMentionOnly,
	)

	groupSession := session
	groupSession.SessionID = group.SessionID
	client := newLiveUserWSClient(t, harness, groupSession)
	client.connect(ctx)
	defer client.close()

	agentA := group.AgentIDs[0]
	agentB := group.AgentIDs[1]
	agentC := group.AgentIDs[2]

	quietWindow := 12 * time.Second

	case1Marker := fmt.Sprintf("E2E_MULTI_GROUP_AT_ONE_%d", time.Now().Unix())
	case1 := client.runMultiAgentConversationProbe(ctx, liveMultiConversationProbeOptions{
		Message:              fmt.Sprintf("@%s 请严格只回复：%s", agentA, case1Marker),
		Extra:                map[string]any{"explicit_mention_user_ids": []string{agentA}},
		ExpectedSenderIDs:    []string{agentA},
		ForbiddenSenderIDs:   []string{agentB, agentC},
		ExpectedTextContains: case1Marker,
		Timeout:              cfg.ConversationTimeout,
		PostMatchQuietWindow: quietWindow,
	})
	quotedOwnerMsgID := extractAgentReplyMsgID(t, case1, agentA)

	case2Marker := fmt.Sprintf("E2E_MULTI_GROUP_QUOTE_OWNER_%d", time.Now().Unix())
	case2 := client.runMultiAgentConversationProbe(ctx, liveMultiConversationProbeOptions{
		Message:              "请严格只回复：" + case2Marker,
		QuotedMessageID:      quotedOwnerMsgID,
		ExpectedSenderIDs:    []string{agentA},
		ForbiddenSenderIDs:   []string{agentB, agentC},
		ExpectedTextContains: case2Marker,
		Timeout:              cfg.ConversationTimeout,
		PostMatchQuietWindow: quietWindow,
	})

	case3Marker := fmt.Sprintf("E2E_MULTI_GROUP_QUOTE_OVERRIDE_%d", time.Now().Unix())
	case3 := client.runMultiAgentConversationProbe(ctx, liveMultiConversationProbeOptions{
		Message:              fmt.Sprintf("@%s 请严格只回复：%s", agentB, case3Marker),
		Extra:                map[string]any{"explicit_mention_user_ids": []string{agentB}},
		QuotedMessageID:      quotedOwnerMsgID,
		ExpectedSenderIDs:    []string{agentB},
		ForbiddenSenderIDs:   []string{agentA, agentC},
		ExpectedTextContains: case3Marker,
		Timeout:              cfg.ConversationTimeout,
		PostMatchQuietWindow: quietWindow,
	})

	case4Marker := fmt.Sprintf("E2E_MULTI_GROUP_MENTION_ALL_%d", time.Now().Unix())
	case4 := client.runMultiAgentConversationProbe(ctx, liveMultiConversationProbeOptions{
		Message:              "@所有人 请各自严格只回复：" + case4Marker,
		Extra:                map[string]any{"mention_all": true},
		ExpectedSenderIDs:    []string{agentA, agentB, agentC},
		ExpectedTextContains: case4Marker,
		Timeout:              cfg.ConversationTimeout + 20*time.Second,
		PostMatchQuietWindow: 5 * time.Second,
	})

	harness.writeJSON("multi-agent-group-routing-summary.json", map[string]any{
		"generated_at":    time.Now().UTC().Format(time.RFC3339),
		"artifacts_dir":   harness.artifactDir,
		"session":         groupSession,
		"group":           group,
		"group_cleanup":   groupCleanupMode,
		"receive_setting": receiveSettings,
		"scenarios": map[string]any{
			"mention_single_agent": map[string]any{
				"marker": case1Marker,
				"probe":  case1,
			},
			"quote_message_owner": map[string]any{
				"marker":            case2Marker,
				"quoted_message_id": quotedOwnerMsgID,
				"probe":             case2,
			},
			"quote_with_explicit_override": map[string]any{
				"marker":            case3Marker,
				"quoted_message_id": quotedOwnerMsgID,
				"probe":             case3,
			},
			"mention_all_agents": map[string]any{
				"marker": case4Marker,
				"probe":  case4,
			},
		},
	})
	t.Logf("multi-agent group routing artifacts: %s", harness.artifactDir)
}

func (c *liveUserWSClient) runCreateAgentDialogueProbe(
	ctx context.Context,
	identity liveProjectSession,
	session liveProjectSession,
	createdAgentName string,
	timeout time.Duration,
) (liveConversationResult, []map[string]any) {
	c.t.Helper()

	message := fmt.Sprintf(
		"请你现在只做一件事：用正规能力创建一个新的 OpenClaw agent，并让它真正上线可对话。名称必须严格为 %s。要求：1. 不要创建 Claude 或 Hermes agent；2. 如果需要先创建远端 API agent，再继续完成本地 OpenClaw 配置和绑定；3. 最终必须让这个新 agent 在项目侧列表里显示为 openclaw 且状态正常；4. 完成后只回复一段简短确认，并且必须包含这个名称；5. 不要创建群，不要做别的动作，不要重复创建。",
		createdAgentName,
	)

	sendAck := c.sendMessage(liveSendMessageOptions{
		Content: message,
	})
	result := liveConversationResult{
		TriggerMsgID: asString(sendAck["msg_id"]),
		SendAck:      sendAck,
	}

	deadline := time.Now().Add(timeout)
	nextHistoryCheckAt := time.Now()
	nextAgentPollAt := time.Now()
	seenReplies := make(map[string]struct{}, 4)
	var latestMatches []map[string]any

	recordReply := func(payload map[string]any, matchSource string) {
		if payload == nil {
			return
		}
		key := liveMessageDedupKey(payload)
		if key != "" {
			if _, ok := seenReplies[key]; ok {
				return
			}
			seenReplies[key] = struct{}{}
		}
		row := cloneMap(payload)
		if matchSource != "" {
			row["_match_source"] = matchSource
		}
		result.AgentPushes = append(result.AgentPushes, row)
	}

	captureMainReplyFromHistory := func(force bool) {
		if !force || strings.TrimSpace(result.TriggerMsgID) == "" {
			return
		}
		matches := c.findHistoryAgentMessages(ctx, result.TriggerMsgID, createdAgentName)
		for _, row := range matches {
			if asString(row["sender_id"]) != session.AgentID {
				continue
			}
			recordReply(row, "history")
		}
	}

	pollCreatedAgents := func(force bool) ([]map[string]any, bool) {
		if !force {
			return latestMatches, false
		}
		matches := c.harness.listAgentsByExactName(ctx, identity, createdAgentName)
		latestMatches = cloneMaps(matches)
		if len(matches) != 1 {
			return latestMatches, false
		}
		if !isLiveEligibleGroupAgent(matches[0]) {
			return latestMatches, false
		}
		return latestMatches, true
	}

	for {
		select {
		case <-ctx.Done():
			c.t.Fatalf("create-agent dialogue probe canceled: %v", ctx.Err())
		default:
		}

		now := time.Now()
		if now.After(deadline) {
			captureMainReplyFromHistory(true)
			latestMatches, _ = pollCreatedAgents(true)
			return result, latestMatches
		}

		if now.After(nextAgentPollAt) {
			var ready bool
			latestMatches, ready = pollCreatedAgents(true)
			nextAgentPollAt = time.Now().Add(2 * time.Second)
			if ready {
				captureMainReplyFromHistory(true)
				return result, latestMatches
			}
		}

		waitWindow := minDuration(4*time.Second, time.Until(deadline))
		packet, payload, err := c.readPacketMaybe(waitWindow)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				captureMainReplyFromHistory(time.Now().After(nextHistoryCheckAt))
				nextHistoryCheckAt = time.Now().Add(2 * time.Second)
				c.reconnect(ctx)
				continue
			}
			c.t.Fatalf("read websocket packet failed while waiting for create-agent result: %v", err)
		}

		result.Events = append(result.Events, map[string]any{
			"cmd":         packet.Cmd,
			"seq":         packet.Seq,
			"payload":     payload,
			"observed_at": time.Now().UnixMilli(),
		})

		switch packet.Cmd {
		case protocol.CmdAgentDeliveryStatus:
			if asString(payload["session_id"]) == c.session.SessionID && asString(payload["trigger_msg_id"]) == result.TriggerMsgID {
				result.DeliveryStatuses = append(result.DeliveryStatuses, payload)
				captureMainReplyFromHistory(time.Now().After(nextHistoryCheckAt))
				nextHistoryCheckAt = time.Now().Add(2 * time.Second)
			}
		case protocol.CmdAgentOutputStatus:
			if asString(payload["session_id"]) == c.session.SessionID && asString(payload["trigger_msg_id"]) == result.TriggerMsgID {
				result.OutputStatuses = append(result.OutputStatuses, payload)
				captureMainReplyFromHistory(time.Now().After(nextHistoryCheckAt))
				nextHistoryCheckAt = time.Now().Add(2 * time.Second)
			}
		case protocol.CmdPushMsg, protocol.CmdPushEdit, protocol.CmdStreamFinish:
			if asString(payload["session_id"]) != c.session.SessionID {
				continue
			}
			if asString(payload["sender_id"]) != session.AgentID {
				continue
			}
			content := extractPacketContent(payload)
			if !strings.Contains(content, createdAgentName) {
				continue
			}
			recordReply(payload, "ws")
		case protocol.CmdStreamChunk:
			if asString(payload["session_id"]) != c.session.SessionID {
				continue
			}
			if asString(payload["sender_id"]) != session.AgentID {
				continue
			}
			captureMainReplyFromHistory(time.Now().After(nextHistoryCheckAt))
			nextHistoryCheckAt = time.Now().Add(2 * time.Second)
		}
	}
}

func liveProbeHasDeliveryStatus(statuses []map[string]any, expected string) bool {
	for _, item := range statuses {
		if asString(item["status"]) == expected {
			return true
		}
	}
	return false
}

func cloneMaps(items []map[string]any) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, cloneMap(item))
	}
	return result
}

func runMainAgentDialogueAction(
	ctx context.Context,
	t *testing.T,
	harness *liveAgentHarness,
	session liveProjectSession,
	label string,
	message string,
	timeout time.Duration,
	verify func(context.Context) (map[string]any, bool),
) (liveConversationResult, map[string]any) {
	t.Helper()

	client := newLiveUserWSClient(t, harness, session)
	client.connect(ctx)
	probe := client.sendDialogueActionAndWaitReceived(ctx, message, minDuration(timeout, 45*time.Second))
	client.close()

	deadline := time.Now().Add(timeout)
	seenReplies := make(map[string]struct{}, len(probe.AgentPushes))
	for _, item := range probe.AgentPushes {
		key := liveMessageDedupKey(item)
		if key == "" {
			continue
		}
		seenReplies[key] = struct{}{}
	}

	var latestVerification map[string]any
	for {
		captureDialogueHistoryReplies(ctx, client, probe.TriggerMsgID, session.AgentID, &probe, seenReplies)

		checkCtx, checkCancel := context.WithTimeout(ctx, 15*time.Second)
		verification, ok := verify(checkCtx)
		checkCancel()
		if verification != nil {
			latestVerification = verification
		}
		if ok {
			harness.writeJSON(label+".json", map[string]any{
				"generated_at":  time.Now().UTC().Format(time.RFC3339),
				"artifacts_dir": harness.artifactDir,
				"message":       message,
				"probe":         probe,
				"verification":  latestVerification,
			})
			return probe, latestVerification
		}
		if time.Now().After(deadline) {
			harness.writeJSON(label+"-failure.json", map[string]any{
				"generated_at":  time.Now().UTC().Format(time.RFC3339),
				"artifacts_dir": harness.artifactDir,
				"message":       message,
				"probe":         probe,
				"verification":  latestVerification,
			})
			t.Fatalf("timed out waiting for main-agent dialogue action %s to take effect; artifacts=%s", label, harness.artifactDir)
		}

		select {
		case <-ctx.Done():
			t.Fatalf("dialogue action %s canceled: %v", label, ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

func (c *liveUserWSClient) sendDialogueActionAndWaitReceived(
	ctx context.Context,
	message string,
	timeout time.Duration,
) liveConversationResult {
	c.t.Helper()

	sendAck := c.sendMessage(liveSendMessageOptions{
		Content: message,
	})
	result := liveConversationResult{
		TriggerMsgID: asString(sendAck["msg_id"]),
		SendAck:      sendAck,
	}

	deadline := time.Now().Add(timeout)
	seenReplies := make(map[string]struct{}, 4)
	for {
		select {
		case <-ctx.Done():
			c.t.Fatalf("dialogue action canceled: %v", ctx.Err())
		default:
		}

		if time.Now().After(deadline) {
			c.captureAgentHistoryReplies(ctx, result.TriggerMsgID, c.session.AgentID, &result, seenReplies)
			c.t.Fatalf("timed out waiting for main agent to receive dialogue action")
		}

		waitWindow := minDuration(4*time.Second, time.Until(deadline))
		packet, payload, err := c.readPacketMaybe(waitWindow)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				c.captureAgentHistoryReplies(ctx, result.TriggerMsgID, c.session.AgentID, &result, seenReplies)
				c.reconnect(ctx)
				continue
			}
			c.t.Fatalf("read websocket packet failed while waiting for main agent delivery: %v", err)
		}

		result.Events = append(result.Events, map[string]any{
			"cmd":         packet.Cmd,
			"seq":         packet.Seq,
			"payload":     payload,
			"observed_at": time.Now().UnixMilli(),
		})

		switch packet.Cmd {
		case protocol.CmdAgentDeliveryStatus:
			if asString(payload["session_id"]) != c.session.SessionID || asString(payload["trigger_msg_id"]) != result.TriggerMsgID {
				continue
			}
			result.DeliveryStatuses = append(result.DeliveryStatuses, payload)
			if status := asString(payload["status"]); status == "received" || status == "responded" {
				c.captureAgentHistoryReplies(ctx, result.TriggerMsgID, c.session.AgentID, &result, seenReplies)
				return result
			}
		case protocol.CmdAgentOutputStatus:
			if asString(payload["session_id"]) != c.session.SessionID || asString(payload["trigger_msg_id"]) != result.TriggerMsgID {
				continue
			}
			result.OutputStatuses = append(result.OutputStatuses, payload)
		case protocol.CmdPushMsg, protocol.CmdPushEdit, protocol.CmdStreamFinish:
			if asString(payload["session_id"]) != c.session.SessionID {
				continue
			}
			if asString(payload["sender_id"]) != c.session.AgentID {
				continue
			}
			appendUniqueConversationReply(&result, payload, "ws", seenReplies)
		case protocol.CmdStreamChunk:
			if asString(payload["session_id"]) != c.session.SessionID {
				continue
			}
			if asString(payload["sender_id"]) != c.session.AgentID {
				continue
			}
			c.captureAgentHistoryReplies(ctx, result.TriggerMsgID, c.session.AgentID, &result, seenReplies)
		}
	}
}

func (c *liveUserWSClient) captureAgentHistoryReplies(
	ctx context.Context,
	triggerMsgID string,
	senderID string,
	result *liveConversationResult,
	seenReplies map[string]struct{},
) {
	c.t.Helper()
	if strings.TrimSpace(triggerMsgID) == "" {
		return
	}
	matches := c.findHistoryAgentMessages(ctx, triggerMsgID, "")
	for _, row := range matches {
		if strings.TrimSpace(senderID) != "" && asString(row["sender_id"]) != strings.TrimSpace(senderID) {
			continue
		}
		appendUniqueConversationReply(result, row, "history", seenReplies)
	}
}

func captureDialogueHistoryReplies(
	ctx context.Context,
	client *liveUserWSClient,
	triggerMsgID string,
	senderID string,
	result *liveConversationResult,
	seenReplies map[string]struct{},
) {
	client.captureAgentHistoryReplies(ctx, triggerMsgID, senderID, result, seenReplies)
}

func appendUniqueConversationReply(
	result *liveConversationResult,
	payload map[string]any,
	matchSource string,
	seenReplies map[string]struct{},
) {
	if payload == nil {
		return
	}
	key := liveMessageDedupKey(payload)
	if key != "" {
		if _, ok := seenReplies[key]; ok {
			return
		}
		seenReplies[key] = struct{}{}
	}
	row := cloneMap(payload)
	if matchSource != "" {
		row["_match_source"] = matchSource
	}
	result.AgentPushes = append(result.AgentPushes, row)
}

func extractConversationReplyText(t *testing.T, probe liveConversationResult) string {
	t.Helper()
	require.NotEmpty(t, probe.AgentPushes, "expected at least one agent reply")

	parts := make([]string, 0, len(probe.AgentPushes))
	seen := make(map[string]struct{}, len(probe.AgentPushes))
	for _, item := range probe.AgentPushes {
		content := strings.TrimSpace(extractPacketContent(item))
		if content == "" {
			continue
		}
		if _, ok := seen[content]; ok {
			continue
		}
		seen[content] = struct{}{}
		parts = append(parts, content)
	}
	require.NotEmpty(t, parts, "expected non-empty reply text")
	return strings.Join(parts, "\n")
}

type liveDialogueContactLookupTarget struct {
	Source      string         `json:"source"`
	PeerID      string         `json:"peer_id"`
	PeerType    int16          `json:"peer_type"`
	DisplayName string         `json:"display_name"`
	Keyword     string         `json:"keyword"`
	Raw         map[string]any `json:"raw,omitempty"`
}

func selectDialogueContactLookupTarget(
	ctx context.Context,
	t *testing.T,
	harness *liveAgentHarness,
	identity liveProjectSession,
	mainAgentID string,
) liveDialogueContactLookupTarget {
	t.Helper()

	friends := harness.fetchFriendList(ctx, identity, "friends-list-dialog-contact")
	for _, row := range friends {
		peerID := asString(row["user_id"])
		displayName := firstNonEmpty(asString(row["remark_name"]), asString(row["nickname"]), asString(row["username"]))
		keyword := firstNonEmpty(asString(row["remark_name"]), asString(row["nickname"]), asString(row["username"]))
		if peerID == "" || displayName == "" || keyword == "" {
			continue
		}
		return liveDialogueContactLookupTarget{
			Source:      "friend",
			PeerID:      peerID,
			PeerType:    1,
			DisplayName: displayName,
			Keyword:     keyword,
			Raw:         cloneMap(row),
		}
	}

	agent := harness.resolveEligibleGroupTargetsExcluding(
		ctx,
		identity,
		1,
		"contact-lookup-target-agents",
		mainAgentID,
	)[0]
	return liveDialogueContactLookupTarget{
		Source:      "agent",
		PeerID:      asString(agent["id"]),
		PeerType:    2,
		DisplayName: asString(agent["agent_name"]),
		Keyword:     asString(agent["agent_name"]),
		Raw:         cloneMap(agent),
	}
}

func createAgentCategoryByMainAgentDialogue(
	ctx context.Context,
	t *testing.T,
	harness *liveAgentHarness,
	identity liveProjectSession,
	session liveProjectSession,
	categoryName string,
	sortOrder int,
	timeout time.Duration,
) (liveConversationResult, map[string]any, map[string]any) {
	t.Helper()

	createMessage := fmt.Sprintf(
		"请你现在只做一件事：通过 grix-admin 创建一个新的 agent 分类。分类名称必须严格为 %s，parent_id 必须是 0，sort_order 必须是 %d。要求：1. 使用正规的 agent 分类管理能力完成；2. 不要修改已有分类；3. 完成后可以简短确认，但不要做别的动作。",
		categoryName,
		sortOrder,
	)
	probe, verification := runMainAgentDialogueAction(
		ctx,
		t,
		harness,
		session,
		"dialog-category-create-"+sanitizeFileName(categoryName),
		createMessage,
		timeout,
		func(checkCtx context.Context) (map[string]any, bool) {
			matches := harness.listAgentCategoriesByExactName(
				checkCtx,
				identity,
				categoryName,
				"categories-list-dialog-create-"+sanitizeFileName(categoryName),
			)
			if len(matches) != 1 {
				return map[string]any{"matches": matches}, false
			}
			if !agentCategoryMatches(matches[0], "", categoryName, 0, sortOrder) {
				return map[string]any{"category": matches[0]}, false
			}
			return map[string]any{
				"category": matches[0],
				"matches":  matches,
			}, true
		},
	)
	category := requireMapField(t, verification, "category")
	return probe, verification, category
}

func missingScopeNames(payload map[string]any, required ...string) []string {
	if len(required) == 0 {
		return nil
	}
	items, _ := payload["scopes"].([]any)
	available := make(map[string]struct{}, len(items))
	for _, item := range items {
		name := strings.TrimSpace(asString(item))
		if name == "" {
			continue
		}
		available[name] = struct{}{}
	}

	var missing []string
	for _, name := range required {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, ok := available[trimmed]; ok {
			continue
		}
		missing = append(missing, trimmed)
	}
	return missing
}

func agentCategoryMatches(
	row map[string]any,
	expectedID string,
	expectedName string,
	expectedParentID int64,
	expectedSort int,
) bool {
	if row == nil {
		return false
	}
	if trimmedID := strings.TrimSpace(expectedID); trimmedID != "" && asString(row["id"]) != trimmedID {
		return false
	}
	if trimmedName := strings.TrimSpace(expectedName); trimmedName != "" && asString(row["name"]) != trimmedName {
		return false
	}
	parentID, err := parseFlexibleInt64(row["parent_id"])
	if err != nil || parentID != expectedParentID {
		return false
	}
	sortOrder, err := parseFlexibleInt64(row["sort_order"])
	if err != nil || sortOrder != int64(expectedSort) {
		return false
	}
	return true
}

func requireMapField(t *testing.T, payload map[string]any, key string) map[string]any {
	t.Helper()
	raw, ok := payload[key]
	require.Truef(t, ok, "missing field %s", key)
	result, ok := raw.(map[string]any)
	require.Truef(t, ok, "field %s is not a map", key)
	return result
}

func extractAgentReplyMsgID(t *testing.T, probe liveMultiConversationResult, senderID string) string {
	t.Helper()
	replies := probe.RepliesBySender[strings.TrimSpace(senderID)]
	require.NotEmptyf(t, replies, "missing reply from sender %s", senderID)

	msgID := asString(replies[0]["msg_id"])
	require.NotEmptyf(t, msgID, "reply from sender %s does not contain msg_id", senderID)
	return msgID
}
