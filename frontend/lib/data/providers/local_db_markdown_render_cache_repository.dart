part of 'local_db.dart';

class LocalDbMarkdownRenderCacheRepository {
  static Future<List<MarkdownRenderCacheRecord>> getMarkdownRenderCachesByKeys(
    List<String> cacheKeys,
  ) async {
    final normalizedKeys = cacheKeys
        .map((key) => key.trim())
        .where((key) => key.isNotEmpty)
        .toSet()
        .toList(growable: false);
    if (normalizedKeys.isEmpty) {
      return const <MarkdownRenderCacheRecord>[];
    }

    return LocalDb._withDatabaseOr<List<MarkdownRenderCacheRecord>>(
      const <MarkdownRenderCacheRecord>[],
      (db) async {
        await LocalDbLifecycle._ensureMarkdownRenderCacheSchema(db);
        final placeholders = List.filled(normalizedKeys.length, '?').join(',');
        final rows = await db.rawQuery('''
          SELECT cache_key, normalized_text, payload
          FROM ${LocalDb._markdownRenderCacheTable}
          WHERE cache_key IN ($placeholders)
          ''', normalizedKeys);
        if (rows.isEmpty) {
          return const <MarkdownRenderCacheRecord>[];
        }

        final records = <MarkdownRenderCacheRecord>[];
        final touchedKeys = <String>[];
        for (final row in rows) {
          final cacheKey = row['cache_key']?.toString().trim() ?? '';
          final normalizedText =
              row['normalized_text']?.toString().trim() ?? '';
          final payload = row['payload']?.toString() ?? '';
          if (cacheKey.isEmpty || normalizedText.isEmpty || payload.isEmpty) {
            continue;
          }
          touchedKeys.add(cacheKey);
          records.add(
            MarkdownRenderCacheRecord(
              cacheKey: cacheKey,
              normalizedText: normalizedText,
              payload: payload,
            ),
          );
        }

        if (touchedKeys.isNotEmpty) {
          final touchPlaceholders = List.filled(
            touchedKeys.length,
            '?',
          ).join(',');
          final now = DateTime.now().millisecondsSinceEpoch;
          await db.rawUpdate(
            '''
            UPDATE ${LocalDb._markdownRenderCacheTable}
            SET updated_at = ?
            WHERE cache_key IN ($touchPlaceholders)
            ''',
            [now, ...touchedKeys],
          );
        }

        return records;
      },
    );
  }

  static Future<void> upsertMarkdownRenderCaches(
    List<MarkdownRenderCacheRecord> records, {
    int maxEntries = 1024,
  }) async {
    final sanitizedRecords = records
        .where(
          (record) =>
              record.cacheKey.trim().isNotEmpty &&
              record.normalizedText.trim().isNotEmpty &&
              record.payload.trim().isNotEmpty,
        )
        .toList(growable: false);
    if (sanitizedRecords.isEmpty) {
      return;
    }

    await LocalDb._withDatabase<void>((db) async {
      await LocalDbLifecycle._ensureMarkdownRenderCacheSchema(db);
      await db.transaction((txn) async {
        final now = DateTime.now().millisecondsSinceEpoch;
        final batch = txn.batch();
        for (final record in sanitizedRecords) {
          batch.insert(
            LocalDb._markdownRenderCacheTable,
            {
              'cache_key': record.cacheKey,
              'normalized_text': record.normalizedText,
              'payload': record.payload,
              'updated_at': now,
            },
            conflictAlgorithm: ConflictAlgorithm.replace,
          );
        }
        await batch.commit(noResult: true);

        if (maxEntries <= 0) {
          await txn.delete(LocalDb._markdownRenderCacheTable);
          return;
        }

        final countResult = await txn.rawQuery(
          'SELECT COUNT(*) as count FROM ${LocalDb._markdownRenderCacheTable}',
        );
        final total = Sqflite.firstIntValue(countResult) ?? 0;
        final overflow = total - maxEntries;
        if (overflow <= 0) {
          return;
        }

        await txn.rawDelete(
          '''
          DELETE FROM ${LocalDb._markdownRenderCacheTable}
          WHERE cache_key IN (
            SELECT cache_key
            FROM ${LocalDb._markdownRenderCacheTable}
            ORDER BY updated_at ASC
            LIMIT ?
          )
          ''',
          [overflow],
        );
      });
    });
  }
}

class MarkdownRenderCacheRecord {
  const MarkdownRenderCacheRecord({
    required this.cacheKey,
    required this.normalizedText,
    required this.payload,
  });

  final String cacheKey;
  final String normalizedText;
  final String payload;
}
