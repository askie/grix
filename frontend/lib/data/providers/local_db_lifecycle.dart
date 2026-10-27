part of 'local_db.dart';

class LocalDbLifecycle {
  static Future<void> initDatabaseFactory() async {
    if (LocalDb._databaseFactoryInitialized) return;
    LocalDb._databaseFactory = await initLocalDatabaseFactory();
    LocalDb._databaseFactoryInitialized = true;
  }

  static Future<void> setActiveUser(String? userId) {
    return LocalDb._runSerialized(() async {
      if (LocalDb._activeUserId == userId) return;
      await LocalDb._closeCurrentDb();
      LocalDb._activeUserId = userId;
      if (userId != null) {
        LocalDb._db = await _initDbForUser(userId);
        debugPrint('✅ LocalDb switched to user namespace: $userId');
      } else {
        debugPrint('✅ LocalDb cleared active user namespace');
      }
    });
  }

  static Future<Database> get database async {
    final userId = LocalDb._activeUserId;
    if (userId == null) {
      throw StateError('LocalDb active user is not set.');
    }
    if (LocalDb._db != null) return LocalDb._db!;
    LocalDb._db = await _initDbForUser(userId);
    return LocalDb._db!;
  }

  static Future<LocalStorageSummary> getStorageSummary({
    String? expectedUserId,
  }) async {
    return LocalDb._withDatabaseOr(const LocalStorageSummary.empty(), (
      db,
    ) async {
      final sessionCount =
          Sqflite.firstIntValue(
            await db.rawQuery('SELECT COUNT(*) FROM sessions'),
          ) ??
          0;
      final messageCount =
          Sqflite.firstIntValue(
            await db.rawQuery('SELECT COUNT(*) FROM messages'),
          ) ??
          0;
      final userCount =
          Sqflite.firstIntValue(
            await db.rawQuery('SELECT COUNT(*) FROM users'),
          ) ??
          0;
      return LocalStorageSummary(
        sessionCount: sessionCount,
        messageCount: messageCount,
        userCount: userCount,
      );
    }, expectedUserId: expectedUserId);
  }

  static Future<void> clearActiveUserData() async {
    await LocalDb._withDatabase<void>((db) async {
      await _rebuildSchema(db);
    });
  }

  static Future<Database> _initDbForUser(String userId) async {
    await initDatabaseFactory();
    final factory = LocalDb.databaseFactory;
    final dbPath = await factory.getDatabasesPath();
    final path = join(dbPath, 'grix_user_$userId.db');

    return factory.openDatabase(
      path,
      options: OpenDatabaseOptions(
        version: 22,
        onCreate: (db, version) async {
          await _createSchema(db);
          await _createIndexes(db);
        },
        onUpgrade: (db, oldVersion, newVersion) async {
          await _upgradeSchema(db, oldVersion, newVersion);
        },
        onOpen: (db) async {
          if (LocalDb.useTestFastPragmas) {
            // 测试态关闭 fsync：避免慢盘（WSL2 等）每事务落盘拖垮 DB 密集型用例。
            await db.execute('PRAGMA synchronous = OFF');
            await db.rawQuery('PRAGMA journal_mode = MEMORY');
          } else if (!kIsWeb) {
            // web（sqflite_common_ffi_web）跳过：其 VFS 没有实现 WAL 依赖的共享内存（xShm*），切不过去。
            await _enableWriteAheadLog(db);
          }
          await _ensureMarkdownRenderCacheSchema(db);
        },
      ),
    );
  }

