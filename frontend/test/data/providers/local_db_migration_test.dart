import 'package:flutter_test/flutter_test.dart';
import 'package:path/path.dart' as p;
import 'package:sqflite/sqflite.dart';

import 'package:grix/data/providers/local_db.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test(
    'v9 upgrade preserves messages and adds current session columns',
    () async {
      final userId =
          'local_db_upgrade_${DateTime.now().millisecondsSinceEpoch}';
      await LocalDb.initDatabaseFactory();
      final dbFactory = LocalDb.databaseFactory;
      final databasesPath = await dbFactory.getDatabasesPath();
      final dbPath = p.join(databasesPath, 'grix_user_$userId.db');

      await dbFactory.deleteDatabase(dbPath);

      final legacyDb = await dbFactory.openDatabase(
        dbPath,
        options: OpenDatabaseOptions(
          version: 9,
          onCreate: (db, version) async {
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
            unread_count INTEGER
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
            reply_to_msg_id VARCHAR(64),
            status VARCHAR(32),
            agent_delivery_status VARCHAR(32),
            local_seq VARCHAR(36),
            inbox_seq INTEGER,
            created_at INTEGER
          )
        ''');
          },
        ),
      );

      await legacyDb.insert('sessions', {
        'session_id': 's-upgrade-1',
        'title': 'upgrade chat',
        'type': 'group',
        'peer_id': '',
        'peer_type': 0,
        'peer_nickname': '',
        'peer_username': '',
        'updated_at': 1700000000000,
        'unread_count': 2,
      });
      await legacyDb.insert('messages', {
        'msg_id': 'm-upgrade-1',
        'session_id': 's-upgrade-1',
        'sender_id': '1002',
        'sender_type': 1,
        'msg_type': 1,
        'content': 'legacy message',
        'reply_to_msg_id': 'm-upgrade-origin',
        'status': 'sent',
        'inbox_seq': 7,
        'created_at': 1700000000000,
      });
      await legacyDb.close();

      await LocalDb.setActiveUser(userId);
      try {
        final sessions = await LocalDb.getSessions();
        final messages = await LocalDb.getLatestMessages('s-upgrade-1');
        final upgradedDb = await LocalDb.database;
        final sessionColumns = await upgradedDb.rawQuery(
          'PRAGMA table_info(sessions)',
        );
        final messageColumns = await upgradedDb.rawQuery(
          'PRAGMA table_info(messages)',
        );
        final syncStateColumns = await upgradedDb.rawQuery(
          'PRAGMA table_info(sync_state)',
        );
        final schemaVersion = Sqflite.firstIntValue(
          await upgradedDb.rawQuery('PRAGMA user_version'),
        );
        final tableRows = await upgradedDb.rawQuery(
          "SELECT name FROM sqlite_master WHERE type = 'table'",
        );
        final tableNames = tableRows
            .map((row) => row['name']?.toString())
            .toSet();

        expect(sessions, hasLength(1));
        expect(messages, hasLength(1));
        expect(sessions.first['session_id'], 's-upgrade-1');
        expect(sessions.first['title'], 'upgrade chat');
        expect(sessions.first['is_pinned'], 0);
        expect(sessions.first['is_muted'], 0);
        expect(sessions.first['pinned_at'], 0);
        // v21 新增列：旧库升级后默认按普通会话处理，等下一次服务端快照纠正。
        expect(sessions.first['is_visitor'], 0);
        // v22 新增的站点归属列：旧库升级后为空，归入无站点兜底组。
        expect(sessions.first['visitor_site_id'], isNull);
        expect(sessions.first['visitor_site_domain'], isNull);
        expect(messages.first['msg_id'], 'm-upgrade-1');
        expect(messages.first['content'], 'legacy message');
        expect(messages.first['quoted_message_id'], 'm-upgrade-origin');
        expect(schemaVersion, 22);
        expect(
          tableNames,
          containsAll(<String>{
            'sync_state',
            'sync_entity_versions',
            'outbox',
            'account_counters',
            'sync_writer_lease',
            'sync_bootstrap_sessions',
          }),
        );
        expect(
          sessionColumns.any((row) => row['name']?.toString() == 'is_pinned'),
          isTrue,
        );
        expect(
          sessionColumns.any((row) => row['name']?.toString() == 'pinned_at'),
          isTrue,
        );
        expect(
          sessionColumns.any((row) => row['name']?.toString() == 'is_muted'),
          isTrue,
        );
        expect(
          sessionColumns.any((row) => row['name']?.toString() == 'is_visitor'),
          isTrue,
        );
        expect(
          sessionColumns.any(
            (row) => row['name']?.toString() == 'visitor_site_id',
          ),
          isTrue,
        );
        expect(
          sessionColumns.any(
            (row) => row['name']?.toString() == 'visitor_site_domain',
          ),
          isTrue,
        );
        expect(
          sessionColumns.any(
            (row) => row['name']?.toString() == 'last_message',
          ),
          isTrue,
        );
        expect(
          sessionColumns.any(
            (row) => row['name']?.toString() == 'last_message_time',
          ),
          isTrue,
        );
        expect(
          sessionColumns.any(
            (row) => row['name']?.toString() == 'group_avatar_members',
          ),
          isTrue,
        );
        expect(
          sessionColumns.any(
            (row) => row['name']?.toString() == 'peer_avatar_url',
          ),
          isTrue,
        );
        expect(
          messageColumns.any(
            (row) => row['name']?.toString() == 'quoted_message_id',
          ),
          isTrue,
        );
        expect(
          messageColumns.any(
            (row) => row['name']?.toString() == 'reply_to_msg_id',
          ),
          isFalse,
        );
        expect(
          syncStateColumns.map((row) => row['name']?.toString()).toSet(),
          containsAll(<String>{
            'bootstrap_page_cursor',
            'bootstrap_head_cursor',
            'bootstrap_snapshot_cursor',
            'bootstrap_in_progress',
          }),
        );
      } finally {
        await LocalDb.setActiveUser(null);
        await dbFactory.deleteDatabase(dbPath);
      }
    },
  );

  test(
    'v21 upgrade adds visitor site columns and re-bootstraps the projection',
    () async {
      final userId =
          'local_db_v21_upgrade_${DateTime.now().millisecondsSinceEpoch}';
      await LocalDb.initDatabaseFactory();
      final dbFactory = LocalDb.databaseFactory;
      final databasesPath = await dbFactory.getDatabasesPath();
      final dbPath = p.join(databasesPath, 'grix_user_$userId.db');
      await dbFactory.deleteDatabase(dbPath);

      final legacyDb = await dbFactory.openDatabase(
        dbPath,
        options: OpenDatabaseOptions(
          version: 21,
          onCreate: (db, version) async {
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
            local_seq VARCHAR(36),
            status VARCHAR(32),
            inbox_seq INTEGER,
            created_at INTEGER
          )
        ''');
            await db.execute('''
          CREATE TABLE markdown_render_cache (
            cache_key CHAR(64) PRIMARY KEY,
            normalized_text TEXT NOT NULL,
            payload TEXT NOT NULL,
            updated_at INTEGER NOT NULL
          )
        ''');
            await db.execute('''
          CREATE TABLE sync_state (
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
          },
        ),
      );

      await legacyDb.insert('sessions', {
        'session_id': 'legacy-visitor',
        'title': '访客',
        'type': 'private',
        'peer_id': '',
        'peer_type': 0,
        'updated_at': 1790000000000,
        'is_visitor': 1,
        'unread_count': 0,
        'last_message': 'hi',
        'last_message_time': 1790000000000,
      });
      await legacyDb.insert('sync_state', {
        'stream_name': 'chat',
        'committed_cursor': 700,
        'bootstrap_cursor': 900,
        'bootstrap_page_cursor': 'session-x',
        'bootstrap_head_cursor': 950,
        'bootstrap_snapshot_cursor': 901,
        'bootstrap_in_progress': 1,
      });
      await legacyDb.close();

      await LocalDb.setActiveUser(userId);
      try {
        final db = await LocalDb.database;
        final schemaVersion = Sqflite.firstIntValue(
          await db.rawQuery('PRAGMA user_version'),
        );
        expect(schemaVersion, 22);

        final sessionColumns = await db.rawQuery('PRAGMA table_info(sessions)');
        expect(
          sessionColumns.any(
            (row) => row['name']?.toString() == 'visitor_site_id',
          ),
          isTrue,
        );
        expect(
          sessionColumns.any(
            (row) => row['name']?.toString() == 'visitor_site_domain',
          ),
          isTrue,
        );

        // 已有的访客标记保留；站点归属留空，归入无站点兜底组。
        final row = (await LocalDb.getSessionRecord('legacy-visitor'))!;
        expect(row['is_visitor'], 1);
        expect(row['visitor_site_id'], isNull);
        expect(row['visitor_site_domain'], isNull);

        // 游标归零，升级后的首次启动重新拉一次权威投影补齐站点归属。
        final state = await LocalDb.getSyncState();
        expect(state.committedCursor, 0);
        expect(state.bootstrapCursor, 0);
        expect(state.bootstrapPageCursor, isEmpty);
        expect(state.bootstrapInProgress, isFalse);
      } finally {
        await LocalDb.setActiveUser(null);
        await dbFactory.deleteDatabase(dbPath);
      }
    },
  );
}
