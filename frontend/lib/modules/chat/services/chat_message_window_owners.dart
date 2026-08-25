import 'package:flutter/foundation.dart';

/// Tracks the chat sessions whose live controllers have entered the shared
/// [ImService] message window, oldest first.
///
/// `ImService.currentMessages` is a single global window. When a chat opened
/// on top of another one leaves (a full-screen chat pushed over the desktop
/// chat pane, or chat → other page → chat), its `leaveSession` clears that
/// window while the earlier chat controller is still alive and about to be
/// shown again. The earlier chat must re-enter its session to get its
/// messages back; this registry tells it which session that is.
class ChatMessageWindowOwners {
  const ChatMessageWindowOwners._();

  static final List<String> _owners = <String>[];

  @visibleForTesting
  static List<String> get ownersForTest => List<String>.unmodifiable(_owners);

  @visibleForTesting
  static void resetForTest() => _owners.clear();

  /// Records that [sessionId] entered the shared window; it becomes the
  /// newest owner even if it was recorded before.
  static void enter(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    _owners.remove(sid);
    _owners.add(sid);
  }

  /// Forgets [sessionId].
  static void leave(String sessionId) {
    _owners.remove(sessionId.trim());
  }

  /// Returns the newest recorded session that [isAlive] still confirms,
  /// dropping stale entries on the way; `null` when none is left.
  static String? latestAlive(bool Function(String sessionId) isAlive) {
    while (_owners.isNotEmpty) {
      final sid = _owners.last;
      if (isAlive(sid)) return sid;
      _owners.removeLast();
    }
    return null;
  }
}
