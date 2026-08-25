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

        expect(sessions, hasLength(1));
        expect(messages, hasLength(1));
        expect(sessions.first['session_id'], 's-upgrade-1');
        expect(sessions.first['title'], 'upgrade chat');
        expect(sessions.first['is_pinned'], 0);
        expect(sessions.first['is_muted'], 0);
        expect(sessions.first['pinned_at'], 0);
        expect(messages.first['msg_id'], 'm-upgrade-1');
        expect(messages.first['content'], 'legacy message');
        expect(messages.first['quoted_message_id'], 'm-upgrade-origin');
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
      } finally {
        await LocalDb.setActiveUser(null);
        await dbFactory.deleteDatabase(dbPath);
      }
    },
  );
}
