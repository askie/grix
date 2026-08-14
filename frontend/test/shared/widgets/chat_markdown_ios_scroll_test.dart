import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/shared/markdown/chat_markdown_dialect.dart';
import 'package:grix/shared/markdown/chat_markdown_normalizer.dart';
import 'package:grix/shared/markdown/chat_markdown_pipeline.dart';
import 'package:grix/shared/markdown/chat_markdown_semantics.dart';
import 'package:grix/shared/widgets/chat_markdown_ast_view.dart';
import 'package:grix/shared/widgets/chat_markdown_table_view.dart';
import 'package:grix/shared/widgets/chat_markdown_view.dart';
import 'package:grix/shared/widgets/message_bubble.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  const longMarkdownSample = '''好，我来研究一下。好的，我来给你整理一下。

## 一、OpenClaw 内置的自动发帖能力

OpenClaw **已经有完整的 RPA 系统**，可以直接创建自动发帖任务。

### 1. 现有的 RPA 技能
- `rpa-intent-router` - 智能意图识别，你说“帮我发帖到微博”，它会自动调用相关技能
- `rpa-task-generator` - 生成发帖任务
- `rpa-workflow-generator` - 生成发帖工作流（多平台联动）
- `rpa-task-manager` - 管理和修改任务
- `rpa-workflow-manager` - 管理和修改工作流

### 2. 支持的自动化操作
- 打开网页（`openTab`）
- 点击按钮（`click`）
- 输入文本（`type`）
- 上传文件（`upload`）
- 抓取数据（`scrape`）
- 循环执行（`loop`）
- 条件判断（`condition`）

**理论上，任何网站的发帖都可以自动化。**

---

## 二、DHF Bee Agent 已有的发帖任务

从你的网站看，**已经有现成的发帖任务**：

| 平台 | 任务类型 | 功能 |
| --- | --- | --- |
| 微博 | Task | 分享到 weibo.com |
| 知乎 | Workflow | 回答后同步摘要 |
| 快手 | Task | 上传发布视频 |
| 抖音 | Task | 上传发布视频 |
| 头条 | Task | 上传发布视频 |
| 掘金 | Task | 发布文章 |
| 小红书 | Workflow | 发布视频笔记 |

**这些都可以直接用！**

---

## 三、如何找到可靠的技能

### 1. ClawHub 技能市场（官方）
```bash
# 搜索技能
npx clawhub@latest search "<关键词>"

# 安装技能
npx clawhub@latest install "<skill-name>"
```

网站：https://clawhub.ai

### 2. 判断技能是否靠谱
1. 看是否有清晰的说明文档。
2. 看最近更新时间和维护频率。
3. 先在测试账号里跑一遍流程。
4. 确认输入输出和权限边界。
''';

  final pipeline = ChatMarkdownPipeline(
    normalizer: const ChatMarkdownNormalizer(),
    parser: ChatMarkdownDialect.buildParserAdapter(),
  );
  final parsed = pipeline.prepareFinalRender(longMarkdownSample);

  Widget buildScrollableMarkdownPage({
    ScrollController? controller,
  }) {
    return MaterialApp(
      theme: AppTheme.lightTheme,
      home: Scaffold(
        body: ListView(
          controller: controller,
          children: [
            const SizedBox(height: 120),
            Padding(
              padding: const EdgeInsets.all(16),
              child: ChatMarkdownView(
                data: parsed.normalizedText,
                textColor: Colors.black,
                isMine: false,
                document: parsed.document,
                semantics: parsed.semantics,
              ),
            ),
            const SizedBox(height: 1200),
          ],
        ),
      ),
    );
  }

  Widget buildScrollableBubblePage({
    ScrollController? controller,
  }) {
    return MaterialApp(
      theme: AppTheme.lightTheme,
      home: Scaffold(
        body: ListView(
          controller: controller,
          children: const [
            SizedBox(height: 120),
            MessageBubble(
              msgId: 'long_markdown_bubble',
              initialContent: longMarkdownSample,
              isMine: false,
            ),
            SizedBox(height: 1200),
          ],
        ),
      ),
    );
  }

  testWidgets('long markdown sample stays on native ast path', (
    WidgetTester tester,
  ) async {
    expect(parsed.shouldUseMarkdown, isTrue);
    expect(parsed.document, isNotNull);
    expect(parsed.semantics?.hasFeature(ChatMarkdownFeature.table), isTrue);

    await tester.pumpWidget(buildScrollableMarkdownPage());
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownAstView), findsOneWidget);
  });

  testWidgets(
    'ios can drag parent list when gesture starts on long markdown content',
    (WidgetTester tester) async {
      final controller = ScrollController();
      addTearDown(controller.dispose);

      await tester
          .pumpWidget(buildScrollableMarkdownPage(controller: controller));
      await tester.pumpAndSettle();

      expect(controller.offset, 0);

      final dragStart = tester.getTopLeft(find.byType(ChatMarkdownView)) +
          const Offset(40, 40);
      await tester.dragFrom(dragStart, const Offset(0, -300));
      await tester.pumpAndSettle();

      expect(controller.offset, greaterThan(0));
    },
    variant: TargetPlatformVariant.only(TargetPlatform.iOS),
  );

  testWidgets(
    'ios can drag parent list when gesture starts on long markdown bubble',
    (WidgetTester tester) async {
      final controller = ScrollController();
      addTearDown(controller.dispose);

      await tester
          .pumpWidget(buildScrollableBubblePage(controller: controller));
      await tester.pumpAndSettle();

      expect(controller.offset, 0);
      expect(find.byType(ChatMarkdownAstView), findsOneWidget);

      final dragStart =
          tester.getTopLeft(find.byType(MessageBubble)) + const Offset(60, 80);
      await tester.dragFrom(dragStart, const Offset(0, -300));
      await tester.pumpAndSettle();

      expect(controller.offset, greaterThan(0));
    },
    variant: TargetPlatformVariant.only(TargetPlatform.iOS),
  );

  testWidgets(
    'ios can drag parent list when gesture starts on table inside long markdown bubble',
    (WidgetTester tester) async {
      final controller = ScrollController();
      addTearDown(controller.dispose);

      await tester
          .pumpWidget(buildScrollableBubblePage(controller: controller));
      await tester.pumpAndSettle();

      expect(find.byType(ChatMarkdownTableView), findsOneWidget);
      await tester.ensureVisible(find.byType(ChatMarkdownTableView));
      await tester.pumpAndSettle();

      final beforeOffset = controller.offset;
      expect(beforeOffset, greaterThan(0));

      final dragStart = tester.getTopLeft(find.byType(ChatMarkdownTableView)) +
          const Offset(80, 80);
      await tester.dragFrom(dragStart, const Offset(0, -300));
      await tester.pumpAndSettle();

      expect(controller.offset, greaterThan(beforeOffset));
    },
    variant: TargetPlatformVariant.only(TargetPlatform.iOS),
  );
}
