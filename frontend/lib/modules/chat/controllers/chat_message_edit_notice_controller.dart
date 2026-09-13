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
      _removePending(msgId);
      return;
    }
    if (owner.pendingUpdatedMessageIds.contains(msgId)) {
      return;
    }
    owner.pendingUpdatedMessageIds.add(msgId);
    _pendingEditCreatedAt[msgId] = message.createdAt;
    _sortByConversationOrder();
  }

  /// createdAt captured when the edit event arrived, for messages outside
  /// the loaded window: keeps the pill queue deterministic and picks the
  /// paging direction when jumping.
  final _pendingEditCreatedAt = <String, int>{};

  void _removePending(String msgId) {
    owner.pendingUpdatedMessageIds.remove(msgId);
    _pendingEditCreatedAt.remove(msgId);
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
      final message = await _ensureMessageInWindow(targetMsgId);
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

  /// Pages the history window toward [msgId] in a single direction chosen
  /// from the edit-time createdAt, reusing the session's standard pagination
  /// until the message is loaded. Returns null when it is not reachable
  /// within the page budget.
  ///
  /// The direction must not alternate: at the resident-message cap every
  /// loadOlder trims the newest end (marking hasNewerMessages), so an
  /// alternating loop would oscillate with zero net progress.
  static const int _maxEnsurePages = 30;

  Future<MessageModel?> _ensureMessageInWindow(String msgId) async {
    final imService = owner.imService;
    MessageModel? find() => imService.currentMessages.firstWhereOrNull(
      (m) => m.msgId == msgId,
    );
    var message = find();
    if (message != null) return message;

    // The window is contiguous, so a missing target sits beyond one of its
    // ends; the edit-time createdAt tells which one.
    final createdAt = _pendingEditCreatedAt[msgId] ?? 0;
    final window = imService.currentMessages;
    final loadOlder =
        window.isEmpty ||
        createdAt <= 0 ||
        createdAt <= window.first.createdAt;

    var pages = 0;
    while (message == null && pages < _maxEnsurePages) {
      if (owner.isClosed) return null;
      final current = imService.currentMessages;
      if (loadOlder) {
        if (!imService.hasOlderMessages) break;
        final boundaryBefore = current.isEmpty ? null : current.first.msgId;
        await imService.loadOlderForCurrentSession();
        if (_boundaryUnchanged(imService, boundaryBefore, older: true)) {
          // No boundary movement: the local page is missing (an async remote
          // backfill is in flight) — spinning would burn the page budget.
          break;
        }
      } else {
        if (!imService.hasNewerMessages) break;
        final boundaryBefore = current.isEmpty ? null : current.last.msgId;
        await imService.loadNewerForCurrentSession();
        if (_boundaryUnchanged(imService, boundaryBefore, older: false)) {
          break;
        }
      }
      pages++;
      message = find();
    }
    return message;
  }

  bool _boundaryUnchanged(
    ImService imService,
    String? boundaryBefore, {
    required bool older,
  }) {
    if (boundaryBefore == null) return false;
    final current = imService.currentMessages;
    if (current.isEmpty) return false;
    final boundaryAfter = older ? current.first.msgId : current.last.msgId;
    return boundaryAfter == boundaryBefore;
  }

  void reset() {
    owner.pendingUpdatedMessageIds.clear();
    _pendingEditCreatedAt.clear();
  }
}
