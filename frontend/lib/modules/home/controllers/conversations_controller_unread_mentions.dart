part of 'conversations_controller.dart';

class _ConversationsUnreadMentions {
  _ConversationsUnreadMentions(this.owner);

  final ConversationsController owner;
  final Map<String, bool> _hasUnreadMentionBySession = <String, bool>{};
  final Map<String, String> _signatureBySession = <String, String>{};
  final Map<String, String> _pendingSignatureBySession = <String, String>{};

  bool hasUnreadMention(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return false;
    }
    return _hasUnreadMentionBySession[sid] ?? false;
  }

  void syncWithSessions(List<SessionModel> sessions) {
    final userId = _resolveCurrentUserId();
    final activeSessionIds = sessions
        .map((session) => session.sessionId.trim())
        .where((sid) => sid.isNotEmpty)
        .toSet();

    _removeInactiveSessions(activeSessionIds);
    if (userId.isEmpty) {
      _clearResolvedState();
      return;
    }

    final unreadCountBySession = <String, int>{};
    final signaturesBySession = <String, String>{};
    for (final session in sessions) {
      final sid = session.sessionId.trim();
      if (sid.isEmpty) {
        continue;
      }

      final signature = _buildSignature(session, userId);
      if (session.unreadCount <= 0) {
        final hadMention = _hasUnreadMentionBySession[sid] ?? false;
        final previousSignature = _signatureBySession[sid];
        _signatureBySession[sid] = signature;
        _pendingSignatureBySession.remove(sid);
        if (hadMention || previousSignature != signature) {
          _hasUnreadMentionBySession[sid] = false;
        }
        continue;
      }

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
    required Map<String, String> signaturesBySession,
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

  String _buildSignature(SessionModel session, String userId) {
    return [
      userId,
      session.unreadCount.toString(),
      session.lastMessageTime.toString(),
      session.updatedAt.toString(),
    ].join('|');
  }

  void _clearResolvedState() {
    _hasUnreadMentionBySession.clear();
    _signatureBySession.clear();
    _pendingSignatureBySession.clear();
  }

  void _removeInactiveSessions(Set<String> activeSessionIds) {
    if (activeSessionIds.isEmpty) {
      _clearResolvedState();
      return;
    }

    _hasUnreadMentionBySession.removeWhere(
      (sid, _) => !activeSessionIds.contains(sid),
    );
    _signatureBySession.removeWhere(
      (sid, _) => !activeSessionIds.contains(sid),
    );
    _pendingSignatureBySession.removeWhere(
      (sid, _) => !activeSessionIds.contains(sid),
    );
  }
}
