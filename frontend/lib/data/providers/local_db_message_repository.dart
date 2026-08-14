part of 'local_db.dart';

class LocalDbMessageRepository {
  // 排除 msg_type=4 流式占位行：占位消息是流式输出过程中的临时态，
  // 正常封板后会变为 msg_type=1（带内容）。本地库里残留的 msg_type=4 行
  // 只可能来自历史接口下发的孤儿占位（content 为空），绝不能当作已封板
  // 消息渲染，否则会显示为空白气泡。活跃流式占位由内存态驱动，不读本地库。
  static const String _excludeStreamingPlaceholder =
      '(msg_type IS NULL OR msg_type != 4)';
  static String get excludeStreamingPlaceholderSql =>
      _excludeStreamingPlaceholder;

  static const String _excludeNonPreviewableMessages =
      "(msg_type IS NULL OR msg_type != 4) AND content NOT LIKE '%](grix://card/%'";
  static String get excludeNonPreviewableMessagesSql =>
      _excludeNonPreviewableMessages;

  static Future<void> batchInsertMessages(
    List<Map<String, dynamic>> msgs,
  ) async {
    if (msgs.isEmpty) return;
    await LocalDb._withDatabase<void>((db) async {
      final batch = db.batch();
      for (final msg in msgs) {
        batch.insert(
          'messages',
          LocalDbLifecycle._filterMessageColumns(msg),
          conflictAlgorithm: ConflictAlgorithm.replace,
        );
      }
      await batch.commit(noResult: true);
    });
  }

  static Future<void> batchUpsertMessages(
    List<Map<String, dynamic>> msgs,
  ) async {
    if (msgs.isEmpty) return;
    final filteredByMsgId = <String, Map<String, dynamic>>{};
    for (final msg in msgs) {
      final filtered = LocalDbLifecycle._filterMessageColumns(msg);
      final msgId = filtered['msg_id']?.toString() ?? '';
      if (msgId.isNotEmpty) {
        filteredByMsgId.update(
          msgId,
          (existing) => {...existing, ...filtered},
          ifAbsent: () => filtered,
        );
      }
    }
    final filteredRows = filteredByMsgId.values.toList(growable: false);
    if (filteredRows.isEmpty) return;

    await LocalDb._withDatabase<void>((db) async {
      await db.transaction((txn) async {
        final existingIds = <String>{};
        const chunkSize = 400;
        for (var start = 0; start < filteredRows.length; start += chunkSize) {
          final end = (start + chunkSize < filteredRows.length)
              ? start + chunkSize
              : filteredRows.length;
          final chunk = filteredRows.sublist(start, end);
          final placeholders = List.filled(chunk.length, '?').join(',');
          final existing = await txn.query(
            'messages',
            columns: ['msg_id'],
            where: 'msg_id IN ($placeholders)',
            whereArgs: chunk.map((row) => row['msg_id'].toString()).toList(),
          );
          for (final row in existing) {
            final msgId = row['msg_id']?.toString() ?? '';
            if (msgId.isNotEmpty) {
              existingIds.add(msgId);
            }
          }
        }

        final batch = txn.batch();
        for (final filtered in filteredRows) {
          final msgId = filtered['msg_id']?.toString() ?? '';
          if (existingIds.contains(msgId)) {
            batch.update(
              'messages',
              filtered,
              where: 'msg_id = ?',
              whereArgs: [msgId],
            );
          } else {
            batch.insert(
              'messages',
              filtered,
              conflictAlgorithm: ConflictAlgorithm.replace,
            );
            existingIds.add(msgId);
          }
        }
        await batch.commit(noResult: true);
      });
    });
  }

  static Future<void> upsertMessage(Map<String, dynamic> msg) async {
    final filtered = LocalDbLifecycle._filterMessageColumns(msg);
    final msgId = filtered['msg_id']?.toString() ?? '';
    if (msgId.isEmpty) return;

    await LocalDb._withDatabase<void>((db) async {
      await db.transaction((txn) async {
        final existing = await txn.query(
          'messages',
          columns: ['msg_id'],
          where: 'msg_id = ?',
          whereArgs: [msgId],
          limit: 1,
        );

        if (existing.isEmpty) {
          await txn.insert(
            'messages',
            filtered,
            conflictAlgorithm: ConflictAlgorithm.replace,
          );
          return;
        }

        await txn.update(
          'messages',
          filtered,
          where: 'msg_id = ?',
          whereArgs: [msgId],
        );
      });
    });
  }

