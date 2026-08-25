class ChatForwardMessageItem {
  const ChatForwardMessageItem({
    required this.senderName,
    required this.content,
    required this.createdAt,
    this.sessionId = '',
    this.messageId = '',
  });

  final String senderName;
  final String content;
  final int createdAt;

  /// 来源会话 ID / 消息 ID：写入转发正文，便于接收方（尤其是 Agent）定位排查。
  final String sessionId;
  final String messageId;
}
