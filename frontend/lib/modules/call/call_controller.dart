import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';
import 'package:get/get.dart';
import 'package:livekit_client/livekit_client.dart';
import 'package:wakelock_plus/wakelock_plus.dart';

import '../../data/providers/im_service.dart';
import '../../data/providers/feature_flag_service.dart';
import '../../shared/utils/toast_util.dart';
import 'call_state.dart';

/// 通话控制器（GetX），管理通话状态机和 LiveKit Room 连接。
class CallController extends GetxController {
  static const _audioSessionChannel = MethodChannel(
    'pub.dhf.grix/audio_session',
  );

  final _session = Rxn<CallSession>();
  Room? _room;
  bool _wakelockOn = false; // 当前是否已请求屏幕常亮（避免重复调用原生）
  Timer? _connectWatchdog; // 测试拨打：超时未连入则收尾
  // 诊断：麦克风发布后延迟自检音轨状态的计时器。
  Timer? _micTrackCheckTimer;
  // 健壮性：麦克风采集恢复进行中标志，避免重入/重复重建。
  bool _micRecovering = false;
  // 健壮性：通话期间持续监听房间人员/音轨变化的订阅句柄（结束时取消）。
  CancelListenFunc? _micGuardSub;
  // 健壮性：LiveKit 房间生命周期（断连/重连）订阅句柄，防通话假死（所有平台）。
  CancelListenFunc? _roomLifecycleSub;
  // 健壮性：AI(语音桥)从房间消失后的兜底超时(副本崩溃则不回，超时结束)。
  Timer? _aiBotGoneTimer;
  // 健壮性：房间事件触发采集保障的去抖计时器（多人同时进出时合并）。
  Timer? _micGuardDebounce;
  Timer? _listenWatchdog; // 旁听：发出 call:listen 后超时未收到 ack 则收尾
  Function(Map<String, dynamic>)? _sendWs;
  List<Map<String, dynamic>>? _iceServers; // 服务端下发的 TURN/STUN 配置

  /// AI 正在代接中的多通通话（key=sessionId）。驱动会话列表"语音中"徽标。
  /// owner 不为这些通话预先连房；点"通话"时才经 call:listen 懒连接其中一通。
  final _delegatedCalls = <String, DelegatedCallInfo>{}.obs;
  RxMap<String, DelegatedCallInfo> get delegatedCalls => _delegatedCalls;

  /// 某会话当前是否有 AI 代接中的语音通话（供会话列表/会话页判断徽标与"通话"入口）。
  bool hasVoiceCallForSession(String sessionId) =>
      _delegatedCalls.containsKey(sessionId);

  /// 来电弹窗构建器，由 app 启动时注入（避免循环 import）
  static WidgetBuilder? incomingDialogBuilder;

  // --- 折叠/展开状态 ---

  /// 通话是否已折叠为顶部横幅
  final _isMinimized = false.obs;
  bool get isMinimized => _isMinimized.value;

  /// 通话计时器（从 ActiveCallDialog 迁入，持久化跨折叠/展开）
  final callStopwatch = Stopwatch();

  /// 静音状态（持久化跨折叠/展开）
  final isMuted = false.obs;

  /// 扬声器状态（持久化跨折叠/展开）
  final isSpeakerOn = false.obs;

  CallSession? get session => _session.value;
  bool get isInCall =>
      _session.value != null && _session.value!.state != CallState.ended;

  /// 待命档：窗口已打开但未连入房间（不听不说）。四档中的默认档。
  final isStandby = false.obs;

  /// 待命/旁听时记住的会话 ID，供从待命切回旁听时重新 call:listen。
  String? _modeSessionId;

  /// 连入房间后要落到的目标档（待命直接切"加入/接管"时，先连房再应用）。
  CallMode? _pendingMode;

  /// 主人当前所处的参与档（由 标志 + 通话状态 + 静音 推导）。
  CallMode get callMode {
    if (isStandby.value) return CallMode.standby;
    final s = _session.value;
    if (s == null) return CallMode.standby;
    if (s.state == CallState.humanActive) return CallMode.takeover;
    if (s.state == CallState.aiDelegated) {
      return isMuted.value ? CallMode.listening : CallMode.joined;
    }
    // connecting 等过渡态：显示目标档或旁听
    return _pendingMode ?? CallMode.listening;
  }

  /// 是否处于活跃通话（非振铃、非空闲、非结束）
  bool get isActiveCallOverlayVisible {
    final s = _session.value;
    if (s == null) return false;
    final state = s.state;
    return state == CallState.connecting ||
        state == CallState.active ||
        state == CallState.aiDelegated ||
        state == CallState.humanActive ||
        state == CallState.queued;
  }

  void minimize() => _isMinimized.value = true;
  void expand() => _isMinimized.value = false;

  // --- WS 事件入口 ---

  void onCallRing(Map<String, dynamic> payload) {
    if (!Get.find<FeatureFlagService>().isEnabled('voice_call')) return;
    final callId = payload['call_id']?.toString() ?? '';
    final callerIdStr = payload['caller_id']?.toString() ?? '0';
    final callerName = payload['caller_name']?.toString() ?? '';
    final callMode = (payload['call_mode'] as num?)?.toInt() ?? 1;

    _session.value = CallSession(
      callId: callId,
      peerId: callerIdStr,
      peerName: callerName,
      callMode: callMode,
      state: CallState.ringing,
    );

    WidgetsBinding.instance.addPostFrameCallback(
      (_) => _showIncomingDialog(incomingDialogBuilder),
    );
  }

  /// 主叫方收到 invite_ack：更新 call_id，连接 LiveKit Room。
  /// 直拨 AI 通话时保持 connecting 状态，等待 AI participant 加入房间后才切换到 aiDelegated。
  void onCallInviteAck(Map<String, dynamic> payload) {
    if (!Get.find<FeatureFlagService>().isEnabled('voice_call')) return;
    final callId = payload['call_id']?.toString() ?? '';
    final roomToken = payload['room_token']?.toString() ?? '';
    final roomUrl = payload['room_url']?.toString() ?? '';
    // 解析服务端下发的 TURN/STUN ICE 服务器
    final rawIce = payload['ice_servers'];
    if (rawIce is List && rawIce.isNotEmpty) {
      _iceServers = rawIce.whereType<Map<String, dynamic>>().toList();
    }
    _diag(
      'invite_ack_received callId=$callId roomUrl=$roomUrl tokenLen=${roomToken.length} iceServers=${_iceServers?.length ?? 0}',
    );
    if (callId.isEmpty) {
      _diag('invite_ack_SKIPPED callId_empty=true');
      return;
    }
    _connectWatchdog?.cancel();
    _diag('invite_ack_watchdog_cancelled');

    _session.value = _session.value?.copyWith(
      callId: callId,
      roomToken: roomToken,
      roomUrl: roomUrl,
      // 直拨 AI：收到 invite_ack 表示房间已建，进入"等待 AI 接入"阶段
      connectingPhase:
          _session.value?.connectingPhase == ConnectingPhase.launching
          ? ConnectingPhase.waiting
          : null,
    );

    if (roomToken.isNotEmpty) _connectRoom(roomUrl, roomToken).ignore();
  }

