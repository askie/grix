part of 'im_service.dart';

class _PendingAgentToolbarAck {
  _PendingAgentToolbarAck(this.callback);

  final void Function(bool accepted) callback;
  Timer? timer;

  void settle(bool accepted) {
    timer?.cancel();
    timer = null;
    callback(accepted);
  }
}

class _PendingAgentToolbarSelect {
  const _PendingAgentToolbarSelect({
    required this.clientActionId,
    required this.itemId,
    required this.actionId,
    required this.optionId,
    required this.label,
    required this.startedValue,
    required this.createdAtMs,
  });

  static const int ttlMs = 30 * 1000;

  final String clientActionId;
  final String itemId;
  final String actionId;
  final String optionId;
  final String label;
  final String startedValue;
  final int createdAtMs;

  bool get isExpired =>
      DateTime.now().millisecondsSinceEpoch - createdAtMs > ttlMs;

  bool matchesAction(String actionId) =>
      clientActionId.trim().isNotEmpty &&
      clientActionId.trim() == actionId.trim();

  bool matchesItem(AgentToolbarItemModel item) {
    return itemId == item.itemId.trim() && actionId == item.actionId.trim();
  }

  bool isConfirmedBy(AgentToolbarItemModel item) {
    if (!matchesItem(item)) {
      return false;
    }
    final normalizedOption = optionId.toLowerCase();
    final normalizedLabel = label.toLowerCase();
    final itemValue = item.value.trim().toLowerCase();
    final itemLabel = item.label.trim().toLowerCase();
    return itemValue == normalizedOption ||
        itemValue == normalizedLabel ||
        itemLabel == normalizedLabel ||
        itemLabel == normalizedOption;
  }

  bool isOverriddenBy(AgentToolbarItemModel item) {
    if (!matchesItem(item)) {
      return false;
    }
    final nextValue = item.value.trim();
    return nextValue.isNotEmpty &&
        nextValue != startedValue.trim() &&
        !isConfirmedBy(item);
  }
}

extension ImServiceAgentToolbarX on ImService {
  void _settlePendingAgentToolbarActionAcks(bool accepted) {
    final pendingAcks = _agentToolbarActionAckCallbacks.values.toList();
    _agentToolbarActionAckCallbacks.clear();
    for (final pendingAck in pendingAcks) {
      pendingAck.settle(accepted);
    }
  }

