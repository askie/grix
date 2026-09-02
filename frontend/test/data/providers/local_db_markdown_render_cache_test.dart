import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/local_db.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() async {
    await LocalDb.setActiveUser('markdown_cache_test_user');
    await LocalDb.clearActiveUserData();
  });

  tearDown(() async {
    await LocalDb.setActiveUser(null);
  });

  test('markdown render cache keeps bounded size', () async {
    await LocalDb.upsertMarkdownRenderCaches(const <MarkdownRenderCacheRecord>[
      MarkdownRenderCacheRecord(
        cacheKey:
            'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
        normalizedText: 'one',
        payload: '{"k":"one"}',
      ),
    ], maxEntries: 2);
    await Future<void>.delayed(const Duration(milliseconds: 2));

    await LocalDb.upsertMarkdownRenderCaches(const <MarkdownRenderCacheRecord>[
      MarkdownRenderCacheRecord(
        cacheKey:
            'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
        normalizedText: 'two',
        payload: '{"k":"two"}',
      ),
    ], maxEntries: 2);
    await Future<void>.delayed(const Duration(milliseconds: 2));

    await LocalDb.upsertMarkdownRenderCaches(const <MarkdownRenderCacheRecord>[
      MarkdownRenderCacheRecord(
        cacheKey:
            'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
        normalizedText: 'three',
        payload: '{"k":"three"}',
      ),
    ], maxEntries: 2);

    final rows = await LocalDb.getMarkdownRenderCachesByKeys(const <String>[
      'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
      'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
      'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
    ]);
    final keys = rows.map((row) => row.cacheKey).toSet();

    expect(rows.length, 2);
    expect(
      keys.contains(
        'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
      ),
      isFalse,
    );
    expect(
      keys.contains(
        'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
      ),
      isTrue,
    );
    expect(
      keys.contains(
        'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
      ),
      isTrue,
    );
  });
}