  static Future<int> getMaxInboxSeq() async {
    return LocalDb._withDatabaseOr<int>(0, (db) async {
      final result = await db.rawQuery(
        'SELECT MAX(inbox_seq) as max_seq FROM messages',
      );
      if (result.isNotEmpty) {
        final raw = result.first['max_seq'];
        if (raw == null) return 0;
        try {
          return LocalDbLifecycle._requireInt(
            raw,
            fieldName: 'messages.max_seq',
          );
        } on FormatException catch (e) {
          debugPrint('LocalDb getMaxInboxSeq parse error: $e');
          return 0;
        }
      }
      return 0;
    });
  }

  /// 检测会话本地 inbox_seq 是否存在断档（空洞）。
  ///
  static Future<void> insertLocalStub(Map<String, dynamic> msg) async {
    await LocalDb._withDatabase<void>((db) async {
      await db.insert('messages', LocalDbLifecycle._filterMessageColumns(msg));
    });
  }

  static List<Map<String, dynamic>> _normalizeMessageRows(
    List<Map<String, Object?>> rows,
  ) {
    final normalized = rows.map((row) {
      final m = Map<String, dynamic>.from(row);
      final ts = LocalDbLifecycle._requireInt(
        m['created_at'],
        fieldName: 'messages.created_at',
      );
      m['created_at'] = LocalDbLifecycle._normalizeCreatedAt(ts);
      if (m.containsKey('msg_type') && m['msg_type'] != null) {
        LocalDbLifecycle._requireInt(
          m['msg_type'],
          fieldName: 'messages.msg_type',
        );
      }
      return m;
    }).toList();

    normalized.sort((a, b) {
      final ta = LocalDbLifecycle._requireInt(
        a['created_at'],
        fieldName: 'messages.created_at',
      );
      final tb = LocalDbLifecycle._requireInt(
        b['created_at'],
        fieldName: 'messages.created_at',
      );
      final cmp = ta.compareTo(tb);
      if (cmp != 0) return cmp;
      final ida = a['msg_id']?.toString() ?? '';
      final idb = b['msg_id']?.toString() ?? '';
      return ida.compareTo(idb);
    });

    return normalized;
  }

  static Future<List<Map<String, dynamic>>> getLatestMessages(
    String sessionId, {
    int limit = 60,
  }) async {
    return LocalDb._withDatabaseOr<List<Map<String, dynamic>>>(
      const <Map<String, dynamic>>[],
      (db) async {
        final res = await db.query(
          'messages',
          where: 'session_id = ? AND $_excludeStreamingPlaceholder',
          whereArgs: [sessionId],
          orderBy: 'created_at DESC, msg_id DESC',
          limit: limit,
        );
        return _normalizeMessageRows(
          res.map((row) => Map<String, Object?>.from(row)).toList(),
        );
      },
    );
  }

  /// 会话摘要专用：取最近一条"可预览"消息（排除流式占位与纯卡片消息）。
  /// 卡片消息只推进会话活跃时间，不参与摘要，保证摘要停留在上一条可读文本上。
  static Future<Map<String, dynamic>?> getLatestPreviewableMessage(
    String sessionId,
  ) async {
    return LocalDb._withDatabaseOr<Map<String, dynamic>?>(null, (db) async {
      final res = await db.query(
        'messages',
        where: 'session_id = ? AND $_excludeNonPreviewableMessages',
        whereArgs: [sessionId],
        orderBy: 'created_at DESC, msg_id DESC',
        limit: 1,
      );
      if (res.isEmpty) return null;
      final normalized = _normalizeMessageRows(
        res.map((row) => Map<String, Object?>.from(row)).toList(),
      );
      return normalized.isEmpty ? null : normalized.first;
    });
  }

