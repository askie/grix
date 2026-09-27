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

  Timer? _autoDismissTimer;

  static const _autoDismissDuration = Duration(seconds: 5);

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
      _removePending(msgId);
      return;
    }
    if (owner.pendingUpdatedMessageIds.contains(msgId)) {
      return;
    }
    owner.pendingUpdatedMessageIds.add(msgId);
    _pendingEditCreatedAt[msgId] = message.createdAt;
    _sortByConversationOrder();
    _scheduleAutoDismiss();
  }

  void _scheduleAutoDismiss() {
    _autoDismissTimer?.cancel();
    _autoDismissTimer = Timer(_autoDismissDuration, () {
      _autoDismissTimer = null;
      if (owner.pendingUpdatedMessageIds.isNotEmpty) {
        reset();
      }
    });
  }

  void _cancelAutoDismiss() {
    _autoDismissTimer?.cancel();
    _autoDismissTimer = null;
  }

  /// createdAt captured when the edit event arrived, for messages outside
  /// the loaded window: keeps the pill queue deterministic and picks the
  /// paging direction when jumping.
  final _pendingEditCreatedAt = <String, int>{};

  void _removePending(String msgId) {
    owner.pendingUpdatedMessageIds.remove(msgId);
    _pendingEditCreatedAt.remove(msgId);
    if (owner.pendingUpdatedMessageIds.isEmpty) {
      _cancelAutoDismiss();
    }
  }

  void _sortByConversationOrder() {
    final order = <String, int>{};
    final messages = owner.imService.currentMessages;
    for (var i = 0; i < messages.length; i++) {
      order[messages[i].msgId] = i;
    }
    owner.pendingUpdatedMessageIds.sort((a, b) {
      final indexA = order[a];
      final indexB = order[b];
      if (indexA != null && indexB != null) {
        return indexA.compareTo(indexB);
      }
      if (indexA != null) return -1;
      if (indexB != null) return 1;
      // Both outside the window: fall back to the edit-time createdAt so
      // "earliest" stays deterministic (List.sort is not stable).
      return (_pendingEditCreatedAt[a] ?? 0).compareTo(
        _pendingEditCreatedAt[b] ?? 0,
      );
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
      final keep = stillPending.toSet();
      _pendingEditCreatedAt.removeWhere((id, _) => !keep.contains(id));
      if (stillPending.isEmpty) {
        _cancelAutoDismiss();
      }
    }
  }

  bool _jumpInFlight = false;

  /// True while a pill tap is paging/scrolling toward an edited message;
  /// the scroll listener suspends its own auto history paging meanwhile.
  bool get isJumpInFlight => _jumpInFlight;

  /// Jumps to the earliest still-pending edited message and clears it from
  /// the list once reached. No-op when the list is empty.
  Future<void> jumpToEarliestUpdatedMessage() async {
    if (_jumpInFlight || owner.pendingUpdatedMessageIds.isEmpty) {
      return;
    }
    _jumpInFlight = true;
    // Jumping away from the bottom is an explicit leave, same as a user
    // scroll-up: keep bottom-follow from yanking the viewport back down
    // when the paged history (or a new message) changes the list.
    final wasAutoFollowBottom = owner._autoFollowBottom;
    owner._autoFollowBottom = false;
    try {
      final targetMsgId = owner.pendingUpdatedMessageIds.first;
      final message = await _ensureMessageInWindow(
        owner,
        msgId: targetMsgId,
        createdAt: _pendingEditCreatedAt[targetMsgId] ?? 0,
      );
      if (message == null) {
        _removePending(targetMsgId);
        // Jump failed: nothing was shown, so restore the previous follow
        // state instead of leaving bottom-follow paused forever.
        owner._autoFollowBottom = wasAutoFollowBottom;
        return;
      }
      final itemKey = ChatMessageIdentity.selectionKey(message);
      final found = await owner._chatMessageJumpController.jumpToItem(itemKey);
      if (found) {
        _removePending(targetMsgId);
      } else {
        owner._autoFollowBottom = wasAutoFollowBottom;
      }
    } finally {
      _jumpInFlight = false;
    }
  }

  void reset() {
    _cancelAutoDismiss();
    owner.pendingUpdatedMessageIds.clear();
    _pendingEditCreatedAt.clear();
  }

  void dispose() {
    _cancelAutoDismiss();
  }
}