  /// owner 收到 call:ai_delegated：其语音托管 AI 正在代接一通来电。
  /// 多访客客服模型：仅登记到 _delegatedCalls 驱动会话列表"语音中"徽标，
  /// 不再自动连房；owner 点会话内"通话"按钮时才经 call:listen 懒连接旁听。
  void onCallAiDelegated(Map<String, dynamic> payload) {
    if (!Get.find<FeatureFlagService>().isEnabled('voice_call')) return;
    final callId = payload['call_id']?.toString() ?? '';
    final sessionId = payload['session_id']?.toString() ?? '';
    if (callId.isEmpty || sessionId.isEmpty) return;
    _delegatedCalls[sessionId] = DelegatedCallInfo(
      callId: callId,
      sessionId: sessionId,
      peerName: payload['peer_name']?.toString() ?? '',
    );
    _diag(
      'ai_delegated_registered call=$callId session=$sessionId total=${_delegatedCalls.length}',
    );
  }

  /// owner 在会话内点"通话"：进房旁听该会话当前 AI 代接的通话。
  /// 人单线：若已在另一通话内，先对其发 call:leave 交回 AI（带提醒），再 call:listen 新通。
  void listenToDelegatedCall(
    String sessionId,
    Function(Map<String, dynamic>) sendWs,
  ) {
    if (!Get.find<FeatureFlagService>().isEnabled('voice_call')) return;
    final info = _delegatedCalls[sessionId];
    if (info == null) return;
    _sendWs = sendWs;
    _modeSessionId = sessionId;
    isStandby.value = false;

    // 已在另一通话中（旁听/接管）→ 先交回 AI 再切换
    final cur = _session.value;
    if (cur != null &&
        cur.state != CallState.ended &&
        cur.callId != info.callId) {
      sendWs({
        'cmd': 'call:leave',
        'payload': {'call_id': cur.callId},
      });
      _teardownRoom();
      CustomToast.show('call_switch_handed_back'.tr);
    }

    // 进入 connecting（待 listen_ack 返回 token 后连房旁听）
    _session.value = CallSession(
      callId: info.callId,
      peerId: '',
      peerName: info.peerName,
      callMode: 1,
      state: CallState.connecting,
    ).copyWith(delegationMode: DelegationMode.aiDelegated);

    sendWs({
      'cmd': 'call:listen',
      'payload': {'call_id': info.callId},
    });

    _listenWatchdog?.cancel();
    _listenWatchdog = Timer(const Duration(seconds: 10), () {
      if (_session.value?.callId == info.callId &&
          _session.value?.state == CallState.connecting) {
        _diag('listen_watchdog_FIRE call=${info.callId}');
        CustomToast.show('call_listen_failed'.tr);
        _teardownRoom();
        _session.value = null;
      }
    });
  }

  /// 收到 call:listen_ack：拿到 callee token，静音进房旁听。
  void onCallListenAck(Map<String, dynamic> payload) {
    if (!Get.find<FeatureFlagService>().isEnabled('voice_call')) return;
    _listenWatchdog?.cancel();
    final callId = payload['call_id']?.toString() ?? '';
    final roomToken = payload['room_token']?.toString() ?? '';
    final roomUrl = payload['room_url']?.toString() ?? '';
    final rawIce = payload['ice_servers'];
    if (rawIce is List && rawIce.isNotEmpty) {
      _iceServers = rawIce.whereType<Map<String, dynamic>>().toList();
    }
    if (callId.isEmpty || _session.value?.callId != callId) return;
    isStandby.value = false;
    isMuted.value = true;
    _session.value = _session.value?.copyWith(
      state: CallState.aiDelegated,
      delegationMode: DelegationMode.aiDelegated,
      roomToken: roomToken,
      roomUrl: roomUrl,
    );
    if (roomToken.isNotEmpty) _connectRoom(roomUrl, roomToken).ignore();
    _startStopwatchIfNeeded();
  }

  /// 收到 call:queued：访客进入等待队列，显示排队界面。
  void onCallQueued(Map<String, dynamic> payload) {
    if (!Get.find<FeatureFlagService>().isEnabled('voice_call')) return;
    final callId = payload['call_id']?.toString() ?? '';
    final position = (payload['position'] as num?)?.toInt() ?? 1;
    if (callId.isEmpty) return;
    _session.value = _session.value?.copyWith(
      callId: callId,
      state: CallState.queued,
      queuePosition: position,
    );
    _diag('call_queued callId=$callId position=$position');
  }

  /// 收到 call:queue_update：排队位置变化，更新显示。
  void onCallQueueUpdate(Map<String, dynamic> payload) {
    final position = (payload['position'] as num?)?.toInt() ?? 1;
    if (_session.value?.state == CallState.queued) {
      _session.value = _session.value?.copyWith(queuePosition: position);
    }
    _diag('call_queue_update position=$position');
  }

  /// 收到 call:queue_expired：排队超时，关闭通话界面并提示。
  void onCallQueueExpired(Map<String, dynamic> payload) {
    if (_session.value?.state == CallState.queued) {
      _endCall();
      CustomToast.show('call_queue_expired'.tr);
    }
    _diag('call_queue_expired');
  }

  /// 收到 call:voice_status_end：某通通话已结束，清除徽标；若 owner 正在其中则收尾。
  void onCallVoiceStatusEnd(Map<String, dynamic> payload) {
    final callId = payload['call_id']?.toString() ?? '';
    final sessionId = payload['session_id']?.toString() ?? '';
    if (sessionId.isNotEmpty) _delegatedCalls.remove(sessionId);
    if (callId.isNotEmpty && _session.value?.callId == callId) {
      _endCall();
    }
    _diag(
      'voice_status_end call=$callId session=$sessionId remaining=${_delegatedCalls.length}',
    );
  }

  /// pull_sync 全量对账：用服务端下发的"语音中"快照整份覆盖本地徽标集合。
  /// 这是"语音中"状态的权威来源——离线期间漏收的开始(call:ai_delegated)或
  /// 结束(call:voice_status_end)通知，都会在重连 pull_sync 时由这份快照纠正，
  /// 修复"通话已结束但麦克风/徽标仍残留"的卡死。空快照表示当前无进行中通话，整体清空。
  void applyDelegatedSnapshot(List<Map<String, dynamic>> rawCalls) {
    if (!Get.find<FeatureFlagService>().isEnabled('voice_call')) return;
    final next = <String, DelegatedCallInfo>{};
    for (final c in rawCalls) {
      final callId = c['call_id']?.toString() ?? '';
      final sessionId = c['session_id']?.toString() ?? '';
      if (callId.isEmpty || sessionId.isEmpty) continue;
      next[sessionId] = DelegatedCallInfo(
        callId: callId,
        sessionId: sessionId,
        peerName: c['peer_name']?.toString() ?? '',
      );
    }
    _delegatedCalls.assignAll(next);
    _diag('delegated_snapshot_applied total=${_delegatedCalls.length}');
  }

