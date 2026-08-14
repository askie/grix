/// Unified result from local LIKE-based search across sessions and messages.
class LocalSearchResult {
  const LocalSearchResult({
    this.matchedSessions = const <MatchedSession>[],
    this.matchedMessages = const <MatchedMessage>[],
  });

  /// Sessions whose title, peer_nickname, peer_username, or last_message
  /// matched at least one keyword.
  final List<MatchedSession> matchedSessions;

  /// Messages whose content matched at least one keyword.
  final List<MatchedMessage> matchedMessages;

  bool get isEmpty => matchedSessions.isEmpty && matchedMessages.isEmpty;
  bool get isNotEmpty => !isEmpty;
}

/// A session row that matched a search query.
class MatchedSession {
  const MatchedSession({
    required this.sessionId,
    required this.title,
    required this.type,
    required this.peerNickname,
    required this.peerUsername,
    required this.lastMessage,
  });

  final String sessionId;
  final String title;
  final String type;
  final String peerNickname;
  final String peerUsername;
  final String lastMessage;
}

/// A message row that matched a search query.
class MatchedMessage {
  const MatchedMessage({
    required this.msgId,
    required this.sessionId,
    required this.content,
    required this.createdAt,
  });

  final String msgId;
  final String sessionId;
  final String content;
  final int createdAt;
}
