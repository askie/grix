import 'package:get/get.dart';

class ConversationAuditPreferenceService extends GetxService {
  final Map<String, RxBool> _serverStates = <String, RxBool>{};

  /// Sends `conversation_audit_set` over the IM WebSocket; injected by the
  /// IM layer when the first snapshot carrying `audit_enabled` arrives.
  /// Returns whether the packet was sent.
  ///
  /// 位置参数形式：dart2wasm 的 wasm-opt 对命名参数函数类型字段的调用会
  /// 生成非法 wasm（`local.set's value type must be correct`），编译 Web 版失败。
  bool Function(String sessionId, String agentId, bool enabled)?
      serverStateSender;

  /// Feeds a server state delivered by a toolbar snapshot (`audit_enabled`).
  void applyServerState({required String agentId, required bool enabled}) {
    final id = agentId.trim();
    if (id.isEmpty) {
      return;
    }
    stateForAgent(agentId: id).value = enabled;
  }

  RxBool stateForAgent({required String agentId}) {
    return _serverStates.putIfAbsent(agentId.trim(), () => false.obs);
  }

  Future<void> setEnabled({
    required String agentId,
    required bool enabled,
    String sessionId = '',
  }) async {
    final id = agentId.trim();
    if (id.isEmpty) {
      return;
    }
    final state = stateForAgent(agentId: id);
    final previous = state.value;
    // 乐观更新；conversation_audit_set_resp 与随后的工具栏快照会校准为服务端真值。
    state.value = enabled;
    final sender = serverStateSender;
    var sent = false;
    try {
      // 拆成显式 if：避免 try 块内 `&&` 短路表达式触发 dart2wasm wasm-opt 校验 bug
      if (sender != null) {
        sent = sender(sessionId, id, enabled);
      }
    } catch (_) {
      sent = false;
    }
    if (!sent) {
      state.value = previous;
    }
  }

  Future<void> toggle({required String agentId, String sessionId = ''}) async {
    final state = stateForAgent(agentId: agentId);
    await setEnabled(
      agentId: agentId,
      enabled: !state.value,
      sessionId: sessionId,
    );
  }

  /// 账号切换时清空镜像与发送通道，避免上一个用户的开关状态泄漏给下一个用户。
  void resetForAccountSwitch() {
    _serverStates.clear();
    serverStateSender = null;
  }
}