  /// owner 离开当前旁听/接管的通话（不结束）：交回 AI，访客继续与 AI 通话。
  /// 与 hangup（结束整通）区分。该会话仍保留"语音中"徽标。
  void leaveCall(Function(Map<String, dynamic>) sendWs) {
    final s = _session.value;
    if (s == null) return;
    sendWs({
      'cmd': 'call:leave',
      'payload': {'call_id': s.callId},
    });
    _teardownRoom();
    _session.value = null;
  }

  void onCallPeerAnswered(Map<String, dynamic> payload) {
    if (!Get.find<FeatureFlagService>().isEnabled('voice_call')) return;
    final callId = payload['call_id']?.toString() ?? '';
    final mode = payload['mode']?.toString() ?? 'human';
    final roomToken = payload['room_token']?.toString() ?? '';
    final roomUrl = payload['room_url']?.toString() ?? '';
    _diag('peer_answered callId=$callId mode=$mode');

    if (_session.value?.callId != callId) return;

    final newState = mode == 'ai_delegated'
        ? CallState.aiDelegated
        : CallState.active;
    final newDelegation = mode == 'ai_delegated'
        ? DelegationMode.aiDelegated
        : DelegationMode.human;

    _session.value = _session.value?.copyWith(
      state: newState,
      delegationMode: newDelegation,
      roomToken: roomToken.isNotEmpty ? roomToken : null,
      roomUrl: roomUrl.isNotEmpty ? roomUrl : null,
    );

    // AI 代接时 callee 以监听者身份加入，不发布音频
    if (mode == 'ai_delegated') {
      isMuted.value = true;
    } else {
      isMuted.value = false;
    }

    if (roomToken.isNotEmpty) _connectRoom(roomUrl, roomToken).ignore();
    _startStopwatchIfNeeded();
  }

  /// Phase 2: 处理 AI 托管状态变更（call:state 中含 mode 字段）
  void onCallState(Map<String, dynamic> payload) {
    if (!Get.find<FeatureFlagService>().isEnabled('voice_call')) return;
    final callId = payload['call_id']?.toString() ?? '';
    final state = (payload['state'] as num?)?.toInt();
    final mode = payload['mode']?.toString();
    final sessionCallId = _session.value?.callId ?? '';
    _diag(
      'call_state_event callId=$callId state=$state mode=$mode sessionCallId=$sessionCallId',
    );

    if (_session.value?.callId != callId) {
      _diag('call_state_IGNORED callId_mismatch');
      return;
    }

    final reason = payload['reason']?.toString() ?? '';
    if (reason == 'answered_elsewhere') {
      _diag('call_state_END_CALL answered_elsewhere');
      _endCall();
      return;
    }

    if (mode == 'human_active') {
      _session.value = _session.value?.copyWith(
        state: CallState.humanActive,
        delegationMode: DelegationMode.mixed,
      );
      // 仅 owner（被叫/接管方）才在接管时激活麦克风；visitor 不操控麦克风
      if (_session.value?.isCaller == false) {
        _syncMuteState(shouldBeMuted: false);
      }
      return;
    }
    if (mode == 'ai_delegated') {
      _session.value = _session.value?.copyWith(
        state: CallState.aiDelegated,
        delegationMode: DelegationMode.aiDelegated,
      );
      // AI 重新接管，关闭麦克风
      _syncMuteState(shouldBeMuted: true);
      return;
    }

    // 通话结束（state >= 2，或收到 busy/timeout 但无 state 字段）
    if (state != null && state >= 2) {
      _diag('call_state_END_CALL server_state=$state reason=$reason');
      _endCall();
    } else if (state == null && reason.isNotEmpty) {
      // call:busy / call:timeout 等无 state 字段的终止信令
      _diag('call_state_END_CALL no_state reason=$reason');
      _endCall();
    }
  }

  // --- 用户操作 ---

  /// 主叫方发起通话
  Future<void> inviteCall(
    String peerId,
    String peerName,
    Function(Map<String, dynamic>) sendWs,
  ) async {
    if (!Get.find<FeatureFlagService>().isEnabled('voice_call')) return;
    if (!await _ensureMicPermission()) return;
    _sendWs = sendWs;
    sendWs({
      'cmd': 'call:invite',
      'payload': {'peer_id': peerId, 'peer_type': 'user', 'call_mode': 1},
    });
    _session.value = CallSession(
      callId: '',
      peerId: peerId,
      peerName: peerName,
      callMode: 1,
      state: CallState.ringing,
      isCaller: true,
    );
  }

  /// owner 直接拨打语音大模型 agent。
  /// 仅 Web/桌面启用；服务端回 invite_ack 后经 onCallInviteAck 连入房间。
  Future<void> directCallAgent(
    String agentId,
    String agentName,
    Function(Map<String, dynamic>) sendWs,
  ) async {
    _diag(
      'test_start agentId=$agentId kIsWeb=$kIsWeb platform=${defaultTargetPlatform.name}',
    );
    if (!Get.find<FeatureFlagService>().isEnabled('voice_call')) {
      _diag('direct_ai_start_BLOCKED capability_disabled');
      return;
    }

    _diag('direct_ai_ensure_mic_before');
    final micOk = await _ensureMicPermission();
    _diag('direct_ai_ensure_mic_after granted=$micOk');
    if (!micOk) return;

    // iOS Safari getUserMedia 权限弹窗可能冻结页面，导致 WS 断连，等待恢复后再发。
    _diag('direct_ai_wait_ws_before');
    final wsWaitMs = await _waitForWsReady();
    _diag('direct_ai_wait_ws_after waitMs=$wsWaitMs');

    _sendWs = sendWs;

    // 检查 WS 连接状态
    bool wsConnected = false;
    bool wsAuthed = false;
    try {
      final im = Get.find<ImService>();
      wsConnected = im.isConnected;
      wsAuthed = im.isAuthenticated;
    } catch (_) {}
    _diag('direct_ai_send_before wsConnected=$wsConnected wsAuthed=$wsAuthed');

    sendWs({
      'cmd': 'call:direct_ai',
      'payload': {'agent_id': agentId},
    });
    _diag('direct_ai_send_after call:direct_ai sent');

    // 初始 connecting 状态：等待 LiveKit 房间中 AI participant 加入后才切换到
    // aiDelegated 并开始计时。invite_ack 只是说明房间已创建，不代表 AI 已就绪。
    _session.value =
        CallSession(
          callId: '',
          peerId: '',
          peerName: agentName,
          callMode: 1,
          state: CallState.connecting,
          isCaller: true,
        ).copyWith(
          delegationMode: DelegationMode.aiDelegated,
          agentId: agentId,
          agentName: agentName,
          connectingPhase: ConnectingPhase.launching,
        );
    // 超时仍未收到 invite_ack（起会失败/被拒）则收尾，避免通话框卡住。
    // 服务端 invite_ack 延迟可能超过 20 秒（voicebridge 启动 + Egress 录制尝试超时），
    // 因此 watchdog 设为 45 秒以覆盖最坏情况。
    _connectWatchdog?.cancel();
    _connectWatchdog = Timer(const Duration(seconds: 45), () {
      final callId = _session.value?.callId ?? '';
      final stillInCall = isInCall;
      _diag(
        'watchdog_FIRE callId=$callId isEmpty=${callId.isEmpty} isInCall=$stillInCall',
      );
      if (isInCall && (_session.value?.callId.isEmpty ?? true)) {
        _endCall();
      }
    });
    _diag('direct_ai_watchdog_set 45s');
    _diag('direct_ai_end_dialog_shown');
  }

