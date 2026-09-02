import 'chat_message_card_data.dart';
import 'chat_message_card_type.dart';

/// 呼叫主人卡片：agent 主动呼叫主人进入会话语音沟通。
/// 主人打开会话时，前端识别新鲜卡片自动拉起语音大脑通话（亦可点"接听"手动拉起）。
class ChatCallOwnerCardData extends ChatMessageCardData {
  const ChatCallOwnerCardData({
    this.agentName = '',
    this.sessionId = '',
    this.ts = 0,
  }) : super(type: ChatMessageCardType.callOwner);

  /// 发起呼叫的 agent 名称。
  final String agentName;

  /// 目标会话 ID。
  final String sessionId;

  /// 发起呼叫的时间戳（epoch 毫秒），用于新鲜度判定。
  final int ts;

  String get displayAgentName => agentName.trim();

  @override
  Map<String, dynamic> toPayload() => <String, dynamic>{
    'agent_name': agentName,
    'session_id': sessionId,
    'ts': ts,
  };
}
