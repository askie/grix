import 'package:get/get.dart';

/// Agent 状态卡/绑定卡文案本地化。
///
/// 后端把绑定卡与会话控制卡的 summary、detail_text 以固定模板拼成
/// 成品文案下发（历史原因中英混杂），消息体里没有结构化字段可用。
/// 这里按模板逆向匹配，还原成 i18n key + 参数后按客户端语言重新渲染；
/// 匹配不上的文案原样返回，兼容后端未来新增的文案。
class ChatAgentCardTextLocalizer {
  const ChatAgentCardTextLocalizer._();

  /// 精确匹配的整句文案 → i18n key。
  static const Map<String, String> _exactPatterns = <String, String>{
    '目录绑定成功。': 'chat_message_card_agent_status_text_bound_ok',
    '已解绑工作目录。': 'chat_message_card_agent_status_text_unbound',
    'Workspace unbound.': 'chat_message_card_agent_status_text_unbound',
    '会话已过期，请新建会话后继续对话。': 'chat_message_card_agent_status_text_session_expired',
    '插件未在规定时间内响应，请稍后重试。': 'chat_message_card_agent_status_text_timeout_detail',
    'A workspace path is required.':
        'chat_message_card_agent_status_text_err_cwd_required',
    'The workspace path is invalid.':
        'chat_message_card_agent_status_text_err_cwd_invalid',
    'This chat is already bound to another workspace.':
        'chat_message_card_agent_status_text_err_rebind_forbidden',
    'The session control action is invalid.':
        'chat_message_card_agent_status_text_err_verb_invalid',
    'Choose a workspace folder before submitting again.':
        'chat_message_card_agent_status_text_choose_folder',
    'Choose a valid workspace folder and try again.':
        'chat_message_card_agent_status_text_choose_valid_folder',
    'Please try again.': 'chat_message_card_agent_status_text_try_again',
    // exec approval（执行审批卡）
    'Exec approval allowed once.':
        'chat_message_card_agent_status_text_exec_approval_once',
    'Exec approval allowed always.':
        'chat_message_card_agent_status_text_exec_approval_always',
    'Exec approval allowed by rule.':
        'chat_message_card_agent_status_text_exec_approval_rule',
    'Exec approval denied.':
        'chat_message_card_agent_status_text_exec_approval_denied',
    'Exec approval submitted.':
        'chat_message_card_agent_status_text_exec_approval_submitted',
    'Approval submission timed out.':
        'chat_message_card_agent_status_text_exec_approval_submit_timeout',
    'The agent did not confirm the approval before the request timed out.':
        'chat_message_card_agent_status_text_exec_approval_submit_timeout_detail',
    'Approval submission is unavailable.':
        'chat_message_card_agent_status_text_exec_approval_unavailable',
    'Approval replies are not supported by the connected agent.':
        'chat_message_card_agent_status_text_exec_approval_unavailable_detail',
    'Exec approvals are not enabled for this agent.':
        'chat_message_card_agent_status_text_exec_approval_disabled',
    'This agent is not configured to accept approval replies.':
        'chat_message_card_agent_status_text_exec_approval_disabled_detail',
    'You are not allowed to approve this request.':
        'chat_message_card_agent_status_text_exec_approval_unauthorized',
    'The current approver does not have permission to submit this approval.':
        'chat_message_card_agent_status_text_exec_approval_unauthorized_detail',
    'Exec approval expired.':
        'chat_message_card_agent_status_text_exec_approval_expired',
    'This approval request is no longer valid.':
        'chat_message_card_agent_status_text_exec_approval_expired_detail',
    'Failed to submit approval.':
        'chat_message_card_agent_status_text_exec_approval_failed',
    'The agent rejected the approval request.':
        'chat_message_card_agent_status_text_exec_approval_failed_detail',
    // Claude 问答/交互回复卡
    'Claude did not confirm the answer before the request timed out.':
        'chat_message_card_agent_status_text_question_timeout_detail',
    'Reply recorded.': 'chat_message_card_agent_status_text_reply_recorded',
    'Approval recorded.':
        'chat_message_card_agent_status_text_approval_recorded',
    'Approval could not be recorded.':
        'chat_message_card_agent_status_text_approval_record_failed',
    'Reply could not be recorded.':
        'chat_message_card_agent_status_text_reply_record_failed',
    'Request ID is required.':
        'chat_message_card_agent_status_text_err_request_id_required',
    'The request could not be found.':
        'chat_message_card_agent_status_text_err_request_not_found',
    'The request is no longer pending.':
        'chat_message_card_agent_status_text_err_request_not_pending',
    'The reply format is invalid.':
        'chat_message_card_agent_status_text_err_reply_invalid',
    'Claude did not accept the reply.':
        'chat_message_card_agent_status_text_err_reply_rejected',
    // Claude 配对卡
    'Paired! Say hi to Claude.':
        'chat_message_card_agent_status_text_paired_ok',
    // 目录绑定不支持
    '该插件不支持目录绑定操作。': 'chat_message_card_agent_status_text_bind_unsupported',
    '请直接在插件中配置工作目录。':
        'chat_message_card_agent_status_text_bind_unsupported_detail',
    // 上下文压缩
    '正在压缩上下文，请稍候...': 'chat_message_card_agent_status_text_compact_pending',
    '上下文压缩完成。': 'chat_message_card_agent_status_text_compact_done',
    '上下文压缩失败。': 'chat_message_card_agent_status_text_compact_failed',
    '请稍后重试。': 'chat_message_card_agent_status_text_retry_later',
    '上下文压缩超时。': 'chat_message_card_agent_status_text_compact_timeout',
    // 用量查询
    '用量查询超时。': 'chat_message_card_agent_status_text_usage_timeout',
    '当前会话尚未绑定，无法查询用量。': 'chat_message_card_agent_status_text_usage_no_binding',
    '未找到当前会话的用量数据。': 'chat_message_card_agent_status_text_usage_not_found',
    '当前连接暂不支持用量查询。': 'chat_message_card_agent_status_text_usage_unsupported',
    '用量查询参数无效。': 'chat_message_card_agent_status_text_usage_invalid_params',
    '用量查询失败。': 'chat_message_card_agent_status_text_usage_failed',
    '请先绑定会话后再试。': 'chat_message_card_agent_status_text_usage_no_binding_detail',
    '会话存在，但暂未采集到可用的 token 用量记录。':
        'chat_message_card_agent_status_text_usage_not_found_detail',
    '请升级并重启 connector 后重试。':
        'chat_message_card_agent_status_text_usage_unsupported_detail',
    '请求中缺少 session_id。':
        'chat_message_card_agent_status_text_usage_invalid_params_detail',
    // Gemini 工具栏切换
    'Gemini 插件确认后会自动刷新工具栏最终状态。':
        'chat_message_card_agent_status_text_gemini_toolbar_pending_detail',
    '当前插件版本未提供这个本地动作。':
        'chat_message_card_agent_status_text_gemini_toolbar_unsupported_detail',
  };