  /// 语音大脑通话：在当前会话里，用用户级语音大脑当语音通道呼出。
  /// agentId 是文字 agent；sessionId 是发起时所在会话（转写落此会话）。
  Future<void> voiceBrainCallAgent(
    String agentId,
    String agentName,
    String sessionId,
    Function(Map<String, dynamic>) sendWs,
  ) async {
    _diag(
      'voice_brain_start agentId=$agentId kIsWeb=$kIsWeb platform=${defaultTargetPlatform.name}',
    );
    if (!Get.find<FeatureFlagService>().isEnabled('voice_brain')) {
      _diag('voice_brain_start_BLOCKED capability_disabled');
      return;
    }

    _diag('voice_brain_ensure_mic_before');
    final micOk = await _ensureMicPermission();
    _diag('voice_brain_ensure_mic_after granted=$micOk');
    if (!micOk) return;

    // iOS Safari getUserMedia 权限弹窗可能冻结页面，导致 WS 断连，等待恢复后再发。
    _diag('voice_brain_wait_ws_before');
    final wsWaitMs = await _waitForWsReady();
    _diag('voice_brain_wait_ws_after waitMs=$wsWaitMs');

    _sendWs = sendWs;

    bool wsConnected = false;
    bool wsAuthed = false;
    try {
      final im = Get.find<ImService>();
      wsConnected = im.isConnected;
      wsAuthed = im.isAuthenticated;
    } catch (_) {}
    _diag(
      'voice_brain_send_before wsConnected=$wsConnected wsAuthed=$wsAuthed',
    );

    sendWs({
      'cmd': 'call:voice_brain',
      'payload': {'agent_id': agentId, 'session_id': sessionId},
    });
    _diag('voice_brain_send_after call:voice_brain sent');

    _session.value =
        CallSession(
          callId: '',
          peerId: '',
          peerName: agentName,
          callMode: 1,
          state: CallState.connecting,
          isCaller: true,
        ).copyWith(
          delegationMode: DelegationMode.aiDelegated,
          agentId: agentId,
          agentName: agentName,
          connectingPhase: ConnectingPhase.launching,
        );
    _connectWatchdog?.cancel();
    _connectWatchdog = Timer(const Duration(seconds: 45), () {
      final callId = _session.value?.callId ?? '';
      final stillInCall = isInCall;
      _diag(
        'voice_brain_watchdog_FIRE callId=$callId isEmpty=${callId.isEmpty} isInCall=$stillInCall',
      );
      if (isInCall && (_session.value?.callId.isEmpty ?? true)) {
        _endCall();
      }
    });
    _diag('voice_brain_watchdog_set 45s');
  }

  /// 真人接听
  Future<void> answer(Function(Map<String, dynamic>) sendWs) async {
    final s = _session.value;
    if (s == null || s.state != CallState.ringing) return;
    if (!await _ensureMicPermission()) return;
    _sendWs = sendWs;
    sendWs({
      'cmd': 'call:answer',
      'payload': {'call_id': s.callId},
    });
    _session.value = s.copyWith(state: CallState.connecting);
  }

  /// Phase 2: AI 代接，选定 agent 后调用
  void answerWithAI(
    String agentId,
    String agentName,
    Function(Map<String, dynamic>) sendWs,
  ) {
    final s = _session.value;
    if (s == null || s.state != CallState.ringing) return;
    _sendWs = sendWs;
    sendWs({
      'cmd': 'call:answer_with_ai',
      'payload': {'call_id': s.callId, 'agent_id': agentId},
    });
    _session.value = s.copyWith(
      state: CallState.connecting,
      delegationMode: DelegationMode.aiDelegated,
      agentId: agentId,
      agentName: agentName,
    );
  }

  /// Phase 2: B 接管（AI 静默旁听）
  void takeover(Function(Map<String, dynamic>) sendWs) {
    final s = _session.value;
    if (s == null || s.state != CallState.aiDelegated) return;
    sendWs({
      'cmd': 'call:takeover',
      'payload': {'call_id': s.callId},
    });
    _session.value = s.copyWith(
      state: CallState.humanActive,
      delegationMode: DelegationMode.mixed,
    );
    // 真人接管，开启麦克风
    _syncMuteState(shouldBeMuted: false);
  }

  /// Phase 2: B 将通话交回给 AI
  void handBack(Function(Map<String, dynamic>) sendWs) {
    final s = _session.value;
    if (s == null || s.state != CallState.humanActive) return;
    sendWs({
      'cmd': 'call:hand_back',
      'payload': {'call_id': s.callId},
    });
    _session.value = s.copyWith(
      state: CallState.aiDelegated,
      delegationMode: DelegationMode.aiDelegated,
    );
    // AI 重新接管，关闭麦克风
    _syncMuteState(shouldBeMuted: true);
  }

  // ─── 四档参与模型（待命/旁听/加入/接管）─────────────────────────

  /// 打开通话窗口并停在「待命」：不连房间、不听不说。四档界面默认入口。
  void openStandby(String sessionId, Function(Map<String, dynamic>) sendWs) {
    if (!Get.find<FeatureFlagService>().isEnabled('voice_call')) return;
    final info = _delegatedCalls[sessionId];
    if (info == null) return;
    _sendWs = sendWs;
    // 已在另一通话中（旁听/加入/接管）→ 先离开
    final cur = _session.value;
    if (cur != null &&
        cur.state != CallState.ended &&
        cur.callId != info.callId) {
      sendWs({
        'cmd': 'call:leave',
        'payload': {'call_id': cur.callId},
      });
      _teardownRoom();
    }
    _modeSessionId = sessionId;
    _pendingMode = null;
    isStandby.value = true;
    // 通话本身是 AI 代接中；待命仅表示 owner 未连入房间。
    _session.value = CallSession(
      callId: info.callId,
      peerId: '',
      peerName: info.peerName,
      callMode: 1,
      state: CallState.aiDelegated,
    ).copyWith(delegationMode: DelegationMode.aiDelegated);
  }

  /// 切到「待命」：离开房间但保留窗口（AI 继续与访客通话）。
  void goStandby(Function(Map<String, dynamic>) sendWs) {
    final s = _session.value;
    if (s == null) return;
    // 接管中先交回，避免离开后把访客晾给静音的 AI
    if (s.state == CallState.humanActive) {
      sendWs({
        'cmd': 'call:hand_back',
        'payload': {'call_id': s.callId},
      });
    }
    if (_room != null) {
      sendWs({
        'cmd': 'call:leave',
        'payload': {'call_id': s.callId},
      });
      _teardownRoom();
    }
    _pendingMode = null;
    isStandby.value = true;
    _session.value = s.copyWith(
      state: CallState.aiDelegated,
      delegationMode: DelegationMode.aiDelegated,
    );
  }