  /// 默认 DELETE 日志模式下每个写事务都要新建再删除 `-journal` 并多次 fsync，
  /// 同步频繁时造成大量物理写。WAL 下提交只追加 `-wal`，配合 synchronous=NORMAL
  /// 只在 checkpoint 时 fsync；代价是断电可能回滚最近几次提交，本地库可从服务端重新同步。
  static Future<void> _enableWriteAheadLog(Database db) async {
    // journal_mode 有返回行，Android 上 execute 会报错，必须用 rawQuery。
    final rows = await db.rawQuery('PRAGMA journal_mode = WAL');
    final mode = rows.isEmpty ? null : rows.first.values.first;
    // NORMAL 只在 WAL 下能保证断电不损坏库；没切成功就保留平台默认的 synchronous。
    if (mode?.toString().toLowerCase() == 'wal') {
      await db.execute('PRAGMA synchronous = NORMAL');
    }
  }

  static Future<void> _upgradeSchema(
    Database db,
    int oldVersion,
    int newVersion,
  ) async {
    if (oldVersion >= newVersion) {
      await _ensureMarkdownRenderCacheSchema(db);
      return;
    }

    if (oldVersion < 9) {
      await _rebuildSchema(db);
      return;
    }

    if (oldVersion < 10) {
      await _upgradeToV10(db);
    }
    if (oldVersion < 11) {
      await _upgradeToV11(db);
    }
    if (oldVersion < 12) {
      await _upgradeToV12(db);
    }
    if (oldVersion < 13) {
      await _upgradeToV13(db);
    }
    if (oldVersion < 14) {
      await _upgradeToV14(db);
    }
    if (oldVersion < 15) {
      await _upgradeToV15(db);
    }
    if (oldVersion < 16) {
      await _upgradeToV16(db);
    }
    if (oldVersion < 17) {
      await _upgradeToV17(db);
    }
    if (oldVersion < 18) {
      await _upgradeToV18(db);
    }
    if (oldVersion < 19) {
      await _upgradeToV19(db);
    }
    if (oldVersion < 20) {
      await _upgradeToV20(db);
    }
    if (oldVersion < 21) {
      await _upgradeToV21(db);
    }
    if (oldVersion < 22) {
      await _upgradeToV22(db);
    }

    await _ensureMarkdownRenderCacheSchema(db);
  }

  static Future<void> _upgradeToV10(Database db) async {
    await db.transaction((txn) async {
      await _ensureTableColumn(
        txn,
        tableName: 'sessions',
        columnName: 'is_pinned',
        columnDefinition: 'INTEGER NOT NULL DEFAULT 0',
      );
      await _ensureTableColumn(
        txn,
        tableName: 'sessions',
        columnName: 'pinned_at',
        columnDefinition: 'INTEGER NOT NULL DEFAULT 0',
      );
      await _ensureMarkdownRenderCacheSchema(txn);
      await _createIndexes(txn);
    });
  }

  static Future<void> _upgradeToV11(Database db) async {
    await db.transaction((txn) async {
      final hasLegacyColumn = await _hasTableColumn(
        txn,
        tableName: 'messages',
        columnName: 'reply_to_msg_id',
      );
      final hasQuotedColumn = await _hasTableColumn(
        txn,
        tableName: 'messages',
        columnName: 'quoted_message_id',
      );

      if (hasLegacyColumn && !hasQuotedColumn) {
        await txn.execute(
          'ALTER TABLE messages RENAME COLUMN reply_to_msg_id TO quoted_message_id',
        );
      }

      await _createIndexes(txn);
    });
  }

  static Future<void> _upgradeToV12(Database db) async {
    await db.transaction((txn) async {
      await _ensureTableColumn(
        txn,
        tableName: 'sessions',
        columnName: 'is_muted',
        columnDefinition: 'INTEGER NOT NULL DEFAULT 0',
      );
      await _createIndexes(txn);
    });
  }

  static Future<void> _upgradeToV13(Database db) async {
    await db.transaction((txn) async {
      await _ensureTableColumn(
        txn,
        tableName: 'sessions',
        columnName: 'last_message',
        columnDefinition: 'TEXT',
      );
      await _ensureTableColumn(
        txn,
        tableName: 'sessions',
        columnName: 'last_message_time',
        columnDefinition: 'INTEGER',
      );
      await _createIndexes(txn);
    });
  }

