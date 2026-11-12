import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/shared/markdown/chat_markdown_dialect.dart';
import 'package:grix/shared/markdown/chat_markdown_normalizer.dart';
import 'package:grix/shared/markdown/chat_markdown_pipeline.dart';
import 'package:grix/shared/widgets/chat_markdown_view.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  final pipeline = ChatMarkdownPipeline(
    normalizer: const ChatMarkdownNormalizer(),
    parser: ChatMarkdownDialect.buildParserAdapter(),
  );

  Widget buildParsedView(String content) {
    final result = pipeline.prepareFinalRender(content);
    return GetMaterialApp(
      translations: AppTranslations(),
      locale: const Locale('zh', 'CN'),
      theme: AppTheme.lightTheme,
      home: Scaffold(
        body: ChatMarkdownView(
          data: result.normalizedText,
          textColor: Colors.black,
          isMine: false,
          document: result.document,
          semantics: result.semantics,
        ),
      ),
    );
  }

  final wideTable = [
    '| VeryLongHeaderOne | VeryLongHeaderTwo | VeryLongHeaderThree | VeryLongHeaderFour |',
    '| --- | --- | --- | --- |',
    for (var index = 0; index < 20; index += 1)
      '| VeryLongCellValueOne | VeryLongCellValueTwo | VeryLongCellValueThree | VeryLongCellValueFour |',
  ].join(String.fromCharCode(10));

  testWidgets('desktop wide table shows a bottom horizontal scrollbar', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(buildParsedView(wideTable));
    await tester.pumpAndSettle();

    final horizontalScrollbarFinder = find.byWidgetPredicate(
      (widget) =>
          widget is Scrollbar &&
          widget.scrollbarOrientation == ScrollbarOrientation.bottom,
    );
    expect(horizontalScrollbarFinder, findsOneWidget);
    final horizontalScrollbar = tester.widget<Scrollbar>(
      horizontalScrollbarFinder,
    );
    expect(horizontalScrollbar.thumbVisibility, isTrue);
    expect(horizontalScrollbar.controller?.hasClients, isTrue);
    expect(horizontalScrollbar.controller?.position.axis, Axis.horizontal);
    expect(
      horizontalScrollbar.controller?.position.maxScrollExtent,
      greaterThan(0),
    );

    final viewportRect = tester.getRect(
      find.byKey(const ValueKey('markdown_table_viewport')),
    );
    expect(
      tester.getRect(horizontalScrollbarFinder).bottom,
      closeTo(viewportRect.bottom, 0.01),
    );

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  }, variant: TargetPlatformVariant.desktop());

  testWidgets(
    'mobile wide table stays scrollable without a desktop scrollbar',
    (WidgetTester tester) async {
      await tester.pumpWidget(buildParsedView(wideTable));
      await tester.pumpAndSettle();

      expect(find.byType(Scrollbar), findsOneWidget);
      final horizontalScrollableFinder = find.byWidgetPredicate(
        (widget) =>
            widget is Scrollable && widget.axisDirection == AxisDirection.right,
      );
      expect(horizontalScrollableFinder, findsOneWidget);
      final horizontalScrollable = tester.state<ScrollableState>(
        horizontalScrollableFinder,
      );
      expect(horizontalScrollable.position.maxScrollExtent, greaterThan(0));
      horizontalScrollable.position.jumpTo(80);
      await tester.pump();
      expect(horizontalScrollable.position.pixels, 80);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
    },
    variant: TargetPlatformVariant.mobile(),
  );
}
