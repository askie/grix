part of 'local_db.dart';

class LocalDbSessionRepository {
  static Future<List<Map<String, dynamic>>> getSessions() async {
    return LocalDb._withDatabaseOr<List<Map<String, dynamic>>>(
      const <Map<String, dynamic>>[],
      (db) async {
        final rows = await db.query(
          'sessions',
          orderBy: 'is_pinned DESC, pinned_at DESC, updated_at DESC',
        );
        return rows.map((row) => Map<String, dynamic>.from(row)).toList();
      },
    );
  }

  static Future<Map<String, dynamic>?> getSessionRecord(
    String sessionId,
  ) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return null;

    return LocalDb._withDatabaseOr<Map<String, dynamic>?>(null, (db) async {
      final rows = await db.query(
        'sessions',
        where: 'session_id = ?',
        whereArgs: [sid],
        limit: 1,
      );
      if (rows.isEmpty) {
        return null;
      }
      return Map<String, dynamic>.from(rows.first);
    });
  }

  static Future<void> upsertSession(Map<String, dynamic> session) async {
    final filtered = LocalDbLifecycle._filterSessionColumns(session);
    final sid = filtered['session_id']?.toString().trim() ?? '';
    if (sid.isEmpty) return;
    filtered['session_id'] = sid;

    await LocalDb._withDatabase<void>((db) async {
      await db.transaction((txn) async {
        final existing = await txn.query(
          'sessions',
          where: 'session_id = ?',
          whereArgs: [sid],
          limit: 1,
        );
        final merged = existing.isEmpty
            ? filtered
            : {...Map<String, dynamic>.from(existing.first), ...filtered};
        await txn.insert(
          'sessions',
          merged,
          conflictAlgorithm: ConflictAlgorithm.replace,
        );
      });
    });
  }

  static Future<void> upsertSessionGroupAvatarMembers(
    String sessionId,
    List<SessionAvatarMember> members,
  ) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;

    final normalizedMembers = members
        .where((member) => member.memberId.trim().isNotEmpty)
        .take(9)
        .map((member) => member.toJson())
        .toList(growable: false);
    final encoded = normalizedMembers.isEmpty
        ? ''
        : jsonEncode(normalizedMembers);

    await LocalDb._withDatabase<void>((db) async {
      await db.update(
        'sessions',
        {'group_avatar_members': encoded},
        where: 'session_id = ?',
        whereArgs: [sid],
      );
    });
  }

  static Future<void> deleteSessionRecord(String sessionId) async {
    await LocalDb._withDatabase<void>((db) async {
      await db.delete(
        'sessions',
        where: 'session_id = ?',
        whereArgs: [sessionId],
      );
    });
  }

  static Future<void> deleteConversation(String sessionId) async {
    await LocalDb._withDatabase<void>((db) async {
      await db.transaction((txn) async {
        await txn.delete(
          'sessions',
          where: 'session_id = ?',
          whereArgs: [sessionId],
        );
        await txn.delete(
          'messages',
          where: 'session_id = ?',
          whereArgs: [sessionId],
        );
      });
    });
  }

  static Future<void> updateSessionLastMsg(
    String sessionId,
    String lastMsg,
    int updatedAt, {
    String type = 'private',
    String peerId = '',
    int peerType = 0,
  }) async {
    if (updatedAt > 0 && updatedAt < 10000000000) {
      updatedAt = updatedAt * 1000;
    }
    final pid = peerId.trim();

    await LocalDb._withDatabase<void>((db) async {
      await db.transaction((txn) async {
        final existing = await txn.query(
          'sessions',
          columns: ['session_id', 'peer_id'],
          where: 'session_id = ?',
          whereArgs: [sessionId],
          limit: 1,
        );
        if (existing.isEmpty) {
          await txn.insert('sessions', {
            'session_id': sessionId,
            'title': '',
            'type': type,
            'peer_id': pid,
            'peer_type': pid.isNotEmpty ? peerType : 0,
            'peer_nickname': '',
            'peer_username': '',
            'updated_at': updatedAt,
            'is_pinned': 0,
            'is_muted': 0,
            'pinned_at': 0,
            'unread_count': 0,
            'last_message': lastMsg,
            'last_message_time': updatedAt,
          }, conflictAlgorithm: ConflictAlgorithm.ignore);
          return;
        }

        final updates = <String, dynamic>{
          'updated_at': updatedAt,
          'last_message_time': updatedAt,
        };
        if (lastMsg.isNotEmpty) {
          updates['last_message'] = lastMsg;
        }
        if (type.trim().isNotEmpty) {
          updates['type'] = type;
        }
        // 现有记录对端身份缺失时补齐，不覆盖已有正确值
        final existingPeerId =
            (existing.first['peer_id'] ?? '').toString().trim();
        if (existingPeerId.isEmpty && pid.isNotEmpty) {
          updates['peer_id'] = pid;
          updates['peer_type'] = peerType;
        }
        await txn.update(
          'sessions',
          updates,
          where: 'session_id = ?',
          whereArgs: [sessionId],
        );
      });
    });
  }

  /// 仅前移会话活跃时间，不修改 last_message 文本与未读数。
  /// 用于工具消息（msg_type=4）等"不该展示但应让会话上浮"的事件——
  /// 与 pull_sync_resp 批量 delta 路径口径统一。
  /// 时间倒退或会话不存在时不做任何写入。
  static Future<void> bumpSessionActivity(
    String sessionId,
    int updatedAt,
  ) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    if (updatedAt > 0 && updatedAt < 10000000000) {
      updatedAt = updatedAt * 1000;
    }
    if (updatedAt <= 0) return;
    await LocalDb._withDatabase<void>((db) async {
      await db.update(
        'sessions',
        <String, dynamic>{
          'updated_at': updatedAt,
          'last_message_time': updatedAt,
        },
        where:
            'session_id = ? AND (updated_at IS NULL OR updated_at < ?) AND (last_message_time IS NULL OR last_message_time < ?)',
        whereArgs: [sid, updatedAt, updatedAt],
      );
    });
  }

  /// 回填由消息/未读快照创建的会话记录所缺的对端身份字段。
  /// 只更新 peer 相关列，不触碰未读数与最后消息。
  static Future<void> updateSessionPeerIdentity(
    String sessionId, {
    required String peerId,
    required int peerType,
    String peerNickname = '',
  }) async {
    final sid = sessionId.trim();
    final pid = peerId.trim();
    if (sid.isEmpty || pid.isEmpty) return;
    await LocalDb._withDatabase<void>((db) async {
      final updates = <String, dynamic>{
        'peer_id': pid,
        'peer_type': peerType,
      };
      final nickname = peerNickname.trim();
      if (nickname.isNotEmpty) {
        updates['peer_nickname'] = nickname;
      }
      await db.update(
        'sessions',
        updates,
        where: 'session_id = ?',
        whereArgs: [sid],
      );
    });
  }

  static Future<void> incrementUnread(String sessionId) async {
    await LocalDb._withDatabase<void>((db) async {
      await db.rawUpdate(
        'UPDATE sessions SET unread_count = COALESCE(unread_count, 0) + 1 WHERE session_id = ?',
        [sessionId],
      );
    });
  }

  static Future<void> incrementUnreadBy(String sessionId, int delta) async {
    if (delta <= 0) return;
    await LocalDb._withDatabase<void>((db) async {
      await db.rawUpdate(
        'UPDATE sessions SET unread_count = COALESCE(unread_count, 0) + ? WHERE session_id = ?',
        [delta, sessionId],
      );
    });
  }

  /// Batch-apply session deltas (last message + unread increment) in a
  /// single DB transaction.  Replaces N individual updateSessionLastMsg +
  /// incrementUnreadBy calls (2N DB round-trips) with one transaction.
  static Future<void> batchApplySessionDeltas(
    Map<String, Map<String, dynamic>> deltas,
    Map<String, String> typeHints,
  ) async {
    if (deltas.isEmpty) return;
    await LocalDb._withDatabase<void>((db) async {
      await db.transaction((txn) async {
        for (final entry in deltas.entries) {
          final sid = entry.key;
          final delta = entry.value;
          final lastContent = delta['last_content']?.toString() ?? '';
          final lastCreatedAt = delta['last_created_at'] as int? ?? 0;
          final unreadInc = delta['unread_inc'] as int? ?? 0;
          final type = typeHints[sid] ?? 'private';
          // 私聊对端身份（由消息发送者推导，见 _touchSessionByMessage 同口径）。
          final peerId = (delta['peer_id']?.toString() ?? '').trim();
          final peerType =
              peerId.isNotEmpty ? delta['peer_type'] as int? ?? 0 : 0;

          final existing = await txn.query(
            'sessions',
            columns: ['session_id', 'peer_id'],
            where: 'session_id = ?',
            whereArgs: [sid],
            limit: 1,
          );
          if (existing.isEmpty) {
            await txn.insert('sessions', {
              'session_id': sid,
              'title': '',
              'type': type,
              'peer_id': peerId,
              'peer_type': peerType,
              'peer_nickname': '',
              'peer_username': '',
              'updated_at': lastCreatedAt,
              'is_pinned': 0,
              'is_muted': 0,
              'pinned_at': 0,
              'unread_count': unreadInc > 0 ? unreadInc : 0,
              'last_message': lastContent,
              'last_message_time': lastCreatedAt,
            }, conflictAlgorithm: ConflictAlgorithm.ignore);
            continue;
          }

          final updates = <String, dynamic>{
            'updated_at': lastCreatedAt,
            'last_message_time': lastCreatedAt,
          };
          // Only overwrite preview text when content is present.
          // Empty last_content means the latest batch message was a tool card
          // whose JSON was intentionally excluded from preview.
          if (lastContent.isNotEmpty) {
            updates['last_message'] = lastContent;
          }
          if (type.trim().isNotEmpty) {
            updates['type'] = type;
          }
          // 现有记录对端身份缺失时补齐，不覆盖已有正确值（与
          // updateSessionLastMsg 同口径，顺带修复历史遗留的无 peer 占位行）。
          final existingPeerId = (existing.first['peer_id'] ?? '')
              .toString()
              .trim();
          if (existingPeerId.isEmpty && peerId.isNotEmpty) {
            updates['peer_id'] = peerId;
            updates['peer_type'] = peerType;
          }
          await txn.update(
            'sessions',
            updates,
            where: 'session_id = ?',
            whereArgs: [sid],
          );
          if (unreadInc > 0) {
            await txn.rawUpdate(
              'UPDATE sessions SET unread_count = COALESCE(unread_count, 0) + ? WHERE session_id = ?',
              [unreadInc, sid],
            );
          }
        }
      });
    });
  }

  static Future<void> replaceUnreadCounts(Map<String, int> snapshot) async {
    await LocalDb._withDatabase<void>((db) async {
      await db.transaction((txn) async {
        await txn.rawUpdate('UPDATE sessions SET unread_count = 0');
        final now = DateTime.now().millisecondsSinceEpoch;
        for (final entry in snapshot.entries) {
          final sessionId = entry.key;
          final unread = entry.value;
          if (sessionId.isEmpty || unread <= 0) {
            continue;
          }
          final existing = await txn.query(
            'sessions',
            columns: ['session_id'],
            where: 'session_id = ?',
            whereArgs: [sessionId],
            limit: 1,
          );
          if (existing.isEmpty) {
            await txn.insert('sessions', {
              'session_id': sessionId,
              'title': '',
              'type': 'private',
              'peer_id': '',
              'peer_type': 0,
              'peer_nickname': '',
              'peer_username': '',
              'updated_at': now,
              'is_pinned': 0,
              'is_muted': 0,
              'pinned_at': 0,
              'unread_count': unread,
            }, conflictAlgorithm: ConflictAlgorithm.ignore);
          } else {
            await txn.update(
              'sessions',
              {'unread_count': unread},
              where: 'session_id = ?',
              whereArgs: [sessionId],
            );
          }
        }
      });
    });
  }

  static Future<void> clearUnread(String sessionId) async {
    await LocalDb._withDatabase<void>((db) async {
      await db.update(
        'sessions',
        {'unread_count': 0},
        where: 'session_id = ?',
        whereArgs: [sessionId],
      );
    });
  }

  static Future<void> setUnreadCount(String sessionId, int unreadCount) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    final safeUnread = unreadCount < 0 ? 0 : unreadCount;

    await LocalDb._withDatabase<void>((db) async {
      await db.update(
        'sessions',
        {'unread_count': safeUnread},
        where: 'session_id = ?',
        whereArgs: [sid],
      );
    });
  }

  /// 批量写入未读数：单事务更新给定 session，不整表清零（区别于
  /// [replaceUnreadCounts]）。供点击多会话弹窗时一次对账使用，避免 N 次
  /// 独立 DB 往返。
  static Future<void> setUnreadCounts(Map<String, int> unreadBySession) async {
    if (unreadBySession.isEmpty) return;
    final normalized = <String, int>{};
    unreadBySession.forEach((sessionId, unreadCount) {
      final sid = sessionId.trim();
      if (sid.isEmpty) return;
      normalized[sid] = unreadCount < 0 ? 0 : unreadCount;
    });
    if (normalized.isEmpty) return;

    await LocalDb._withDatabase<void>((db) async {
      await db.transaction((txn) async {
        for (final entry in normalized.entries) {
          await txn.update(
            'sessions',
            {'unread_count': entry.value},
            where: 'session_id = ?',
            whereArgs: [entry.key],
          );
        }
      });
    });
  }

  static Future<void> setSessionPinned(
    String sessionId, {
    required bool isPinned,
    required int pinnedAt,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;

    await LocalDb._withDatabase<void>((db) async {
      await db.transaction((txn) async {
        final existing = await txn.query(
          'sessions',
          columns: ['session_id'],
          where: 'session_id = ?',
          whereArgs: [sid],
          limit: 1,
        );

        final pinValue = isPinned ? 1 : 0;
        final pinTimestamp = isPinned
            ? LocalDbLifecycle._normalizeCreatedAt(pinnedAt)
            : 0;
        if (existing.isEmpty) {
          await txn.insert('sessions', {
            'session_id': sid,
            'title': '',
            'type': 'private',
            'peer_id': '',
            'peer_type': 0,
            'peer_nickname': '',
            'peer_username': '',
            'updated_at': DateTime.now().millisecondsSinceEpoch,
            'is_pinned': pinValue,
            'is_muted': 0,
            'pinned_at': pinTimestamp,
            'unread_count': 0,
          }, conflictAlgorithm: ConflictAlgorithm.ignore);
          return;
        }

        await txn.update(
          'sessions',
          {'is_pinned': pinValue, 'pinned_at': pinTimestamp},
          where: 'session_id = ?',
          whereArgs: [sid],
        );
      });
    });
  }

  /// Persist friend/peer-level pin used by private conversation grouping.
  static Future<void> setFriendPinned(
    String sessionId, {
    required bool isPinned,
    required int pinnedAt,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;

    await LocalDb._withDatabase<void>((db) async {
      await db.transaction((txn) async {
        final existing = await txn.query(
          'sessions',
          columns: ['session_id'],
          where: 'session_id = ?',
          whereArgs: [sid],
          limit: 1,
        );

        final pinValue = isPinned ? 1 : 0;
        final pinTimestamp = isPinned
            ? LocalDbLifecycle._normalizeCreatedAt(pinnedAt)
            : 0;
        if (existing.isEmpty) {
          await txn.insert('sessions', {
            'session_id': sid,
            'title': '',
            'type': 'private',
            'peer_id': '',
            'peer_type': 0,
            'peer_nickname': '',
            'peer_username': '',
            'updated_at': DateTime.now().millisecondsSinceEpoch,
            'is_pinned': 0,
            'is_muted': 0,
            'pinned_at': 0,
            'friend_is_pinned': pinValue,
            'friend_pinned_at': pinTimestamp,
            'unread_count': 0,
          }, conflictAlgorithm: ConflictAlgorithm.ignore);
          return;
        }

        await txn.update(
          'sessions',
          {'friend_is_pinned': pinValue, 'friend_pinned_at': pinTimestamp},
          where: 'session_id = ?',
          whereArgs: [sid],
        );
      });
    });
  }

  static Future<void> setFriendMuted(
    String sessionId, {
    required bool isMuted,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;

    await LocalDb._withDatabase<void>((db) async {
      await db.transaction((txn) async {
        final existing = await txn.query(
          'sessions',
          columns: ['session_id'],
          where: 'session_id = ?',
          whereArgs: [sid],
          limit: 1,
        );

        final muteValue = isMuted ? 1 : 0;
        if (existing.isEmpty) {
          await txn.insert('sessions', {
            'session_id': sid,
            'title': '',
            'type': 'private',
            'peer_id': '',
            'peer_type': 0,
            'peer_nickname': '',
            'peer_username': '',
            'updated_at': DateTime.now().millisecondsSinceEpoch,
            'is_pinned': 0,
            'is_muted': 0,
            'pinned_at': 0,
            'friend_is_pinned': 0,
            'friend_pinned_at': 0,
            'friend_is_muted': muteValue,
            'unread_count': 0,
          }, conflictAlgorithm: ConflictAlgorithm.ignore);
          return;
        }

        await txn.update(
          'sessions',
          {'friend_is_muted': muteValue},
          where: 'session_id = ?',
          whereArgs: [sid],
        );
      });
    });
  }

  static Future<void> setSessionMuted(
    String sessionId, {
    required bool isMuted,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;

    await LocalDb._withDatabase<void>((db) async {
      await db.transaction((txn) async {
        final existing = await txn.query(
          'sessions',
          columns: ['session_id'],
          where: 'session_id = ?',
          whereArgs: [sid],
          limit: 1,
        );

        final muteValue = isMuted ? 1 : 0;
        if (existing.isEmpty) {
          await txn.insert('sessions', {
            'session_id': sid,
            'title': '',
            'type': 'private',
            'peer_id': '',
            'peer_type': 0,
            'peer_nickname': '',
            'peer_username': '',
            'updated_at': DateTime.now().millisecondsSinceEpoch,
            'is_pinned': 0,
            'is_muted': muteValue,
            'pinned_at': 0,
            'unread_count': 0,
          }, conflictAlgorithm: ConflictAlgorithm.ignore);
          return;
        }

        await txn.update(
          'sessions',
          {'is_muted': muteValue},
          where: 'session_id = ?',
          whereArgs: [sid],
        );
      });
    });
  }

  static Future<Map<String, Map<String, dynamic>>> getLastMessages() async {
    return LocalDb._withDatabaseOr<Map<String, Map<String, dynamic>>>(
      const <String, Map<String, dynamic>>{},
      (db) async {
        // 枚举会话后，逐会话经 idx_msg_session_created_msg 相关子查询取最新
        // 一条可预览消息（LIMIT 1），整体单次往返。可预览过滤含对 content 的
        // LIKE，这里把它限制在每个会话最新几行上；换成全表 GROUP BY 聚合则
        // 每条消息正文都要参与比对，消息量大时冷启动会被拖垮；拆成 Dart 层
        // 逐会话循环查询则会话数多时 N+1 往返反向劣化。
        final filter =
            LocalDbMessageRepository.excludeNonPreviewableMessagesSql;
        final res = await db.rawQuery('''
      SELECT m.* FROM messages m
      WHERE m.msg_id IN (
        SELECT (
          SELECT m2.msg_id FROM messages m2
          WHERE m2.session_id = s.session_id AND $filter
          ORDER BY m2.created_at DESC, m2.msg_id DESC
          LIMIT 1
        )
        FROM (SELECT DISTINCT session_id FROM messages) s
      )
    ''');
        final map = <String, Map<String, dynamic>>{};
        for (final row in res) {
          final sid = row['session_id']?.toString() ?? '';
          if (sid.isNotEmpty) {
            map[sid] = Map<String, dynamic>.from(row);
          }
        }
        return map;
      },
    );
  }

  static Future<void> upsertSessionTitle(
    String sessionId,
    String title, {
    String type = 'private',
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;

    await LocalDb._withDatabase<void>((db) async {
      await db.transaction((txn) async {
        final existing = await txn.query(
          'sessions',
          columns: ['session_id'],
          where: 'session_id = ?',
          whereArgs: [sid],
          limit: 1,
        );

        if (existing.isEmpty) {
          await txn.insert('sessions', {
            'session_id': sid,
            'title': title,
            'type': type,
            'peer_id': '',
            'peer_type': 0,
            'peer_nickname': '',
            'peer_username': '',
            'updated_at': DateTime.now().millisecondsSinceEpoch,
            'is_pinned': 0,
            'is_muted': 0,
            'pinned_at': 0,
            'unread_count': 0,
          }, conflictAlgorithm: ConflictAlgorithm.ignore);
          return;
        }

        final updates = <String, Object?>{'title': title};
        if (type.trim().isNotEmpty) {
          updates['type'] = type;
        }
        await txn.update(
          'sessions',
          updates,
          where: 'session_id = ?',
          whereArgs: [sid],
        );
      });
    });
  }

  static Future<void> upsertSessionTypes(Map<String, String> typeMap) async {
    if (typeMap.isEmpty) return;

    await LocalDb._withDatabase<void>((db) async {
      final now = DateTime.now().millisecondsSinceEpoch;
      await db.transaction((txn) async {
        for (final entry in typeMap.entries) {
          final sid = entry.key.trim();
          final type = entry.value.trim();
          if (sid.isEmpty || type.isEmpty) continue;

          final existing = await txn.query(
            'sessions',
            columns: ['session_id'],
            where: 'session_id = ?',
            whereArgs: [sid],
            limit: 1,
          );
          if (existing.isEmpty) {
            await txn.insert('sessions', {
              'session_id': sid,
              'title': '',
              'type': type,
              'peer_id': '',
              'peer_type': 0,
              'peer_nickname': '',
              'peer_username': '',
              'updated_at': now,
              'is_pinned': 0,
              'is_muted': 0,
              'pinned_at': 0,
              'unread_count': 0,
            }, conflictAlgorithm: ConflictAlgorithm.ignore);
            continue;
          }

          await txn.update(
            'sessions',
            {'type': type},
            where: 'session_id = ?',
            whereArgs: [sid],
          );
        }
      });
    });
  }
}
