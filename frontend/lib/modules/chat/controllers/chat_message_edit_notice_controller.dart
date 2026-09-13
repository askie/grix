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
  _ChatMessageEditNoticeController(this.owner);

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
  /// tracked message the reader has scrolled into view on their own. Messages
  /// outside the loaded window are kept — the jump handler pages history
  /// until they load, so there is still somewhere to jump to.
  void onScrollSettled() {
    if (owner.pendingUpdatedMessageIds.isEmpty) {
      return;
    }
    final messages = owner.imService.currentMessages;
    final stillPending = <String>[];
    for (final msgId in owner.pendingUpdatedMessageIds) {
      final message = messages.firstWhereOrNull((m) => m.msgId == msgId);
      if (message == null) {
        stillPending.add(msgId);
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

  bool _jumpInFlight = false;

  /// Jumps to the earliest still-pending edited message and clears it from
  /// the list once reached. No-op when the list is empty.
  Future<void> jumpToEarliestUpdatedMessage() async {
    if (_jumpInFlight || owner.pendingUpdatedMessageIds.isEmpty) {
      return;
    }
    _jumpInFlight = true;
    try {
      final targetMsgId = owner.pendingUpdatedMessageIds.first;
      final message = await _ensureMessageInWindow(targetMsgId);
      if (message == null) {
        owner.pendingUpdatedMessageIds.remove(targetMsgId);
        return;
      }
      final itemKey = ChatMessageIdentity.selectionKey(message);
      final found = await owner._chatMessageJumpController.jumpToItem(itemKey);
      if (found) {
        owner.pendingUpdatedMessageIds.remove(targetMsgId);
      }
    } finally {
      _jumpInFlight = false;
    }
  }

  /// Pages the history window toward [msgId] — older first, alternating with
  /// newer — reusing the session's standard pagination until the message is
  /// loaded. Returns null when it is not reachable within the page budget.
  static const int _maxEnsurePages = 30;

  Future<MessageModel?> _ensureMessageInWindow(String msgId) async {
    final imService = owner.imService;
    MessageModel? find() => imService.currentMessages.firstWhereOrNull(
      (m) => m.msgId == msgId,
    );
    var message = find();
    var preferOlder = true;
    var pages = 0;
    while (message == null && pages < _maxEnsurePages) {
      if (preferOlder && imService.hasOlderMessages) {
        await imService.loadOlderForCurrentSession();
      } else if (imService.hasNewerMessages) {
        await imService.loadNewerForCurrentSession();
      } else if (imService.hasOlderMessages) {
        await imService.loadOlderForCurrentSession();
      } else {
        break;
      }
      preferOlder = !preferOlder;
      pages++;
      message = find();
    }
    return message;
  }

  void reset() {
    owner.pendingUpdatedMessageIds.clear();
  }
}
