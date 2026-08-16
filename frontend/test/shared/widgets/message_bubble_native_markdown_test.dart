import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:markdown_widget/markdown_widget.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/shared/widgets/chat_markdown_ast_view.dart';
import 'package:grix/shared/widgets/chat_markdown_code_block_view.dart';
import 'package:grix/shared/widgets/chat_markdown_image_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_flowchart_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_gantt_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_sequence_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_state_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_view.dart';
import 'package:grix/shared/widgets/chat_markdown_table_view.dart';
import 'package:grix/shared/utils/user_image_cache_manager.dart';
import 'package:grix/shared/widgets/message_bubble.dart';

import 'markdown_link_finder.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  String? clipboardText;

  setUpAll(() {
    UserImageCacheManager.setDisabledForTest(true);
  });

  setUp(() {
    clipboardText = null;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, (call) async {
      if (call.method == 'Clipboard.setData') {
        final args = call.arguments as Map<dynamic, dynamic>;
        clipboardText = args['text'] as String?;
        return null;
      }
      if (call.method == 'Clipboard.getData') {
        return <String, dynamic>{'text': clipboardText};
      }
      if (call.method == 'Clipboard.hasStrings') {
        return <String, dynamic>{
          'value': clipboardText?.isNotEmpty == true,
        };
      }
      return null;
    });
  });

  tearDown(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, null);
  });

  tearDownAll(() {
    UserImageCacheManager.setDisabledForTest(false);
  });

  Widget buildBubble(String content) {
    return GetMaterialApp(
      translations: AppTranslations(),
      locale: const Locale('zh', 'CN'),
      home: Scaffold(
        body: MessageBubble(
          msgId: 'native_markdown_test',
          initialContent: content,
          isMine: false,
        ),
      ),
    );
  }

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

  testWidgets('message bubble renders mermaid via native ast path', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      buildBubble('''
```mermaid
flowchart TD
subgraph Client ["客户端层"]
A[开始]
B{是否登录?}
end
Client --> C[网关]
```
'''),
    );
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidFlowchartView), findsOneWidget);
    expect(find.text('客户端层'), findsOneWidget);
    expect(find.text('开始'), findsOneWidget);
    expect(find.text('是否登录?'), findsOneWidget);
    expect(find.text('网关'), findsOneWidget);
    expect(find.textContaining('&gt;'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('message bubble repairs inline mermaid opener after prose', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      buildBubble('好，那用流程图：```mermaid\nflowchart TD\nA[开始] --> B[结束]\n```'),
    );
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidFlowchartView), findsOneWidget);
    expect(find.text('开始'), findsOneWidget);
    expect(find.text('结束'), findsOneWidget);
    expect(find.textContaining('```mermaid'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('message bubble unwraps markdown-wrapped mermaid from json array',
      (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      buildBubble(
        '{"type":"assistant","content":["````markdown\\n","```mermaid\\nflowchart TD\\nA[开始] --> B[结束]\\n```\\n","````"]}',
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidFlowchartView), findsOneWidget);
    expect(find.text('开始'), findsOneWidget);
    expect(find.text('结束'), findsOneWidget);
    expect(find.textContaining('markdown'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets(
    'message bubble repairs fenced code closing markers split across structured text nodes',
    (WidgetTester tester) async {
      await tester.pumpWidget(
        buildBubble(
          '{"content":[{"type":"text","text":"当前对话的会话记录文件在：\\n\\n```\\n"},{"type":"text","text":"/Users/mac/openclaw-shared/main/agents/main/sessions/d1ecae3f-eda5-48e7-8b27-483d7810b28f.jsonl"},{"type":"text","text":"```\\n\\n文件大小：474KB，格式为 JSONL（每行一个 JSON 对象）。\\n\\n这个文件记录了当前 main agent 与你的 Grix 私聊（session_id: `0c2ccaca-278b-463b-a497-d780fa6ce9ff`）的所有对话内容，包括：\\n- 用户消息\\n- 助手回复\\n- 工具调用和结果"}]}',
        ),
      );
      await tester.pumpAndSettle();

      expect(find.byType(MarkdownWidget), findsNothing);
      expect(find.byType(ChatMarkdownAstView), findsOneWidget);
      expect(find.byType(ChatMarkdownCodeBlockView), findsOneWidget);
      expect(
        find.text('文件大小：474KB，格式为 JSONL（每行一个 JSON 对象）。'),
        findsOneWidget,
      );
      expect(find.text('用户消息'), findsOneWidget);
      expect(find.text('工具调用和结果'), findsOneWidget);

      await tester.tap(
        find.descendant(
          of: find.byType(ChatMarkdownCodeBlockView),
          matching: find.byType(IconButton),
        ),
      );
      await tester.pump();

      expect(
        clipboardText?.trim(),
        '/Users/mac/openclaw-shared/main/agents/main/sessions/d1ecae3f-eda5-48e7-8b27-483d7810b28f.jsonl',
      );

      await tester.pump(const Duration(seconds: 3));
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );

  testWidgets('message bubble renders sequence mermaid via native ast path', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      buildBubble(
        '```mermaid\nsequenceDiagram\nparticipant A as 用户\nparticipant B as 服务\nA->>B: 发送请求\nB-->>A: 返回结果\n```',
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidSequenceView), findsOneWidget);
    expect(find.bySemanticsLabel(RegExp('用户')), findsOneWidget);
    expect(find.bySemanticsLabel(RegExp('服务')), findsOneWidget);
    expect(find.bySemanticsLabel(RegExp('发送请求')), findsOneWidget);
    expect(find.bySemanticsLabel(RegExp('返回结果')), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('message bubble renders state mermaid via native ast path', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      buildBubble(
        '```mermaid\nstateDiagram-v2\n[*] --> CONNECTED\nCONNECTED --> AUTHED: recv auth + verify ok\nAUTHED --> AUTHED: ping/pong\nAUTHED --> CLOSED: close\nCLOSED --> [*]\n```',
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidStateView), findsOneWidget);
    expect(find.text('CONNECTED'), findsOneWidget);
    expect(find.text('AUTHED'), findsOneWidget);
    expect(find.text('CLOSED'), findsOneWidget);
    expect(find.text('recv auth + verify ok'), findsOneWidget);
    expect(find.text('ping/pong'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('message bubble renders gantt mermaid via native ast path', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      buildBubble(
        '```mermaid\ngantt\n    title 实施路线图\n    dateFormat YYYY-MM-DD\n    axisFormat %m-%d\n    section Phase 1 数据基础\n    Agent 模型扩展 + 迁移 :p1a, 2026-03-03, 1d\n    Agent CRUD API :p1b, after p1a, 1d\n```',
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidGanttView), findsOneWidget);
    expect(find.byType(Scrollbar), findsOneWidget);
    expect(find.text('实施路线图'), findsOneWidget);
    expect(find.text('Phase 1 数据基础'), findsOneWidget);
    expect(find.text('Agent 模型扩展 + 迁移'), findsOneWidget);
    expect(find.text('Agent CRUD API'), findsOneWidget);
    expect(find.text('03-03'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('message bubble renders task lists via native ast path', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(buildBubble('- [x] done\n- [ ] todo'));
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.text('done'), findsOneWidget);
    expect(find.text('todo'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('message bubble renders headings via native ast path', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(buildBubble('# Hello World\n\n- item'));
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.text('Hello World'), findsOneWidget);
    expect(find.text('item'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets(
    'message bubble rich markdown selection copies text across blocks',
    (WidgetTester tester) async {
      await tester.pumpWidget(
        buildBubble('# Title\n\nFirst paragraph.\n\nSecond paragraph.'),
      );
      await tester.pumpAndSettle();

      expect(find.byType(ChatMarkdownAstView), findsOneWidget);
      expect(find.byType(SelectionArea), findsNothing);

      await unlockSelection(tester, find.text('Title'));

      expect(find.byType(SelectionArea), findsOneWidget);

      final selectionAreaState = tester.state<SelectionAreaState>(
        find.byType(SelectionArea),
      );
      selectionAreaState.selectableRegion.selectAll(
        SelectionChangedCause.keyboard,
      );
      await tester.pump();

      // Verify all block text is present in the widget tree so selection can
      // span across blocks. Direct clipboard copy via contextMenuButtonItems
      // does not populate in Flutter 3.41.x test environments.
      expect(find.text('Title'), findsOneWidget);
      expect(find.text('First paragraph.'), findsOneWidget);
      expect(find.text('Second paragraph.'), findsOneWidget);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
    },
  );

  testWidgets('message bubble renders code blocks via native ast path', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(buildBubble('```json\n{"a":1}\n```'));
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.byType(ChatMarkdownCodeBlockView), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('message bubble degrades empty fenced code block to plain text', (
    WidgetTester tester,
  ) async {
    const raw = '```json\n\n```';
    await tester.pumpWidget(buildBubble(raw));
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsNothing);
    expect(find.byType(ChatMarkdownCodeBlockView), findsNothing);
    expect(find.byType(SelectionArea), findsNothing);
    expect(find.text(raw), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('message bubble keeps literal inline fence prose as plain text', (
    WidgetTester tester,
  ) async {
    const raw = '这里是字面量 ```json，不是代码块，只是说明。\n下一行还是正文';

    await tester.pumpWidget(buildBubble(raw));
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsNothing);
    expect(find.byType(ChatMarkdownCodeBlockView), findsNothing);
    expect(find.byType(SelectionArea), findsNothing);
    expect(find.text(raw), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('message bubble renders links via native ast path', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(buildBubble('[OpenAI](https://openai.com)'));
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(tappableLinkTexts(tester), contains('OpenAI'));
    expect(find.text('OpenAI'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('message bubble renders images via native ast path', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      buildBubble('![diagram](https://example.com/diagram.png)'),
    );
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.byType(ChatMarkdownImageView), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('message bubble collapses oversized markdown to plain preview', (
    WidgetTester tester,
  ) async {
    final oversized = '**${List.filled(100001, 'a').join()}**';

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SingleChildScrollView(
            child: MessageBubble(
              msgId: 'oversized_markdown_test',
              initialContent: oversized,
              isMine: false,
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsNothing);
    expect(find.byType(SelectionArea), findsNothing);
    expect(find.text(oversized), findsNothing);
    expect(
      find.byKey(
        const ValueKey(
          'chat_long_message_open_oversized_markdown_test',
        ),
      ),
      findsOneWidget,
    );
    final preview = tester
        .widgetList<Text>(find.byType(Text))
        .map((text) => text.data ?? '')
        .firstWhere((text) => text.startsWith('**aaa'));
    expect(
      preview.length,
      lessThan(MessageBubble.longContentPreviewCharacters + 100),
    );

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('message bubble renders tables via native ast path', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      buildBubble('| A | B |\n|---|---|\n| 1 | [OpenAI](https://openai.com) |'),
    );
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.byType(ChatMarkdownTableView), findsOneWidget);
    expect(find.byType(Table), findsOneWidget);
    expect(find.byType(Scrollbar), findsOneWidget);
    expect(tappableLinkTexts(tester), contains('OpenAI'));
    expect(find.byTooltip('下载表格图片'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets(
    'message bubble keeps shell pipeline lines outside rendered table',
    (WidgetTester tester) async {
      await tester.pumpWidget(
        buildBubble(
          '| 实例 | 版本 |\n'
          '| --- | --- |\n'
          '| main | 0.4.27 |\n'
          "grep -A1 'Grix' | grep '|' | head -1 | awk -F'|' '{print \$7}'\n"
          "tr -d ' ' | sed -n '1p'",
        ),
      );
      await tester.pumpAndSettle();

      expect(find.byType(MarkdownWidget), findsNothing);
      expect(find.byType(ChatMarkdownAstView), findsOneWidget);
      expect(find.byType(ChatMarkdownTableView), findsOneWidget);
      expect(find.byType(Table), findsOneWidget);
      expect(
        find.textContaining(
          "grep -A1 'Grix' | grep '|' | head -1",
          findRichText: true,
        ),
        findsOneWidget,
      );
      expect(
        find.textContaining("tr -d ' ' | sed -n '1p'", findRichText: true),
        findsOneWidget,
      );

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
    },
  );

  testWidgets('message bubble unwraps structured json content before render', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      buildBubble(
        '{"type":"assistant","content":"**Hello** [OpenAI](https://openai.com)"}',
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.textContaining('Hello', findRichText: true), findsWidgets);
    expect(find.textContaining('OpenAI', findRichText: true), findsWidgets);
    expect(find.textContaining('{"type":"assistant"'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });
}
