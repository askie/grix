import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/shared/widgets/chat_markdown_view.dart';
import 'package:grix/shared/widgets/message_bubble.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    MessageStreamController.resetForTest();
    MessageBubble.resetFinalRenderCacheForTest();
  });

  tearDown(() {
    MessageStreamController.resetForTest();
    MessageBubble.resetFinalRenderCacheForTest();
  });

  Widget buildBubble(String content) {
    return MaterialApp(
      home: Scaffold(
        body: MessageBubble(
          msgId: 'dispatch_result_msg',
          initialContent: content,
          isStreaming: false,
          isMine: false,
        ),
      ),
    );
  }

  testWidgets('dispatch-result bubble renders the callback layout', (
    tester,
  ) async {
    const content = '''
[dispatch-result]
**status**:
```text
completed
```
**summary**:
```text
已完成改动
```
**detail**:
```text
测试通过
```
**session**:
```text
2e47d561-fd53-48a3-8d92-277cf1c49264
```
[/dispatch-result]
''';

    await tester.pumpWidget(buildBubble(content));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('chat_dispatch_result_bubble')), findsOneWidget);
    expect(find.byKey(const Key('chat_dispatch_result_card')), findsOneWidget);
    expect(
      find.byKey(const Key('chat_dispatch_result_status_pill')),
      findsOneWidget,
    );
    expect(find.textContaining('[dispatch-result]'), findsNothing);
    expect(find.textContaining('[/dispatch-result]'), findsNothing);
    expect(find.byType(ChatMarkdownView), findsNothing);
    expect(find.text('status'), findsNothing);
    expect(find.text('summary'), findsNothing);
    expect(find.text('detail'), findsNothing);
    expect(find.text('session'), findsNothing);
    expect(find.text('completed'), findsOneWidget);
    expect(find.text('已完成改动'), findsOneWidget);
    expect(find.text('测试通过'), findsOneWidget);
    expect(
      find.text('ID：  2e47d561-fd53-48a3-8d92-277cf1c49264'),
      findsOneWidget,
    );

    final summary = tester.widget<Text>(
      find.byKey(const Key('chat_dispatch_result_summary')),
    );
    final detail = tester.widget<Text>(
      find.byKey(const Key('chat_dispatch_result_detail')),
    );
    expect(summary.style?.fontWeight, FontWeight.w700);
    expect(detail.style?.fontWeight, isNot(FontWeight.w700));
  });

  testWidgets('dispatch-result bubble uses tinted chrome unlike plain bubble', (
    tester,
  ) async {
    const content = '''
[dispatch-result]
**status**: failed
**summary**: boom
[/dispatch-result]
''';

    await tester.pumpWidget(buildBubble(content));
    await tester.pumpAndSettle();

    final container = tester.widget<Container>(
      find.byKey(const Key('chat_dispatch_result_bubble')),
    );
    final decoration = container.decoration! as BoxDecoration;
    expect(
      decoration.color,
      AppTheme.successColor.withValues(alpha: 0.08),
    );
    expect(
      decoration.border?.top.color,
      AppTheme.successColor.withValues(alpha: 0.22),
    );
  });

  testWidgets('plain message keeps white bubble and does not match key', (
    tester,
  ) async {
    await tester.pumpWidget(buildBubble('普通消息'));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('chat_dispatch_result_bubble')), findsNothing);
  });
}
