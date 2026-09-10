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

/// Restricts [LocalDbSearchRepository] queries to one private conversation
/// or one specific session, instead of the full local library.
///
/// Passing no scope (the default everywhere) preserves the home page's
/// full-library search behavior unchanged.
class LocalSearchScope {
  const LocalSearchScope._peer(this.peerType, this.peerId) : sessionId = null;
  const LocalSearchScope._session(this.sessionId)
    : peerType = null,
      peerId = null;

  /// Scopes to a single private conversation, identified the same way
  /// `sessions.type = 'private'` rows are: by `peer_type` + `peer_id`.
  factory LocalSearchScope.peer({
    required int peerType,
    required String peerId,
  }) => LocalSearchScope._peer(peerType, peerId);

  /// Scopes to one specific session id.
  factory LocalSearchScope.session(String sessionId) =>
      LocalSearchScope._session(sessionId);

  final int? peerType;
  final String? peerId;
  final String? sessionId;
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

/// 本地缓存里匹配到的联系人或 Agent。
///
/// 好友与 Agent 都以「私聊对端」的形式呈现，[peerType] 与
/// `ChatRouteNavigator.createAndOpenPrivateChat` 的口径一致：1=用户，2=Agent。
class MatchedContact {
  const MatchedContact({
    required this.peerId,
    required this.peerType,
    required this.displayName,
    this.username = '',
    this.introduction = '',
    this.avatarUrl = '',
  });

  final String peerId;
  final int peerType;
  final String displayName;
  final String username;
  final String introduction;
  final String avatarUrl;

  bool get isAgent => peerType == 2;

  /// 参与关键词匹配的字段。
  List<String> get searchableFields => <String>[
    displayName,
    username,
    introduction,
  ];

  @override
  bool operator ==(Object other) =>
      identical(this, other) ||
      other is MatchedContact &&
          other.peerId == peerId &&
          other.peerType == peerType &&
          other.displayName == displayName &&
          other.username == username &&
          other.introduction == introduction &&
          other.avatarUrl == avatarUrl;

  @override
  int get hashCode => Object.hash(
    peerId,
    peerType,
    displayName,
    username,
    introduction,
    avatarUrl,
  );
}
