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

  testWidgets('dispatch-result bubble strips tags and renders markdown', (
    tester,
  ) async {
    const content = '''
[dispatch-result]
**status**: completed
**summary**: 已完成改动
[/dispatch-result]
''';

    await tester.pumpWidget(buildBubble(content));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('chat_dispatch_result_bubble')), findsOneWidget);
    expect(find.textContaining('[dispatch-result]'), findsNothing);
    expect(find.textContaining('[/dispatch-result]'), findsNothing);
    expect(find.byType(ChatMarkdownView), findsOneWidget);
    expect(find.textContaining('completed'), findsWidgets);
    expect(find.textContaining('已完成改动'), findsWidgets);
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