  static Future<void> _upgradeToV14(Database db) async {
    await db.transaction((txn) async {
      await _ensureTableColumn(
        txn,
        tableName: 'messages',
        columnName: 'visible_to',
        columnDefinition: 'TEXT',
      );
      await _createIndexes(txn);
    });
  }

  static Future<void> _upgradeToV15(Database db) async {
    await db.transaction((txn) async {
      await _ensureTableColumn(
        txn,
        tableName: 'sessions',
        columnName: 'friend_is_pinned',
        columnDefinition: 'INTEGER NOT NULL DEFAULT 0',
      );
      await _ensureTableColumn(
        txn,
        tableName: 'sessions',
        columnName: 'friend_pinned_at',
        columnDefinition: 'INTEGER NOT NULL DEFAULT 0',
      );
      await _createIndexes(txn);
    });
  }

  static Future<void> _upgradeToV16(Database db) async {
    await db.transaction((txn) async {
      await _ensureTableColumn(
        txn,
        tableName: 'sessions',
        columnName: 'group_avatar_members',
        columnDefinition: 'TEXT',
      );
      await _createIndexes(txn);
    });
  }

  static Future<void> _upgradeToV17(Database db) async {
    await db.transaction((txn) async {
      await _ensureTableColumn(
        txn,
        tableName: 'sessions',
        columnName: 'friend_is_muted',
        columnDefinition: 'INTEGER NOT NULL DEFAULT 0',
      );
      await _createIndexes(txn);
    });
  }

  static Future<void> _upgradeToV18(Database db) async {
    await db.transaction((txn) async {
      await _ensureTableColumn(
        txn,
        tableName: 'sessions',
        columnName: 'peer_avatar_url',
        columnDefinition: 'TEXT',
      );
      await _createIndexes(txn);
    });
  }

  static Future<void> _upgradeToV19(Database db) async {
    await db.transaction((txn) async {
      await _createSyncSchema(txn);
      await _createIndexes(txn);
    });
  }

  static Future<void> _upgradeToV20(Database db) async {
    await db.transaction((txn) async {
      await _ensureTableColumn(
        txn,
        tableName: 'sync_state',
        columnName: 'bootstrap_page_cursor',
        columnDefinition: "TEXT NOT NULL DEFAULT ''",
      );
      await _ensureTableColumn(
        txn,
        tableName: 'sync_state',
        columnName: 'bootstrap_head_cursor',
        columnDefinition: 'INTEGER NOT NULL DEFAULT 0',
      );
      await _ensureTableColumn(
        txn,
        tableName: 'sync_state',
        columnName: 'bootstrap_snapshot_cursor',
        columnDefinition: 'INTEGER NOT NULL DEFAULT 0',
      );
      await _ensureTableColumn(
        txn,
        tableName: 'sync_state',
        columnName: 'bootstrap_in_progress',
        columnDefinition: 'INTEGER NOT NULL DEFAULT 0',
      );
      await _createSyncBootstrapSchema(txn);
    });
  }

  static Future<void> _upgradeToV21(Database db) async {
    await db.transaction((txn) async {
      // 旧库没有访客标记：默认 0（普通会话）。这个默认值本身不是权威值，
      // 必须在升级后的首次启动用服务端快照纠正回来。
      await _ensureTableColumn(
        txn,
        tableName: 'sessions',
        columnName: 'is_visitor',
        columnDefinition: 'INTEGER NOT NULL DEFAULT 0',
      );
      // sync v2 的老用户只有 bootstrap_cursor<=0 时才会做全量 bootstrap，
      // 否则只发 sync_resume 收增量事件，而增量投影里没有 is_visitor；摘要
      // 接口默认关闭。不归零游标的话，旧访客会话会永久显示成普通会话。
      // 同时归零 v1 的 committed_cursor，让 v1 用户退回窗口全量快照。
      await _resetVisitorProjectionSyncCursors(txn);
      await _createIndexes(txn);
    });
  }