  static Future<List<Map<String, dynamic>>> getMessagesBefore(
    String sessionId, {
    required int beforeCreatedAt,
    required String beforeMsgId,
    int limit = 20,
  }) async {
    return LocalDb._withDatabaseOr<
      List<Map<String, dynamic>>
    >(const <Map<String, dynamic>>[], (db) async {
      final res = await db.query(
        'messages',
        where:
            'session_id = ? AND (created_at < ? OR (created_at = ? AND msg_id < ?)) AND $_excludeStreamingPlaceholder',
        whereArgs: [
          sessionId,
          LocalDbLifecycle._normalizeCreatedAt(beforeCreatedAt),
          LocalDbLifecycle._normalizeCreatedAt(beforeCreatedAt),
          beforeMsgId,
        ],
        orderBy: 'created_at DESC, msg_id DESC',
        limit: limit,
      );
      return _normalizeMessageRows(
        res.map((row) => Map<String, Object?>.from(row)).toList(),
      );
    });
  }

  static Future<List<Map<String, dynamic>>> getMessagesAfter(
    String sessionId, {
    required int afterCreatedAt,
    required String afterMsgId,
    int limit = 20,
  }) async {
    return LocalDb._withDatabaseOr<
      List<Map<String, dynamic>>
    >(const <Map<String, dynamic>>[], (db) async {
      final res = await db.query(
        'messages',
        where:
            'session_id = ? AND (created_at > ? OR (created_at = ? AND msg_id > ?)) AND $_excludeStreamingPlaceholder',
        whereArgs: [
          sessionId,
          LocalDbLifecycle._normalizeCreatedAt(afterCreatedAt),
          LocalDbLifecycle._normalizeCreatedAt(afterCreatedAt),
          afterMsgId,
        ],
        orderBy: 'created_at ASC, msg_id ASC',
        limit: limit,
      );
      return _normalizeMessageRows(
        res.map((row) => Map<String, Object?>.from(row)).toList(),
      );
    });
  }

  static Future<List<Map<String, dynamic>>> getRecentMessagesForSession(
    String sessionId, {
    int limit = 30,
  }) async {
    return LocalDb._withDatabaseOr<List<Map<String, dynamic>>>(
      const <Map<String, dynamic>>[],
      (db) async {
        final res = await db.query(
          'messages',
          where: 'session_id = ?',
          whereArgs: [sessionId],
          orderBy: 'created_at DESC, msg_id DESC',
          limit: limit,
        );
        return _normalizeMessageRows(
          res.map((row) => Map<String, Object?>.from(row)).toList(),
        );
      },
    );
  }

  // 返回该会话最新一条服务端消息的 msg_id（字符串）。
  //
  // msg_id 是 19 位雪花号，以 TEXT 存储。排序在 SQLite 内用 CAST(... AS INTEGER)
  // 做数值比较（SQLite 内部 64 位整数安全），但取回的是原始 TEXT 值——绝不把
  // 整数跨回 Dart，否则 Web 端（JS 53 位精度）会把尾部截断。无有效消息时返回空串。
  static Future<String> getLatestServerMessageId(String sessionId) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return '';

