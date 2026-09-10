part of 'local_db.dart';

/// 本地关键词搜索的唯一实现：会话（sessions）与聊天记录（messages）。
///
/// 多关键词按「命中数」排序：单条 SQL 里给每个关键词算一个 0/1 命中位再求和
/// 作为 hits 列，`ORDER BY hits DESC` 让全部命中的行排在只命中部分的前面，
/// 一次扫描替代原来的「先 AND 再 OR」两遍扫描。保持纯 LIKE 实现，不引入 FTS5。
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
  ///
  /// [scope] 为空时是首页现状：全库搜索，行为与改动前完全一致。非空时把范围
  /// 转成参数化条件拼进内层 WHERE，与关键词命中位同一条 SQL 一次扫描完成，
  /// 不再依赖调用方在拿到全库 200 条结果后自己按对端在内存里过滤截断。
  static Future<List<Map<String, dynamic>>> searchSessionRecords(
    List<String> keywords, {
    int limit = defaultSessionLimit,
    LocalSearchScope? scope,
    bool Function()? isCancelled,
    void Function(int waitMs, int runMs)? onTiming,
  }) async {
    final sanitized = _sanitizeKeywords(keywords);
    if (sanitized.isEmpty || limit <= 0) {
      return const <Map<String, dynamic>>[];
    }

    final scopeFilter = _sessionsScopeFilter(scope);
    return LocalDb._withDatabaseOr<List<Map<String, dynamic>>>(
      const <Map<String, dynamic>>[],
      (db) => _collectRanked(
        db: db,
        table: 'sessions',
        searchColumns: _sessionSearchColumns,
        keywords: sanitized,
        orderBy: 'updated_at DESC',
        limit: limit,
        extraWhere: scopeFilter.sql,
        extraWhereArgs: scopeFilter.args,
      ),
      isCancelled: isCancelled,
      onTiming: onTiming,
    );
  }

  /// 会话搜索：返回搜索结果视图模型。
  static Future<List<MatchedSession>> searchSessions(
    List<String> keywords, {
    int limit = defaultSessionLimit,
    LocalSearchScope? scope,
  }) async {
    final rows = await searchSessionRecords(keywords, limit: limit, scope: scope);
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

  /// 聊天记录搜索：只匹配 content，排除 msg_type=4 的流式占位消息；
  /// [scope] 非空时再叠加范围条件（AND），语义同 [searchSessionRecords]。
  static Future<List<MatchedMessage>> searchMessages(
    List<String> keywords, {
    int limit = defaultMessageLimit,
    LocalSearchScope? scope,
    bool Function()? isCancelled,
    void Function(int waitMs, int runMs)? onTiming,
  }) async {
    final sanitized = _sanitizeKeywords(keywords);
    if (sanitized.isEmpty || limit <= 0) {
      return const <MatchedMessage>[];
    }

    final scopeFilter = _messagesScopeFilter(scope);
    final combinedWhere = _combineWhere(<String?>[
      LocalDbMessageRepository.excludeStreamingPlaceholderSql,
      scopeFilter.sql,
    ]);

    return LocalDb._withDatabaseOr<List<MatchedMessage>>(
      const <MatchedMessage>[],
      (db) async {
        final rows = await _collectRanked(
          db: db,
          table: 'messages',
          columns: const <String>[
            'msg_id',
            'session_id',
            'content',
            'created_at',
          ],
          searchColumns: const <String>['content'],
          keywords: sanitized,
          orderBy: 'created_at DESC',
          extraWhere: combinedWhere,
          extraWhereArgs: scopeFilter.args,
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
      isCancelled: isCancelled,
      onTiming: onTiming,
    );
  }

  /// 会话 + 聊天记录的组合搜索。
  static Future<LocalSearchResult> search(
    List<String> keywords, {
    int sessionLimit = defaultSessionLimit,
    int messageLimit = defaultMessageLimit,
    LocalSearchScope? scope,
  }) async {
    final results = await Future.wait([
      searchSessions(keywords, limit: sessionLimit, scope: scope),
      searchMessages(keywords, limit: messageLimit, scope: scope),
    ]);
    return LocalSearchResult(
      matchedSessions: results[0] as List<MatchedSession>,
      matchedMessages: results[1] as List<MatchedMessage>,
    );
  }

  /// [scope] 转成的一条参数化 WHERE 片段 + 对应参数，供 [_collectRanked]
  /// 拼进内层查询；scope 为空时 sql 为 null，行为与改动前完全一致。
  static _ScopeFilter _sessionsScopeFilter(LocalSearchScope? scope) {
    if (scope == null) return const _ScopeFilter();
    final sessionId = scope.sessionId;
    if (sessionId != null) {
      return _ScopeFilter(sql: 'session_id = ?', args: <Object?>[sessionId]);
    }
    return _ScopeFilter(
      sql: 'type = ? AND peer_type = ? AND peer_id = ?',
      args: <Object?>['private', scope.peerType, scope.peerId],
    );
  }

  /// messages 表没有对端字段，peer 范围要经 sessions 表反查 session_id 集合；
  /// session 范围直接等值匹配 session_id 即可。
  static _ScopeFilter _messagesScopeFilter(LocalSearchScope? scope) {
    if (scope == null) return const _ScopeFilter();
    final sessionId = scope.sessionId;
    if (sessionId != null) {
      return _ScopeFilter(sql: 'session_id = ?', args: <Object?>[sessionId]);
    }
    return _ScopeFilter(
      sql:
          'session_id IN (SELECT session_id FROM sessions '
          'WHERE type = ? AND peer_type = ? AND peer_id = ?)',
      args: <Object?>['private', scope.peerType, scope.peerId],
    );
  }

  /// 把多个可能为空的 WHERE 片段用 AND 拼起来；全空时返回 null（不加 WHERE）。
  static String? _combineWhere(List<String?> parts) {
    final nonEmpty = <String>[
      for (final part in parts)
        if (part != null && part.isNotEmpty) part,
    ];
    if (nonEmpty.isEmpty) return null;
    if (nonEmpty.length == 1) return nonEmpty.first;
    return nonEmpty.map((p) => '($p)').join(' AND ');
  }

  /// 单条 SQL 取行：给每个关键词算一个 0/1 命中位（该关键词命中任一搜索列即为 1），
  /// 求和作为 hits 列，`WHERE hits > 0` 过滤、`ORDER BY hits DESC` 排序——全部
  /// 关键词都命中的行 hits 最大排最前，只命中部分的行降权排后，语义与原来的
  /// 「先 AND 再 OR」两遍扫描一致，但只扫一遍表。列值用 COALESCE 兜底避免
  /// NULL 参与 OR/求和时把命中位污染成 NULL 导致整行被误判为不命中。
  static Future<List<Map<String, dynamic>>> _collectRanked({
    required Database db,
    required String table,
    required List<String> searchColumns,
    required List<String> keywords,
    required String orderBy,
    required int limit,
    List<String>? columns,
    String? extraWhere,
    List<Object?> extraWhereArgs = const <Object?>[],
  }) async {
    final selectColumns = columns == null ? '*' : columns.join(', ');
    final hitsTerms = <String>[];
    final args = <Object?>[];
    for (final keyword in keywords) {
      final pattern = '%$keyword%';
      final matchTerms = searchColumns
          .map((c) => "COALESCE($c, '') LIKE ?")
          .join(' OR ');
      hitsTerms.add('($matchTerms)');
      args.addAll(List<Object?>.filled(searchColumns.length, pattern));
    }
    final hitsExpr = hitsTerms.join(' + ');
    final innerWhere = extraWhere == null ? '' : 'WHERE $extraWhere';
    final sql =
        'SELECT * FROM ('
        'SELECT $selectColumns, ($hitsExpr) AS hits FROM $table $innerWhere'
        ') WHERE hits > 0 ORDER BY hits DESC, $orderBy LIMIT ?';
    // extraWhere 的占位符在 SQL 文本里排在 hitsExpr 之后、LIMIT 之前，
    // extraWhereArgs 必须插在这个位置，否则会跟错占位符绑错值。
    args.addAll(extraWhereArgs);
    args.add(limit);

    final rows = await db.rawQuery(sql, args);
    return rows.map((row) {
      final copy = Map<String, dynamic>.from(row);
      copy.remove('hits');
      return copy;
    }).toList(growable: false);
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

/// [LocalSearchScope] 转成的 SQL 片段 + 参数对；`sql == null` 表示不加范围条件。
class _ScopeFilter {
  const _ScopeFilter({this.sql, this.args = const <Object?>[]});

  final String? sql;
  final List<Object?> args;
}
