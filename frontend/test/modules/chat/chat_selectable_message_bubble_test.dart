import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/modules/chat/widgets/chat_selectable_message_bubble.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_view.dart';
import 'package:grix/shared/widgets/message_bubble.dart';

void main() {
  Widget buildBubble({required VoidCallback onLongPress}) {
    return MaterialApp(
      theme: AppTheme.lightTheme,
      home: Scaffold(
        body: Center(
          child: SizedBox(
            width: 360,
            child: ChatSelectableMessageBubble(
              isMine: false,
              selectionMode: false,
              selected: false,
              onLongPress: onLongPress,
              child: const ChatMarkdownMermaidView(
                source: 'flowchart TD\nA[开始] --> B[结束]',
                textStyle: TextStyle(fontSize: 14, color: Color(0xFF2A2214)),
                backgroundColor: Color(0xFFF7F4EC),
                decoration: null,
                padding: EdgeInsets.zero,
              ),
            ),
          ),
        ),
      ),
    );
  }

  testWidgets('mermaid viewport does not bubble long press to message menu', (
    WidgetTester tester,
  ) async {
    var menuOpened = false;

    await tester.pumpWidget(
      buildBubble(
        onLongPress: () {
          menuOpened = true;
        },
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byType(InteractiveViewer), findsOneWidget);

    await tester.longPress(find.byType(InteractiveViewer));
    await tester.pumpAndSettle();

    expect(menuOpened, isFalse);
  });

  testWidgets(
      'selection indicator sits above content top so it does not cover text',
      (WidgetTester tester) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.lightTheme,
        home: Scaffold(
          body: Center(
            child: SizedBox(
              width: 360,
              child: ChatSelectableMessageBubble(
                isMine: false,
                selectionMode: true,
                selected: true,
                child: Container(
                  padding: const EdgeInsets.fromLTRB(10, 8, 10, 8),
                  child: const Text('Hello world from peer'),
                ),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    final indicator = find.byIcon(Icons.check_rounded);
    final text = find.text('Hello world from peer');
    expect(indicator, findsOneWidget);
    expect(text, findsOneWidget);

    // 选择框应坐在气泡内容上方（顶角外缘），其顶部高于首行文字顶部，不再压住文字。
    expect(
      tester.getRect(indicator).top < tester.getRect(text).top,
      isTrue,
      reason: 'selection indicator must be above the first text line',
    );
  });

  testWidgets('message bubble long press still reaches message menu by default',
      (WidgetTester tester) async {
    var menuOpened = false;

    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.lightTheme,
        home: Scaffold(
          body: Center(
            child: SizedBox(
              width: 360,
              child: ChatSelectableMessageBubble(
                isMine: false,
                selectionMode: false,
                selected: false,
                onLongPress: () {
                  menuOpened = true;
                },
                child: const MessageBubble(
                  msgId: 'long_press_default_menu',
                  initialContent: '# Title\n\nFirst paragraph.',
                  isMine: false,
                ),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Title'), findsOneWidget);

    await tester.longPress(find.text('Title'));
    await tester.pumpAndSettle();

    expect(menuOpened, isTrue);
  });
}
