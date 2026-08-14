class ChatForwardMessageItem {
  const ChatForwardMessageItem({
    required this.senderName,
    required this.content,
    required this.createdAt,
  });

  final String senderName;
  final String content;
  final int createdAt;
}