  static Future<void> _upgradeToV22(Database db) async {
    await db.transaction((txn) async {
      // 访客会话按接入站点分组：旧库没有站点归属，只能先留空，等下一次
      // 服务端快照/摘要/增量事件补齐。
      await _ensureTableColumn(
        txn,
        tableName: 'sessions',
        columnName: 'visitor_site_id',
        columnDefinition: 'TEXT',
      );
      await _ensureTableColumn(
        txn,
        tableName: 'sessions',
        columnName: 'visitor_site_domain',
        columnDefinition: 'TEXT',
      );
      // 与 v21 同一原因：v2 老用户只收增量事件，不会重放未变化会话的
      // 站点归属；v1 的窗口快照也只覆盖最近一页。归零游标让升级后的首次
      // 启动重新拉一次权威投影，否则旧访客会话永远落进无站点的兜底组。
      await _resetVisitorProjectionSyncCursors(txn);
      await _createIndexes(txn);
    });
  }

  static Future<void> _resetVisitorProjectionSyncCursors(
    DatabaseExecutor txn,
  ) async {
    await txn.update('sync_state', {
      'committed_cursor': 0,
      'bootstrap_cursor': 0,
      'bootstrap_page_cursor': '',
      'bootstrap_head_cursor': 0,
      'bootstrap_snapshot_cursor': 0,
      'bootstrap_in_progress': 0,
    });
    if (await _hasTable(txn, 'sync_bootstrap_sessions')) {
      await txn.delete('sync_bootstrap_sessions');
    }
  }

  static Future<void> _ensureTableColumn(
    DatabaseExecutor db, {
    required String tableName,
    required String columnName,
    required String columnDefinition,
  }) async {
    if (await _hasTableColumn(
      db,
      tableName: tableName,
      columnName: columnName,
    )) {
      return;
    }
    await db.execute(
      'ALTER TABLE $tableName ADD COLUMN $columnName $columnDefinition',
    );
  }

  static Future<bool> _hasTableColumn(
    DatabaseExecutor db, {
    required String tableName,
    required String columnName,
  }) async {
    final rows = await db.rawQuery('PRAGMA table_info($tableName)');
    for (final row in rows) {
      final name = row['name']?.toString().trim() ?? '';
      if (name == columnName) {
        return true;
      }
    }
    return false;
  }

  static Future<void> _rebuildSchema(Database db) async {
    await db.transaction((txn) async {
      await txn.execute('DROP TABLE IF EXISTS users');
      await txn.execute('DROP TABLE IF EXISTS messages');
      await txn.execute('DROP TABLE IF EXISTS sessions');
      await txn.execute('DROP TABLE IF EXISTS sync_state');
      await txn.execute('DROP TABLE IF EXISTS sync_entity_versions');
      await txn.execute('DROP TABLE IF EXISTS outbox');
      await txn.execute('DROP TABLE IF EXISTS account_counters');
      await txn.execute('DROP TABLE IF EXISTS sync_writer_lease');
      await txn.execute('DROP TABLE IF EXISTS sync_bootstrap_sessions');
      await txn.execute(
        'DROP TABLE IF EXISTS ${LocalDb._markdownRenderCacheTable}',
      );
      await _createSchema(txn);
      await _createIndexes(txn);
    });
  }

