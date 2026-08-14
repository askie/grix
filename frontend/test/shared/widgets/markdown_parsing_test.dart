import 'package:flutter/material.dart';
import 'package:markdown_widget/markdown_widget.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/widgets/chat_markdown_code_block_view.dart';
import 'package:grix/shared/widgets/message_bubble.dart';
import 'package:grix/shared/widgets/chat_markdown_ast_view.dart';
import 'package:grix/shared/widgets/chat_markdown_image_view.dart';
import 'package:grix/shared/widgets/chat_markdown_table_view.dart';
import 'package:grix/shared/utils/user_image_cache_manager.dart';
import 'package:grix/app/themes/app_theme.dart';

import 'markdown_link_finder.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUpAll(() {
    UserImageCacheManager.setDisabledForTest(true);
  });

  tearDownAll(() {
    UserImageCacheManager.setDisabledForTest(false);
  });

  Widget buildTestableWidget(String content) {
    return MaterialApp(
      theme: AppTheme.lightTheme,
      home: Scaffold(
        body: MessageBubble(
          msgId: 'test_msg_id',
          initialContent: content,
          isMine: false,
        ),
      ),
    );
  }

  testWidgets('native ast view parses basic headings',
      (WidgetTester tester) async {
    await tester.runAsync(() async {
      await tester.pumpWidget(buildTestableWidget('# Hello World'));
      await tester.pump();
      expect(find.byType(MarkdownWidget), findsNothing);
      expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    });
  });

  testWidgets('native ast view parses basic tables properly',
      (WidgetTester tester) async {
    await tester.runAsync(() async {
      await tester.pumpWidget(buildTestableWidget(
          '| Header A | Header B |\n|---|---|\n| Value 1 | Value 2 |'));
      await tester.pump();
      expect(find.byType(MarkdownWidget), findsNothing);
      expect(find.byType(ChatMarkdownAstView), findsOneWidget);
      expect(find.byType(ChatMarkdownTableView), findsOneWidget);
      expect(find.byType(Table), findsOneWidget);
    });
  });

  testWidgets('native ast view parses code blocks',
      (WidgetTester tester) async {
    await tester.runAsync(() async {
      await tester
          .pumpWidget(buildTestableWidget('```json\n{"key": "value"}\n```'));
      await tester.pump();
      expect(find.byType(MarkdownWidget), findsNothing);
      expect(find.byType(ChatMarkdownAstView), findsOneWidget);
      expect(find.byType(ChatMarkdownCodeBlockView), findsOneWidget);
    });
  });

  testWidgets('native ast view parses lists', (WidgetTester tester) async {
    await tester.runAsync(() async {
      await tester.pumpWidget(buildTestableWidget('- Item 1\n- Item 2'));
      await tester.pump();
      expect(find.byType(MarkdownWidget), findsNothing);
      expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    });
  });

  testWidgets('native ast view parses bold and italic text',
      (WidgetTester tester) async {
    await tester.runAsync(() async {
      await tester.pumpWidget(buildTestableWidget('**BoldText** *ItalicText*'));
      await tester.pump();
      expect(find.byType(MarkdownWidget), findsNothing);
      expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    });
  });

  testWidgets('MessageBubble strips code blocks purely containing tables',
      (WidgetTester tester) async {
    const rawText = "```markdown\n| a | b |\n|---|---|\n| 1 | 2 |\n```";
    await tester.runAsync(() async {
      await tester.pumpWidget(buildTestableWidget(rawText));
      await tester.pumpAndSettle();
      expect(find.byType(MarkdownWidget), findsNothing);
      expect(find.byType(ChatMarkdownAstView), findsOneWidget);
      expect(find.byType(ChatMarkdownTableView), findsOneWidget);
      expect(find.byType(Table), findsOneWidget);
    });
  });

  testWidgets('native ast view parses bare url links',
      (WidgetTester tester) async {
    await tester.runAsync(() async {
      await tester.pumpWidget(
        buildTestableWidget('Visit https://openai.com/research'),
      );
      await tester.pump();
      expect(find.byType(MarkdownWidget), findsNothing);
      expect(find.byType(ChatMarkdownAstView), findsOneWidget);
      expect(
        tappableLinkTexts(tester),
        contains('https://openai.com/research'),
      );
    });
  });

  testWidgets('native ast view parses markdown images',
      (WidgetTester tester) async {
    await tester.runAsync(() async {
      await tester.pumpWidget(
        buildTestableWidget('![alt](https://example.com/a.png)'),
      );
      await tester.pump();
      expect(find.byType(MarkdownWidget), findsNothing);
      expect(find.byType(ChatMarkdownAstView), findsOneWidget);
      expect(find.byType(ChatMarkdownImageView), findsOneWidget);
    });
  });

  testWidgets('native ast view parses tables with inline rich content',
      (WidgetTester tester) async {
    await tester.runAsync(() async {
      await tester.pumpWidget(
        buildTestableWidget(
          '| A | B |\n|:--|--:|\n| ![alt](https://example.com/a.png) | [OpenAI](https://openai.com) |',
        ),
      );
      await tester.pump();
      expect(find.byType(MarkdownWidget), findsNothing);
      expect(find.byType(ChatMarkdownAstView), findsOneWidget);
      expect(find.byType(ChatMarkdownTableView), findsOneWidget);
      expect(find.byType(ChatMarkdownImageView), findsOneWidget);
      expect(tappableLinkTexts(tester), contains('OpenAI'));
    });
  });
}
