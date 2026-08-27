import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_highlight/themes/a11y-dark.dart';
import 'package:flutter_highlight/themes/a11y-light.dart';
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

  Widget buildCodeBlockView({
    required String code,
    String? language,
    ThemeData? theme,
  }) {
    return MaterialApp(
      theme: theme ?? AppTheme.lightTheme,
      home: Builder(
        builder: (context) {
          final resolvedTheme = Theme.of(context);
          final styleSheet = ChatMarkdownStyleSheet.fromTheme(
            theme: resolvedTheme,
            textColor: resolvedTheme.colorScheme.onSurface,
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

  testWidgets('dark theme code block uses dark card background and text', (
    WidgetTester tester,
  ) async {
    const code = 'final value = 42;\nprint(value);\n';

    await tester.pumpWidget(
      buildCodeBlockView(code: code, language: 'dart', theme: AppTheme.darkTheme),
    );
    await tester.pump();

    expect(
      find.byWidgetPredicate((widget) {
        if (widget is! Container) {
          return false;
        }
        final decoration = widget.decoration;
        return decoration is BoxDecoration &&
            decoration.color == AppTheme.darkCard;
      }),
      findsOneWidget,
    );

    final label = tester.widget<Text>(find.text('dart'));
    expect(label.style?.color, AppTheme.darkTextSecondary);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  test('dark theme style sheet resolves dark code block colors', () {
    final styleSheet = ChatMarkdownStyleSheet.fromTheme(
      theme: AppTheme.darkTheme,
      textColor: AppTheme.darkTextPrimary,
      isMine: false,
    );

    expect(styleSheet.preBackgroundColor, AppTheme.darkCard);
    expect(styleSheet.preTextStyle.color, AppTheme.darkTextPrimary);
    expect(styleSheet.preLabelStyle.color, AppTheme.darkTextSecondary);
    expect(styleSheet.preSyntaxTheme, a11yDarkTheme);
  });

  test('light theme style sheet keeps existing light code block colors', () {
    final styleSheet = ChatMarkdownStyleSheet.fromTheme(
      theme: AppTheme.lightTheme,
      textColor: AppTheme.lightTextPrimary,
      isMine: false,
    );

    expect(styleSheet.preBackgroundColor, AppTheme.lightCard);
    expect(styleSheet.preTextStyle.color, AppTheme.lightTextPrimary);
    expect(styleSheet.preLabelStyle.color, AppTheme.lightTextSecondary);
    expect(styleSheet.preSyntaxTheme, a11yLightTheme);
  });
}
