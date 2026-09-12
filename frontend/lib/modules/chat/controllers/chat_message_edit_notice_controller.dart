part of 'chat_controller.dart';

/// Tracks messages that were edited in place (a real `message.edit` sync
/// event, not a send-ack or stream finalize) while off-screen, so the chat
/// page can surface a bottom "messages updated above" pill instead of
/// silently refreshing content the reader can't see.
///
/// A message drops off this list the moment it is confirmed visible again —
/// either because the pill's own jump handler scrolled to it, or because the
/// reader scrolled there on their own.
class _ChatMessageEditNoticeController {
  const _ChatMessageEditNoticeController(this.owner);

  final ChatController owner;

  void onMessageEdited(MessageModel message) {
    if (message.sessionId.trim() != owner.sessionId.trim()) {
      return;
    }
    final msgId = message.msgId.trim();
    if (msgId.isEmpty) {
      return;
    }
    final itemKey = ChatMessageIdentity.selectionKey(message);
    final isVisible = owner._pageStateController.isItemVisibleInViewport(
      itemKey,
    );
    if (isVisible) {
      owner.pendingUpdatedMessageIds.remove(msgId);
      return;
    }
    if (owner.pendingUpdatedMessageIds.contains(msgId)) {
      return;
    }
    owner.pendingUpdatedMessageIds.add(msgId);
    _sortByConversationOrder();
  }

  void _sortByConversationOrder() {
    final order = <String, int>{};
    final messages = owner.imService.currentMessages;
    for (var i = 0; i < messages.length; i++) {
      order[messages[i].msgId] = i;
    }
    owner.pendingUpdatedMessageIds.sort((a, b) {
      final indexA = order[a] ?? 1 << 30;
      final indexB = order[b] ?? 1 << 30;
      return indexA.compareTo(indexB);
    });
  }

  /// Called on every scroll update while the pill is showing: drops any
  /// tracked message the reader has scrolled into view on their own, and
  /// silently forgets one that aged out of the loaded window (nothing to
  /// jump to any more).
  void onScrollSettled() {
    if (owner.pendingUpdatedMessageIds.isEmpty) {
      return;
    }
    final messages = owner.imService.currentMessages;
    final stillPending = <String>[];
    for (final msgId in owner.pendingUpdatedMessageIds) {
      final message = messages.firstWhereOrNull((m) => m.msgId == msgId);
      if (message == null) {
        continue;
      }
      final itemKey = ChatMessageIdentity.selectionKey(message);
      if (!owner._pageStateController.isItemVisibleInViewport(itemKey)) {
        stillPending.add(msgId);
      }
    }
    if (stillPending.length != owner.pendingUpdatedMessageIds.length) {
      owner.pendingUpdatedMessageIds
        ..clear()
        ..addAll(stillPending);
    }
  }

  /// Jumps to the earliest still-pending edited message and clears it from
  /// the list once reached. No-op when the list is empty.
  Future<void> jumpToEarliestUpdatedMessage() async {
    if (owner.pendingUpdatedMessageIds.isEmpty) {
      return;
    }
    final targetMsgId = owner.pendingUpdatedMessageIds.first;
    final message = owner.imService.currentMessages.firstWhereOrNull(
      (m) => m.msgId == targetMsgId,
    );
    if (message == null) {
      owner.pendingUpdatedMessageIds.remove(targetMsgId);
      return;
    }
    final itemKey = ChatMessageIdentity.selectionKey(message);
    final found = await owner._chatMessageJumpController.jumpToItem(itemKey);
    if (found) {
      owner.pendingUpdatedMessageIds.remove(targetMsgId);
    }
  }

  void reset() {
    owner.pendingUpdatedMessageIds.clear();
  }
}
