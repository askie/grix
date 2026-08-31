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

  Future<void> pumpUnderLooseWidth(
    WidgetTester tester,
    String url, {
    double maxWidth = 300,
  }) {
    return tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Align(
            alignment: Alignment.topLeft,
            child: ConstrainedBox(
              constraints: BoxConstraints(maxWidth: maxWidth),
              child: ChatMarkdownImageView(src: url),
            ),
          ),
        ),
      ),
    );
  }

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

  testWidgets('keeps a small image at its intrinsic size', (tester) async {
    const url = 'https://example.com/small.png';
    ChatImageDimensionCache.store(url, const Size(100, 50));

    await pumpUnderLooseWidth(tester, url);
    await tester.pump();

    expect(
      tester.getSize(find.byType(ChatMarkdownImageView)),
      const Size(100, 50),
    );
  });

  testWidgets('clamps a tall image to the max height', (tester) async {
    const url = 'https://example.com/tall.png';
    ChatImageDimensionCache.store(url, const Size(400, 1600));

    await pumpUnderLooseWidth(tester, url);
    await tester.pump();

    expect(
      tester.getSize(find.byType(ChatMarkdownImageView)),
      const Size(70, 280),
    );
  });

  testWidgets('lays out inside an intrinsic-width table cell', (tester) async {
    const url = 'https://example.com/in-table.png';
    ChatImageDimensionCache.store(url, const Size(600, 300));

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Align(
            alignment: Alignment.topLeft,
            child: SizedBox(
              width: 400,
              child: Table(
                defaultVerticalAlignment: TableCellVerticalAlignment.middle,
                columnWidths: const <int, TableColumnWidth>{
                  0: IntrinsicColumnWidth(),
                },
                children: const [
                  TableRow(children: [ChatMarkdownImageView(src: url)]),
                ],
              ),
            ),
          ),
        ),
      ),
    );
    await tester.pump();

    expect(tester.takeException(), isNull);
    final size = tester.getSize(find.byType(ChatMarkdownImageView));
    expect(size.width, greaterThan(0));
    expect(size.height, greaterThan(0));
  });
}
