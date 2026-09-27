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
      createdAt: message.createdAt,
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

  bool _jumpInFlight = false;

  /// True while a pin-bar tap is paging/scrolling toward the pinned message;
  /// the scroll listener suspends its own auto history paging meanwhile.
  bool get isJumpInFlight => _jumpInFlight;

  Future<void> jumpToPinnedMessage() async {
    final pinned = owner.pinnedMessage.value;
    if (pinned == null || _jumpInFlight) return;
    _jumpInFlight = true;
    // Jumping away from the bottom is an explicit leave, same as a user
    // scroll-up: keep bottom-follow from yanking the viewport back down
    // when the paged history (or a new message) changes the list.
    final wasAutoFollowBottom = owner._autoFollowBottom;
    owner._autoFollowBottom = false;
    try {
      // The pin record is device-local and may outlive the loaded window:
      // page history toward the pinned createdAt instead of concluding the
      // message was deleted just because the current window misses it.
      final message = await _ensureMessageInWindow(
        owner,
        msgId: pinned.msgId,
        createdAt: pinned.createdAt,
      );
      if (message == null) {
        final activeSessionId = owner.imService.currentSessionId?.trim() ?? '';
        if (owner.isClosed ||
            owner.sessionId.trim() != pinned.sessionId.trim() ||
            (activeSessionId.isNotEmpty &&
                activeSessionId != pinned.sessionId.trim())) {
          owner._autoFollowBottom = wasAutoFollowBottom;
          return;
        }
        CustomToast.show('chat_pinned_message_not_found'.tr);
        // Jump failed: nothing was shown, so restore the previous follow
        // state instead of leaving bottom-follow paused forever.
        owner._autoFollowBottom = wasAutoFollowBottom;
        return;
      }
      final itemKey = ChatMessageIdentity.selectionKey(message);
      final found = await owner._chatMessageJumpController.jumpToItem(itemKey);
      if (!found) {
        owner._autoFollowBottom = wasAutoFollowBottom;
      }
    } finally {
      _jumpInFlight = false;
    }
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