  /// 四档选择器统一入口：把主人参与档切到 target。
  void setCallMode(CallMode target, Function(Map<String, dynamic>) sendWs) {
    if (!Get.find<FeatureFlagService>().isEnabled('voice_call')) return;
    if (callMode == target) return;
    _sendWs = sendWs;
    final s = _session.value;
    if (s == null) return;

    // 待命（未连房）出发：先连房旁听，连上后再落到目标档
    if (isStandby.value) {
      if (target == CallMode.standby || _modeSessionId == null) return;
      _pendingMode = target;
      isStandby.value = false;
      listenToDelegatedCall(_modeSessionId!, sendWs);
      return;
    }

    switch (target) {
      case CallMode.standby:
        goStandby(sendWs);
        break;
      case CallMode.listening:
        if (s.state == CallState.humanActive) {
          handBack(sendWs); // 交回 AI（会关麦）→ 旁听
        } else {
          _syncMuteState(shouldBeMuted: true); // aiDelegated：关麦
        }
        break;
      case CallMode.joined:
        if (s.state == CallState.humanActive) {
          handBack(sendWs); // 先交回 AI（会关麦）
        }
        _syncMuteState(shouldBeMuted: false); // 开麦，AI 继续说 → 三方
        break;
      case CallMode.takeover:
        if (s.state == CallState.aiDelegated) {
          takeover(sendWs); // AI 静音 + 开麦
        }
        break;
    }
  }

  /// 连房成功后落到待命直切的目标档（仅 setCallMode 从待命直切加入/接管时设置）。
  void _applyPendingModeAfterConnect() {
    final pending = _pendingMode;
    _pendingMode = null;
    if (pending == null) return;
    if (pending == CallMode.joined) {
      _syncMuteState(shouldBeMuted: false);
    } else if (pending == CallMode.takeover) {
      if (_session.value?.state == CallState.aiDelegated && _sendWs != null) {
        takeover(_sendWs!);
      }
    }
  }

  void reject(Function(Map<String, dynamic>) sendWs) {
    final s = _session.value;
    if (s == null || s.state != CallState.ringing) return;
    sendWs({
      'cmd': 'call:reject',
      'payload': {'call_id': s.callId, 'reason': 'rejected'},
    });
    _endCall();
  }

  /// 只关闭当前设备的来电界面，不影响其它设备继续接听。
  void dismissIncoming() {
    final s = _session.value;
    if (s == null || s.state != CallState.ringing) return;
    _endCall();
  }

  void hangup(Function(Map<String, dynamic>) sendWs) {
    final s = _session.value;
    if (s == null) return;
    sendWs({
      'cmd': 'call:hangup',
      'payload': {'call_id': s.callId},
    });
    _endCall();
  }

  // --- LiveKit 控制 ---

  /// 首次进入活跃通话状态时启动计时器
  void _startStopwatchIfNeeded() {
    if (!callStopwatch.isRunning) {
      callStopwatch
        ..reset()
        ..start();
    }
  }

  /// 切换静音状态
  Future<void> setMuted(bool muted) async {
    try {
      await _room?.localParticipant?.setMicrophoneEnabled(!muted);
    } catch (e) {
      debugPrint('LiveKit setMuted error: $e');
    }
  }

  /// 根据通话状态同步麦克风：AI 托管时静音，真人接管/通话时开麦。
  void _syncMuteState({required bool shouldBeMuted}) {
    isMuted.value = shouldBeMuted;
    setMuted(shouldBeMuted);
    _diag('sync_mute_state muted=$shouldBeMuted');
    // 健壮性：从“不发麦”切到“要发麦”（旁听→加入/接管）时，保障采集已就绪。
    if (!shouldBeMuted) {
      _ensureMicCaptureActive('mode_unmute', forceRebuild: true);
    }
  }

  /// 切换扬声器/听筒
  Future<void> setSpeakerOn(bool on) async {
    try {
      await Hardware.instance.setSpeakerphoneOn(on);
    } catch (e) {
      debugPrint('Hardware setSpeakerOn error: $e');
    }
  }

  // --- 内部 ---

  /// 统一诊断日志，带时间戳，方便在 Safari Web Inspector Console 中过滤 `[CallDiag]`。
  void _diag(String msg) {
    final ts = DateTime.now().millisecondsSinceEpoch;
    debugPrint('[CallDiag] $ts $msg');
  }

  /// 显示来电弹窗（仅用于 IncomingCallDialog）
  void _showIncomingDialog(WidgetBuilder? builder) {
    if (builder == null) return;
    Get.dialog(
      Builder(builder: builder),
      barrierDismissible: false,
      barrierColor: const Color(0xDD000000),
      useSafeArea: false,
    );
  }