  bool _shouldUseAgentToolbar(String sessionId, {String targetAgentId = ''}) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return false;
    }
    final session = findSessionById(sid);
    if (session == null) {
      // Session metadata may lag behind toolbar packets during initial enter.
      // Keep toolbar flow alive and let backend/session sync converge.
      return true;
    }
    final sessionType = session.type.trim().toLowerCase();
    if (sessionType == 'private') {
      return session.peerType == 2 || session.isVisitor;
    }
    if (sessionType == 'group') {
      return targetAgentId.trim().isNotEmpty;
    }
    return false;
  }

  // target agent id 是雪花号（19 位），Web 端（编译为 JS）整数只有 53 位精度，
  // 一旦转成 int 尾部会被舍入（如 ...624 变 ...600），发给后端会查不到成员导致
  // 工具栏 403。全程以字符串传递，绝不转 int。
  String _resolveToolbarTargetAgentId(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return '';
    }
    return _agentToolbarTargetAgentIdBySession[sid]?.trim() ?? '';
  }

  void setGroupToolbarTargetAgent(String sessionId, {required String agentId}) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return;
    }
    final session = findSessionById(sid);
    if (session == null || session.type.trim().toLowerCase() != 'group') {
      return;
    }
    final normalized = agentId.trim();
    final previous = _agentToolbarTargetAgentIdBySession[sid]?.trim() ?? '';
    if (normalized == previous) {
      return;
    }
    if (normalized.isEmpty) {
      _agentToolbarTargetAgentIdBySession.remove(sid);
      agentToolbars.remove(sid);
      _clearAgentToolbarPendingState(sid);
      return;
    }
    _agentToolbarTargetAgentIdBySession[sid] = normalized;
    _requestAgentToolbarSnapshot(sid);
  }

  /// 外部调用：主动拉取指定会话的工具栏快照（如页面 resume 时刷新用量）。
  void refreshAgentToolbar(String sessionId) {
    _requestAgentToolbarSnapshot(sessionId);
  }

  void _requestAgentToolbarSnapshot(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return;
    }
    final targetAgentId = _resolveToolbarTargetAgentId(sid);
    if (!_shouldUseAgentToolbar(sid, targetAgentId: targetAgentId)) {
      // Don't eagerly clear existing toolbar state here; caller may be in a
      // transient metadata phase (session list/snapshot not fully hydrated).
      return;
    }
    final payload = <String, dynamic>{'session_id': sid};
    if (targetAgentId.isNotEmpty) {
      payload['target_agent_id'] = targetAgentId;
    }
    _sendPacket({
      'cmd': 'agent_toolbar_get',
      'seq': DateTime.now().millisecondsSinceEpoch,
      'payload': payload,
    }, requireAuthenticated: true);
  }

  AgentToolbarModel? getAgentToolbar(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return null;
    }
    final toolbar = agentToolbars[sid];
    if (toolbar == null) {
      return null;
    }
    final session = findSessionById(sid);
    if (session == null) {
      // Session 元数据可能还未同步到 sessions 列表（新建对话场景），
      // 与 _shouldUseAgentToolbar 对齐：信任已存储的 toolbar 数据。
      return toolbar;
    }
    if (session.type.trim().toLowerCase() == 'group') {
      final targetAgentId = _resolveToolbarTargetAgentId(sid);
      if (targetAgentId.isEmpty || toolbar.agentId.trim() != targetAgentId) {
        return null;
      }
    }
    if (session.type.trim().toLowerCase() == 'private') {
      if (session.peerType != 2 && !session.isVisitor) {
        return null;
      }
    }
    return toolbar;
  }

  AgentToolbarItemModel getToolbarItemForDisplay(
    String sessionId,
    AgentToolbarItemModel item,
  ) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return item;
    }
    var displayItem = item;
    final pendingSelect = _agentToolbarPendingSelectBySession[sid];
    if (pendingSelect != null &&
        !pendingSelect.isExpired &&
        pendingSelect.matchesItem(item)) {
      displayItem = displayItem.copyWith(
        label: pendingSelect.label,
        badgeText: pendingSelect.label,
        value: pendingSelect.optionId,
      );
    }
    final loadingItemId = _agentToolbarLoadingItemBySession[sid]?.trim() ?? '';
    if (loadingItemId.isEmpty || loadingItemId != item.itemId) {
      return displayItem;
    }
    return displayItem.copyWith(loading: true, disabled: true);
  }

  Future<bool> sendAgentToolbarAction({
    required String sessionId,
    required AgentToolbarModel toolbar,
    required AgentToolbarItemModel item,
    required String event,
    String optionId = '',
    // actionId 缺省用 item.actionId；DeepSeek「新建 Profile」这类伪选项需要
    // 以同一选择器项发出另一个 action（create_profile），由调用方显式覆盖。
    String actionId = '',
    void Function(bool accepted)? onAck,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty || toolbar.toolbarId.isEmpty || item.itemId.isEmpty) {
      return false;
    }
    final clientActionId = const Uuid().v4();
    if (onAck != null) {
      _agentToolbarActionAckCallbacks[clientActionId] = _PendingAgentToolbarAck(
        onAck,
      );
    }
    _agentToolbarLoadingItemBySession[sid] = item.itemId;
    _agentToolbarPendingActionBySession[sid] = clientActionId;
    final normalizedEvent = event.trim().toLowerCase();
    final normalizedOptionId = optionId.trim();
    if (_shouldTrackAgentToolbarPendingSelect(
      item,
      event: normalizedEvent,
      optionId: normalizedOptionId,
    )) {
      final option = item.options.firstWhereOrNull(
        (candidate) => candidate.optionId.trim() == normalizedOptionId,
      );
      final optionLabel = option?.label.trim();
      _agentToolbarPendingSelectBySession[sid] = _PendingAgentToolbarSelect(
        clientActionId: clientActionId,
        itemId: item.itemId.trim(),
        // 注意：这里记的是 item.actionId 而非调用方覆盖的 actionId——pendingSelect
        // 的乐观值跟踪只关心「选择器项回到哪个值」，与具体 action 无关；目前覆盖
        // 场景（create_profile）也进不了 _shouldTrackAgentToolbarPendingSelect 的
        // 白名单。若未来给选择类 action 加覆盖，需同步这里的取值。
        actionId: item.actionId.trim(),
        optionId: normalizedOptionId,
        label: optionLabel == null || optionLabel.isEmpty
            ? normalizedOptionId
            : optionLabel,
        startedValue: item.value.trim(),
        createdAtMs: DateTime.now().millisecondsSinceEpoch,
      );
    }
    final isStopAction = item.actionId.trim() == 'stop_output';
    if (isStopAction) {
      // [stop-trace] 工具栏停止按钮点击：记录会话/运行态，便于与后端、connector 日志串联。
      final outState = agentOutputStates[sid];
      debugPrint(
        '[stop-trace] frontend toolbar stop_output click session=$sid '
        'item=${item.itemId} client_action_id=$clientActionId '
        'run_id=${outState?['run_id']?.toString() ?? "-"} '
        'state=${outState?['state']?.toString() ?? "-"}',
      );
    }
    final sent = _sendPacket({
      'cmd': 'agent_toolbar_action',
      'seq': DateTime.now().millisecondsSinceEpoch,
      'payload': {
        'session_id': sid,
        if (toolbar.agentId.trim().isNotEmpty)
          'target_agent_id': toolbar.agentId.trim(),
        'toolbar_id': toolbar.toolbarId,
        'revision': toolbar.revision,
        'item_id': item.itemId,
        'action_id': actionId.trim().isNotEmpty
            ? actionId.trim()
            : item.actionId,
        'client_action_id': clientActionId,
        'event': event,
        'option_id': optionId,
      },
    }, requireAuthenticated: true);
    if (!sent) {
      _clearAgentToolbarPendingStateForAction(sid, clientActionId);
      _agentToolbarActionAckCallbacks.remove(clientActionId)?.settle(false);
    } else {
      final pendingAck = _agentToolbarActionAckCallbacks[clientActionId];
      if (pendingAck != null) {
        pendingAck.timer = Timer(const Duration(seconds: 75), () {
          if (identical(
            _agentToolbarActionAckCallbacks[clientActionId],
            pendingAck,
          )) {
            _agentToolbarActionAckCallbacks.remove(clientActionId);
            pendingAck.settle(false);
          }
        });
      }
    }
    if (isStopAction) {
      debugPrint(
        '[stop-trace] frontend toolbar stop_output ${sent ? "sent" : "NOT sent"} '
        'session=$sid client_action_id=$clientActionId '
        'connected=${_isConnected.value} authenticated=${_isAuthenticated.value}',
      );
    }
    return sent;
  }

  void _applyAgentToolbarSnapshotPayload(Map<String, dynamic> payload) {
    final rawSnapshot = payload['snapshot'];
    final snapshotMap = rawSnapshot is Map
        ? Map<String, dynamic>.from(rawSnapshot)
        : Map<String, dynamic>.from(payload);
    final toolbar = AgentToolbarModel.fromJson(snapshotMap);
    final sid = toolbar.sessionId.trim();
    if (sid.isEmpty) {
      return;
    }
    final targetAgentId = _resolveToolbarTargetAgentId(sid);
    if (!_shouldUseAgentToolbar(sid, targetAgentId: targetAgentId)) {
      // Ignore unsupported session types, but don't clear to avoid flicker
      // when session metadata is still catching up.
      return;
    }
    if (targetAgentId.isNotEmpty && toolbar.agentId.trim() != targetAgentId) {
      return;
    }
    final currentToolbar = agentToolbars[sid];
    if (currentToolbar != null &&
        toolbar.revision > 0 &&
        toolbar.revision < currentToolbar.revision) {
      // 后端 toolbar revision 单调递增；乱序到达的旧快照会覆盖已更新的状态，
      // 典型如 Kimi 切模型时 action-ack 触发的立即刷新与 local_action_result
      // 触发的最终刷新竞态，导致工具栏先显示新模型、一会儿又跳回旧模型。
      return;
    }
    if (currentToolbar == null || !currentToolbar.hasSameContent(toolbar)) {
      agentToolbars[sid] = toolbar;
    }
    _reconcileAgentToolbarPendingSelect(sid, toolbar);
    // 对话审计开关服务端状态：快照带 audit_enabled 字段时同步到偏好镜像。
    // 字段缺席表示后端不接管该场景（Feature Gate 未开 / 访客会话），镜像保持不变。
    final auditAgentId = toolbar.agentId.trim();
    final auditEnabled = toolbar.auditEnabled;
    if (auditAgentId.isNotEmpty && auditEnabled != null) {
      final preferenceService = _ensureConversationAuditPreferenceService();
      preferenceService.serverStateSender ??= sendConversationAuditSet;
      preferenceService.applyServerState(
        agentId: auditAgentId,
        enabled: auditEnabled,
      );
    }
    final queueItem = toolbar.items
        .where((e) => e.itemId == 'show_queue')
        .cast<AgentToolbarItemModel?>()
        .firstWhere((e) => true, orElse: () => null);
    debugPrint(
      '[queue-debug] front toolbar_snapshot session=$sid agent=${toolbar.agentId} '
      'visible=${toolbar.visible} revision=${toolbar.revision} '
      'has_show_queue=${queueItem != null} queue_badge="${queueItem?.badgeText ?? ''}"',
    );
    _clearAgentToolbarActionState(sid);
  }

  void _handleAgentToolbarActionAck(Map<String, dynamic> payload) {
    final sid = payload['session_id']?.toString().trim() ?? '';
    if (sid.isEmpty) {
      return;
    }
    final accepted = payload['accepted'] == true;
    final clientActionId = payload['client_action_id']?.toString().trim() ?? '';
    final code = payload['code']?.toString().trim() ?? '';
    final msg = payload['msg']?.toString().trim() ?? '';
    _agentToolbarActionAckCallbacks.remove(clientActionId)?.settle(accepted);
    if (accepted) {
      if (_isPendingAgentToolbarSelectAction(sid, clientActionId)) {
        _clearAgentToolbarActionState(sid);
      } else {
        _clearAgentToolbarPendingStateForAction(sid, clientActionId);
        _requestAgentToolbarSnapshot(sid);
      }
      return;
    }
    if (!accepted) {
      _clearAgentToolbarPendingSelectForAction(sid, clientActionId);
      _clearAgentToolbarPendingStateForAction(sid, clientActionId);
      final silent = _isSilentToolbarAckError(code: code, msg: msg);
      if (silent) {
        _requestAgentToolbarSnapshot(sid);
      }
      if (!silent && msg.isNotEmpty) {
        CustomToast.show(msg);
      }
    }
  }

  ConversationAuditPreferenceService
  _ensureConversationAuditPreferenceService() {
    if (Get.isRegistered<ConversationAuditPreferenceService>()) {
      return Get.find<ConversationAuditPreferenceService>();
    }
    return Get.put<ConversationAuditPreferenceService>(
      ConversationAuditPreferenceService(),
      permanent: true,
    );
  }

  /// 发送对话审计开关设置（user+agent 维度，服务端持久化）。
  /// target agent id 为雪花号字符串，全程不转 int（Web 端 53 位精度问题）。
  /// 位置参数形式：与 ConversationAuditPreferenceService.serverStateSender
  /// 保持一致（dart2wasm wasm-opt 对命名参数函数类型字段的调用会编译失败）。
  bool sendConversationAuditSet(
    String sessionId,
    String agentId,
    bool enabled,
  ) {
    final sid = sessionId.trim();
    final aid = agentId.trim();
    if (sid.isEmpty || aid.isEmpty) {
      return false;
    }
    return _sendPacket({
      'cmd': 'conversation_audit_set',
      'seq': DateTime.now().millisecondsSinceEpoch,
      'payload': {'session_id': sid, 'agent_id': aid, 'enabled': enabled},
    }, requireAuthenticated: true);
  }

  void _handleConversationAuditSetResp(Map<String, dynamic> payload) {
    final agentId = payload['agent_id']?.toString().trim() ?? '';
    if (agentId.isEmpty) {
      return;
    }
    final rawCode = payload['code'];
    final code = rawCode is int
        ? rawCode
        : int.tryParse(rawCode?.toString() ?? '') ?? -1;
    if (code == 0) {
      // 服务端返回落库后的实际值，校准乐观更新。
      _ensureConversationAuditPreferenceService().applyServerState(
        agentId: agentId,
        enabled: payload['enabled'] == true,
      );
      return;
    }
    final msg = payload['msg']?.toString().trim() ?? '';
    if (msg.isNotEmpty) {
      CustomToast.show(msg);
    }
    // 失败时重新拉快照，用服务端真值回滚乐观更新。
    final sid = payload['session_id']?.toString().trim() ?? '';
    if (sid.isNotEmpty) {
      _requestAgentToolbarSnapshot(sid);
    }
  }

  void _clearAgentToolbarPendingState(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return;
    }
    _clearAgentToolbarActionState(sid);
    _agentToolbarPendingSelectBySession.remove(sid);
  }

  void _clearAgentToolbarActionState(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return;
    }
    _agentToolbarLoadingItemBySession.remove(sid);
    _agentToolbarPendingActionBySession.remove(sid);
  }

  void _clearAgentToolbarPendingStateForAction(
    String sessionId,
    String clientActionId,
  ) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return;
    }
    final actionId = clientActionId.trim();
    if (actionId.isEmpty) {
      _clearAgentToolbarPendingState(sid);
      return;
    }
    final pendingActionId =
        _agentToolbarPendingActionBySession[sid]?.trim() ?? '';
    if (pendingActionId != actionId) {
      return;
    }
    _clearAgentToolbarActionState(sid);
    final pendingSelect = _agentToolbarPendingSelectBySession[sid];
    if (pendingSelect != null && pendingSelect.matchesAction(actionId)) {
      _agentToolbarPendingSelectBySession.remove(sid);
    }
  }

  void _clearAgentToolbarPendingSelectForAction(
    String sessionId,
    String clientActionId,
  ) {
    final sid = sessionId.trim();
    final actionId = clientActionId.trim();
    if (sid.isEmpty || actionId.isEmpty) {
      return;
    }
    final pendingSelect = _agentToolbarPendingSelectBySession[sid];
    if (pendingSelect != null && pendingSelect.matchesAction(actionId)) {
      _agentToolbarPendingSelectBySession.remove(sid);
    }
  }

  bool _isPendingAgentToolbarSelectAction(
    String sessionId,
    String clientActionId,
  ) {
    final sid = sessionId.trim();
    final actionId = clientActionId.trim();
    if (sid.isEmpty || actionId.isEmpty) {
      return false;
    }
    return _agentToolbarPendingSelectBySession[sid]?.matchesAction(actionId) ??
        false;
  }

  void _reconcileAgentToolbarPendingSelect(
    String sessionId,
    AgentToolbarModel toolbar,
  ) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return;
    }
    final pendingSelect = _agentToolbarPendingSelectBySession[sid];
    if (pendingSelect == null) {
      return;
    }
    if (pendingSelect.isExpired) {
      _agentToolbarPendingSelectBySession.remove(sid);
      return;
    }
    var matchedItem = false;
    for (final item in toolbar.items) {
      if (!pendingSelect.matchesItem(item)) {
        continue;
      }
      matchedItem = true;
      if (pendingSelect.isConfirmedBy(item) ||
          pendingSelect.isOverriddenBy(item)) {
        _agentToolbarPendingSelectBySession.remove(sid);
        return;
      }
    }
    if (!matchedItem) {
      _agentToolbarPendingSelectBySession.remove(sid);
    }
  }

  bool _shouldTrackAgentToolbarPendingSelect(
    AgentToolbarItemModel item, {
    required String event,
    required String optionId,
  }) {
    if (event != 'select' || optionId.isEmpty || !item.isSelect) {
      return false;
    }
    switch (item.actionId.trim().toLowerCase()) {
      case 'select_model':
      case 'select_mode':
      case 'select_provider':
      case 'select_preset':
      case 'select_reasoning_effort':
      case 'select_service_tier':
      case 'select_sandbox_mode':
        return true;
      default:
        return false;
    }
  }

  bool _isSilentToolbarAckError({required String code, required String msg}) {
    final normalizedCode = code.trim().toLowerCase();
    final normalizedMsg = msg.trim().toLowerCase();
    if (normalizedCode == 'toolbar_mismatch' ||
        normalizedCode == 'invalid_action') {
      return true;
    }
    if (normalizedCode == 'action_failed' &&
        normalizedMsg.contains('toolbar') &&
        (normalizedMsg.contains('invalid') ||
            normalizedMsg.contains('mismatch') ||
            normalizedMsg.contains('not found'))) {
      return true;
    }
    return false;
  }
}
