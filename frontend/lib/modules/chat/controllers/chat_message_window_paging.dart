part of 'chat_controller.dart';

/// Pages the history window toward [msgId] in a single direction chosen from
/// [createdAt], reusing the session's standard pagination until the message
/// is loaded. Returns null when it is not reachable within the page budget.
///
/// Shared by the edited-message notice pill and the pinned message bar: both
/// jump to messages that may sit outside the currently loaded window.
///
/// The direction must not alternate: at the resident-message cap every
/// loadOlder trims the newest end (marking hasNewerMessages), so an
/// alternating loop would oscillate with zero net progress. A legacy
/// [createdAt] of 0 (or an empty window) defaults to paging older.
Future<MessageModel?> _ensureMessageInWindow(
  ChatController owner, {
  required String msgId,
  required int createdAt,
}) async {
  const maxEnsurePages = 30;
  final imService = owner.imService;
  final sessionId = owner.sessionId.trim();
  bool isSessionCurrent() {
    if (owner.isClosed || owner.sessionId.trim() != sessionId) return false;
    final activeSessionId = imService.currentSessionId?.trim() ?? '';
    return activeSessionId.isEmpty || activeSessionId == sessionId;
  }

  MessageModel? find() =>
      imService.currentMessages.firstWhereOrNull((m) => m.msgId == msgId);
  var message = find();
  if (message != null) return message;

  // The window is contiguous, so a missing target sits beyond one of its
  // ends; createdAt tells which one.
  final window = imService.currentMessages;
  final loadOlder =
      window.isEmpty || createdAt <= 0 || createdAt <= window.first.createdAt;

  var pages = 0;
  while (message == null && pages < maxEnsurePages) {
    if (!isSessionCurrent()) return null;
    final current = imService.currentMessages;
    if (loadOlder) {
      if (!imService.hasOlderMessages) break;
      final boundaryBefore = current.isEmpty ? null : current.first.msgId;
      await imService.loadOlderForCurrentSessionAwaitingBackfill();
      if (!isSessionCurrent()) return null;
      if (_pagingBoundaryUnchanged(imService, boundaryBefore, older: true)) {
        // The awaited local/remote page produced no new boundary. Stop rather
        // than burning the page budget or oscillating at the resident cap.
        break;
      }
    } else {
      if (!imService.hasNewerMessages) break;
      final boundaryBefore = current.isEmpty ? null : current.last.msgId;
      await imService.loadNewerForCurrentSession();
      if (!isSessionCurrent()) return null;
      if (_pagingBoundaryUnchanged(imService, boundaryBefore, older: false)) {
        break;
      }
    }
    pages++;
    message = find();
  }
  return message;
}

bool _pagingBoundaryUnchanged(
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
