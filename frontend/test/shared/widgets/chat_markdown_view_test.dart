import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_math_fork/flutter_math.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:markdown_widget/markdown_widget.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/chat/message_cards/widgets/chat_message_card_view.dart';
import 'package:grix/shared/markdown/chat_markdown_dialect.dart';
import 'package:grix/shared/markdown/chat_markdown_normalizer.dart';
import 'package:grix/shared/markdown/chat_markdown_pipeline.dart';
import 'package:grix/shared/widgets/chat_markdown_ast_view.dart';
import 'package:grix/shared/widgets/chat_markdown_image_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_flowchart_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_gantt_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_gitgraph_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_mindmap_view.dart';
import 'package:grix/shared/widgets/chat_markdown_plain_text_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_sequence_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_state_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_timeline_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_treemap_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_block_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_packet_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_requirement_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_quadrant_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_kanban_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_radar_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_sankey_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_class_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_er_view.dart';
import 'package:grix/shared/widgets/chat_markdown_math_block_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_view.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_zoomable_viewport.dart';
import 'package:grix/shared/widgets/chat_markdown_style_sheet.dart';
import 'package:grix/shared/widgets/chat_markdown_table_view.dart';
import 'package:grix/shared/widgets/chat_markdown_view.dart';
import 'package:grix/shared/widgets/chat_markdown_audio_view.dart';
import 'package:grix/shared/widgets/chat_markdown_video_view.dart';
import 'package:grix/shared/widgets/chat_message_video_preview_dialog.dart';
import 'package:grix/shared/widgets/chat_markdown_zoomable_image_viewport.dart';
import 'package:grix/shared/utils/user_image_cache_manager.dart';

import 'markdown_link_finder.dart';