  /// 带参数的模板文案，按声明顺序匹配；捕获组按 [_TemplatePattern.params]
  /// 依次填入 i18n 参数。精确文案先于模板匹配，避免被泛化模板误捕获。
  static final List<_TemplatePattern> _templatePatterns = <_TemplatePattern>[
    _TemplatePattern(
      RegExp(r'^已绑定 (.+)$'),
      'chat_message_card_agent_status_text_bound_path',
      <String>['path'],
    ),
    _TemplatePattern(
      RegExp(r'^No (.+?) session is bound to this chat\.$'),
      'chat_message_card_agent_status_text_binding_missing',
      <String>['provider'],
    ),
    _TemplatePattern(
      RegExp(r'^Current (.+?) workspace is (.+)\.$'),
      'chat_message_card_agent_status_text_where_path',
      <String>['provider', 'path'],
    ),
    _TemplatePattern(
      RegExp(r'^(.+?) session is already bound\.$'),
      'chat_message_card_agent_status_text_already_bound',
      <String>['provider'],
    ),
    _TemplatePattern(
      RegExp(r'^(.+?) workspace location is available\.$'),
      'chat_message_card_agent_status_text_where_ok',
      <String>['provider'],
    ),
    _TemplatePattern(
      RegExp(r'^(.+?) session stopped for (.+)\.$'),
      'chat_message_card_agent_status_text_stopped_path',
      <String>['provider', 'path'],
    ),
    _TemplatePattern(
      RegExp(r'^(.+?) session stopped\.$'),
      'chat_message_card_agent_status_text_stopped',
      <String>['provider'],
    ),
    _TemplatePattern(
      RegExp(r'^(.+?) session restarted for (.+)\.$'),
      'chat_message_card_agent_status_text_restarted_path',
      <String>['provider', 'path'],
    ),
    _TemplatePattern(
      RegExp(r'^(.+?) session restarted\.$'),
      'chat_message_card_agent_status_text_restarted',
      <String>['provider'],
    ),
    _TemplatePattern(
      RegExp(r'^(.+?) session status is available\.$'),
      'chat_message_card_agent_status_text_status_ok',
      <String>['provider'],
    ),
    _TemplatePattern(
      RegExp(r'^(.+?) session could not be opened\.$'),
      'chat_message_card_agent_status_text_open_failed',
      <String>['provider'],
    ),
    _TemplatePattern(
      RegExp(r'^(.+?) session could not be stopped\.$'),
      'chat_message_card_agent_status_text_stop_failed',
      <String>['provider'],
    ),
    _TemplatePattern(
      RegExp(r'^(.+?) session could not be restarted\.$'),
      'chat_message_card_agent_status_text_restart_failed',
      <String>['provider'],
    ),
    _TemplatePattern(
      RegExp(r'^(.+?) workspace could not be read\.$'),
      'chat_message_card_agent_status_text_where_failed',
      <String>['provider'],
    ),
    _TemplatePattern(
      RegExp(r'^(.+?) session status could not be read\.$'),
      'chat_message_card_agent_status_text_status_failed',
      <String>['provider'],
    ),
    _TemplatePattern(
      RegExp(r'^(.+?) 会话操作超时。$'),
      'chat_message_card_agent_status_text_timeout',
      <String>['provider'],
    ),
    _TemplatePattern(
      RegExp(r'^(.+?) returned a runtime error\.$'),
      'chat_message_card_agent_status_text_runtime_error',
      <String>['provider'],
    ),
    _TemplatePattern(
      RegExp(r'^(.+?) workspace path is required\.$'),
      'chat_message_card_agent_status_text_open_cwd_required',
      <String>['provider'],
    ),
    _TemplatePattern(
      RegExp(r'^(.+?) workspace path is invalid\.$'),
      'chat_message_card_agent_status_text_open_cwd_invalid',
      <String>['provider'],
    ),
    _TemplatePattern(
      RegExp(r'^(.+?) command completed\.$'),
      'chat_message_card_agent_status_text_command_completed',
      <String>['provider'],
    ),
    _TemplatePattern(
      RegExp(r'^Workspace: (.+)$'),
      'chat_message_card_agent_status_text_detail_workspace',
      <String>['path'],
    ),
    _TemplatePattern(
      RegExp(r'^Worker: (.+)$'),
      'chat_message_card_agent_status_text_detail_worker',
      <String>['status'],
    ),
    _TemplatePattern(
      RegExp(r'^Question request (.+?) answers recorded\.$'),
      'chat_message_card_agent_status_text_question_recorded',
      <String>['request_id'],
    ),
    _TemplatePattern(
      RegExp(r'^Question request (.+?) timed out\.$'),
      'chat_message_card_agent_status_text_question_timeout',
      <String>['request_id'],
    ),
    _TemplatePattern(
      RegExp(r'^Question request (.+?) could not be recorded\.$'),
      'chat_message_card_agent_status_text_question_record_failed',
      <String>['request_id'],
    ),
    _TemplatePattern(
      RegExp(
        r'^Pairing request (.+?) was denied\. '
        r'Ask the Claude Code user to request a new pairing code '
        r'if you still need access\.$',
      ),
      'chat_message_card_agent_status_text_pairing_denied',
      <String>['request_id'],
    ),
    // Gemini 专用模式必须排在通用切换模式之前，避免被通用正则先行捕获。
    _TemplatePattern(
      RegExp(r'^正在切换 Gemini (.+?)到 (.+?)…$'),
      'chat_message_card_agent_status_text_gemini_toolbar_pending',
      <String>['type', 'label'],
    ),
    _TemplatePattern(
      RegExp(r'^Gemini (.+?)已切换为 (.+?)。$'),
      'chat_message_card_agent_status_text_gemini_switched',
      <String>['type', 'label'],
    ),
    _TemplatePattern(
      RegExp(r'^切换 Gemini (.+?)失败。$'),
      'chat_message_card_agent_status_text_gemini_switch_failed',
      <String>['type'],
    ),
    _TemplatePattern(
      RegExp(r'^Gemini 未能切换到 (.+?)。$'),
      'chat_message_card_agent_status_text_gemini_switch_failed_detail',
      <String>['label'],
    ),
    _TemplatePattern(
      RegExp(r'^Gemini 当前不支持切换(.+?)。$'),
      'chat_message_card_agent_status_text_gemini_switch_unsupported',
      <String>['type'],
    ),
    _TemplatePattern(
      RegExp(r'^切换 Gemini (.+?)超时。$'),
      'chat_message_card_agent_status_text_gemini_switch_timeout',
      <String>['type'],
    ),
    // 通用工具栏切换（模型/模式/推理力度/设置）
    _TemplatePattern(
      RegExp(r'^(.+?)已切换为 (.+?)。$'),
      'chat_message_card_agent_status_text_switched',
      <String>['type', 'label'],
    ),
    _TemplatePattern(
      RegExp(r'^(.+?)切换成功。$'),
      'chat_message_card_agent_status_text_switch_ok',
      <String>['type'],
    ),
    _TemplatePattern(
      RegExp(r'^切换(.+?)失败。$'),
      'chat_message_card_agent_status_text_switch_failed',
      <String>['type'],
    ),
    _TemplatePattern(
      RegExp(r'^未能切换到 (.+?)。$'),
      'chat_message_card_agent_status_text_switch_failed_detail',
      <String>['label'],
    ),
  ];

