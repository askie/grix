import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_math_fork/flutter_math.dart';
import 'package:markdown/markdown.dart' as md;
import 'package:markdown_widget/markdown_widget.dart';
import 'package:grix/shared/markdown/chat_markdown_latex_syntax.dart';
import 'package:grix/shared/widgets/chat_markdown_ast_view.dart';
import 'package:grix/shared/widgets/message_bubble.dart';
import 'package:grix/app/themes/app_theme.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Widget buildTestableWidget(String content) {
    return MaterialApp(
      theme: AppTheme.lightTheme,
      home: Scaffold(
        body: SingleChildScrollView(
          child: MessageBubble(
            msgId: 'latex_test',
            initialContent: content,
            isMine: false,
          ),
        ),
      ),
    );
  }

  // ------------------------------------------------------------------
  // Unit tests: LatexBlockSyntax AST parsing
  // ------------------------------------------------------------------
  group('LatexBlockSyntax', () {
    md.Document makeDoc() => md.Document(
          blockSyntaxes: [const LatexBlockSyntax()],
          inlineSyntaxes: [LatexInlineSyntax()],
          extensionSet: md.ExtensionSet.gitHubFlavored,
        );

    test('parses multiline block formula', () {
      final doc = makeDoc();
      final lines = r'''
$$
x = \frac{-b \pm \sqrt{b^2 - 4ac}}{2a}
$$
'''
          .trim()
          .split('\n');
      final nodes = doc.parseLines(lines);
      expect(nodes, isNotEmpty);
      final latexNode =
          nodes.whereType<md.Element>().where((e) => e.tag == 'latex-block');
      expect(latexNode, isNotEmpty,
          reason: 'Should produce a latex-block element');
      expect(latexNode.first.attributes['tex'], contains(r'\frac'));
    });

    test('parses single-line block formula', () {
      final doc = makeDoc();
      final lines = [r'$$ E = mc^2 $$'];
      final nodes = doc.parseLines(lines);
      final latexNode =
          nodes.whereType<md.Element>().where((e) => e.tag == 'latex-block');
      expect(latexNode, isNotEmpty);
      expect(latexNode.first.attributes['tex'], contains('E = mc^2'));
    });

    test(r'parses block formula with \[ ... \] delimiters', () {
      final doc = makeDoc();
      final lines = r'''
\[
\int_0^1 x^2 dx = \frac{1}{3}
\]
'''
          .trim()
          .split('\n');
      final nodes = doc.parseLines(lines);
      final latexNode =
          nodes.whereType<md.Element>().where((e) => e.tag == 'latex-block');
      expect(latexNode, isNotEmpty);
      expect(latexNode.first.attributes['tex'], contains(r'\int_0^1'));
    });

    test(r'parses $$\begin{align}...\end{align}$$ wrapped multiline formula',
        () {
      final doc = makeDoc();
      final lines = r'''
$$\begin{align}
x + y &= 5 \\
2x - y &= 1
\end{align}$$
'''
          .trim()
          .split('\n');
      final nodes = doc.parseLines(lines);
      final latexNode =
          nodes.whereType<md.Element>().where((e) => e.tag == 'latex-block');
      expect(latexNode, isNotEmpty);
      expect(latexNode.first.attributes['tex'], contains(r'\begin{align}'));
      expect(latexNode.first.attributes['tex'], contains(r'\end{align}'));
    });
  });

  group('LatexEnvironmentBlockSyntax', () {
    md.Document makeDoc() => md.Document(
          blockSyntaxes: [const LatexEnvironmentBlockSyntax()],
          inlineSyntaxes: [LatexInlineSyntax()],
          extensionSet: md.ExtensionSet.gitHubFlavored,
        );

    test(r'parses \begin{align}...\end{align} as a block formula', () {
      final doc = makeDoc();
      final lines = r'''
\begin{align}
x + y &= 5 \\
2x - y &= 1
\end{align}
'''
          .trim()
          .split('\n');
      final nodes = doc.parseLines(lines);
      final latexNode =
          nodes.whereType<md.Element>().where((e) => e.tag == 'latex-block');
      expect(latexNode, isNotEmpty);
      expect(latexNode.first.attributes['tex'], contains(r'\begin{align}'));
      expect(latexNode.first.attributes['tex'], contains(r'\end{align}'));
    });

    test(r'parses single-line \begin{equation}...\end{equation}', () {
      final doc = makeDoc();
      final lines = [r'\begin{equation}E = mc^2\end{equation}'];
      final nodes = doc.parseLines(lines);
      final latexNode =
          nodes.whereType<md.Element>().where((e) => e.tag == 'latex-block');
      expect(latexNode, isNotEmpty);
      expect(latexNode.first.attributes['tex'], contains(r'\begin{equation}'));
    });
  });

  // ------------------------------------------------------------------
  // Unit tests: LatexInlineSyntax AST parsing
  // ------------------------------------------------------------------
  group('LatexInlineSyntax', () {
    md.Document makeDoc() => md.Document(
          inlineSyntaxes: [LatexInlineSyntax()],
          extensionSet: md.ExtensionSet.gitHubFlavored,
        );

    test(r'parses inline $...$ formula', () {
      final doc = makeDoc();
      final lines = [r'The formula $E = mc^2$ is famous.'];
      final nodes = doc.parseLines(lines);
      // Inside a paragraph, we should find a latex-inline element
      final paragraph = nodes.whereType<md.Element>().firstWhere(
            (e) => e.tag == 'p',
            orElse: () => md.Element.empty('none'),
          );
      expect(paragraph.tag, 'p');
      final latexChildren = _findElements(paragraph, 'latex-inline');
      expect(latexChildren, isNotEmpty,
          reason: 'Should produce a latex-inline element inside paragraph');
    });

    test(r'parses inline $$...$$ display formula', () {
      final doc = makeDoc();
      final lines = [r'Inline display: $$\alpha + \beta$$'];
      final nodes = doc.parseLines(lines);
      final paragraph = nodes.whereType<md.Element>().firstWhere(
            (e) => e.tag == 'p',
            orElse: () => md.Element.empty('none'),
          );
      final latexChildren = _findElements(paragraph, 'latex-block');
      expect(latexChildren, isNotEmpty,
          reason: r'Should produce a latex-block for inline $$...$$');
    });

    test(r'does not match currency $100', () {
      final doc = makeDoc();
      final lines = [r'The price is $100 dollars.'];
      final nodes = doc.parseLines(lines);
      final paragraph = nodes.whereType<md.Element>().firstWhere(
            (e) => e.tag == 'p',
            orElse: () => md.Element.empty('none'),
          );
      final latexChildren = _findElements(paragraph, 'latex-inline');
      expect(latexChildren, isEmpty, reason: r'Should not match $100 as LaTeX');
    });

    test(r'parses inline \( ... \) formula', () {
      final doc = makeDoc();
      final lines = [r'Inline parenthesis: \(\alpha + \beta\)'];
      final nodes = doc.parseLines(lines);
      final paragraph = nodes.whereType<md.Element>().firstWhere(
            (e) => e.tag == 'p',
            orElse: () => md.Element.empty('none'),
          );
      final latexChildren = _findElements(paragraph, 'latex-inline');
      expect(latexChildren, isNotEmpty);
      expect(
          latexChildren.first.attributes['tex'], contains(r'\alpha + \beta'));
    });
  });

  // ------------------------------------------------------------------
  // Widget tests: normalization pipeline converts fenced latex to formula
  // ------------------------------------------------------------------
  group('LaTeX normalization pipeline', () {
    testWidgets('strips ```latex code block into formula',
        (WidgetTester tester) async {
      const raw = '```latex\nx = \\frac{1}{2}\n```';
      await tester.runAsync(() async {
        await tester.pumpWidget(buildTestableWidget(raw));
        await tester.pumpAndSettle();
        expect(find.byType(ChatMarkdownAstView), findsOneWidget);
        expect(find.byType(Math), findsWidgets);
      });
    });
  });

  // ------------------------------------------------------------------
  // Widget tests: LaTeX rendering via MessageBubble
  // ------------------------------------------------------------------
  group('LaTeX widget rendering', () {
    testWidgets('renders block-level formula with Math widget',
        (WidgetTester tester) async {
      const content = r'Look at this formula:'
          '\n\n'
          r'$$'
          '\n'
          r'x = \frac{-b \pm \sqrt{b^2 - 4ac}}{2a}'
          '\n'
          r'$$';

      await tester.runAsync(() async {
        await tester.pumpWidget(buildTestableWidget(content));
        await tester.pumpAndSettle();
        expect(find.byType(ChatMarkdownAstView), findsOneWidget);
        // Math.tex widget should be rendered
        expect(find.byType(Math), findsWidgets);
      });
    });

    testWidgets('renders inline formula with Math widget',
        (WidgetTester tester) async {
      const content = r'The energy is $E = mc^2$ according to Einstein.';

      await tester.runAsync(() async {
        await tester.pumpWidget(buildTestableWidget(content));
        await tester.pumpAndSettle();
        expect(find.byType(ChatMarkdownAstView), findsOneWidget);
        expect(find.byType(Math), findsWidgets);
      });
    });

    testWidgets('renders single-line block formula',
        (WidgetTester tester) async {
      const content = r'$$\int_0^1 x^2 dx = \frac{1}{3}$$';

      await tester.runAsync(() async {
        await tester.pumpWidget(buildTestableWidget(content));
        await tester.pumpAndSettle();
        expect(find.byType(ChatMarkdownAstView), findsOneWidget);
        expect(find.byType(Math), findsWidgets);
      });
    });

    testWidgets(r'renders single-line \[ ... \] display formula',
        (WidgetTester tester) async {
      const content = r'\[ y = \frac{5x^3 - 8}{2x^2} \qquad x > 0 \]';

      await tester.runAsync(() async {
        await tester.pumpWidget(buildTestableWidget(content));
        await tester.pumpAndSettle();
        expect(find.byType(ChatMarkdownAstView), findsOneWidget);
        expect(find.byType(Math), findsWidgets);
        expect(find.textContaining(r'\frac'), findsNothing);
      });
    });

    testWidgets(r'renders \[ ... \] embedded mid-paragraph as display formula',
        (WidgetTester tester) async {
      const content =
          r'inline \(C\) and display \[ \dfrac{\mathrm{d}y}{\mathrm{d}x} \] end.';

      await tester.runAsync(() async {
        await tester.pumpWidget(buildTestableWidget(content));
        await tester.pumpAndSettle();
        expect(find.byType(ChatMarkdownAstView), findsOneWidget);
        // 行内 \(C\) + 段落中间的显示公式 \[...\] 都应渲染为 Math
        expect(find.byType(Math), findsAtLeastNWidgets(2));
        expect(find.textContaining(r'\dfrac'), findsNothing);
      });
    });

    testWidgets('renders align environment without fallback raw text',
        (WidgetTester tester) async {
      const content = r'''
\begin{align}
  x + y &= 5 \\
  2x - y &= 1
\end{align}
''';

      await tester.runAsync(() async {
        await tester.pumpWidget(buildTestableWidget(content));
        await tester.pumpAndSettle();
        expect(find.byType(ChatMarkdownAstView), findsOneWidget);
        expect(find.byType(Math), findsWidgets);
        expect(find.textContaining(r'$$\begin{align}'), findsNothing);
      });
    });

    testWidgets('formula with unsupported command falls back gracefully',
        (WidgetTester tester) async {
      // Use a deliberately invalid TeX command
      const content = r'$$\invalidcommand{test}$$';

      await tester.runAsync(() async {
        await tester.pumpWidget(buildTestableWidget(content));
        await tester.pumpAndSettle();
        // Should not crash
        expect(find.byType(ChatMarkdownAstView), findsOneWidget);
      });
    });

    testWidgets('LaTeX mixed with table renders both correctly',
        (WidgetTester tester) async {
      const content = 'The formula \$E = mc^2\$ applies.\n\n'
          '| a | b |\n|---|---|\n| 1 | 2 |';

      await tester.runAsync(() async {
        await tester.pumpWidget(buildTestableWidget(content));
        await tester.pumpAndSettle();
        expect(find.byType(MarkdownWidget), findsNothing);
        expect(find.byType(ChatMarkdownAstView), findsOneWidget);
        // Both Math and Table should be present
        expect(find.byType(Math), findsWidgets);
        expect(find.byType(Table), findsOneWidget);
      });
    });

    testWidgets('plain text without LaTeX does not render Math widget',
        (WidgetTester tester) async {
      const content = 'Just a normal message without formulas.';

      await tester.runAsync(() async {
        await tester.pumpWidget(buildTestableWidget(content));
        await tester.pumpAndSettle();
        // Should be plain text, not markdown
        expect(find.byType(Math), findsNothing);
      });
    });

    testWidgets('latex source snippet still renders embedded formulas',
        (WidgetTester tester) async {
      const content = r'''
$$% LaTeX 文档示例
\documentclass{article}
\usepackage{amsmath}
\usepackage{amssymb}

\section{基础公式}
行内公式：质能方程 $E = mc^2$

独立公式：
\[
\int_{0}^{\infty} e^{-x^2} \, dx = \frac{\sqrt{\pi}}{2}
\]
''';

      await tester.runAsync(() async {
        await tester.pumpWidget(buildTestableWidget(content));
        await tester.pumpAndSettle();
        expect(find.byType(ChatMarkdownAstView), findsOneWidget);
        expect(find.byType(Math), findsWidgets);
        expect(find.textContaining(r'\documentclass{article}'), findsNothing);
        expect(find.text('基础公式'), findsOneWidget);
      });
    });
  });
}

/// Recursively find all [md.Element] descendants with a given tag.
List<md.Element> _findElements(md.Element root, String tag) {
  final results = <md.Element>[];
  for (final child in root.children ?? <md.Node>[]) {
    if (child is md.Element) {
      if (child.tag == tag) results.add(child);
      results.addAll(_findElements(child, tag));
    }
  }
  return results;
}
