part of 'conversations_controller.dart';

class _ConversationsUnreadMentions {
  _ConversationsUnreadMentions(this.owner);

  final ConversationsController owner;
  final Map<String, bool> _hasUnreadMentionBySession = <String, bool>{};
  final Map<String, _MentionSignature> _signatureBySession =
      <String, _MentionSignature>{};
  final Map<String, _MentionSignature> _pendingSignatureBySession =
      <String, _MentionSignature>{};

  bool hasUnreadMention(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return false;
    }
    return _hasUnreadMentionBySession[sid] ?? false;
  }

  void syncWithSessions(List<SessionModel> sessions) {
    final userId = _resolveCurrentUserId();
    if (userId.isEmpty) {
      _clearResolvedState();
      return;
    }

    // State is kept only for listed sessions that have unread messages. A
    // session without unread has no mention by definition, and when it gets
    // unread again its signature cannot match anything kept, so it is
    // resolved afresh — the same outcome as tracking every session, without
    // touching thousands of read sessions on every list rebuild.
    final unreadSessionIds = <String>{};
    final unreadCountBySession = <String, int>{};
    final signaturesBySession = <String, _MentionSignature>{};
    for (final session in sessions) {
      if (session.unreadCount <= 0) continue;
      final sid = session.sessionId.trim();
      if (sid.isEmpty) {
        continue;
      }
      unreadSessionIds.add(sid);

      final signature = _buildSignature(session, userId);
      final previousSignature = _signatureBySession[sid];
      _signatureBySession[sid] = signature;
      if (previousSignature == signature &&
          _hasUnreadMentionBySession.containsKey(sid)) {
        continue;
      }
      if (_pendingSignatureBySession[sid] == signature) {
        continue;
      }

      unreadCountBySession[sid] = session.unreadCount;
      signaturesBySession[sid] = signature;
      _pendingSignatureBySession[sid] = signature;
    }
    _retainOnly(unreadSessionIds);

    if (unreadCountBySession.isEmpty) {
      return;
    }
    unawaited(
      _resolveUnreadMentions(
        userId: userId,
        unreadCountBySession: unreadCountBySession,
        signaturesBySession: signaturesBySession,
      ),
    );
  }

  void dispose() {
    _hasUnreadMentionBySession.clear();
    _signatureBySession.clear();
    _pendingSignatureBySession.clear();
  }

  Future<void> _resolveUnreadMentions({
    required String userId,
    required Map<String, int> unreadCountBySession,
    required Map<String, _MentionSignature> signaturesBySession,
  }) async {
    try {
      final matchedSessionIds = await LocalDb.getSessionsWithUnreadMentions(
        unreadCountBySession,
        userId: userId,
      );
      var changed = false;
      for (final entry in signaturesBySession.entries) {
        final sid = entry.key;
        final signature = entry.value;
        if (_pendingSignatureBySession[sid] == signature) {
          _pendingSignatureBySession.remove(sid);
        }
        if (_signatureBySession[sid] != signature) {
          continue;
        }
        final next = matchedSessionIds.contains(sid);
        if (_hasUnreadMentionBySession[sid] == next) {
          continue;
        }
        _hasUnreadMentionBySession[sid] = next;
        changed = true;
      }
      if (changed) {
        owner._onUnreadMentionStateChanged();
      }
    } catch (e) {
      debugPrint('Resolve unread mentions failed: $e');
      for (final entry in signaturesBySession.entries) {
        if (_pendingSignatureBySession[entry.key] == entry.value) {
          _pendingSignatureBySession.remove(entry.key);
        }
      }
    }
  }

  String _resolveCurrentUserId() {
    final authUserId = owner._authService?.userId?.trim() ?? '';
    if (authUserId.isNotEmpty) {
      return authUserId;
    }
    return LocalDb.activeUserId?.trim() ?? '';
  }

  // A record compares by value like the old joined string, without building
  // a string for every session on every list rebuild.
  _MentionSignature _buildSignature(SessionModel session, String userId) {
    return (
      userId,
      session.unreadCount,
      session.lastMessageTime,
      session.updatedAt,
    );
  }

  void _clearResolvedState() {
    _hasUnreadMentionBySession.clear();
    _signatureBySession.clear();
    _pendingSignatureBySession.clear();
  }

  void _retainOnly(Set<String> sessionIds) {
    if (sessionIds.isEmpty) {
      _clearResolvedState();
      return;
    }
    _hasUnreadMentionBySession.removeWhere(
      (sid, _) => !sessionIds.contains(sid),
    );
    _signatureBySession.removeWhere((sid, _) => !sessionIds.contains(sid));
    _pendingSignatureBySession.removeWhere(
      (sid, _) => !sessionIds.contains(sid),
    );
  }
}

typedef _MentionSignature = (String, int, int, int);
