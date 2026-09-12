part of 'chat_controller.dart';

/// Conversation-level "pin one message to the top" feature.
///
/// Device-local only for now — see [ChatPinnedMessage]'s doc comment for
/// why. A session has at most one pinned message; pinning another replaces
/// it.
class _ChatPinnedMessageController {
  _ChatPinnedMessageController(this.owner);

  final ChatController owner;
  static const int _summaryMaxLength = 80;

  Future<void> loadForCurrentSession() async {
    final userId = owner.authService.userId?.trim() ?? '';
    final sid = owner.sessionId.trim();
    if (userId.isEmpty || sid.isEmpty) {
      owner.pinnedMessage.value = null;
      return;
    }
    final loaded = await ChatPinnedMessageStore.load(
      userId: userId,
      sessionId: sid,
    );
    if (owner.sessionId.trim() != sid) {
      // Session switched away while the read was in flight.
      return;
    }
    owner.pinnedMessage.value = loaded;
  }

  bool isMessagePinned(String msgId) {
    final id = msgId.trim();
    if (id.isEmpty) return false;
    return owner.pinnedMessage.value?.msgId == id;
  }

  Future<void> togglePin(MessageModel message) async {
    if (isMessagePinned(message.msgId)) {
      await unpin();
      return;
    }
    final sid = owner.sessionId.trim();
    final msgId = message.msgId.trim();
    if (sid.isEmpty || msgId.isEmpty) {
      return;
    }
    final pinned = ChatPinnedMessage(
      sessionId: sid,
      msgId: msgId,
      summary: buildSummary(message),
      pinnedAt: DateTime.now().millisecondsSinceEpoch,
    );
    owner.pinnedMessage.value = pinned;
    final userId = owner.authService.userId?.trim() ?? '';
    if (userId.isNotEmpty) {
      await ChatPinnedMessageStore.save(userId: userId, pinned: pinned);
    }
  }

  Future<void> unpin() async {
    final sid = owner.sessionId.trim();
    owner.pinnedMessage.value = null;
    final userId = owner.authService.userId?.trim() ?? '';
    if (userId.isNotEmpty && sid.isNotEmpty) {
      await ChatPinnedMessageStore.clear(userId: userId, sessionId: sid);
    }
  }

  /// Re-derives the pin bar summary when the pinned message itself is
  /// edited, and persists the refreshed text.
  void onMessageEdited(MessageModel message) {
    final pinned = owner.pinnedMessage.value;
    if (pinned == null ||
        pinned.sessionId.trim() != message.sessionId.trim() ||
        pinned.msgId.trim() != message.msgId.trim()) {
      return;
    }
    final refreshed = pinned.copyWith(summary: buildSummary(message));
    owner.pinnedMessage.value = refreshed;
    final userId = owner.authService.userId?.trim() ?? '';
    if (userId.isNotEmpty) {
      unawaited(ChatPinnedMessageStore.save(userId: userId, pinned: refreshed));
    }
  }

  Future<void> jumpToPinnedMessage() async {
    final pinned = owner.pinnedMessage.value;
    if (pinned == null) return;
    final message = owner.imService.currentMessages.firstWhereOrNull(
      (m) => m.msgId == pinned.msgId,
    );
    if (message == null) {
      CustomToast.show('chat_pinned_message_not_found'.tr);
      return;
    }
    final itemKey = ChatMessageIdentity.selectionKey(message);
    await owner._chatMessageJumpController.jumpToItem(itemKey);
  }

  static String buildSummary(MessageModel message) {
    final plain =
        ChatMessageCardCodec.buildCopyableText(message.content) ??
        message.content;
    final firstLine = plain
        .split('\n')
        .map((line) => line.trim())
        .firstWhere((line) => line.isNotEmpty, orElse: () => plain.trim());
    if (firstLine.length <= _summaryMaxLength) {
      return firstLine;
    }
    return '${firstLine.substring(0, _summaryMaxLength)}…';
  }
}