  Future<void> _connectRoom(String url, String token) async {
    if (url.isEmpty || token.isEmpty) return;
    _diag(
      'room_connect_start url=$url tokenLen=${token.length} iceServers=${_iceServers?.length ?? 0}',
    );
    _reportDiag('room_connect_start');
    try {
      _room = Room();
      // 构建 ConnectOptions：传入服务端下发的 TURN/STUN ICE 服务器用于 NAT 穿透
      ConnectOptions connectOpts = const ConnectOptions();
      if (_iceServers != null && _iceServers!.isNotEmpty) {
        final iceServers = _iceServers!.map((s) {
          final urls = (s['urls'] as List?)?.cast<String>() ?? <String>[];
          return RTCIceServer(
            urls: urls.isNotEmpty ? urls : null,
            username: s['username'] as String?,
            credential: s['credential'] as String?,
          );
        }).toList();
        connectOpts = ConnectOptions(
          rtcConfiguration: RTCConfiguration(iceServers: iceServers),
        );
      }
      await _room!.connect(url, token, connectOptions: connectOpts);
      // 网页端：浏览器 autoplay 策略会拦截远端音频播放（旁听不开麦时尤甚，
      // 会出现"连上了却没声"）。在用户点击的手势窗口内主动解锁一次远端音频。
      if (kIsWeb && _room != null && !_room!.canPlaybackAudio) {
        try {
          await _room!.startAudio();
          _reportDiag('web_audio_unlocked');
        } catch (e) {
          _reportDiag('web_audio_unlock_error', detail: e.toString());
        }
      }
      _reportDiag('room_connect_ok');
      _diag('room_connect_ok');
      // AI 托管状态下以监听者身份加入，不发布音频；其余状态正常开麦。
      final shouldPublish = _session.value?.state != CallState.aiDelegated;
      // 强制重新配置并激活 iOS AudioSession（必须在 setMicrophoneEnabled 之前调用）。
      // LiveKit SDK 的自动音频管理依赖全局静态音轨计数器（_remoteTrackCount / _audioTrackState），
      // 仅在状态变化时才调 Native.configureAudio（setCategory + setActive(true)）。跨通话计数残留
      // 会让本通状态不变化而跳过配置；叠加上一通结束时 releaseAudioSession 的 setActive(false)，
      // 会话被停用后再不会被重新激活，导致麦克风采集全静音（peak=0/rms=0）。
      // setSpeakerphoneOn 是 SDK 公开 API，会无条件触发 configureAudio 把会话拉回 active，
      // 复用 SDK 自身机制而非裸操作 AVAudioSession。
      // 注意：必须在 setMicrophoneEnabled 之前调用，否则 LiveKit Flutter SDK 2.7.0 下
      // configureAudio 的重配置会中断已启动的麦克风采集，导致全静音。
      final speakerOn = shouldPublish ? isSpeakerOn.value : true;
      await Hardware.instance.setSpeakerphoneOn(speakerOn);
      // 同步扬声器状态：旁听/代接进房强制外放(true)，但此前只调了硬件、未回写
      // isSpeakerOn，导致随后 _ensureMicCaptureActive 用旧值(false)重激活时把音频
      // 切回听筒，接管后外放没声。回写后状态与实际一致，UI 扬声器开关也不再错位。
      isSpeakerOn.value = speakerOn;
      // 诊断：上报外放/听筒状态（speaker=true 即外放），验证外放假设。
      _reportDiag('audio_session_reconfigured', detail: 'speaker=$speakerOn');
      _diag('audio_session_reconfigured speaker=$speakerOn');
      // AudioSession 已激活，再发麦克风。
      await _room!.localParticipant?.setMicrophoneEnabled(shouldPublish);
      isMuted.value = !shouldPublish;
      _reportDiag(
        shouldPublish ? 'mic_publish_ok' : 'mic_publish_skipped_ai_delegated',
      );
      _diag(
        shouldPublish ? 'mic_publish_ok' : 'mic_publish_skipped_ai_delegated',
      );
      // 诊断+健壮性：发布后立即记录音轨状态，并在 2.5s 后再检测一次；
      // 若届时音轨异常则自动重建采集（兜底）。
      if (shouldPublish) {
        _reportMicTrackState('after_publish');
        _micTrackCheckTimer?.cancel();
        _micTrackCheckTimer = Timer(const Duration(milliseconds: 2500), () {
          _reportMicTrackState('after_2500ms');
          _ensureMicCaptureActive('delayed_check');
        });
      }
      // 健壮性：整通通话期间持续监听房间人员/音轨变化。任何改变音轨数量的事件
      //（多人加入、外人中途进出、远端音轨发布订阅变化、网络重连后重新加入）
      // 都可能触发 iOS 重配置音频、打断我方采集，故每次都去抖后保障采集健康。
      _wireRoomLifecycle();
      _startMicCaptureGuard();
      // 直拨 AI 通话：房间连入成功后，检测 AI participant 是否已加入。
      // 已加入 → 立即切换 aiDelegated + 开始计时；
      // 未加入 → 监听 ParticipantConnectedEvent，AI 加入后再切换。
      if (_session.value?.state == CallState.connecting) {
        _awaitAIParticipant();
      }
      _applyPendingModeAfterConnect();
    } catch (e) {
      _reportDiag('room_connect_error', detail: e.toString());
      _diag('room_connect_error err=$e');
      CustomToast.show('call_mic_failed'.tr);
      _endCall();
    }
  }

  /// 等待 AI participant 加入 LiveKit 房间，然后切换到 aiDelegated 并开始计时。
  void _awaitAIParticipant() {
    final room = _room;
    if (room == null) return;

    // AI participant 可能已在房间中（voicebridge 先于客户端加入）
    if (room.remoteParticipants.isNotEmpty) {
      // AI 早到（安全）：其加入早于本端开麦，通常不打断采集。
      _reportDiag(
        'ai_participant_already_present',
        detail: 'count=${room.remoteParticipants.length}',
      );
      _diag(
        'ai_participant_already_present count=${room.remoteParticipants.length}',
      );
      _confirmAIConnected();
      // 健壮性：仍轻量重激活会话兜底（不强制重建）。
      unawaited(_ensureMicCaptureActive('early_ai'));
      return;
    }

    _reportDiag('ai_participant_waiting');
    _diag('ai_participant_waiting');
    CancelListenFunc? cancelListen;
    // 兜底超时：30 秒内 AI 未加入则强制切换（防止永久卡在 connecting）
    final fallback = Timer(const Duration(seconds: 30), () {
      cancelListen?.call();
      if (_session.value?.state == CallState.connecting) {
        _reportDiag('ai_participant_fallback_timeout');
        _diag('ai_participant_fallback_timeout');
        _confirmAIConnected();
      }
    });

    cancelListen = room.events.listen((event) {
      if (event is ParticipantConnectedEvent) {
        // AI 晚到（疑点 B）：其加入触发的音频重配置可能打断已启动的采集。
        _reportDiag(
          'ai_participant_connected',
          detail: 'identity=${event.participant.identity}',
        );
        _diag(
          'ai_participant_connected identity=${event.participant.identity}',
        );
        cancelListen?.call();
        fallback.cancel();
        _confirmAIConnected();
        // 健壮性：AI 晚加入是打断采集的已知风险点，加入后无条件重建一次采集，
        // 消除“被晚到的 AI 打断、导致零帧”的隐患（疑点 B）。
        unawaited(_ensureMicCaptureActive('late_ai', forceRebuild: true));
      }
    });
  }

  /// AI participant 已就绪：connecting → aiDelegated，开始计时。
  void _confirmAIConnected() {
    if (_session.value?.state != CallState.connecting) return;
    _session.value = _session.value?.copyWith(
      state: CallState.aiDelegated,
      clearConnectingPhase: true,
    );
    _startStopwatchIfNeeded();
    _diag('confirm_ai_connected state=aiDelegated');
  }

  void _reportDiag(String stage, {String? callId, String detail = ''}) {
    final sendWs = _sendWs;
    if (sendWs == null) return;
    sendWs({
      'cmd': 'call:client_diag',
      'payload': {
        'call_id': callId ?? _session.value?.callId ?? '',
        'stage': stage,
        if (detail.isNotEmpty) 'detail': detail,
      },
    });
  }

  /// 诊断：读取本地麦克风音轨的实际发布状态并上报，用于线上坐实
  /// “音轨发布成功但没有数据”这一偶发竞态。失败不影响通话（try-catch 兜底）。
  void _reportMicTrackState(String phase) {
    try {
      final lp = _room?.localParticipant;
      if (lp == null) {
        _reportDiag('mic_track_state', detail: '$phase lp=null');
        return;
      }
      final pubs = lp.audioTrackPublications;
      if (pubs.isEmpty) {
        _reportDiag('mic_track_state', detail: '$phase pubs=0 no_audio_track');
        return;
      }
      final p = pubs.first;
      _reportDiag(
        'mic_track_state',
        detail:
            '$phase pubs=${pubs.length} muted=${p.muted} '
            'hasTrack=${p.track != null} sid=${p.sid}',
      );
      _diag(
        'mic_track_state $phase muted=${p.muted} hasTrack=${p.track != null}',
      );
    } catch (e) {
      _reportDiag('mic_track_state', detail: '$phase error=$e');
    }
  }