  static Future<void> _createSchema(DatabaseExecutor db) async {
    await db.execute('''
      CREATE TABLE users (
        user_id VARCHAR(64) PRIMARY KEY,
        username VARCHAR(255),
        avatar_url VARCHAR(255)
      )
    ''');

    await db.execute('''
      CREATE TABLE sessions (
        session_id VARCHAR(64) PRIMARY KEY,
        title VARCHAR(255),
        type VARCHAR(32),
        peer_id VARCHAR(64),
        peer_type INTEGER,
        peer_nickname VARCHAR(255),
        peer_username VARCHAR(255),
        updated_at INTEGER,
        is_pinned INTEGER NOT NULL DEFAULT 0,
        is_muted INTEGER NOT NULL DEFAULT 0,
        pinned_at INTEGER NOT NULL DEFAULT 0,
        friend_is_pinned INTEGER NOT NULL DEFAULT 0,
        friend_pinned_at INTEGER NOT NULL DEFAULT 0,
        friend_is_muted INTEGER NOT NULL DEFAULT 0,
        is_visitor INTEGER NOT NULL DEFAULT 0,
        visitor_site_id TEXT,
        visitor_site_domain TEXT,
        unread_count INTEGER,
        last_message TEXT,
        last_message_time INTEGER,
        group_avatar_members TEXT,
        peer_avatar_url TEXT
      )
    ''');

    await db.execute('''
      CREATE TABLE messages (
        msg_id VARCHAR(64) PRIMARY KEY,
        session_id VARCHAR(64),
        sender_id VARCHAR(64),
        sender_type INTEGER DEFAULT 1,
        msg_type INTEGER,
        content TEXT,
        extra TEXT,
        quoted_message_id VARCHAR(64),
        status VARCHAR(32),
        agent_delivery_status VARCHAR(32),
        local_seq VARCHAR(36),
        inbox_seq INTEGER,
        created_at INTEGER,
        visible_to TEXT
      )
    ''');

    await db.execute('''
      CREATE TABLE ${LocalDb._markdownRenderCacheTable} (
        cache_key CHAR(64) PRIMARY KEY,
        normalized_text TEXT NOT NULL,
        payload TEXT NOT NULL,
        updated_at INTEGER NOT NULL
      )
    ''');

    await _createSyncSchema(db);
  }

  static Future<void> _createSyncSchema(DatabaseExecutor db) async {
    await db.execute('''
      CREATE TABLE IF NOT EXISTS sync_state (
        user_id TEXT NOT NULL DEFAULT '',
        stream_name TEXT PRIMARY KEY,
        committed_cursor INTEGER NOT NULL DEFAULT 0,
        server_head_cursor INTEGER NOT NULL DEFAULT 0,
        generation TEXT NOT NULL DEFAULT '',
        bootstrap_cursor INTEGER NOT NULL DEFAULT 0,
        bootstrap_page_cursor TEXT NOT NULL DEFAULT '',
        bootstrap_head_cursor INTEGER NOT NULL DEFAULT 0,
        bootstrap_snapshot_cursor INTEGER NOT NULL DEFAULT 0,
        bootstrap_in_progress INTEGER NOT NULL DEFAULT 0,
        updated_at INTEGER NOT NULL DEFAULT 0
      )
    ''');
    await _createSyncBootstrapSchema(db);
    await db.execute('''
      CREATE TABLE IF NOT EXISTS sync_entity_versions (
        entity_type TEXT NOT NULL,
        entity_id TEXT NOT NULL,
        state_version INTEGER NOT NULL DEFAULT 0,
        tombstone INTEGER NOT NULL DEFAULT 0,
        last_event_cursor INTEGER NOT NULL DEFAULT 0,
        PRIMARY KEY (entity_type, entity_id)
      )
    ''');
    await db.execute('''
      CREATE TABLE IF NOT EXISTS outbox (
        command_id TEXT PRIMARY KEY,
        command_kind TEXT NOT NULL,
        payload TEXT NOT NULL,
        state TEXT NOT NULL DEFAULT 'pending',
        attempt_count INTEGER NOT NULL DEFAULT 0,
        next_attempt_at INTEGER NOT NULL DEFAULT 0,
        created_at INTEGER NOT NULL,
        updated_at INTEGER NOT NULL
      )
    ''');
    await db.execute('''
      CREATE TABLE IF NOT EXISTS account_counters (
        counter_id INTEGER PRIMARY KEY CHECK (counter_id = 1),
        total_unread INTEGER NOT NULL DEFAULT 0,
        notification_unread INTEGER NOT NULL DEFAULT 0,
        muted_unread INTEGER NOT NULL DEFAULT 0,
        mention_unread INTEGER NOT NULL DEFAULT 0,
        updated_at INTEGER NOT NULL DEFAULT 0
      )
    ''');
    await db.execute('''
      INSERT OR IGNORE INTO account_counters (
        counter_id, total_unread, notification_unread,
        muted_unread, mention_unread, updated_at
      ) VALUES (1, 0, 0, 0, 0, 0)
    ''');
    await db.execute('''
      CREATE TABLE IF NOT EXISTS sync_writer_lease (
        account_id TEXT PRIMARY KEY,
        owner_id TEXT NOT NULL,
        expires_at INTEGER NOT NULL,
        updated_at INTEGER NOT NULL
      )
    ''');
  }

