part of 'local_db.dart';

class LocalDbSearchRepository {
  /// Search sessions table by pre-tokenized keywords.
  ///
  /// Searches [title], [peer_nickname], [peer_username], and [last_message]
  /// columns using LIKE. A session matches if ANY keyword matches ANY
  /// searchable column (OR semantics).
  static Future<List<MatchedSession>> searchSessions(
    List<String> keywords,
  ) async {
    final sanitized = _sanitizeKeywords(keywords);
    if (sanitized.isEmpty) {
      return const <MatchedSession>[];
    }

    return LocalDb._withDatabaseOr<List<MatchedSession>>(
      const <MatchedSession>[],
      (db) async {
        // Build: WHERE (title LIKE ? OR peer_nickname LIKE ? OR ...) OR (...)
        // Each keyword gets 4 placeholders (one per searchable column).
        final buffer = StringBuffer();
        final args = <String>[];
        for (var i = 0; i < sanitized.length; i++) {
          if (i > 0) buffer.write(' OR ');
          final pattern = '%${sanitized[i]}%';
          buffer.write(
            '(title LIKE ? OR peer_nickname LIKE ? '
            'OR peer_username LIKE ? OR last_message LIKE ?)',
          );
          args.addAll([pattern, pattern, pattern, pattern]);
        }

        final rows = await db.query(
          'sessions',
          columns: const [
            'session_id',
            'title',
            'type',
            'peer_nickname',
            'peer_username',
            'last_message',
          ],
          where: buffer.toString(),
          whereArgs: args,
          orderBy: 'updated_at DESC',
          limit: 200,
        );

        return rows
            .map(
              (row) => MatchedSession(
                sessionId: row['session_id']?.toString() ?? '',
                title: row['title']?.toString() ?? '',
                type: row['type']?.toString() ?? 'private',
                peerNickname: row['peer_nickname']?.toString() ?? '',
                peerUsername: row['peer_username']?.toString() ?? '',
                lastMessage: row['last_message']?.toString() ?? '',
              ),
            )
            .toList(growable: false);
      },
    );
  }

  /// Search messages table by pre-tokenized keywords.
  ///
  /// Searches [content] column only. Excludes msg_type=4 streaming
  /// placeholders. A message matches if ANY keyword matches (OR semantics).
  static Future<List<MatchedMessage>> searchMessages(
    List<String> keywords, {
    int limit = 200,
  }) async {
    final sanitized = _sanitizeKeywords(keywords);
    if (sanitized.isEmpty) {
      return const <MatchedMessage>[];
    }

    return LocalDb._withDatabaseOr<List<MatchedMessage>>(
      const <MatchedMessage>[],
      (db) async {
        final excludeStreaming =
            LocalDbMessageRepository.excludeStreamingPlaceholderSql;

        final buffer = StringBuffer('(content LIKE ?');
        final args = <String>['%${sanitized[0]}%'];
        for (var i = 1; i < sanitized.length; i++) {
          buffer.write(' OR content LIKE ?');
          args.add('%${sanitized[i]}%');
        }
        buffer.write(') AND $excludeStreaming');

        final rows = await db.query(
          'messages',
          columns: const ['msg_id', 'session_id', 'content', 'created_at'],
          where: buffer.toString(),
          whereArgs: args,
          orderBy: 'created_at DESC',
          limit: limit,
        );

        return rows
            .map(
              (row) => MatchedMessage(
                msgId: row['msg_id']?.toString() ?? '',
                sessionId: row['session_id']?.toString() ?? '',
                content: row['content']?.toString() ?? '',
                createdAt: StrictIntParser.tryParse(row['created_at']) ?? 0,
              ),
            )
            .toList(growable: false);
      },
    );
  }

  /// Combined search: returns both session and message matches.
  static Future<LocalSearchResult> search(
    List<String> keywords, {
    int messageLimit = 200,
  }) async {
    final results = await Future.wait([
      searchSessions(keywords),
      searchMessages(keywords, limit: messageLimit),
    ]);
    return LocalSearchResult(
      matchedSessions: results[0] as List<MatchedSession>,
      matchedMessages: results[1] as List<MatchedMessage>,
    );
  }

  static Future<List<Map<String, dynamic>>> searchSessionRecords(
    List<String> keywords,
  ) async {
    final sanitized = _sanitizeKeywords(keywords);
    if (sanitized.isEmpty) {
      return const <Map<String, dynamic>>[];
    }

    return LocalDb._withDatabaseOr<List<Map<String, dynamic>>>(
      const <Map<String, dynamic>>[],
      (db) async {
        final buffer = StringBuffer();
        final args = <String>[];
        for (var i = 0; i < sanitized.length; i++) {
          if (i > 0) buffer.write(' OR ');
          final pattern = '%${sanitized[i]}%';
          buffer.write(
            '(title LIKE ? OR peer_nickname LIKE ? '
            'OR peer_username LIKE ? OR last_message LIKE ?)',
          );
          args.addAll([pattern, pattern, pattern, pattern]);
        }

        final rows = await db.query(
          'sessions',
          where: buffer.toString(),
          whereArgs: args,
          orderBy: 'updated_at DESC',
          limit: 200,
        );

        return rows
            .map((row) => Map<String, dynamic>.from(row))
            .toList(growable: false);
      },
    );
  }

  /// Strip empty/whitespace keywords and deduplicate case-insensitively.
  static List<String> _sanitizeKeywords(List<String> keywords) {
    final seen = <String>{};
    final out = <String>[];
    for (final kw in keywords) {
      final trimmed = kw.trim();
      if (trimmed.isEmpty) continue;
      final lowered = trimmed.toLowerCase();
      if (seen.add(lowered)) {
        out.add(trimmed);
      }
    }
    return out;
  }
}