/// 求某子树下所有 [RepaintBoundary] 的最大高度，用于校验导出边界覆盖完整画布。
double _maxRepaintBoundaryHeight(WidgetTester tester, {required Finder of}) {
  var maxHeight = 0.0;
  final finder = find.descendant(of: of, matching: find.byType(RepaintBoundary));
  for (final element in finder.evaluate()) {
    final renderObject = element.renderObject;
    if (renderObject is RenderBox && renderObject.hasSize) {
      final height = renderObject.size.height;
      if (height > maxHeight) {
        maxHeight = height;
      }
    }
  }
  return maxHeight;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUpAll(() {
    UserImageCacheManager.setDisabledForTest(true);
  });

  tearDownAll(() {
    UserImageCacheManager.setDisabledForTest(false);
  });
  final pipeline = ChatMarkdownPipeline(
    normalizer: const ChatMarkdownNormalizer(),
    parser: ChatMarkdownDialect.buildParserAdapter(),
  );

  Widget buildView(String content, {bool isMine = false}) {
    return GetMaterialApp(
      translations: AppTranslations(),
      locale: const Locale('zh', 'CN'),
      theme: AppTheme.lightTheme,
      home: Builder(
        builder: (context) => Scaffold(
          body: ChatMarkdownView(
            data: content,
            textColor: isMine
                ? Theme.of(context).colorScheme.onPrimary
                : Theme.of(context).colorScheme.onSurface,
            isMine: isMine,
          ),
        ),
      ),
    );
  }

  Widget buildParsedView(String content, {bool isMine = false}) {
    final result = pipeline.prepareFinalRender(content);
    return GetMaterialApp(
      translations: AppTranslations(),
      locale: const Locale('zh', 'CN'),
      theme: AppTheme.lightTheme,
      home: Builder(
        builder: (context) => Scaffold(
          body: ChatMarkdownView(
            data: result.normalizedText,
            textColor: isMine
                ? Theme.of(context).colorScheme.onPrimary
                : Theme.of(context).colorScheme.onSurface,
            isMine: isMine,
            document: result.document,
            semantics: result.semantics,
          ),
        ),
      ),
    );
  }

  Widget buildZoomableImageViewport(
    TransformationController controller, {
    ChatMarkdownImageZoomController? zoomController,
    VoidCallback? onDismiss,
    double? minScale,
  }) {
    return MaterialApp(
      theme: AppTheme.lightTheme,
      home: Scaffold(
        body: Center(
          child: SizedBox(
            width: 320,
            height: 240,
            child: ColoredBox(
              color: Colors.black,
              child: ChatMarkdownZoomableImageViewport(
                transformationController: controller,
                controller: zoomController,
                onDismiss: onDismiss,
                minScale: minScale ?? 0.625,
                child: const Center(child: FlutterLogo(size: 120)),
              ),
            ),
          ),
        ),
      ),
    );
  }

  Future<void> performDoubleTap(WidgetTester tester, Finder finder) async {
    final position = tester.getCenter(finder);
    await tester.tapAt(position);
    await tester.pump(const Duration(milliseconds: 80));
    await tester.tapAt(position);
    await tester.pump();
  }

  test('uses a light markdown surface palette even for own-message blocks', () {
    final styleSheet = ChatMarkdownStyleSheet.fromTheme(
      theme: AppTheme.lightTheme,
      textColor: Colors.white,
      isMine: true,
    );

    final decoration = styleSheet.preDecoration as BoxDecoration;

    expect(decoration.color, AppTheme.lightCard);
    expect(styleSheet.preTextStyle.color, AppTheme.lightTextPrimary);
    expect(styleSheet.preLabelStyle.color, AppTheme.lightTextSecondary);
  });

  testWidgets('renders latex and table using shared markdown dialect', (
    WidgetTester tester,
  ) async {
    const content =
        r'The energy is $E = mc^2$.'
        '\n\n| a | b |\n|---|---|\n| 1 | 2 |';

    await tester.pumpWidget(buildView(content));
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsOneWidget);
    expect(find.byType(Math), findsWidgets);
    expect(find.byType(Table), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('renders mermaid fenced blocks with dedicated fallback view', (
    WidgetTester tester,
  ) async {
    const content = '''
```mermaid
flowchart TD
subgraph Client ["客户端层"]
A[开始]
B{是否登录?}
end
Client --> C[网关]
```
''';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidFlowchartView), findsOneWidget);
    expect(find.byTooltip('查看流程图'), findsOneWidget);
    expect(find.text('流程图'), findsOneWidget);
    expect(find.text('客户端层'), findsOneWidget);
    expect(find.text('开始'), findsOneWidget);
    expect(find.text('是否登录?'), findsOneWidget);
    expect(find.text('网关'), findsOneWidget);
    expect(find.textContaining('flowchart TD'), findsNothing);
    expect(find.textContaining('&gt;'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('mermaid flowchart download opens preview dialog before saving', (
    WidgetTester tester,
  ) async {
    const content = '''
```mermaid
flowchart TD
A[开始] --> B[结束]
```
''';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    await tester.tap(find.byTooltip('查看流程图'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));

    expect(
      find.byKey(const ValueKey('markdown_image_preview_dialog')),
      findsOneWidget,
    );
    expect(find.byTooltip('关闭图片预览'), findsOneWidget);

    await tester.tap(find.byTooltip('关闭图片预览'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));

    expect(
      find.byKey(const ValueKey('markdown_image_preview_dialog')),
      findsNothing,
    );

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('math block shows download button and preview dialog', (
    WidgetTester tester,
  ) async {
    const content = r'''
$$
E = mc^2
$$
''';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownMathBlockView), findsOneWidget);
    expect(find.byTooltip('下载公式图片'), findsOneWidget);

    await tester.tap(find.byTooltip('下载公式图片'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));

    expect(
      find.byKey(const ValueKey('markdown_image_preview_dialog')),
      findsOneWidget,
    );
    expect(find.byTooltip('关闭图片预览'), findsOneWidget);

    await tester.tap(find.byTooltip('关闭图片预览'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));

    expect(
      find.byKey(const ValueKey('markdown_image_preview_dialog')),
      findsNothing,
    );

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('uppercase mermaid fence still shows flowchart download button', (
    WidgetTester tester,
  ) async {
    const content = '''
```Mermaid
flowchart TD
A[开始] --> B[结束]
```
''';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidFlowchartView), findsOneWidget);
    expect(find.byTooltip('查看流程图'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets(
    'flowchart with direction directives still shows download button',
    (WidgetTester tester) async {
      const content = '''
```mermaid
flowchart TD
direction TB
subgraph Client [客户端]
direction LR
A[开始] --> B[结束]
end
```
''';

      await tester.pumpWidget(buildParsedView(content));
      await tester.pumpAndSettle();

      expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
      expect(find.byType(ChatMarkdownMermaidFlowchartView), findsOneWidget);
      expect(find.byTooltip('查看流程图'), findsOneWidget);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
    },
  );

  testWidgets('renders light code block surfaces for own-message markdown', (
    WidgetTester tester,
  ) async {
    const content = '''
```dart
final answer = 42;
```
''';

    await tester.pumpWidget(buildParsedView(content, isMine: true));
    await tester.pumpAndSettle();

    expect(
      find.byWidgetPredicate((widget) {
        if (widget is! Container) {
          return false;
        }
        final decoration = widget.decoration;
        return decoration is BoxDecoration &&
            decoration.color == AppTheme.lightCard;
      }),
      findsWidgets,
    );
    expect(
      find.byWidgetPredicate((widget) {
        if (widget is! Container) {
          return false;
        }
        final decoration = widget.decoration;
        return decoration is BoxDecoration &&
            (decoration.color == const Color(0xFF1F2937) ||
                decoration.color == const Color(0xFF151515));
      }),
      findsNothing,
    );

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('native ast view decodes escaped html characters', (
    WidgetTester tester,
  ) async {
    const content = 'A > B & C\n\n`X > Y & Z`';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.textContaining('A > B & C'), findsOneWidget);
    expect(find.textContaining('X > Y & Z'), findsOneWidget);
    expect(find.textContaining('&gt;'), findsNothing);
    expect(find.textContaining('&amp;'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('fallback markdown widget decodes mermaid source text', (
    WidgetTester tester,
  ) async {
    const content = '''
```mermaid
flowchart TD
subgraph Client ["客户端层"]
A[开始]
end
Client --> B[结束]
```
''';

    await tester.pumpWidget(buildView(content));
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidFlowchartView), findsOneWidget);
    expect(find.text('客户端层'), findsOneWidget);
    expect(find.text('开始'), findsOneWidget);
    expect(find.text('结束'), findsOneWidget);
    expect(find.textContaining('&gt;'), findsNothing);
    expect(find.textContaining('&amp;'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('raw html falls back to plain text instead of markdown widget', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(buildParsedView('<div>unsafe html</div>'));
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownPlainTextView), findsOneWidget);
    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsNothing);
    expect(find.text('<div>unsafe html</div>'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('unsafe links are rendered as plain text in native ast path', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(buildParsedView('[attack](javascript:alert(1))'));
    await tester.pumpAndSettle();

    // 非法 scheme 降级为纯文本 TextSpan（不挂点击手势），文本照常显示、不可点。
    expect(tappableLinkSpans(tester), isEmpty);
    expect(find.text('attack'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('fallback path keeps fenced table-like code as code content', (
    WidgetTester tester,
  ) async {
    const content = '```text\n| a | b |\n|---|---|\n| 1 | 2 |\n```';

    await tester.pumpWidget(buildView(content));
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsOneWidget);
    expect(find.byType(Table), findsNothing);
    expect(find.textContaining('| a | b |'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('fallback path renders unsafe markdown links as plain text', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(buildView('[attack](javascript:alert(1))'));
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsOneWidget);
    expect(tappableLinkSpans(tester), isEmpty);
    expect(find.text('attack'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('unsafe image sources fall back to text in native ast path', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(buildParsedView('![bad](file:///tmp/secret.png)'));
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownImageView), findsOneWidget);
    expect(find.text('bad'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('renders light mermaid surfaces for own-message markdown', (
    WidgetTester tester,
  ) async {
    const content = '''
```mermaid
flowchart TD
A[开始] --> B[结束]
```
''';

    await tester.pumpWidget(buildParsedView(content, isMine: true));
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(
      find.byWidgetPredicate((widget) {
        if (widget is! Container) {
          return false;
        }
        final decoration = widget.decoration;
        return decoration is BoxDecoration &&
            decoration.color == AppTheme.lightCard;
      }),
      findsWidgets,
    );
    expect(
      find.byWidgetPredicate((widget) {
        if (widget is! Container) {
          return false;
        }
        final decoration = widget.decoration;
        return decoration is BoxDecoration &&
            (decoration.color == const Color(0xFF1F2937) ||
                decoration.color == const Color(0xFF151515));
      }),
      findsNothing,
    );

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('unsupported mermaid syntax falls back to raw source text', (
    WidgetTester tester,
  ) async {
    const content = '```mermaid\nC4Context\ntitle Reach and impact\n```';
    String? copiedText;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, (call) async {
          if (call.method == 'Clipboard.setData') {
            copiedText = (call.arguments as Map<dynamic, dynamic>)['text']
                ?.toString();
          }
          return null;
        });
    addTearDown(() {
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(SystemChannels.platform, null);
    });

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidFlowchartView), findsNothing);
    expect(find.byType(ChatMarkdownMermaidSequenceView), findsNothing);
    expect(find.byTooltip('查看流程图'), findsNothing);
    expect(find.byTooltip('复制流程图代码'), findsOneWidget);
    expect(find.textContaining('C4Context'), findsOneWidget);
    expect(find.textContaining('title Reach and impact'), findsOneWidget);

    await tester.tap(find.byTooltip('复制流程图代码'));
    await tester.pump();

    expect(copiedText, 'C4Context\ntitle Reach and impact');

    await tester.pump(const Duration(seconds: 3));
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('renders sequence diagrams with native sequence view', (
    WidgetTester tester,
  ) async {
    const content = '''
```mermaid
sequenceDiagram
participant Caller as 调用方
participant SS as StreamSession
Note over Caller, SS: 首个 chunk
Caller->>SS: NewStreamSession(config)
loop 每个 chunk
Caller->>SS: AppendChunk(delta)
SS-->>Caller: ok
end
```
''';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidSequenceView), findsOneWidget);
    expect(find.byTooltip('查看流程图'), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidFlowchartView), findsNothing);
    expect(find.bySemanticsLabel(RegExp('调用方')), findsOneWidget);
    expect(find.bySemanticsLabel(RegExp('StreamSession')), findsOneWidget);
    expect(find.bySemanticsLabel(RegExp('首个 chunk')), findsOneWidget);
    expect(
      find.bySemanticsLabel(RegExp('AppendChunk\\(delta\\)')),
      findsOneWidget,
    );

    await tester.tap(find.byTooltip('查看流程图'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));

    expect(
      find.byKey(const ValueKey('markdown_image_preview_dialog')),
      findsOneWidget,
    );

    await tester.tap(find.byTooltip('关闭图片预览'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));
    expect(
      find.byKey(const ValueKey('markdown_image_preview_dialog')),
      findsNothing,
    );

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('renders state diagrams with native state view', (
    WidgetTester tester,
  ) async {
    const content = '''
```mermaid
stateDiagram-v2
    [*] --> CONNECTED
    CONNECTED --> AUTHED: recv auth + verify ok
    CONNECTED --> CLOSED: auth timeout / auth fail
    AUTHED --> AUTHED: ping/pong
    AUTHED --> CLOSED: kicked / network error / close
    CLOSED --> [*]
```
''';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidStateView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidFlowchartView), findsNothing);
    expect(find.byType(ChatMarkdownMermaidSequenceView), findsNothing);
    expect(find.text('CONNECTED'), findsOneWidget);
    expect(find.text('AUTHED'), findsOneWidget);
    expect(find.text('CLOSED'), findsOneWidget);
    expect(find.text('recv auth + verify ok'), findsOneWidget);
    expect(find.text('ping/pong'), findsOneWidget);
    expect(find.textContaining('stateDiagram-v2'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('renders gantt diagrams with native gantt view', (
    WidgetTester tester,
  ) async {
    const content = '''
```mermaid
gantt
    title 实施路线图
    dateFormat YYYY-MM-DD
    axisFormat %m-%d

    section Phase 1 数据基础
    Agent 模型扩展 + 迁移 :p1a, 2026-03-03, 1d
    Agent CRUD API :p1b, after p1a, 1d
```
''';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidGanttView), findsOneWidget);
    expect(find.byType(Scrollbar), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidFlowchartView), findsNothing);
    expect(find.byType(ChatMarkdownMermaidSequenceView), findsNothing);
    expect(find.byType(ChatMarkdownMermaidStateView), findsNothing);
    expect(find.text('实施路线图'), findsOneWidget);
    expect(find.text('Phase 1 数据基础'), findsOneWidget);
    expect(find.text('Agent 模型扩展 + 迁移'), findsOneWidget);
    expect(find.text('Agent CRUD API'), findsOneWidget);
    expect(find.text('03-03'), findsOneWidget);
    expect(find.textContaining('gantt'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('renders timeline diagrams with native timeline view', (
    WidgetTester tester,
  ) async {
    const content = '''
```mermaid
timeline
    title 社媒发展史
    section 早期
    2002 : LinkedIn
    2004 : Facebook : Google
    section 移动时代
    2010 : Instagram
```
''';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidTimelineView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidFlowchartView), findsNothing);
    expect(find.text('社媒发展史'), findsOneWidget);
    expect(find.text('早期'), findsOneWidget);
    expect(find.text('移动时代'), findsOneWidget);
    expect(find.text('2004'), findsOneWidget);
    expect(find.text('Facebook'), findsOneWidget);
    expect(find.text('Google'), findsOneWidget);
    expect(find.text('Instagram'), findsOneWidget);
    expect(find.textContaining('timeline'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('renders quadrant charts with native quadrant view', (
    WidgetTester tester,
  ) async {
    const content = '''
```mermaid
quadrantChart
    title 活动象限
    x-axis 低触达 --> 高触达
    y-axis 低参与 --> 高参与
    quadrant-1 应扩大
    quadrant-2 需推广
    quadrant-3 重新评估
    quadrant-4 可改进
    活动甲: [0.3, 0.6]
```
''';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidQuadrantView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidFlowchartView), findsNothing);
    expect(find.text('活动象限'), findsOneWidget);
    expect(find.text('应扩大'), findsOneWidget);
    expect(find.text('重新评估'), findsOneWidget);
    expect(find.text('活动甲'), findsOneWidget);
    expect(find.textContaining('quadrantChart'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('renders sankey diagrams with native sankey view', (
    WidgetTester tester,
  ) async {
    const content = '''
```mermaid
sankey-beta
原料,加工,100
加工,成品,70
加工,损耗,30
```
''';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidSankeyView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidFlowchartView), findsNothing);
    expect(find.textContaining('sankey'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('renders radar diagrams with native radar view', (
    WidgetTester tester,
  ) async {
    const content = '''
```mermaid
radar-beta
    title 能力评估
    axis 速度, 力量, 技巧
    curve 选手甲{8, 6, 9}
    curve 选手乙{5, 9, 7}
    max 10
```
''';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidRadarView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidFlowchartView), findsNothing);
    expect(find.text('能力评估'), findsOneWidget);
    // 图例标签为 Text widget
    expect(find.text('选手甲'), findsOneWidget);
    expect(find.text('选手乙'), findsOneWidget);
    expect(find.textContaining('radar-beta'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('renders kanban diagrams with native kanban view', (
    WidgetTester tester,
  ) async {
    const content = '''
```mermaid
kanban
  待办
    需求评审
    设计接口@{ assigned: 张三, priority: 'High' }
  进行中
    id6[实现渲染]@{ ticket: MC-100 }
  完成
    id5[联调]
```
''';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidKanbanView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidFlowchartView), findsNothing);
    expect(find.text('待办'), findsOneWidget);
    expect(find.text('进行中'), findsOneWidget);
    expect(find.text('需求评审'), findsOneWidget);
    expect(find.text('实现渲染'), findsOneWidget);
    expect(find.text('@张三'), findsOneWidget);
    expect(find.text('#MC-100'), findsOneWidget);
    expect(find.text('High'), findsOneWidget);
    expect(find.textContaining('kanban'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('renders treemap diagrams with native treemap view', (
    WidgetTester tester,
  ) async {
    const content = '''
```mermaid
treemap-beta
"预算"
    "人力": 50
    "营销": 30
"运营"
    "服务器": 20
```
''';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidTreemapView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidFlowchartView), findsNothing);
    expect(find.textContaining('treemap'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('renders block diagrams with native block view', (
    WidgetTester tester,
  ) async {
    const content = '''
```mermaid
block-beta
  columns 3
  前端["前端"] 后端["后端"] db[("数据库")]
  前端 --> 后端
```
''';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidBlockView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidFlowchartView), findsNothing);
    expect(find.textContaining('block-beta'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('renders packet diagrams with native packet view', (
    WidgetTester tester,
  ) async {
    const content = '''
```mermaid
packet-beta
title UDP 包
0-15: "源端口"
16-31: "目的端口"
32-63: "长度"
```
''';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidPacketView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidFlowchartView), findsNothing);
    expect(find.text('UDP 包'), findsOneWidget);
    expect(find.textContaining('packet-beta'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('renders requirement diagrams with native requirement view', (
    WidgetTester tester,
  ) async {
    const content = '''
```mermaid
requirementDiagram
requirement test_req {
id: 1
text: 测试需求文本
risk: high
verifymethod: test
}
element test_entity {
type: simulation
docref: reqs/doc1
}
test_entity - satisfies -> test_req
```
''';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidRequirementView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidFlowchartView), findsNothing);
    expect(find.text('test_req'), findsOneWidget);
    expect(find.text('test_entity'), findsOneWidget);
    expect(find.textContaining('satisfies'), findsOneWidget);
    expect(find.textContaining('requirementDiagram'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets(
    'renders class diagrams with native class view and zoom controls',
    (WidgetTester tester) async {
      const content = '''
```mermaid
classDiagram
class User {
  +String id
  +login()
}
class Session
User --> Session : creates
```
''';

      await tester.pumpWidget(buildParsedView(content));
      await tester.pumpAndSettle();

      expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
      expect(find.byType(ChatMarkdownMermaidClassView), findsOneWidget);
      expect(find.text('User'), findsOneWidget);
      expect(find.text('Session'), findsOneWidget);
      expect(find.textContaining('classDiagram'), findsNothing);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
    },
  );

  testWidgets('renders er diagrams with native er view and zoom controls', (
    WidgetTester tester,
  ) async {
    const content = '''
```mermaid
erDiagram
USER ||--o{ ORDER : places
```
''';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
    expect(find.byType(ChatMarkdownMermaidErView), findsOneWidget);
    expect(find.text('USER'), findsOneWidget);
    expect(find.text('ORDER'), findsOneWidget);
    expect(find.textContaining('erDiagram'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets(
    'renders mindmap diagrams with native mindmap view and zoom controls',
    (WidgetTester tester) async {
      const content = '''
```mermaid
mindmap
  root((项目))
    前端
    后端
```
''';

      await tester.pumpWidget(buildParsedView(content));
      await tester.pumpAndSettle();

      expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
      expect(find.byType(ChatMarkdownMermaidMindmapView), findsOneWidget);
      expect(find.textContaining('mindmap'), findsNothing);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
    },
  );

  testWidgets(
    'renders git graph diagrams with native git graph view and zoom controls',
    (WidgetTester tester) async {
      const content = '''
```mermaid
gitGraph
  commit id: "a1"
  branch feature
  checkout feature
  commit id: "b1"
  checkout main
  merge feature
```
''';

      await tester.pumpWidget(buildParsedView(content));
      await tester.pumpAndSettle();

      expect(find.byType(ChatMarkdownMermaidView), findsOneWidget);
      expect(find.byType(ChatMarkdownMermaidGitGraphView), findsOneWidget);
      expect(find.textContaining('gitGraph'), findsNothing);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
    },
  );

  testWidgets('renders task lists via native ast view', (
    WidgetTester tester,
  ) async {
    const content = '- [x] done\n- [ ] todo';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.text('done'), findsOneWidget);
    expect(find.text('todo'), findsOneWidget);
    expect(find.byIcon(Icons.check_box_rounded), findsOneWidget);
    expect(find.byIcon(Icons.check_box_outline_blank_rounded), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('renders footnotes via native ast view', (
    WidgetTester tester,
  ) async {
    const content = '[^1] note\n\n[^1]: footnote';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.textContaining('[1]'), findsOneWidget);
    expect(find.text('1.'), findsOneWidget);
    expect(find.textContaining('footnote'), findsWidgets);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('renders headings and emphasis via native ast view', (
    WidgetTester tester,
  ) async {
    const content = '# Hello\n\n**Bold** text';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.text('Hello'), findsOneWidget);
    expect(find.textContaining('Bold'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('renders external links via native ast view', (
    WidgetTester tester,
  ) async {
    const content = 'Visit [OpenAI](https://openai.com).';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    // 链接现在是段落 RichText 内的一个 TextSpan（不再是独立 Text 组件），
    // 所以用可点链接文案断言，而非 find.text('OpenAI')。
    expect(tappableLinkTexts(tester), contains('OpenAI'));

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('renders grix card links as chat cards in native ast view', (
    WidgetTester tester,
  ) async {
    const content = '''
现在路线，先创建远端 API agent。
[已创建远端 Agent](grix://card/egg_install_status?install_id=eggins_204216015836196864&status=running&step=agent_created&target_agent_id=2042126968270360576&summary=%E5%B7%B2%E5%88%9B%E5%BB%BA%E8%BF%9C%E7%AB%AF%20Agent)
远端 agent 已创建成功，开始下载 persona 包。
''';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.byType(ChatMessageCardView), findsOneWidget);
    expect(
      find.byKey(const Key('chat_message_card_egg_install_status')),
      findsOneWidget,
    );
    expect(tappableLinkSpans(tester), isEmpty);
    expect(find.textContaining('grix://card/'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('renders images via native ast view', (
    WidgetTester tester,
  ) async {
    const content = '![chart](https://example.com/chart.png)';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.byType(ChatMarkdownImageView), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets(
    'markdown image tap opens fullscreen preview with actions',
    (WidgetTester tester) async {
      const content = '![chart](https://example.com/chart.png)';

      await tester.pumpWidget(buildParsedView(content));
      await tester.pumpAndSettle();

      expect(find.byType(ChatMarkdownImageView), findsOneWidget);

      await tester.tap(find.byType(ChatMarkdownImageView));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));

      expect(
        find.byKey(const ValueKey('markdown_image_preview_dialog')),
        findsOneWidget,
      );
      expect(find.byType(ChatMarkdownZoomableImageViewport), findsOneWidget);
      expect(find.byTooltip('下载图片'), findsOneWidget);
      expect(find.byTooltip('关闭图片预览'), findsOneWidget);

      final dialogSize = tester.getSize(
        find.byKey(const ValueKey('markdown_image_preview_dialog')),
      );
      expect(
        dialogSize,
        tester.view.physicalSize / tester.view.devicePixelRatio,
      );

      final downloadCenter = tester.getCenter(
        find.byKey(const ValueKey('markdown_image_preview_download_button')),
      );
      final closeCenter = tester.getCenter(
        find.byKey(const ValueKey('markdown_image_preview_close_button')),
      );
      expect(downloadCenter.dx, lessThan(dialogSize.width / 2));
      expect(closeCenter.dx, greaterThan(dialogSize.width / 2));

      await tester.tap(find.byTooltip('关闭图片预览'));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));

      expect(
        find.byKey(const ValueKey('markdown_image_preview_dialog')),
        findsNothing,
      );

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
    },
  );

  testWidgets(
    'zoomable image viewport supports double tap reset and wheel zoom',
    (WidgetTester tester) async {
      final controller = TransformationController();

      await tester.pumpWidget(buildZoomableImageViewport(controller));
      await tester.pumpAndSettle();

      final viewportFinder = find.byType(ChatMarkdownZoomableImageViewport);
      expect(viewportFinder, findsOneWidget);
      expect(controller.value.getMaxScaleOnAxis(), 1);

      await performDoubleTap(tester, viewportFinder);

      expect(controller.value.getMaxScaleOnAxis(), greaterThan(2));

      await performDoubleTap(tester, viewportFinder);

      final translation = controller.value.getTranslation();
      expect(controller.value.getMaxScaleOnAxis(), closeTo(1, 0.001));
      expect(translation.x, closeTo(0, 0.001));
      expect(translation.y, closeTo(0, 0.001));

      final pointer = TestPointer(1, PointerDeviceKind.mouse);
      final position = tester.getCenter(viewportFinder);
      await tester.sendEventToBinding(pointer.hover(position));
      await tester.sendEventToBinding(pointer.scroll(const Offset(0, -120)));
      await tester.pump();

      expect(controller.value.getMaxScaleOnAxis(), greaterThan(1));

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
      controller.dispose();
    },
  );

  testWidgets(
    'image zoom controller steps scale within range and clamps at limits',
    (WidgetTester tester) async {
      final transform = TransformationController();
      final zoom = ChatMarkdownImageZoomController();

      await tester.pumpWidget(
        buildZoomableImageViewport(transform, zoomController: zoom),
      );
      await tester.pumpAndSettle();

      expect(zoom.isBound, isTrue);
      expect(zoom.isAtBaseScale, isTrue);
      // 默认状态允许向下缩小一个级别。
      expect(zoom.canZoomOut, isTrue);
      expect(zoom.canZoomIn, isTrue);

      zoom.zoomOut();
      await tester.pump();
      expect(transform.value.getMaxScaleOnAxis(), closeTo(0.625, 0.001));
      expect(zoom.isAtBaseScale, isFalse);
      // 已到最小档，不能继续缩小。
      expect(zoom.canZoomOut, isFalse);

      zoom.reset();
      await tester.pump();
      expect(transform.value.getMaxScaleOnAxis(), closeTo(1, 0.001));
      expect(zoom.isAtBaseScale, isTrue);
      expect(zoom.canZoomOut, isTrue);

      zoom.zoomIn();
      await tester.pump();
      expect(transform.value.getMaxScaleOnAxis(), greaterThan(1));
      expect(zoom.isAtBaseScale, isFalse);
      expect(zoom.canZoomOut, isTrue);

      for (var i = 0; i < 10; i++) {
        zoom.zoomIn();
        await tester.pump();
      }
      expect(transform.value.getMaxScaleOnAxis(), closeTo(10, 0.001));
      expect(zoom.canZoomIn, isFalse);

      zoom.reset();
      await tester.pump();
      expect(transform.value.getMaxScaleOnAxis(), closeTo(1, 0.001));
      expect(zoom.isAtBaseScale, isTrue);
      expect(zoom.canZoomOut, isTrue);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
      transform.dispose();
      zoom.dispose();
    },
  );

  testWidgets(
    'image zoom controller snaps back to identity when minScale is base scale',
    (WidgetTester tester) async {
      final transform = TransformationController();
      final zoom = ChatMarkdownImageZoomController();

      await tester.pumpWidget(
        buildZoomableImageViewport(transform, zoomController: zoom, minScale: 1),
      );
      await tester.pumpAndSettle();

      expect(zoom.isBound, isTrue);
      expect(zoom.canZoomOut, isFalse);

      zoom.zoomIn();
      await tester.pump();
      expect(transform.value.getMaxScaleOnAxis(), greaterThan(1));
      expect(zoom.isAtBaseScale, isFalse);

      // 缩回最小档（即原始比例）时吸附复位，同时清掉平移。
      zoom.zoomOut();
      await tester.pump();
      expect(transform.value.getMaxScaleOnAxis(), closeTo(1, 0.001));
      final translation = transform.value.getTranslation();
      expect(translation.x, closeTo(0, 0.001));
      expect(translation.y, closeTo(0, 0.001));
      expect(zoom.isAtBaseScale, isTrue);
      expect(zoom.canZoomOut, isFalse);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
      transform.dispose();
      zoom.dispose();
    },
  );

  testWidgets(
    'mermaid zoomable viewport steps zoom from a fit scale below 1',
    (WidgetTester tester) async {
      final zoom = ChatMarkdownMermaidZoomController();

      await tester.pumpWidget(
        MaterialApp(
          theme: AppTheme.lightTheme,
          home: Scaffold(
            body: Center(
              child: SizedBox(
                width: 320,
                height: 240,
                child: ChatMarkdownMermaidZoomableViewport(
                  viewportHeight: 240,
                  canvasSize: const Size(800, 600),
                  zoomController: zoom,
                  child: const ColoredBox(
                    color: Colors.blue,
                    child: SizedBox(width: 800, height: 600),
                  ),
                ),
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      // 画布大于视口，适配缩放为 0.4；读回的缩放必须反映真实值（回归：
      // z 轴固定为 1 时 getMaxScaleOnAxis 会把小于 1 的缩放读成 1）。
      expect(zoom.currentScale, closeTo(0.4, 0.001));
      expect(zoom.canZoomIn, isTrue);

      zoom.zoomIn();
      await tester.pump();
      expect(zoom.currentScale, closeTo(0.6, 0.001));

      zoom.zoomOut();
      await tester.pump();
      expect(zoom.currentScale, closeTo(0.4, 0.001));

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
      zoom.dispose();
    },
  );

  testWidgets(
    'viewport dismisses only on touch swipe down at base scale',
    (WidgetTester tester) async {
      final transform = TransformationController();
      var dismissed = 0;

      await tester.pumpWidget(
        buildZoomableImageViewport(transform, onDismiss: () => dismissed++),
      );
      await tester.pumpAndSettle();

      final viewportFinder = find.byType(ChatMarkdownZoomableImageViewport);

      await performDoubleTap(tester, viewportFinder);
      expect(transform.value.getMaxScaleOnAxis(), greaterThan(2));
      await tester.drag(viewportFinder, const Offset(0, 200));
      await tester.pump();
      expect(dismissed, 0);

      await performDoubleTap(tester, viewportFinder);
      expect(transform.value.getMaxScaleOnAxis(), closeTo(1, 0.001));

      await tester.drag(
        viewportFinder,
        const Offset(0, 200),
        kind: PointerDeviceKind.mouse,
      );
      await tester.pump();
      expect(dismissed, 0);

      await tester.drag(viewportFinder, const Offset(0, 200));
      await tester.pump();
      expect(dismissed, 1);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
      transform.dispose();
    },
  );

  testWidgets(
    'image preview exposes zoom controls that scale the viewport',
    (WidgetTester tester) async {
      const content = '![chart](https://example.com/chart.png)';

      await tester.pumpWidget(buildParsedView(content));
      await tester.pumpAndSettle();

      await tester.tap(find.byType(ChatMarkdownImageView));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));

      const zoomIn = ValueKey('markdown_image_preview_zoom_in');
      const zoomOut = ValueKey('markdown_image_preview_zoom_out');
      const zoomReset = ValueKey('markdown_image_preview_zoom_reset');
      expect(find.byKey(zoomIn), findsOneWidget);
      expect(find.byKey(zoomOut), findsOneWidget);
      expect(find.byKey(zoomReset), findsOneWidget);

      IconButton button(ValueKey key) => tester.widget<IconButton>(
            find.byKey(key),
          );
      // 默认状态允许向下缩小一个级别。
      expect(button(zoomOut).onPressed, isNotNull);

      await tester.tap(find.byKey(zoomOut));
      await tester.pump();

      expect(
        tester
            .widget<InteractiveViewer>(find.byType(InteractiveViewer))
            .transformationController!
            .value
            .getMaxScaleOnAxis(),
        lessThan(1),
      );

      await tester.tap(find.byKey(zoomReset));
      await tester.pump();

      await tester.tap(find.byKey(zoomIn));
      await tester.pump();

      final viewer = tester.widget<InteractiveViewer>(
        find.byType(InteractiveViewer),
      );
      expect(
        viewer.transformationController!.value.getMaxScaleOnAxis(),
        greaterThan(1),
      );
      expect(button(zoomOut).onPressed, isNotNull);

      await tester.tap(find.byTooltip('关闭图片预览'));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));
      expect(
        find.byKey(const ValueKey('markdown_image_preview_dialog')),
        findsNothing,
      );

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
    },
  );

  testWidgets(
    'image preview swipe down at base scale closes the dialog',
    (WidgetTester tester) async {
      const content = '![chart](https://example.com/chart.png)';

      await tester.pumpWidget(buildParsedView(content));
      await tester.pumpAndSettle();

      await tester.tap(find.byType(ChatMarkdownImageView));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));

      const dialogKey = ValueKey('markdown_image_preview_dialog');
      expect(find.byKey(dialogKey), findsOneWidget);

      await tester.drag(
        find.byType(ChatMarkdownZoomableImageViewport),
        const Offset(0, 260),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));

      expect(find.byKey(dialogKey), findsNothing);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
    },
  );

  testWidgets('renders tables via native ast view', (
    WidgetTester tester,
  ) async {
    const content =
        '| A | B |\n|:--|--:|\n| 1 | [OpenAI](https://openai.com) |';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.byType(ChatMarkdownTableView), findsOneWidget);
    expect(find.byType(Table), findsOneWidget);
    expect(find.byType(Scrollbar), findsOneWidget);
    expect(tappableLinkTexts(tester), contains('OpenAI'));
    expect(find.byTooltip('下载表格图片'), findsOneWidget);

    await tester.tap(find.byTooltip('下载表格图片'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));

    expect(
      find.byKey(const ValueKey('markdown_image_preview_dialog')),
      findsOneWidget,
    );
    expect(find.byTooltip('关闭图片预览'), findsOneWidget);

    await tester.tap(find.byTooltip('关闭图片预览'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));

    expect(
      find.byKey(const ValueKey('markdown_image_preview_dialog')),
      findsNothing,
    );

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('table export boundary spans the full scrollable table', (
    WidgetTester tester,
  ) async {
    final header = List.generate(
      6,
      (index) => '标题${index + 1}超长字段',
    ).join(' | ');
    final divider = List.filled(6, '---').join(' | ');
    final rows = List.generate(
      14,
      (rowIndex) => List.generate(
        6,
        (columnIndex) => '第${rowIndex + 1}行第${columnIndex + 1}列的长内容用于拉宽和拉高表格',
      ).join(' | '),
    ).map((row) => '| $row |').join('\n');
    final content = '| $header |\n| $divider |\n$rows';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    final viewportSize = tester.getSize(
      find.byKey(const ValueKey('markdown_table_viewport')),
    );
    final exportBoundarySize = tester.getSize(
      find.byKey(const ValueKey('markdown_table_export_boundary')),
    );

    expect(exportBoundarySize.width, greaterThan(viewportSize.width + 100));
    expect(exportBoundarySize.height, greaterThan(viewportSize.height + 100));

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('sequence diagram export boundary spans the full canvas', (
    WidgetTester tester,
  ) async {
    final buffer = StringBuffer('```mermaid\nsequenceDiagram\n');
    buffer.writeln('participant A as 调用方');
    buffer.writeln('participant B as 服务端');
    for (var i = 0; i < 40; i++) {
      buffer.writeln('A->>B: 第 $i 次请求');
      buffer.writeln('B-->>A: 第 $i 次响应');
    }
    buffer.writeln('```');

    await tester.pumpWidget(buildParsedView(buffer.toString()));
    await tester.pumpAndSettle();

    final viewSize = tester.getSize(
      find.byType(ChatMarkdownMermaidSequenceView),
    );
    final maxBoundaryHeight = _maxRepaintBoundaryHeight(
      tester,
      of: find.byType(ChatMarkdownMermaidSequenceView),
    );
    // 导出边界覆盖完整画布，应明显高于被裁切的视口高度。
    expect(maxBoundaryHeight, greaterThan(viewSize.height + 100));

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('gantt diagram export boundary spans the full content', (
    WidgetTester tester,
  ) async {
    final buffer = StringBuffer('```mermaid\ngantt\n');
    buffer.writeln('  title 导出测试');
    buffer.writeln('  dateFormat YYYY-MM-DD');
    buffer.writeln('  axisFormat %m-%d');
    buffer.writeln('  section 阶段一');
    buffer.writeln('  任务0 :t0, 2026-03-01, 1d');
    for (var i = 1; i < 16; i++) {
      buffer.writeln('  任务$i :t$i, after t${i - 1}, 1d');
    }
    buffer.writeln('```');

    await tester.pumpWidget(buildParsedView(buffer.toString()));
    await tester.pumpAndSettle();

    final viewSize = tester.getSize(
      find.byType(ChatMarkdownMermaidGanttView),
    );
    final maxBoundaryHeight = _maxRepaintBoundaryHeight(
      tester,
      of: find.byType(ChatMarkdownMermaidGanttView),
    );
    // 导出边界覆盖完整内容，应明显高于被裁切的视口高度。
    expect(maxBoundaryHeight, greaterThan(viewSize.height + 100));

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('zoomable image viewport supports mouse wheel and double tap', (
    WidgetTester tester,
  ) async {
    final controller = TransformationController();

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Center(
            child: SizedBox(
              width: 320,
              height: 240,
              child: ChatMarkdownZoomableImageViewport(
                transformationController: controller,
                child: Container(color: Colors.blue),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    final viewportFinder = find.byType(ChatMarkdownZoomableImageViewport);
    final center = tester.getCenter(viewportFinder);

    final pointer = TestPointer(1, PointerDeviceKind.mouse);
    await tester.sendEventToBinding(pointer.hover(center));
    await tester.sendEventToBinding(pointer.scroll(const Offset(0, -140)));
    await tester.pump();

    expect(controller.value.getMaxScaleOnAxis(), greaterThan(1));

    await tester.tapAt(center);
    await tester.pump(const Duration(milliseconds: 80));
    await tester.tapAt(center);
    await tester.pump(const Duration(milliseconds: 400));

    expect(controller.value.getMaxScaleOnAxis(), closeTo(1, 0.01));
  });

  testWidgets('renders <video> tag as a tappable player card', (
    WidgetTester tester,
  ) async {
    const content =
        '<video src="https://example.com/clip.mp4" controls width="640">'
        '</video>';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.byType(ChatMarkdownVideoView), findsOneWidget);
    expect(find.byIcon(Icons.play_arrow_rounded), findsOneWidget);
    // The raw tag text must not leak into the rendered output.
    expect(find.textContaining('<video'), findsNothing);

    await tester.tap(find.byType(ChatMarkdownVideoView));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 100));

    expect(find.byType(ChatMessageVideoPreviewDialog), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('renders <audio> tag as a player card with play control', (
    WidgetTester tester,
  ) async {
    const content =
        '<audio src="https://example.com/voice.mp3" title="语音" controls>'
        '</audio>';

    await tester.pumpWidget(buildParsedView(content));
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
    expect(find.byType(ChatMarkdownAudioView), findsOneWidget);
    expect(find.byIcon(Icons.play_arrow_rounded), findsOneWidget);
    // The raw tag text must not leak into the rendered output.
    expect(find.textContaining('<audio'), findsNothing);
  });
}