  static Future<void> _createSyncBootstrapSchema(DatabaseExecutor db) async {
    await db.execute('''
      CREATE TABLE IF NOT EXISTS sync_bootstrap_sessions (
        stream_name TEXT NOT NULL,
        session_id TEXT NOT NULL,
        PRIMARY KEY (stream_name, session_id)
      )
    ''');
  }

  static Future<void> _createIndexes(DatabaseExecutor db) async {
    await db.execute('''
      CREATE INDEX IF NOT EXISTS idx_msg_local_seq
      ON messages(local_seq)
      WHERE local_seq IS NOT NULL
    ''');
    await db.execute('''
      CREATE INDEX IF NOT EXISTS idx_msg_session_created_msg
      ON messages(session_id, created_at DESC, msg_id DESC)
    ''');
    await db.execute('''
      CREATE INDEX IF NOT EXISTS idx_msg_session_inbox_seq
      ON messages(session_id, inbox_seq)
    ''');
    await db.execute('''
      CREATE INDEX IF NOT EXISTS idx_msg_pending_outbound
      ON messages(status, created_at)
      WHERE local_seq IS NOT NULL
    ''');
    await db.execute('''
      CREATE INDEX IF NOT EXISTS idx_sessions_updated_at
      ON sessions(updated_at)
    ''');
    await db.execute('''
      CREATE INDEX IF NOT EXISTS idx_sessions_pin_order
      ON sessions(is_pinned, pinned_at, updated_at)
    ''');
    await db.execute('''
      CREATE INDEX IF NOT EXISTS idx_markdown_render_cache_updated_at
      ON ${LocalDb._markdownRenderCacheTable}(updated_at)
    ''');
    // _createIndexes is also called by every historical upgrade step. Guard
    // v19-only tables so v9..v18 databases reach _upgradeToV19 before these
    // indexes are created.
    if (await _hasTable(db, 'outbox')) {
      await db.execute('''
        CREATE INDEX IF NOT EXISTS idx_outbox_pending
        ON outbox(state, next_attempt_at, created_at)
      ''');
    }
    if (await _hasTable(db, 'sync_entity_versions')) {
      await db.execute('''
        CREATE INDEX IF NOT EXISTS idx_sync_entity_cursor
        ON sync_entity_versions(last_event_cursor)
      ''');
    }
  }

  static Future<bool> _hasTable(DatabaseExecutor db, String tableName) async {
    final rows = await db.rawQuery(
      "SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ? LIMIT 1",
      [tableName],
    );
    return rows.isNotEmpty;
  }

  static Future<void> _ensureMarkdownRenderCacheSchema(
    DatabaseExecutor db,
  ) async {
    if (LocalDb._markdownRenderCacheSchemaEnsured) {
      return;
    }
    await db.execute('''
      CREATE TABLE IF NOT EXISTS ${LocalDb._markdownRenderCacheTable} (
        cache_key CHAR(64) PRIMARY KEY,
        normalized_text TEXT NOT NULL,
        payload TEXT NOT NULL,
        updated_at INTEGER NOT NULL
      )
    ''');
    await db.execute('''
      CREATE INDEX IF NOT EXISTS idx_markdown_render_cache_updated_at
      ON ${LocalDb._markdownRenderCacheTable}(updated_at)
    ''');
    LocalDb._markdownRenderCacheSchemaEnsured = true;
  }

