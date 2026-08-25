import 'package:flutter/foundation.dart';

/// Tracks the chat sessions whose live controllers have entered the shared
/// [ImService] message window, oldest first, together with the account that
/// opened them.
///
/// `ImService.currentMessages` is a single global window. When a chat opened
/// on top of another one leaves (a full-screen chat pushed over the desktop
/// chat pane, or chat → other page → chat), when its session is removed, or
/// when local chat data is reset, that window is cleared while an earlier chat
/// controller is still alive and about to be shown again. The earlier chat
/// must re-enter its session to get its messages back; this registry tells it
/// which session that is.
class ChatMessageWindowOwners {
  const ChatMessageWindowOwners._();

  static final List<_WindowOwner> _owners = <_WindowOwner>[];

  @visibleForTesting
  static List<String> get ownersForTest =>
      List<String>.unmodifiable(_owners.map((owner) => owner.sessionId));

  @visibleForTesting
  static void resetForTest() => _owners.clear();

  /// Records that [sessionId] entered the shared window on behalf of
  /// [userId]; it becomes the newest owner even if it was recorded before.
  static void enter(String sessionId, {required String userId}) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    leave(sid);
    _owners.add(_WindowOwner(sessionId: sid, userId: userId.trim()));
  }

  /// Forgets [sessionId].
  static void leave(String sessionId) {
    final sid = sessionId.trim();
    _owners.removeWhere((owner) => owner.sessionId == sid);
  }

  /// Returns the newest recorded session of [userId] that [isAlive] still
  /// confirms, dropping stale entries (dead controllers, other accounts) on
  /// the way; `null` when none is left.
  static String? latestAlive({
    required String userId,
    required bool Function(String sessionId) isAlive,
  }) {
    final uid = userId.trim();
    while (_owners.isNotEmpty) {
      final owner = _owners.last;
      if (owner.userId == uid && isAlive(owner.sessionId)) {
        return owner.sessionId;
      }
      _owners.removeLast();
    }
    return null;
  }
}

class _WindowOwner {
  const _WindowOwner({required this.sessionId, required this.userId});

  final String sessionId;
  final String userId;
}