    return LocalDb._withDatabaseOr<String>('', (db) async {
      final rows = await db.rawQuery(
        '''
        SELECT msg_id
        FROM messages
        WHERE session_id = ?
          AND local_seq IS NULL
          AND msg_id GLOB '[0-9]*'
        ORDER BY CAST(msg_id AS INTEGER) DESC
        LIMIT 1
        ''',
        [sid],
      );
      if (rows.isEmpty) return '';
      return rows.first['msg_id']?.toString() ?? '';
    });
  }

  static Future<String> getFirstMessageContentBySession(
    String sessionId,
  ) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return '';

    return LocalDb._withDatabaseOr<String>('', (db) async {
      final rows = await db.query(
        'messages',
        columns: ['content'],
        where: 'session_id = ?',
        whereArgs: [sid],
        orderBy: 'created_at ASC, msg_id ASC',
        limit: 1,
      );
      if (rows.isEmpty) return '';
      return rows.first['content']?.toString() ?? '';
    });
  }

  static Future<void> deleteMessage(String msgId) async {
    await LocalDb._withDatabase<void>((db) async {
      await db.delete('messages', where: 'msg_id = ?', whereArgs: [msgId]);
    });
  }

  static Future<void> deleteMessageByLocalSeq(String localSeq) async {
    await LocalDb._withDatabase<void>((db) async {
      await db.delete(
        'messages',
        where: 'local_seq = ?',
        whereArgs: [localSeq],
      );
    });
  }

  static Future<Map<String, String>> getLatestPeerSenderIds({
    required String myUserId,
  }) async {
    final me = myUserId.trim();
    if (me.isEmpty) return {};

    return LocalDb._withDatabaseOr<Map<String, String>>(
      const <String, String>{},
      (db) async {
        final rows = await db.rawQuery(
          '''
      SELECT m.session_id, m.sender_id FROM messages m
      INNER JOIN (
        SELECT session_id, MAX(created_at) AS max_ts
        FROM messages
        WHERE sender_id != ?
        GROUP BY session_id
      ) latest ON latest.session_id = m.session_id AND latest.max_ts = m.created_at
      ''',
          [me],
        );

        final map = <String, String>{};
        for (final row in rows) {
          final sid = row['session_id']?.toString().trim() ?? '';
          final senderId = row['sender_id']?.toString().trim() ?? '';
          if (sid.isEmpty || senderId.isEmpty || senderId == me) {
            continue;
          }
          map[sid] = senderId;
        }
        return map;
      },
    );
  }

  static Future<Set<String>> getSessionsWithUnreadMentions(
    Map<String, int> unreadCountBySession, {
    required String userId,
  }) async {
    final normalizedUserId = userId.trim();
    if (normalizedUserId.isEmpty || unreadCountBySession.isEmpty) {
      return <String>{};
    }

    final candidates = <String, int>{};
    unreadCountBySession.forEach((sessionId, unreadCount) {
      final sid = sessionId.trim();
      if (sid.isEmpty || unreadCount <= 0) {
        return;
      }
      candidates[sid] = unreadCount;
    });
    if (candidates.isEmpty) {
      return <String>{};
    }

    return LocalDb._withDatabaseOr<Set<String>>(<String>{}, (db) async {
      final matched = <String>{};
      for (final entry in candidates.entries) {
        final rows = await db.query(
          'messages',
          columns: ['extra'],
          where: 'session_id = ? AND local_seq IS NULL AND sender_id != ?',
          whereArgs: [entry.key, normalizedUserId],
          orderBy: 'created_at DESC, msg_id DESC',
          limit: entry.value,
        );
        for (final row in rows) {
          if (_messageMentionsUser(row['extra'], normalizedUserId)) {
            matched.add(entry.key);
            break;
          }
        }
      }
      return matched;
    });
  }

  static bool _messageMentionsUser(dynamic rawExtra, String userId) {
    final mentions = _extractMentionUserIds(rawExtra);
    if (mentions.isEmpty) {
      return false;
    }
    return mentions.contains(userId.trim());
  }

  static Set<String> _extractMentionUserIds(dynamic rawExtra) {
    Map<String, dynamic>? extra;
    if (rawExtra is Map<String, dynamic>) {
      extra = Map<String, dynamic>.from(rawExtra);
    } else if (rawExtra is Map) {
      extra = Map<String, dynamic>.from(rawExtra);
    } else if (rawExtra is String) {
      final trimmed = rawExtra.trim();
      if (trimmed.isEmpty) {
        return <String>{};
      }
      try {
        final decoded = jsonDecode(trimmed);
        if (decoded is Map) {
          extra = Map<String, dynamic>.from(decoded);
        }
      } catch (_) {
        return <String>{};
      }
    }
    if (extra == null || extra.isEmpty) {
      return <String>{};
    }

    final rawMentions = extra['mention_user_ids'];
    if (rawMentions is! List) {
      return <String>{};
    }

    final normalized = <String>{};
    for (final item in rawMentions) {
      final mentionId = item?.toString().trim() ?? '';
      if (mentionId.isEmpty) {
        continue;
      }
      normalized.add(mentionId);
    }
    return normalized;
  }

  static Future<void> updateAckMsg(
    String localSeq,
    String msgId,
    int inboxSeq, {
    int? createdAt,
  }) async {
    await LocalDb._withDatabase<void>((db) async {
      await db.transaction((txn) async {
        final countRes = await txn.rawQuery(
          'SELECT COUNT(*) as count FROM messages WHERE msg_id = ?',
          [msgId],
        );
        final int count = Sqflite.firstIntValue(countRes) ?? 0;

        if (count > 0) {
          await txn.delete(
            'messages',
            where: 'local_seq = ?',
            whereArgs: [localSeq],
          );
        } else {
          final updates = <String, dynamic>{
            'msg_id': msgId,
            'status': 'success',
            'inbox_seq': inboxSeq,
            'local_seq': null,
          };
          if (createdAt != null && createdAt > 0) {
            updates['created_at'] = LocalDbLifecycle._normalizeCreatedAt(
              createdAt,
            );
          }
          await txn.update(
            'messages',
            updates,
            where: 'local_seq = ?',
            whereArgs: [localSeq],
          );
        }
      });
    });
  }

  static Future<void> updateMessageStatusByLocalSeq(
    String localSeq,
    String status,
  ) async {
    await LocalDb._withDatabase<void>((db) async {
      await db.update(
        'messages',
        {'status': status},
        where: 'local_seq = ?',
        whereArgs: [localSeq],
      );
    });
  }

  static Future<void> updateAgentDeliveryStatusByMsgId(
    String msgId,
    String agentDeliveryStatus,
  ) async {
    final normalizedMsgId = msgId.trim();
    final normalizedStatus = agentDeliveryStatus.trim();
    if (normalizedMsgId.isEmpty || normalizedStatus.isEmpty) {
      return;
    }
    await LocalDb._withDatabase<void>((db) async {
      await db.update(
        'messages',
        {'agent_delivery_status': normalizedStatus},
        where: 'msg_id = ?',
        whereArgs: [normalizedMsgId],
      );
    });
  }

  /// Batch variant of [updateAgentDeliveryStatusByMsgId]. All updates run
  /// inside a single SQFlite transaction, avoiding per-row queue overhead
  /// on Web/IndexedDB when replaying stored statuses on connect.
  /// Later entries (by list order) win when the same msgId appears twice.
  static Future<void> updateAgentDeliveryStatusBatch(
    List<MapEntry<String, String>> entries,
  ) async {
    if (entries.isEmpty) return;
    await LocalDb._withDatabase<void>((db) async {
      await db.transaction((txn) async {
        for (final entry in entries) {
          final msgId = entry.key.trim();
          final status = entry.value.trim();
          if (msgId.isEmpty || status.isEmpty) continue;
          await txn.update(
            'messages',
            {'agent_delivery_status': status},
            where: 'msg_id = ?',
            whereArgs: [msgId],
          );
        }
      });
    });
  }

  static Future<Map<String, dynamic>?> getMessageByMsgId(String msgId) async {
    return LocalDb._withDatabase<Map<String, dynamic>?>((db) async {
      final res = await db.query(
        'messages',
        where: 'msg_id = ?',
        whereArgs: [msgId],
        limit: 1,
      );
      if (res.isEmpty) return null;
      return Map<String, dynamic>.from(res.first);
    });
  }

  static Future<Map<String, dynamic>?> getMessageByLocalSeq(
    String localSeq,
  ) async {
    return LocalDb._withDatabase<Map<String, dynamic>?>((db) async {
      final res = await db.query(
        'messages',
        where: 'local_seq = ?',
        whereArgs: [localSeq],
        limit: 1,
      );
      if (res.isEmpty) return null;
      return Map<String, dynamic>.from(res.first);
    });
  }

  static Future<List<Map<String, dynamic>>> getPendingOutboundMessages({
    int limit = 200,
  }) async {
    return LocalDb._withDatabaseOr<List<Map<String, dynamic>>>(
      const <Map<String, dynamic>>[],
      (db) async {
        final res = await db.query(
          'messages',
          where: 'local_seq IS NOT NULL AND status = ?',
          whereArgs: ['sending'],
          orderBy: 'created_at ASC',
          limit: limit,
        );
        return res.map((e) => Map<String, dynamic>.from(e)).toList();
      },
    );
  }
}