  static const _messageColumns = {
    'msg_id',
    'session_id',
    'sender_id',
    'sender_type',
    'msg_type',
    'content',
    'extra',
    'quoted_message_id',
    'status',
    'agent_delivery_status',
    'local_seq',
    'inbox_seq',
    'created_at',
    'visible_to',
  };
  static const _messageIntegerColumns = {
    'sender_type',
    'msg_type',
    'inbox_seq',
    'created_at',
  };
  static const _sessionColumns = {
    'session_id',
    'title',
    'type',
    'peer_id',
    'peer_type',
    'peer_nickname',
    'peer_username',
    'updated_at',
    'is_pinned',
    'is_muted',
    'pinned_at',
    'friend_is_pinned',
    'friend_pinned_at',
    'friend_is_muted',
    'is_visitor',
    'visitor_site_id',
    'visitor_site_domain',
    'unread_count',
    'last_message',
    'last_message_time',
    'group_avatar_members',
    'peer_avatar_url',
  };
  static const _sessionIntegerColumns = {
    'peer_type',
    'updated_at',
    'is_pinned',
    'is_muted',
    'pinned_at',
    'friend_is_pinned',
    'friend_pinned_at',
    'friend_is_muted',
    'is_visitor',
    'unread_count',
    'last_message_time',
  };

  static Map<String, dynamic> _filterMessageColumns(Map<String, dynamic> msg) {
    final filtered = <String, dynamic>{};
    for (var key in msg.keys) {
      if (_messageColumns.contains(key)) {
        var value = msg[key];
        if (value is bool) value = value ? 1 : 0;
        if (key == 'extra' && (value is Map || value is List)) {
          value = jsonEncode(value);
        }
        if (key == 'visible_to' && value is List) {
          value = jsonEncode(value);
        }
        if (_messageIntegerColumns.contains(key)) {
          if (value == null) {
            filtered[key] = null;
            continue;
          }
          value = _requireInt(value, fieldName: 'messages.$key');
          if (key == 'created_at') {
            value = _normalizeCreatedAt(value);
          }
        }
        filtered[key] = value;
      }
    }
    return filtered;
  }

  static Map<String, dynamic> _filterSessionColumns(
    Map<String, dynamic> session,
  ) {
    final filtered = <String, dynamic>{};
    for (final key in session.keys) {
      if (!_sessionColumns.contains(key)) continue;
      var value = session[key];
      if (value is bool) value = value ? 1 : 0;
      if (key == 'group_avatar_members' && value is List) {
        value = jsonEncode(value);
      }
      if (_sessionIntegerColumns.contains(key)) {
        if (value == null) {
          filtered[key] = null;
          continue;
        }
        value = _requireInt(value, fieldName: 'sessions.$key');
        if (key == 'updated_at' ||
            key == 'pinned_at' ||
            key == 'last_message_time') {
          value = _normalizeCreatedAt(value);
        }
      }
      filtered[key] = value;
    }
    return filtered;
  }

  static int _requireInt(dynamic value, {required String fieldName}) {
    return StrictIntParser.parse(value, fieldName: fieldName);
  }

  static int _normalizeCreatedAt(int value) {
    if (value > 0 && value < 10000000000) {
      return value * 1000;
    }
    return value;
  }
}

class LocalStorageSummary {
  const LocalStorageSummary({
    required this.sessionCount,
    required this.messageCount,
    required this.userCount,
  });

  const LocalStorageSummary.empty()
    : sessionCount = 0,
      messageCount = 0,
      userCount = 0;

  final int sessionCount;
  final int messageCount;
  final int userCount;
}
