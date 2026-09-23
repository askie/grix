import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/utils/chat_image_dimension_cache.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SharedPreferences.setMockInitialValues(<String, Object>{});
    ChatImageDimensionCache.resetForTest();
  });

  tearDown(ChatImageDimensionCache.resetForTest);

  test('shares one record across signed URL variants of the same image',
      () async {
    await ChatImageDimensionCache.ensureLoadedForTest();
    ChatImageDimensionCache.store(
      'https://oss.example.com/a.png?Expires=100&Signature=abc',
      const Size(400, 300),
    );

    final hit = ChatImageDimensionCache.lookup(
      'https://oss.example.com/a.png?Expires=200&Signature=def',
    );
    expect(hit, const Size(400, 300));
  });

  test('persists sizes across a cold restart', () async {
    await ChatImageDimensionCache.ensureLoadedForTest();
    ChatImageDimensionCache.store(
      'https://example.com/persisted.png',
      const Size(640, 480),
    );
    await ChatImageDimensionCache.flushForTest();

    // Simulate a cold start: in-memory cache wiped, prefs retained.
    ChatImageDimensionCache.resetForTest();
    await ChatImageDimensionCache.ensureLoadedForTest();

    final hit = ChatImageDimensionCache.lookup(
      'https://example.com/persisted.png',
    );
    expect(hit, const Size(640, 480));
  });

  test('ignores corrupt persisted payloads', () async {
    SharedPreferences.setMockInitialValues(<String, Object>{
      'chat_image_dims_v1': 'not-json',
    });
    ChatImageDimensionCache.resetForTest();
    await ChatImageDimensionCache.ensureLoadedForTest();

    expect(
      ChatImageDimensionCache.lookup('https://example.com/whatever.png'),
      isNull,
    );
    // Storing after a corrupt load still works.
    ChatImageDimensionCache.store(
      'https://example.com/whatever.png',
      const Size(10, 20),
    );
    expect(
      ChatImageDimensionCache.lookup('https://example.com/whatever.png'),
      const Size(10, 20),
    );
  });
}
