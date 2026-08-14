/// 通话状态枚举（Phase 2 新增 aiDelegated / humanActive）
enum CallState {
  idle,
  ringing,
  connecting,   // 直拨 AI：等待 AI participant 加入 LiveKit 房间
  active,       // Phase 1: 真人通话中
  aiDelegated,  // Phase 2: AI 托管中
  humanActive,  // Phase 2: 真人接管中（AI 静默旁听）
  queued,       // Widget 访客排队等待客服接入
  ended,
}

/// 委托模式
enum DelegationMode { human, aiDelegated, mixed }

/// 主人侧通话参与四档（owner 在 AI 代接的通话窗口里的参与等级）。
/// standby 待命：在窗口里但不连房间、不听不说（默认）
/// listening 旁听：连入房间、只听不说（AI 正常发声）
/// joined 加入：连入房间、开麦说话（AI 仍发声，三方）
/// takeover 接管：开麦说话、AI 静音（只主人说）
enum CallMode { standby, listening, joined, takeover }

/// 直拨 AI 连接阶段（仅在 state=connecting 且 delegationMode=aiDelegated 时有效）
enum ConnectingPhase {
  launching,   // 已发出 call:direct_ai，等待服务端建房（最慢阶段）
  waiting,     // 收到 invite_ack，房间已建，等待 AI participant 加入
}

/// AI 正在代接中的一通通话的轻量元数据（多访客客服）。
/// 仅用于驱动会话列表"语音中"徽标与定位接管入口，不含媒体连接。
class DelegatedCallInfo {
  final String callId;
  final String sessionId;
  final String peerName;

  const DelegatedCallInfo({
    required this.callId,
    required this.sessionId,
    this.peerName = '',
  });
}

/// 通话会话数据
class CallSession {
  final String callId;
  final String peerId;
  final String peerName;
  final int callMode; // 1=voice
  final CallState state;
  final DelegationMode delegationMode;
  final String? agentId;   // Phase 2: 当前托管的 agent ID
  final String? agentName; // Phase 2: 当前托管的 agent 名称
  final String? roomToken;
  final String? roomUrl;
  /// 直拨 AI 时 connecting 阶段的细分（null 表示非直拨 AI 场景）
  final ConnectingPhase? connectingPhase;

  /// 是否为主叫方（visitor 发起通话时为 true；owner 接听时为 false）。
  /// 用于区分 owner(被叫) 和 visitor(主叫) 在 AI 托管状态下的 UI 和麦克风策略。
  final bool isCaller;

  /// 排队位置（仅 state==queued 时有效，其余为 null）。
  final int? queuePosition;

  const CallSession({
    required this.callId,
    required this.peerId,
    required this.peerName,
    required this.callMode,
    required this.state,
    this.delegationMode = DelegationMode.human,
    this.agentId,
    this.agentName,
    this.roomToken,
    this.roomUrl,
    this.connectingPhase,
    this.isCaller = false,
    this.queuePosition,
  });

  CallSession copyWith({
    String? callId,
    CallState? state,
    DelegationMode? delegationMode,
    String? agentId,
    String? agentName,
    String? roomToken,
    String? roomUrl,
    ConnectingPhase? connectingPhase,
    bool clearConnectingPhase = false,
    int? queuePosition,
    bool clearQueuePosition = false,
  }) {
    return CallSession(
      callId: callId ?? this.callId,
      peerId: peerId,
      peerName: peerName,
      callMode: callMode,
      state: state ?? this.state,
      delegationMode: delegationMode ?? this.delegationMode,
      agentId: agentId ?? this.agentId,
      agentName: agentName ?? this.agentName,
      roomToken: roomToken ?? this.roomToken,
      roomUrl: roomUrl ?? this.roomUrl,
      connectingPhase: clearConnectingPhase ? null : (connectingPhase ?? this.connectingPhase),
      isCaller: isCaller, // 角色不变
      queuePosition: clearQueuePosition ? null : (queuePosition ?? this.queuePosition),
    );
  }

  /// 是否处于 AI 托管状态（aiDelegated 或 humanActive 均表示曾经 AI 托管）
  bool get isAIInvolved =>
      state == CallState.aiDelegated || state == CallState.humanActive;
}