  /// 监听 LiveKit 房间生命周期（断连/重连），防止网络断开/被踢/token 失效后
  /// 通话"假死"（UI 仍显示通话中但已无音频）。所有平台生效。
  void _wireRoomLifecycle() {
    final room = _room;
    if (room == null) return;
    _roomLifecycleSub?.call();
    _aiBotGoneTimer?.cancel();
    _roomLifecycleSub = room.events.listen((event) {
      _trackAiBotPresence(event);
      if (event is RoomDisconnectedEvent) {
        final reason = event.reason;
        _reportDiag('room_disconnected', detail: 'reason=$reason');
        _diag('room_disconnected reason=$reason');
        // 仅异常断开才结束通话；主动挂断(clientInitiated)与服务端迁移
        // (migration，SDK 会自动重连)不处理。
        final benign = reason == DisconnectReason.clientInitiated;
        final s = _session.value;
        if (!benign && s != null && s.state != CallState.ended) {
          CustomToast.show('call_network_lost'.tr);
          _endCall();
        }
      } else if (event is RoomReconnectingEvent) {
        _reportDiag('room_reconnecting');
        _diag('room_reconnecting');
      } else if (event is RoomReconnectedEvent) {
        // 重连成功后媒体重建，保障麦克风采集恢复。
        _reportDiag('room_reconnected');
        _diag('room_reconnected');
        _ensureMicCaptureActive('reconnected', forceRebuild: true);
      }
    });
  }

  /// 健壮性：跟踪 AI(语音桥) 在房间的存在。AI 离开 15s 不回则判定语音桥挂了，
  /// 结束通话+触发后端清理锁；AI 重新加入则取消超时。
  void _trackAiBotPresence(dynamic event) {
    if (event is ParticipantConnectedEvent) {
      if (event.participant.identity.startsWith('ai_bot')) {
        _aiBotGoneTimer?.cancel();
      }
      return;
    }
    if (event is! ParticipantDisconnectedEvent) return;
    final s = _session.value;
    if (s == null) return;
    final aiCall =
        s.state == CallState.aiDelegated || s.state == CallState.humanActive;
    if (!aiCall) return;
    if (!event.participant.identity.startsWith('ai_bot')) return;
    _reportDiag('ai_bot_left');
    _diag('ai_bot_left -> start gone timer');
    _aiBotGoneTimer?.cancel();
    _aiBotGoneTimer = Timer(const Duration(seconds: 15), () {
      final cur = _session.value;
      if (cur != null && cur.state != CallState.ended) {
        _reportDiag('ai_bot_gone_timeout');
        _diag('ai_bot_gone_timeout -> end');
        CustomToast.show('call_network_lost'.tr);
        _endCall();
      }
    });
  }

  /// 启动通话期间的麦克风采集守护：持续监听房间人员/音轨变化，每次变化都
  /// 去抖后保障采集健康。覆盖多人通话、外人中途加入/离开、远端音轨变化、
  /// 网络重连（重连会重新触发 participant 加入事件）。仅 iOS 需要。
  void _startMicCaptureGuard() {
    if (kIsWeb || defaultTargetPlatform != TargetPlatform.iOS) return;
    final room = _room;
    if (room == null) return;
    _micGuardSub?.call();
    _micGuardSub = room.events.listen((event) {
      if (event is ParticipantConnectedEvent ||
          event is ParticipantDisconnectedEvent ||
          event is TrackPublishedEvent ||
          event is TrackUnpublishedEvent ||
          event is TrackSubscribedEvent ||
          event is TrackUnsubscribedEvent) {
        _scheduleMicGuard(event.runtimeType.toString());
      }
    });
  }

  /// 房间事件触发的采集保障，带 400ms 去抖（多人同时进出时合并为一次），
  /// 避免密集事件导致频繁重建采集。
  void _scheduleMicGuard(String reason) {
    if (isMuted.value) return; // 旁听/待命（不发麦）时不保障
    _micGuardDebounce?.cancel();
    _micGuardDebounce = Timer(const Duration(milliseconds: 400), () {
      _ensureMicCaptureActive('guard_$reason');
    });
  }

  /// 健壮性保障：确保麦克风采集处于活动状态，消除“音轨发布了但采集被打断、
  /// 零帧”的隐患。在所有可能打断采集的时点调用（AI/远端加入触发音频重配置、
  /// 发布后延迟自检）。失败绝不影响通话（try-catch 全包）。
  ///
  /// [forceRebuild] 为 true 时无条件重建采集（用于已知打断点，如 AI 晚加入：
  /// 此时音轨发布状态可能“看起来正常”但采集已被打断、读发布状态查不出，故需
  /// 主动重建）；为 false 时仅在检测到音轨发布异常（无音轨/被静音）时才重建。
  Future<void> _ensureMicCaptureActive(
    String reason, {
    bool forceRebuild = false,
  }) async {
    if (kIsWeb || defaultTargetPlatform != TargetPlatform.iOS) return;
    if (_micRecovering) return; // 节流：避免重入与重复重建
    final lp = _room?.localParticipant;
    // 仅当本通应发布麦克风（用户通话中、非纯 AI 托管静音/旁听）时才保障。
    if (lp == null || isMuted.value) return;
    _micRecovering = true;
    try {
      // 1) 无条件重激活 iOS 音频会话（SDK 公开 API，幂等安全），
      //    抵消被 AI 加入等事件触发的 configureAudio 打断。
      await Hardware.instance.setSpeakerphoneOn(isSpeakerOn.value);

      bool trackUnhealthy() {
        final pubs = lp.audioTrackPublications;
        return pubs.isEmpty || pubs.first.track == null || pubs.first.muted;
      }

      if (!forceRebuild && !trackUnhealthy()) {
        _reportDiag('mic_health', detail: '$reason healthy_reactivated');
        return;
      }

      // 2) 重建采集，最多 2 次，确保最终建立或明确上报失败。
      for (var attempt = 1; attempt <= 2; attempt++) {
        await lp.setMicrophoneEnabled(false);
        await lp.setMicrophoneEnabled(true);
        isMuted.value = false;
        await Future<void>.delayed(const Duration(milliseconds: 300));
        if (!trackUnhealthy()) {
          _reportDiag(
            'mic_recover',
            detail: '$reason rebuilt attempt=$attempt ok',
          );
          _reportMicTrackState('after_recover_$reason');
          return;
        }
        _reportDiag(
          'mic_recover',
          detail: '$reason attempt=$attempt still_unhealthy',
        );
      }
      _reportDiag('mic_recover', detail: '$reason failed_after_retries');
    } catch (e) {
      _reportDiag('mic_recover', detail: '$reason error=$e');
    } finally {
      _micRecovering = false;
    }
  }

  /// Web 端提前触发浏览器麦克风权限弹窗。
  ///
  /// iOS Safari 的 getUserMedia 权限弹窗会冻结页面导致 WS 断连，
  /// 因此在用户点击时立即请求权限（弹窗期间 WS 断连由 _waitForWsReady 兜底）。
  /// 权限授予后，_connectRoom 中的 setMicrophoneEnabled(true) 不会再次弹窗，
  /// 直接采集并发布音轨。
  Future<bool> _ensureMicPermission() async {
    if (!kIsWeb) return true;
    try {
      // 触发浏览器权限弹窗，获取后立即释放流。
      // 实际音轨由 setMicrophoneEnabled(true) 在房间连接后创建并发布。
      final track = await LocalAudioTrack.create();
      unawaited(track.stop());
      return true;
    } catch (e) {
      _diag('mic_prepare_error err=$e');
      CustomToast.show('call_mic_denied'.tr);
      return false;
    }
  }

