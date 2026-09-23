import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:path/path.dart' as p;

import 'package:grix/data/providers/local_db.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('production open uses WAL journal with synchronous NORMAL', () async {
    final userId = 'local_db_wal_${DateTime.now().microsecondsSinceEpoch}';
    await LocalDb.initDatabaseFactory();
    final dbFactory = LocalDb.databaseFactory;
    final dbPath = p.join(
      await dbFactory.getDatabasesPath(),
      'grix_user_$userId.db',
    );
    // flutter_test_config.dart 默认打开测试快速 pragma，这里切回生产分支。
    final previousFastPragmas = LocalDb.useTestFastPragmas;
    LocalDb.useTestFastPragmas = false;
    try {
      await LocalDb.setActiveUser(userId);
      final db = await LocalDb.database;

      final journalMode = await db.rawQuery('PRAGMA journal_mode');
      expect(journalMode.single.values.single.toString().toLowerCase(), 'wal');
      final synchronous = await db.rawQuery('PRAGMA synchronous');
      expect(synchronous.single.values.single, 1); // 1 = NORMAL

      // 提交落到 -wal，而不是每次新建再删除 -journal。
      await db.insert('users', {'user_id': 'wal_probe', 'username': 'probe'});
      expect(File('$dbPath-wal').existsSync(), isTrue);
    } finally {
      await LocalDb.setActiveUser(null);
      LocalDb.useTestFastPragmas = previousFastPragmas;
      await dbFactory.deleteDatabase(dbPath);
    }
  });
}
