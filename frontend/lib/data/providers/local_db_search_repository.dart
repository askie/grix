part of 'local_db.dart';

/// 本地关键词搜索的唯一实现：会话（sessions）与聊天记录（messages）。
///
/// 多关键词按「全部命中优先」排序：先取全部关键词都命中的行（AND），再用命中
/// 任意关键词的行（OR）补位并按主键去重，因此完全匹配始终排在部分匹配前面。
/// 保持纯 LIKE 实现，不引入 FTS5。
class LocalDbSearchRepository {
  /// sessions 表参与匹配的列。
  static const List<String> _sessionSearchColumns = <String>[
    'title',
    'peer_nickname',
    'peer_username',
    'last_message',
  ];

  static const int defaultSessionLimit = 200;
  static const int defaultMessageLimit = 200;

  /// 会话搜索：返回原始行，供调用方自行构造会话模型。
  static Future<List<Map<String, dynamic>>> searchSessionRecords(
    List<String> keywords, {
    int limit = defaultSessionLimit,
  }) async {
    final sanitized = _sanitizeKeywords(keywords);
    if (sanitized.isEmpty || limit <= 0) {
      return const <Map<String, dynamic>>[];
    }

    return LocalDb._withDatabaseOr<List<Map<String, dynamic>>>(
      const <Map<String, dynamic>>[],
      (db) => _collectRanked(
        db: db,
        table: 'sessions',
        idColumn: 'session_id',
        searchColumns: _sessionSearchColumns,
        keywords: sanitized,
        orderBy: 'updated_at DESC',
        limit: limit,
      ),
    );
  }

  /// 会话搜索：返回搜索结果视图模型。
  static Future<List<MatchedSession>> searchSessions(
    List<String> keywords, {
    int limit = defaultSessionLimit,
  }) async {
    final rows = await searchSessionRecords(keywords, limit: limit);
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
  }

  /// 聊天记录搜索：只匹配 content，排除 msg_type=4 的流式占位消息。
  static Future<List<MatchedMessage>> searchMessages(
    List<String> keywords, {
    int limit = defaultMessageLimit,
  }) async {
    final sanitized = _sanitizeKeywords(keywords);
    if (sanitized.isEmpty || limit <= 0) {
      return const <MatchedMessage>[];
    }

    return LocalDb._withDatabaseOr<List<MatchedMessage>>(
      const <MatchedMessage>[],
      (db) async {
        final rows = await _collectRanked(
          db: db,
          table: 'messages',
          idColumn: 'msg_id',
          columns: const <String>[
            'msg_id',
            'session_id',
            'content',
            'created_at',
          ],
          searchColumns: const <String>['content'],
          keywords: sanitized,
          orderBy: 'created_at DESC',
          extraWhere: LocalDbMessageRepository.excludeStreamingPlaceholderSql,
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

  /// 会话 + 聊天记录的组合搜索。
  static Future<LocalSearchResult> search(
    List<String> keywords, {
    int sessionLimit = defaultSessionLimit,
    int messageLimit = defaultMessageLimit,
  }) async {
    final results = await Future.wait([
      searchSessions(keywords, limit: sessionLimit),
      searchMessages(keywords, limit: messageLimit),
    ]);
    return LocalSearchResult(
      matchedSessions: results[0] as List<MatchedSession>,
      matchedMessages: results[1] as List<MatchedMessage>,
    );
  }

  /// 先 AND 后 OR 取行并按 [idColumn] 去重：全部关键词命中的行排在前面，
  /// 只命中部分关键词的行降权补位。单关键词时 AND 与 OR 等价，直接走一轮。
  static Future<List<Map<String, dynamic>>> _collectRanked({
    required Database db,
    required String table,
    required String idColumn,
    required List<String> searchColumns,
    required List<String> keywords,
    required String orderBy,
    required int limit,
    List<String>? columns,
    String? extraWhere,
  }) async {
    final collected = <String, Map<String, dynamic>>{};

    Future<void> collect({required bool requireAll}) async {
      if (collected.length >= limit) return;
      final clause = _buildKeywordClause(
        keywords,
        searchColumns,
        requireAll: requireAll,
      );
      final where = extraWhere == null
          ? clause.where
          : '${clause.where} AND $extraWhere';
      final rows = await db.query(
        table,
        columns: columns,
        where: where,
        whereArgs: clause.args,
        orderBy: orderBy,
        limit: limit,
      );
      for (final row in rows) {
        final id = row[idColumn]?.toString() ?? '';
        if (id.isEmpty || collected.containsKey(id)) continue;
        collected[id] = Map<String, dynamic>.from(row);
        if (collected.length >= limit) return;
      }
    }

    if (keywords.length > 1) {
      await collect(requireAll: true);
    }
    await collect(requireAll: false);
    return collected.values.toList(growable: false);
  }

  /// 拼 WHERE 子句：每个关键词占一组「任一列 LIKE」，组间按 AND / OR 连接。
  static _KeywordClause _buildKeywordClause(
    List<String> keywords,
    List<String> searchColumns, {
    required bool requireAll,
  }) {
    final joiner = requireAll ? ' AND ' : ' OR ';
    final buffer = StringBuffer('(');
    final args = <String>[];
    for (var i = 0; i < keywords.length; i++) {
      if (i > 0) buffer.write(joiner);
      final pattern = '%${keywords[i]}%';
      buffer.write('(');
      for (var c = 0; c < searchColumns.length; c++) {
        if (c > 0) buffer.write(' OR ');
        buffer.write('${searchColumns[c]} LIKE ?');
        args.add(pattern);
      }
      buffer.write(')');
    }
    buffer.write(')');
    return _KeywordClause(buffer.toString(), args);
  }

  /// 把一行输入切成关键词：按空白切分、去空、忽略大小写去重。
  static List<String> tokenize(String raw) {
    return _sanitizeKeywords(raw.split(RegExp(r'\s+')));
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

class _KeywordClause {
  const _KeywordClause(this.where, this.args);

  final String where;
  final List<String> args;
}
