import '../../../shared/models/session_avatar_member.dart';
import '../../../shared/utils/chat_message_preview.dart';

class ChatForwardTargetOption {
  ChatForwardTargetOption({
    required this.sessionId,
    required this.avatarColorSeed,
    required this.title,
    required this.isGroup,
    required this.activityAt,
    String? subtitle,
    this.previewSource = '',
    this.avatarUrl = '',
    this.members = const <SessionAvatarMember>[],
  }) : _explicitSubtitle = subtitle;

  final String sessionId;
  final String avatarColorSeed;
  final String title;
  final bool isGroup;
  final int activityAt;

  /// 最后一条消息原文。预览副标题按需从这里清洗（见 [subtitle]），避免在构建整个
  /// 目标列表时对全部会话同步跑预览正则——只有滚动到可视区/被搜索到的行才计算。
  final String previewSource;

  final String avatarUrl;
  final List<SessionAvatarMember> members;

  final String? _explicitSubtitle;
  String? _subtitleCache;

  /// 显示用预览副标题：显式给定则直接用；否则首次访问时从 [previewSource] 清洗并缓存。
  String get subtitle =>
      _explicitSubtitle ??
      (_subtitleCache ??= ChatMessagePreview.summarize(previewSource).trim());
}
