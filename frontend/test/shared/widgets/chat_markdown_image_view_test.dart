import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/utils/chat_image_dimension_cache.dart';
import 'package:grix/shared/utils/user_image_cache_manager.dart';
import 'package:grix/shared/widgets/chat_markdown_image_view.dart';

void main() {
  setUp(() {
    ChatImageDimensionCache.resetForTest();
    UserImageCacheManager.setDisabledForTest(true);
  });

  tearDown(() {
    ChatImageDimensionCache.resetForTest();
    UserImageCacheManager.setDisabledForTest(false);
  });

  group('resolveStableDisplaySize', () {
    test('returns null without cached dimensions', () {
      expect(
        ChatMarkdownImageView.resolveStableDisplaySize(
          url: 'https://example.com/a.png',
          maxWidth: 300,
          maxHeight: 280,
        ),
        isNull,
      );
    });

    test('scales down a wide image preserving aspect ratio', () {
      ChatImageDimensionCache.store(
        'https://example.com/a.png',
        const Size(2000, 1000),
      );
      expect(
        ChatMarkdownImageView.resolveStableDisplaySize(
          url: 'https://example.com/a.png',
          maxWidth: 300,
          maxHeight: 280,
        ),
        const Size(300, 150),
      );
    });

    test('keeps a small image at its intrinsic size', () {
      ChatImageDimensionCache.store(
        'https://example.com/a.png',
        const Size(100, 50),
      );
      expect(
        ChatMarkdownImageView.resolveStableDisplaySize(
          url: 'https://example.com/a.png',
          maxWidth: 300,
          maxHeight: 280,
        ),
        const Size(100, 50),
      );
    });

    test('clamps a tall image to max height', () {
      ChatImageDimensionCache.store(
        'https://example.com/a.png',
        const Size(400, 1600),
      );
      expect(
        ChatMarkdownImageView.resolveStableDisplaySize(
          url: 'https://example.com/a.png',
          maxWidth: 300,
          maxHeight: 280,
        ),
        const Size(70, 280),
      );
    });
  });

  testWidgets('reserves the final layout height before the image loads when '
      'dimensions are cached', (tester) async {
    const url = 'https://example.com/cached.png';
    ChatImageDimensionCache.store(url, const Size(600, 300));

    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: Align(
            alignment: Alignment.topLeft,
            child: SizedBox(width: 300, child: ChatMarkdownImageView(src: url)),
          ),
        ),
      ),
    );
    await tester.pump();

    final size = tester.getSize(find.byType(ChatMarkdownImageView));
    expect(size.width, 300);
    expect(size.height, 150);
  });

  testWidgets(
    'falls back to the loading placeholder height without cached dimensions',
    (tester) async {
      const url = 'https://example.com/uncached.png';

      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(
            body: Align(
              alignment: Alignment.topLeft,
              child: SizedBox(
                width: 300,
                child: ChatMarkdownImageView(src: url),
              ),
            ),
          ),
        ),
      );
      await tester.pump();

      final size = tester.getSize(find.byType(ChatMarkdownImageView));
      expect(size.height, 150);
    },
  );
}
