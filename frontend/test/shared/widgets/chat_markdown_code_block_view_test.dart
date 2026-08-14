import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/shared/widgets/chat_markdown_code_block_view.dart';
import 'package:grix/shared/widgets/chat_markdown_fallback_view.dart';
import 'package:grix/shared/widgets/chat_markdown_style_sheet.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  String? clipboardText;

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
          return null;
        });
  });

  tearDown(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, null);
  });

  Widget buildCodeBlockView({required String code, String? language}) {
    return MaterialApp(
      theme: AppTheme.lightTheme,
      home: Builder(
        builder: (context) {
          final theme = Theme.of(context);
          final styleSheet = ChatMarkdownStyleSheet.fromTheme(
            theme: theme,
            textColor: theme.colorScheme.onSurface,
            isMine: false,
          );
          return Scaffold(
            body: ChatMarkdownCodeBlockView(
              code: code,
              language: language,
              styleSheet: styleSheet,
            ),
          );
        },
      ),
    );
  }

  Widget buildFallbackView(String data) {
    return MaterialApp(
      theme: AppTheme.lightTheme,
      home: Builder(
        builder: (context) {
          final theme = Theme.of(context);
          final styleSheet = ChatMarkdownStyleSheet.fromTheme(
            theme: theme,
            textColor: theme.colorScheme.onSurface,
            isMine: false,
          );
          return Scaffold(
            body: ChatMarkdownFallbackView(data: data, styleSheet: styleSheet),
          );
        },
      ),
    );
  }

  testWidgets('code block copy button copies the full code payload', (
    WidgetTester tester,
  ) async {
    const code = 'final value = 42;\nprint(value);\n';

    await tester.pumpWidget(buildCodeBlockView(code: code, language: 'dart'));
    await tester.pump();

    expect(find.byType(ChatMarkdownCodeBlockView), findsOneWidget);
    expect(find.byIcon(Icons.copy_rounded), findsOneWidget);
    expect(find.text('dart'), findsOneWidget);
    expect(
      find.textContaining('final value = 42;', findRichText: true),
      findsOneWidget,
    );

    await tester.tap(find.byIcon(Icons.copy_rounded));
    await tester.pump();

    expect(clipboardText, code);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('fallback markdown pre blocks reuse shared code block view', (
    WidgetTester tester,
  ) async {
    const markdown = '```text\n  keep leading spaces\nline 2\n```';

    await tester.pumpWidget(buildFallbackView(markdown));
    await tester.pumpAndSettle();

    expect(find.byType(ChatMarkdownCodeBlockView), findsOneWidget);
    expect(find.byIcon(Icons.copy_rounded), findsOneWidget);
    expect(find.text('text'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });
}
