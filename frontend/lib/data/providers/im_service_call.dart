part of 'im_service.dart';

extension ImServiceCallExtension on ImService {
  /// 发送通话信令包（供 CallController 回调使用）。
  void sendCallPacket(Map<String, dynamic> packet) {
    _sendPacket(packet, requireAuthenticated: true);
  }

  /// 为会话开启语音托管（绑定 type=4 语音模型，来电自动代接）。
  void startVoiceDelegate(String sessionId, String agentId) {
    sendCallPacket({
      'cmd': 'call:voice_delegate_start',
      'payload': {'session_id': sessionId, 'agent_id': agentId},
    });
  }

  /// 取消会话语音托管。
  void stopVoiceDelegate(String sessionId) {
    sendCallPacket({
      'cmd': 'call:voice_delegate_stop',
      'payload': {'session_id': sessionId},
    });
  }

  /// 处理 call:voice_delegate_ack
  void _handleCallVoiceDelegateAck(Map<String, dynamic> payload) {
    final sessionId = payload['session_id']?.toString() ?? '';
    if (sessionId.isEmpty) return;
    if (payload['active'] == true) {
      voiceDelegateStates[sessionId] = payload['agent_id']?.toString() ?? '';
    } else {
      voiceDelegateStates.remove(sessionId);
    }
  }

  /// 处理 call:invite_ack 主叫发起确认（含 call_id + room token）
  void _handleCallInviteAck(Map<String, dynamic> payload) {
    if (!Get.isRegistered<CallController>()) return;
    Get.find<CallController>().onCallInviteAck(payload);
  }

  /// 处理 call:ring 来电通知
  void _handleCallRing(Map<String, dynamic> payload) {
    if (!Get.isRegistered<CallController>()) return;
    Get.find<CallController>().onCallRing(payload);
  }

  /// 处理 call:ai_delegated owner 的语音托管 AI 正在代接，可接管
  void _handleCallAiDelegated(Map<String, dynamic> payload) {
    if (!Get.isRegistered<CallController>()) return;
    Get.find<CallController>().onCallAiDelegated(payload);
  }

  /// 处理 call:listen_ack owner 旁听准入，拿到 callee token
  void _handleCallListenAck(Map<String, dynamic> payload) {
    if (!Get.isRegistered<CallController>()) return;
    Get.find<CallController>().onCallListenAck(payload);
  }

  /// 处理 call:voice_status_end 某通通话结束，清除会话"语音中"徽标
  void _handleCallVoiceStatusEnd(Map<String, dynamic> payload) {
    if (!Get.isRegistered<CallController>()) return;
    Get.find<CallController>().onCallVoiceStatusEnd(payload);
  }

  /// pull_sync 全量快照对账"语音中"徽标集合（以服务端为准整份覆盖）。
  /// 与 unread_snapshot 同源同语义，统一在重连/上线时自愈，
  /// 不再依赖单独的 OnUserRegistered 重推。
  void applyVoiceCallSnapshot(List<Map<String, dynamic>> calls) {
    if (!Get.isRegistered<CallController>()) return;
    Get.find<CallController>().applyDelegatedSnapshot(calls);
  }

  /// 处理 call:peer_answered 对方已接
  void _handleCallPeerAnswered(Map<String, dynamic> payload) {
    if (!Get.isRegistered<CallController>()) return;
    Get.find<CallController>().onCallPeerAnswered(payload);
  }

  /// 处理 call:state 状态广播
  void _handleCallState(Map<String, dynamic> payload) {
    if (!Get.isRegistered<CallController>()) return;
    Get.find<CallController>().onCallState(payload);
  }

  /// 处理 call:queued 进入排队等待
  void _handleCallQueued(Map<String, dynamic> payload) {
    if (!Get.isRegistered<CallController>()) return;
    Get.find<CallController>().onCallQueued(payload);
  }

  /// 处理 call:queue_update 排队位置更新
  void _handleCallQueueUpdate(Map<String, dynamic> payload) {
    if (!Get.isRegistered<CallController>()) return;
    Get.find<CallController>().onCallQueueUpdate(payload);
  }

  /// 处理 call:queue_expired 排队超时
  void _handleCallQueueExpired(Map<String, dynamic> payload) {
    if (!Get.isRegistered<CallController>()) return;
    Get.find<CallController>().onCallQueueExpired(payload);
  }
}
