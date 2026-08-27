import 'package:flutter/material.dart';
import 'package:markdown_widget/markdown_widget.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/shared/widgets/chat_markdown_ast_view.dart';
import 'package:grix/shared/widgets/chat_markdown_code_block_view.dart';
import 'package:grix/shared/widgets/chat_markdown_image_view.dart';
import 'package:grix/shared/widgets/chat_markdown_table_view.dart';
import 'package:grix/shared/utils/user_image_cache_manager.dart';
import 'package:grix/shared/widgets/message_bubble.dart';

import 'markdown_link_finder.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUpAll(() {
    UserImageCacheManager.setDisabledForTest(true);
  });

  tearDownAll(() {
    UserImageCacheManager.setDisabledForTest(false);
  });

  late ErrorWidgetBuilder defaultErrorWidgetBuilder;

  setUp(() {
    defaultErrorWidgetBuilder = ErrorWidget.builder;
    MessageBubble.resetFinalRenderCacheForTest();
  });

  tearDown(() async {
    ErrorWidget.builder = defaultErrorWidgetBuilder;
    MessageBubble.resetFinalRenderCacheForTest();
  });

  Future<void> unlockSelection(
    WidgetTester tester,
    Finder finder,
  ) async {
    final center = tester.getCenter(finder);
    await tester.tapAt(center);
    await tester.pump(const Duration(milliseconds: 80));
    await tester.tapAt(center);
    await tester.pump();
  }

  testWidgets('plain text keeps selection locked until double tap',
      (WidgetTester tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: MessageBubble(
            msgId: 'm1',
            initialContent: 'hello plain text',
            isMine: false,
          ),
        ),
      ),
    );
    await tester.pump();

    expect(find.byType(SelectionArea), findsNothing);
    expect(find.text('hello plain text'), findsOneWidget);
    expect(find.byType(MarkdownWidget), findsNothing);

    await unlockSelection(tester, find.text('hello plain text'));

    expect(find.byType(SelectionArea), findsOneWidget);

    ErrorWidget.builder = defaultErrorWidgetBuilder;
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('collapsed table-like xcode reply stays on plain text path',
      (WidgetTester tester) async {
    const content = '''可以。让我先看看Xcode相关数据的具体情况： 找到了主要的Xcode数据：
| 目录 |大小 ||-----|-----||
CoreSimulator（模拟器） | 2.2GB||
DerivedData（编译缓存） | 1.9GB||
iOSDeviceSupport（设备支持） | 5.4GB||总计| ~9.5GB|---
两种方案： ****方案A: 直接清理（推荐）
-DerivedData可以直接删除，Xcode会自动重建，释放1.9GB
-不影响项目，只是下次编译会慢一点
方案B: 移动到外接硬盘
-把三个目录都移到/mnt/external/XcodeData/
-创建符号链接指向新位置
-风险： 外接硬盘必须一直插着，否则Xcode会出问题
''';

    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: MessageBubble(
            msgId: 'm_xcode_collapsed_table',
            initialContent: content,
            isMine: false,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsNothing);
    expect(find.byType(SelectionArea), findsNothing);

    final text = tester.widget<Text>(find.text(content));
    expect(text.style?.fontSize, 14);

    ErrorWidget.builder = defaultErrorWidgetBuilder;
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('structural markdown content uses native ast view',
      (WidgetTester tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: MessageBubble(
            msgId: 'm2',
            initialContent: '# heading\n- item',
            isMine: false,
          ),
        ),
      ),
    );
    await tester.pump();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);

    ErrorWidget.builder = defaultErrorWidgetBuilder;
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('cached markdown skips deferred preview rerender',
      (WidgetTester tester) async {
    const content = '# cached heading';
    MessageBubble.precacheFinalRenderStates(
      const <String>[content],
      maxEntries: 1,
    );

    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: MessageBubble(
            msgId: 'm_cached_defer',
            initialContent: content,
            isMine: false,
            deferMarkdownRender: true,
            markdownRenderDeferDuration: Duration(milliseconds: 400),
          ),
        ),
      ),
    );
    await tester.pump();

    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.byType(MarkdownWidget), findsNothing);
  });

  test('oversized content is excluded from render-cache warmup', () {
    final content = List<String>.filled(
      MessageBubble.maxInlineContentCharacters + 1,
      'a',
      growable: false,
    ).join();

    expect(MessageBubble.isFinalRenderPrecacheEligible(content), isFalse);

    MessageBubble.precacheFinalRenderStates(<String>[content], maxEntries: 1);

    expect(MessageBubble.hasCachedFinalRenderState(content), isFalse);
  });

  testWidgets('oversized message uses a bounded preview and lazy full viewer', (
    WidgetTester tester,
  ) async {
    final content = List<String>.filled(
      (MessageBubble.maxInlineContentCharacters ~/ 15) + 2,
      '[[Tool-output]]',
      growable: false,
    ).join();

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ListView(
            children: [
              MessageBubble(
                msgId: 'm_oversized',
                initialContent: content,
                isMine: false,
              ),
            ],
          ),
        ),
      ),
    );
    await tester.pump();

    final previewTexts = tester
        .widgetList<Text>(
          find.descendant(
            of: find.byType(MessageBubble),
            matching: find.byType(Text),
          ),
        )
        .map((text) => text.data ?? '')
        .where((text) => text.startsWith('[[Tool-output]]'))
        .toList(growable: false);
    expect(previewTexts, hasLength(1));
    expect(
      previewTexts.single.length,
      lessThanOrEqualTo(MessageBubble.longContentPreviewCharacters + 2),
    );
    expect(
      find.byKey(const ValueKey('chat_long_message_open_m_oversized')),
      findsOneWidget,
    );

    final openButton = tester.widget<TextButton>(
      find.byKey(const ValueKey('chat_long_message_open_m_oversized')),
    );
    openButton.onPressed!();
    await tester.pumpAndSettle();

    expect(
      find.byKey(const ValueKey('chat_long_message_viewer')),
      findsOneWidget,
    );
    expect(
      find.byKey(const ValueKey('chat_long_message_chunk_0')),
      findsOneWidget,
    );

    await tester.tap(find.byKey(const ValueKey('chat_long_message_close')));
    await tester.pumpAndSettle();
  });

  testWidgets('own-message bubble uses opaque white background',
      (WidgetTester tester) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.lightTheme,
        home: const Scaffold(
          body: MessageBubble(
            msgId: 'm_mine_white_bg',
            initialContent: 'hello self message',
            isMine: true,
          ),
        ),
      ),
    );
    await tester.pump();

    expect(
      find.byWidgetPredicate((widget) {
        if (widget is! Container) {
          return false;
        }
        final decoration = widget.decoration;
        return decoration is BoxDecoration &&
            decoration.color == Colors.white &&
            decoration.borderRadius == BorderRadius.circular(12);
      }),
      findsOneWidget,
    );

    final text = tester.widget<Text>(find.text('hello self message'));
    expect(text.style?.color, AppTheme.lightTextPrimary);

    ErrorWidget.builder = defaultErrorWidgetBuilder;
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('dark theme bubble uses dark card background and readable text',
      (WidgetTester tester) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.lightTheme,
        darkTheme: AppTheme.darkTheme,
        themeMode: ThemeMode.dark,
        home: const Scaffold(
          body: MessageBubble(
            msgId: 'm_dark_plain_text',
            initialContent: 'dark theme readable text',
            isMine: false,
          ),
        ),
      ),
    );
    await tester.pump();

    expect(
      find.byWidgetPredicate((widget) {
        if (widget is! Container) {
          return false;
        }
        final decoration = widget.decoration;
        return decoration is BoxDecoration &&
            decoration.color == AppTheme.darkCard &&
            decoration.borderRadius == BorderRadius.circular(12);
      }),
      findsOneWidget,
    );

    final text = tester.widget<Text>(find.text('dark theme readable text'));
    expect(text.style?.color, AppTheme.darkTextPrimary);

    ErrorWidget.builder = defaultErrorWidgetBuilder;
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('code block content uses native ast code block renderer',
      (WidgetTester tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: MessageBubble(
            msgId: 'm_code',
            initialContent: '```json\n{"key": "value"}\n```',
            isMine: false,
          ),
        ),
      ),
    );
    await tester.pump();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.byType(ChatMarkdownCodeBlockView), findsOneWidget);

    ErrorWidget.builder = defaultErrorWidgetBuilder;
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('markdown image with empty alt text uses native ast image view',
      (WidgetTester tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: MessageBubble(
            msgId: 'm3',
            initialContent: '![](https://example.com/a.png)',
            isMine: false,
          ),
        ),
      ),
    );
    await tester.pump();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.byType(ChatMarkdownImageView), findsOneWidget);

    ErrorWidget.builder = defaultErrorWidgetBuilder;
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('bare URL content uses native ast link renderer',
      (WidgetTester tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: MessageBubble(
            msgId: 'm4',
            initialContent: 'https://openai.com/research',
            isMine: false,
          ),
        ),
      ),
    );
    await tester.pump();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(
      tappableLinkTexts(tester),
      contains('https://openai.com/research'),
    );

    ErrorWidget.builder = defaultErrorWidgetBuilder;
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('table content uses native ast table renderer',
      (WidgetTester tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: MessageBubble(
            msgId: 'm_table',
            initialContent: '| A | B |\n|---|---|\n| 1 | 2 |',
            isMine: false,
          ),
        ),
      ),
    );
    await tester.pump();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.byType(ChatMarkdownTableView), findsOneWidget);
    expect(find.byType(Table), findsOneWidget);

    ErrorWidget.builder = defaultErrorWidgetBuilder;
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });
}
