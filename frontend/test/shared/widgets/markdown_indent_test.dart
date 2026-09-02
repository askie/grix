import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:markdown_widget/markdown_widget.dart';
import 'package:grix/shared/widgets/chat_markdown_ast_view.dart';
import 'package:grix/shared/widgets/chat_markdown_table_view.dart';
import 'package:grix/shared/widgets/message_bubble.dart';
import 'package:grix/app/themes/app_theme.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets(
    'MessageBubble ignores indented codeblock to parse tables correctly',
    (WidgetTester tester) async {
      const dataWithSpaces = "    | a | b |\n    |---|---|\n    | 1 | 2 |";

      await tester.runAsync(() async {
        await tester.pumpWidget(
          MaterialApp(
            theme: AppTheme.lightTheme,
            home: const Scaffold(
              body: MessageBubble(
                msgId: 'test1',
                initialContent: dataWithSpaces,
                isMine: false,
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();

        expect(find.byType(MarkdownWidget), findsNothing);
        expect(find.byType(ChatMarkdownAstView), findsOneWidget);
        expect(find.byType(ChatMarkdownTableView), findsOneWidget);
        await tester.pump(const Duration(milliseconds: 100));

        final tableFinder = find.byType(Table);
        expect(tableFinder, findsOneWidget);
      });
    },
  );
}