  /// 本地化一段卡片文案；detail_text 可能是多行拼接，逐行处理。
  static String localize(String text) {
    if (text.isEmpty) return text;
    if (!text.contains('\n')) return _localizeLine(text);
    return text.split('\n').map(_localizeLine).join('\n');
  }

  /// 工具栏切换卡的 @type 参数：后端始终吐出固定中文词（模型/模式/...），
  /// 与 provider 名（Codex/Claude 等本就是拉丁字母）不同，这里需要再翻译一次，
  /// 否则英文界面会出现 "Gemini 模型 switched to ..." 这种中英夹杂。
  static const Map<String, String> _toolbarTypeLabels = <String, String>{
    '模型': 'chat_message_card_agent_status_toolbar_type_model',
    '模式': 'chat_message_card_agent_status_toolbar_type_mode',
    '推理力度': 'chat_message_card_agent_status_toolbar_type_reasoning_effort',
    '设置': 'chat_message_card_agent_status_toolbar_type_setting',
    '审批模式': 'chat_message_card_agent_status_toolbar_type_approval_mode',
  };

  static String _localizeLine(String line) {
    final trimmed = line.trim();
    if (trimmed.isEmpty) return line;

    final exactKey = _exactPatterns[trimmed];
    if (exactKey != null) {
      return _translate(exactKey, const <String, String>{}, line);
    }

    for (final pattern in _templatePatterns) {
      final match = pattern.regExp.firstMatch(trimmed);
      if (match == null) continue;
      final params = <String, String>{};
      for (var i = 0; i < pattern.params.length; i++) {
        params[pattern.params[i]] = (match.group(i + 1) ?? '').trim();
      }
      final typeValue = params['type'];
      final typeLabelKey = typeValue == null
          ? null
          : _toolbarTypeLabels[typeValue];
      if (typeLabelKey != null) {
        final translatedType = typeLabelKey.tr;
        if (translatedType.isNotEmpty && translatedType != typeLabelKey) {
          params['type'] = translatedType;
        }
      }
      return _translate(pattern.key, params, line);
    }
    return line;
  }

  static String _translate(
    String key,
    Map<String, String> params,
    String fallback,
  ) {
    final translated = params.isEmpty ? key.tr : key.trParams(params);
    if (translated.isEmpty || translated == key) return fallback;
    return translated;
  }
}

class _TemplatePattern {
  _TemplatePattern(this.regExp, this.key, this.params);

  final RegExp regExp;
  final String key;
  final List<String> params;
}
