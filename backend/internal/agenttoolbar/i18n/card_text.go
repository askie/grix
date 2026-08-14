package i18n

import "fmt"

// cardTemplate 是 agent 状态卡片（session control/exec 审批/问答回复/
// thread compact/用量查询/Gemini 工具栏/访问审批）文案的中英文模板对照，
// 与 frontend/assets/i18n/{zh_CN,en_US}.json 里
// chat_message_card_agent_status_text_* 系列 key 的文案保持一致，
// 避免后端另起一套翻译造成两份维护和风格不一致。
type cardTemplate struct {
	zh string
	en string
}

var cardTemplates = map[string]cardTemplate{
	// session control
	"already_bound":           {"%s 会话已绑定。", "%s session is already bound."},
	"bound_path":              {"已绑定 %s", "Bound to %s"},
	"bound_ok":                {"目录绑定成功。", "Workspace bound successfully."},
	"where_path":              {"当前 %s 工作目录：%s", "Current %s workspace is %s."},
	"where_ok":                {"%s 工作目录信息已就绪。", "%s workspace location is available."},
	"stopped_path":            {"%s 会话已停止（%s）。", "%s session stopped for %s."},
	"stopped":                 {"%s 会话已停止。", "%s session stopped."},
	"restarted_path":          {"%s 会话已重启（%s）。", "%s session restarted for %s."},
	"restarted":               {"%s 会话已重启。", "%s session restarted."},
	"status_ok":               {"%s 会话状态已就绪。", "%s session status is available."},
	"open_failed":             {"%s 会话打开失败。", "%s session could not be opened."},
	"stop_failed":             {"%s 会话停止失败。", "%s session could not be stopped."},
	"restart_failed":          {"%s 会话重启失败。", "%s session could not be restarted."},
	"where_failed":            {"%s 工作目录读取失败。", "%s workspace could not be read."},
	"status_failed":           {"%s 会话状态读取失败。", "%s session status could not be read."},
	"timeout":                 {"%s 会话操作超时。", "%s session action timed out."},
	"runtime_error":           {"%s 返回了运行时错误。", "%s returned a runtime error."},
	"open_cwd_required":       {"%s 需要提供工作目录路径。", "%s workspace path is required."},
	"open_cwd_invalid":        {"%s 工作目录路径无效。", "%s workspace path is invalid."},
	"command_completed":       {"%s 命令已执行完成。", "%s command completed."},
	"command_exec_completed":  {"%s %s 已执行完成。", "%s %s completed."},
	"detail_workspace":        {"工作目录：%s", "Workspace: %s"},
	"detail_worker":           {"运行状态：%s", "Worker: %s"},
	"binding_missing":         {"当前会话未绑定 %s。", "No %s session is bound to this chat."},
	"session_expired":         {"会话已过期，请新建会话后继续对话。", "This session has expired. Start a new chat to continue."},
	"timeout_detail":          {"插件未在规定时间内响应，请稍后重试。", "The plugin did not respond in time. Please try again later."},
	"err_cwd_required":        {"需要提供工作目录路径。", "A workspace path is required."},
	"err_cwd_invalid":         {"工作目录路径无效。", "The workspace path is invalid."},
	"err_rebind_forbidden":    {"当前会话已绑定其他工作目录。", "This chat is already bound to another workspace."},
	"err_verb_invalid":        {"会话控制操作无效。", "The session control action is invalid."},
	"choose_folder":           {"请先选择工作目录，再重新提交。", "Choose a workspace folder before submitting again."},
	"choose_valid_folder":     {"请选择有效的工作目录后重试。", "Choose a valid workspace folder and try again."},
	"try_again":               {"请重试。", "Please try again."},
	"retry_later":             {"请稍后重试。", "Please try again later."},
	"bind_unsupported":        {"该插件不支持目录绑定操作。", "This plugin does not support directory binding."},
	"bind_unsupported_detail": {"请直接在插件中配置工作目录。", "Configure the workspace directly in the plugin."},

	// thread compact
	"compact_pending": {"正在压缩上下文，请稍候...", "Compacting context, please wait..."},
	"compact_done":    {"上下文压缩完成。", "Context compaction complete."},
	"compact_failed":  {"上下文压缩失败。", "Context compaction failed."},
	"compact_timeout": {"上下文压缩超时。", "Context compaction timed out."},

	// session usage
	"usage_timeout":               {"用量查询超时。", "Usage query timed out."},
	"usage_no_binding":            {"当前会话尚未绑定，无法查询用量。", "This chat is not bound yet, so usage cannot be queried."},
	"usage_not_found":             {"未找到当前会话的用量数据。", "No usage data was found for this session."},
	"usage_unsupported":           {"当前连接暂不支持用量查询。", "The current connection does not support usage queries yet."},
	"usage_invalid_params":        {"用量查询参数无效。", "The usage query parameters are invalid."},
	"usage_failed":                {"用量查询失败。", "Usage query failed."},
	"usage_no_binding_detail":     {"请先绑定会话后再试。", "Bind a workspace to this chat before trying again."},
	"usage_not_found_detail":      {"会话存在，但暂未采集到可用的 token 用量记录。", "The session exists, but no usable token usage records have been collected yet."},
	"usage_unsupported_detail":    {"请升级并重启 connector 后重试。", "Upgrade and restart the connector, then try again."},
	"usage_invalid_params_detail": {"请求中缺少 session_id。", "The request is missing session_id."},

	// Gemini 工具栏 / 通用工具栏切换
	"gemini_toolbar_pending":            {"正在切换 Gemini %s到 %s…", "Switching Gemini %s to %s…"},
	"gemini_toolbar_pending_detail":     {"Gemini 插件确认后会自动刷新工具栏最终状态。", "The toolbar will refresh automatically once the Gemini plugin confirms."},
	"gemini_toolbar_unsupported_detail": {"当前插件版本未提供这个本地动作。", "The current plugin version does not provide this local action."},
	"gemini_switched":                   {"Gemini %s已切换为 %s。", "Gemini %s switched to %s."},
	"gemini_switch_failed":              {"切换 Gemini %s失败。", "Failed to switch Gemini %s."},
	"gemini_switch_failed_detail":       {"Gemini 未能切换到 %s。", "Gemini could not switch to %s."},
	"gemini_switch_unsupported":         {"Gemini 当前不支持切换%s。", "Gemini does not currently support switching %s."},
	"gemini_switch_timeout":             {"切换 Gemini %s超时。", "Switching Gemini %s timed out."},
	"switched":                          {"%s已切换为 %s。", "%s switched to %s."},
	"switch_ok":                         {"%s切换成功。", "%s switched successfully."},
	"switch_failed":                     {"切换%s失败。", "Failed to switch %s."},
	"switch_failed_detail":              {"未能切换到 %s。", "Could not switch to %s."},

	// exec 审批
	"exec_approval_once":                  {"已允许执行一次。", "Exec approval allowed once."},
	"exec_approval_always":                {"已允许永久执行。", "Exec approval allowed always."},
	"exec_approval_rule":                  {"已按规则允许执行。", "Exec approval allowed by rule."},
	"exec_approval_denied":                {"已拒绝执行。", "Exec approval denied."},
	"exec_approval_submitted":             {"审批请求已提交。", "Exec approval submitted."},
	"exec_approval_submit_timeout":        {"审批提交超时。", "Approval submission timed out."},
	"exec_approval_submit_timeout_detail": {"Agent 未在规定时间内确认该审批请求。", "The agent did not confirm the approval before the request timed out."},
	"exec_approval_unavailable":           {"审批当前不可用。", "Approval submission is unavailable."},
	"exec_approval_unavailable_detail":    {"已连接的 Agent 不支持审批回复。", "Approval replies are not supported by the connected agent."},
	"exec_approval_disabled":              {"该 Agent 未启用执行审批。", "Exec approvals are not enabled for this agent."},
	"exec_approval_disabled_detail":       {"该 Agent 未配置为接受审批回复。", "This agent is not configured to accept approval replies."},
	"exec_approval_unauthorized":          {"你没有权限审批此请求。", "You are not allowed to approve this request."},
	"exec_approval_unauthorized_detail":   {"当前审批人没有权限提交此审批。", "The current approver does not have permission to submit this approval."},
	"exec_approval_expired":               {"审批请求已过期。", "Exec approval expired."},
	"exec_approval_expired_detail":        {"该审批请求已失效。", "This approval request is no longer valid."},
	"exec_approval_failed":                {"审批提交失败。", "Failed to submit approval."},
	"exec_approval_failed_detail":         {"Agent 拒绝了该审批请求。", "The agent rejected the approval request."},

	// 问答 / 交互回复
	"question_recorded":                {"问答请求 %s 的回答已记录。", "Question request %s answers recorded."},
	"question_timeout":                 {"问答请求 %s 已超时。", "Question request %s timed out."},
	"question_record_failed":           {"问答请求 %s 未能记录。", "Question request %s could not be recorded."},
	"question_timeout_detail":          {"Claude 未在规定时间内确认该回答。", "Claude did not confirm the answer before the request timed out."},
	"reply_recorded":                   {"回复已记录。", "Reply recorded."},
	"approval_recorded":                {"审批已记录。", "Approval recorded."},
	"approval_record_failed":           {"审批记录失败。", "Approval could not be recorded."},
	"reply_record_failed":              {"回复记录失败。", "Reply could not be recorded."},
	"err_request_id_required":          {"缺少请求 ID。", "Request ID is required."},
	"err_request_not_found":            {"未找到该请求。", "The request could not be found."},
	"err_request_not_pending":          {"该请求已不再等待处理。", "The request is no longer pending."},
	"err_reply_invalid":                {"回复格式无效。", "The reply format is invalid."},
	"err_reply_rejected":               {"Claude 未接受该回复。", "Claude did not accept the reply."},
	"paired_ok":                        {"配对成功！和 Claude 打个招呼吧。", "Paired! Say hi to Claude."},
	"pairing_denied":                   {"配对请求 %s 已被拒绝。如果仍需要访问，请让 Claude Code 用户重新生成配对码。", "Pairing request %s was denied. Ask the Claude Code user to request a new pairing code if you still need access."},
	"reply_forwarded":                  {"回复已转为消息发送。", "Reply forwarded as a message."},
	"reply_forwarded_detail":           {"问答卡片已过期，回答已作为普通消息发送给 Agent。", "The question card had expired; the answer was delivered to the agent as a regular message."},
	"interaction_reply_timeout_detail": {"插件未在规定时间内响应，请重试。", "The connector did not respond in time. Please try again."},

	// 群聊访问审批
	"access_reply_unparseable":         {"审批回传无法解析，请重新打开审批卡操作。", "Could not parse the approval reply. Please reopen the approval card and try again."},
	"access_request_unrecognized":      {"审批请求无法识别，请让对方重新在群里 @ 一次。", "This approval request could not be recognized. Ask them to @ the agent again in the group."},
	"access_owner_only":                {"只有 agent 主人可以处理访问申请。", "Only the agent owner can process access requests."},
	"access_cancelled":                 {"已取消。申请在有效期内保留，可随时重新打开卡片处理。", "Canceled. The request remains valid — reopen the card anytime to process it."},
	"access_expired_or_processed_hint": {"该申请已过期或已处理。如对方仍需使用，请让对方重新在群里 @ 一次。", "This request has expired or was already processed. If they still need access, ask them to @ the agent again in the group."},
	"access_expired_or_processed":      {"该申请已过期或已处理。", "This request has expired or was already processed."},
	"access_denied":                    {"已拒绝该访问申请。", "Access request denied."},
	"access_choose_option":             {"请在卡片上选择「允许」或「拒绝」。", "Please choose \"Allow\" or \"Deny\" on the card."},
	"access_approved":                  {"已允许 %s 使用本 agent（已加入访问名单）。", "%s is now allowed to use this agent (added to the access list)."},

	// Gemini 运行时失败
	"gemini_prompt_timeout":         {"Gemini 请求超时。", "Gemini timed out."},
	"gemini_prompt_timeout_detail":  {"本地 Gemini 请求耗时过长，请缩小请求范围或加大超时时间后重试。", "The local Gemini request took too long. Retry with a narrower request or a larger timeout."},
	"gemini_process_exit":           {"Gemini 本地进程已停止。", "Gemini local process stopped."},
	"gemini_process_exit_detail":    {"grix-gemini 管理的 Gemini 进程在请求完成前退出，请重连插件后重试。", "The Gemini process managed by grix-gemini exited before the request finished. Reconnect the plugin and try again."},
	"gemini_prompt_failed":          {"Gemini 请求失败。", "Gemini request failed."},
	"gemini_prompt_failed_detail":   {"Gemini 未能完成该请求。", "Gemini could not complete the request."},
	"gemini_empty_output":           {"Gemini 未返回可见输出。", "Gemini returned no visible output."},
	"gemini_empty_output_detail":    {"Gemini 已结束但未产生任何可见文本。", "Gemini finished without producing any visible text."},
	"gemini_invalid_payload":        {"Gemini 请求负载无效。", "Gemini request payload is invalid."},
	"gemini_invalid_payload_detail": {"发送给 grix-gemini 的后端请求无法解析。", "The backend request sent to grix-gemini could not be parsed."},

	// Gemini 工作区/问答/鉴权交互
	"gemini_cwd_required":                 {"需要提供 Gemini 工作目录路径。", "Gemini workspace path is required."},
	"gemini_cwd_required_detail":          {"请先选择工作目录，再提交 Gemini 工作区卡片。", "Choose a workspace folder before submitting the Gemini workspace card."},
	"gemini_workspace_not_pending":        {"当前没有待处理的 Gemini 工作区请求。", "No Gemini workspace request is pending."},
	"gemini_workspace_not_pending_detail": {"请向 Gemini 发送新消息，如果仍需选择目录，请重新打开工作区卡片。", "Send a new message to Gemini and reopen the workspace card if you still need to choose a folder."},
	"gemini_retry_failed":                 {"Gemini 请求未能重试。", "Gemini request could not be retried."},
	"gemini_retry_failed_detail":          {"工作目录已发送给插件，但原始请求未能重新调度。", "The workspace was sent to the plugin, but the original request could not be rescheduled."},
	"gemini_reply_invalid":                {"Gemini 交互回复无效。", "Gemini interaction reply is invalid."},
	"gemini_reply_invalid_detail":         {"请使用 Gemini 问答卡片上的按钮操作，不要手动编辑动作负载。", "Use the buttons on the Gemini question card instead of editing the action payload manually."},
	"gemini_question_not_pending":         {"当前没有待处理的 Gemini 问答。", "No Gemini question is pending."},
	"gemini_question_not_pending_detail":  {"如果 Gemini 仍在等待你的输入，请打开最新的 Gemini 交互卡片重试。", "Open the latest Gemini interaction card and try again if Gemini is still waiting for your input."},
	"gemini_auth_action_unsupported":      {"该 Gemini 鉴权卡片只支持「完成」或「取消」。", "Gemini only supports Complete or Cancel for this authentication card."},
	"gemini_auth_cancelled":               {"已取消 Gemini 鉴权设置。", "Gemini authentication setup was canceled."},
	"gemini_auth_cancelled_detail":        {"请在运行 grix-gemini 的机器上完成 Gemini CLI 鉴权，准备好后再次向 Gemini 提问。", "Finish Gemini CLI authentication on the machine running grix-gemini, then ask Gemini again when you are ready."},
	"gemini_auth_retrying":                {"正在鉴权后重试 Gemini。", "Retrying Gemini after authentication."},
	"gemini_auth_retrying_detail":         {"正在用当前本地 CLI 配置重新发送原始 Gemini 请求。", "The original Gemini request is being sent again with the current local CLI configuration."},
	"gemini_question_cancelled":           {"已取消 Gemini 问答。", "Gemini question was canceled."},
	"gemini_question_cancelled_detail":    {"准备好回答必填问题后，请重新向 Gemini 发送请求。", "Send the Gemini request again when you are ready to answer the required question."},
	"gemini_answer_invalid":               {"Gemini 回答无效。", "Gemini answer is invalid."},
	"gemini_answer_invalid_detail":        {"请提交 Gemini 问答卡片，而不是发送「完成」或「取消」动作。", "Submit the Gemini question card instead of sending a Complete or Cancel action."},
	"gemini_answer_required":              {"需要填写 Gemini 回答。", "Gemini answer is required."},
	"gemini_answer_required_detail":       {"请先填写 Gemini 问答卡片，再重试该请求。", "Fill in the Gemini question card before retrying the request."},
	"gemini_answer_apply_failed":          {"Gemini 回答未能应用。", "Gemini answer could not be applied."},
	"gemini_answer_apply_failed_detail":   {"后端未能把提交的回答映射回 Gemini 问答卡片。", "The backend could not map the submitted answer back to the Gemini question card."},
	"gemini_answer_retrying":              {"正在用你的回答重试 Gemini。", "Retrying Gemini with your answers."},
	"gemini_answer_retrying_detail":       {"正在用你提供的信息重新发送原始 Gemini 请求。", "The original Gemini request is being sent again with the details you provided."},
	"gemini_retry_not_scheduled":          {"Gemini 重试未能调度。", "Gemini retry could not be scheduled."},
	"gemini_retry_not_scheduled_detail":   {"请等本地插件重连后再次发送 Gemini 请求。", "Try sending your Gemini request again after the local plugin reconnects."},
}

// HasKey 报告 key 是否在模板表中登记，供调用方做编译期之外的静态校验
// （比如扫描源码里所有 T/Tf 调用点，防止引用了表里不存在的 key）。
func HasKey(key string) bool {
	_, ok := cardTemplates[key]
	return ok
}

// T 返回不带参数的固定文案；key 不存在时返回空串（调用方应视为该分支未接入迁移）。
func T(lang, key string) string {
	t, ok := cardTemplates[key]
	if !ok {
		return ""
	}
	if NormalizeLanguage(lang) == "en" {
		return t.en
	}
	return t.zh
}

// Tf 返回按 lang 选择模板后，用 fmt.Sprintf 填入 args 的文案。
func Tf(lang, key string, args ...any) string {
	t, ok := cardTemplates[key]
	if !ok {
		return ""
	}
	format := t.zh
	if NormalizeLanguage(lang) == "en" {
		format = t.en
	}
	return fmt.Sprintf(format, args...)
}