  /// 等待 WebSocket 连接就绪。返回实际等待的毫秒数。
  Future<int> _waitForWsReady() async {
    if (!kIsWeb) return 0;
    final sw = Stopwatch()..start();
    try {
      final im = Get.find<ImService>();
      for (var i = 0; i < 40; i++) {
        if (im.isConnected && im.isAuthenticated) {
          sw.stop();
          _diag('ws_ready ok attempt=$i waitMs=${sw.elapsedMilliseconds}');
          return sw.elapsedMilliseconds;
        }
        _diag(
          'ws_not_ready attempt=$i connected=${im.isConnected} auth=${im.isAuthenticated}',
        );
        await Future.delayed(const Duration(milliseconds: 200));
      }
      sw.stop();
      _diag(
        'ws_ready_timeout waitMs=${sw.elapsedMilliseconds} connected=${im.isConnected} auth=${im.isAuthenticated}',
      );
    } catch (e) {
      _diag('ws_ready_error $e');
    }
    return sw.elapsedMilliseconds;
  }

  /// 断开 LiveKit 房间并复位媒体状态，但不改动 _session（供 leave/切换通话使用）。
  /// 与 _endCall 区别：不把通话置为 ended、不关闭弹窗，由调用方决定后续 _session。
  void _teardownRoom() {
    _connectWatchdog?.cancel();
    _listenWatchdog?.cancel();
    _micGuardSub?.call();
    _micGuardSub = null;
    _roomLifecycleSub?.call();
    _aiBotGoneTimer?.cancel();
    _roomLifecycleSub = null;
    _micGuardDebounce?.cancel();
    _micTrackCheckTimer?.cancel();
    final room = _room;
    _room = null;
    unawaited(_releaseCallAudioResources(room).catchError((_) {}));
    callStopwatch
      ..stop()
      ..reset();
    _isMinimized.value = false;
    isMuted.value = false;
    isSpeakerOn.value = false;
    // 健壮性: 清除待应用的参与档, 防残留 mode 被下一通误触发(误开麦/误接管)
    _pendingMode = null;
  }

  void _endCall() {
    _diag(
      'end_call callId=${_session.value?.callId} state=${_session.value?.state}',
    );
    _connectWatchdog?.cancel();
    _listenWatchdog?.cancel();
    _micGuardSub?.call();
    _micGuardSub = null;
    _roomLifecycleSub?.call();
    _aiBotGoneTimer?.cancel();
    _roomLifecycleSub = null;
    _micGuardDebounce?.cancel();
    _micTrackCheckTimer?.cancel();
    final room = _room;
    _room = null;
    unawaited(_releaseCallAudioResources(room).catchError((_) {}));
    callStopwatch
      ..stop()
      ..reset();
    _isMinimized.value = false;
    isMuted.value = false;
    isSpeakerOn.value = false;
    isStandby.value = false;
    _pendingMode = null;
    _modeSessionId = null;
    _session.value = _session.value?.copyWith(state: CallState.ended);
    Future.delayed(const Duration(milliseconds: 500), () {
      _session.value = null;
      if (Get.isDialogOpen == true) Get.back();
    });
  }

  @override
  void onInit() {
    super.onInit();
    // 通话期间保持屏幕常亮：手机自动息屏会被系统挂起网络，导致语音直接断流。
    // 监听通话状态——任何有效通话态（响铃 / 拨出 / 通话中 / AI 托管 / 接管 / 排队）
    // 都点亮屏幕，通话结束或清空后恢复系统默认息屏。一处收口覆盖所有路径。
    ever<CallSession?>(_session, (s) {
      final shouldKeepOn =
          s != null && s.state != CallState.idle && s.state != CallState.ended;
      unawaited(_setScreenWakelock(shouldKeepOn));
    });
    // 健壮性：监听原生上报的系统音频中断结束（来电/闹钟/其他 App 抢占音频）。
    // 中断结束后，若在通话中且应发麦克风，保障采集恢复（接入采集守护架构）。
    _audioSessionChannel.setMethodCallHandler((call) async {
      if (call.method == 'onAudioInterruptionEnded') {
        _reportDiag('audio_interruption_ended');
        _diag('audio_interruption_ended');
        if (isInCall && !isMuted.value) {
          await _ensureMicCaptureActive(
            'interruption_ended',
            forceRebuild: true,
          );
        }
      }
      return null;
    });
  }

  /// 切换屏幕常亮。幂等：状态未变化时不重复调用原生；平台不支持/失败不影响通话主流程。
  Future<void> _setScreenWakelock(bool on) async {
    if (_wakelockOn == on) return;
    _wakelockOn = on;
    try {
      if (on) {
        await WakelockPlus.enable();
      } else {
        await WakelockPlus.disable();
      }
      _diag('wakelock=$on');
    } catch (e) {
      _diag('wakelock_error on=$on err=$e');
    }
  }

  @override
  void onClose() {
    _connectWatchdog?.cancel();
    _micTrackCheckTimer?.cancel();
    _micGuardSub?.call();
    _roomLifecycleSub?.call();
    _aiBotGoneTimer?.cancel();
    _micGuardDebounce?.cancel();
    unawaited(_setScreenWakelock(false));
    final room = _room;
    _room = null;
    unawaited(_releaseCallAudioResources(room).catchError((_) {}));
    super.onClose();
  }

  Future<void> _releaseCallAudioResources(Room? room) async {
    if (room == null) {
      await _releaseNativeAudioSessionIfNeeded();
      return;
    }

    final completer = Completer<void>();
    runZonedGuarded(
      () async {
        try {
          await room.localParticipant?.setMicrophoneEnabled(false);
        } catch (e) {
          debugPrint('LiveKit disable microphone error: $e');
        }

        try {
          await room.disconnect();
        } catch (e) {
          debugPrint('LiveKit disconnect error: $e');
        }

        try {
          await Hardware.instance.setSpeakerphoneOn(false);
        } catch (e) {
          debugPrint('Hardware speaker reset error: $e');
        }

        await _releaseNativeAudioSessionIfNeeded();
        completer.complete();
      },
      (error, stack) {
        debugPrint('Call audio cleanup zone error: $error');
        if (!completer.isCompleted) completer.complete();
      },
    );
    return completer.future;
  }

  Future<void> _releaseNativeAudioSessionIfNeeded() async {
    if (kIsWeb || defaultTargetPlatform != TargetPlatform.iOS) return;
    try {
      await _audioSessionChannel.invokeMethod('releaseAudioSession');
    } on MissingPluginException {
      // Older builds may not have the native channel yet.
    } on PlatformException catch (e) {
      debugPrint('iOS release audio session error: $e');
    }
  }
}
