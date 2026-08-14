import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/markdown/chat_markdown_latex_document_converter.dart';

void main() {
  const converter = ChatMarkdownLatexDocumentConverter();

  test('converts section commands and inline text styles', () {
    const input = r'''
\section{基础公式}
这是 \textbf{重点} 与 \emph{强调} 内容
\subsection{补充}
''';

    final output = converter.convert(input);

    expect(output, contains('## 基础公式'));
    expect(output, contains('这是 **重点** 与 *强调* 内容'));
    expect(output, contains('### 补充'));
  });

  test('removes document preamble commands and visualizes maketitle block', () {
    const input = r'''
% LaTeX 文档示例
\documentclass{article}
\usepackage{amsmath}
\title{数学公式示例}
\author{小龙虾}
\date{\today}
\begin{document}
\maketitle
正文内容
\end{document}
''';

    final output = converter.convert(input);

    expect(output, contains('LaTeX 文档示例'));
    expect(output, contains('# 数学公式示例'));
    expect(output, contains(RegExp(r'> 小龙虾 · \d{4}-\d{2}-\d{2}')));
    expect(output, contains('正文内容'));
    expect(output, isNot(contains(r'\documentclass{article}')));
    expect(output, isNot(contains(r'\usepackage{amsmath}')));
    expect(output, isNot(contains(r'\begin{document}')));
    expect(output, isNot(contains(r'\end{document}')));
    expect(output, isNot(contains(r'\maketitle')));
  });

  test('converts itemize and enumerate into markdown lists', () {
    const input = r'''
\begin{itemize}
\item 第一项
\item 第二项 \textbf{加粗}
\end{itemize}

\begin{enumerate}
\item 步骤一
\item 步骤二
\end{enumerate}
''';

    final output = converter.convert(input);

    expect(output, contains('- 第一项'));
    expect(output, contains('- 第二项 **加粗**'));
    expect(output, contains('1. 步骤一'));
    expect(output, contains('1. 步骤二'));
  });

  test('preserves math environments without inline markdown conversion', () {
    const input = r'''
\begin{align}
\textbf{x} + y &= 5 \\
2x - y &= 1
\end{align}
''';

    final output = converter.convert(input);

    expect(output, contains(r'\begin{align}'));
    expect(output, contains(r'\textbf{x} + y &= 5 \\'));
    expect(output, contains(r'\end{align}'));
    expect(output, isNot(contains('**x**')));
  });

  test(r'preserves \[ ... \] math blocks without command remapping', () {
    const input = r'''
\[
\int_{0}^{\infty} e^{-x^2} \, dx = \frac{\sqrt{\pi}}{2}
\]
''';

    final output = converter.convert(input);

    expect(output, contains(r'\['));
    expect(output, contains(r'\int_{0}^{\infty}'));
    expect(output, contains(r'\]'));
    expect(output, isNot(contains('**int**')));
  });

  test('supports nested inline style commands', () {
    const input = r'嵌套 \textbf{外层 \emph{内层}} 文本';

    final output = converter.convert(input);

    expect(output, '嵌套 **外层 *内层*** 文本');
  });

  test(r'unwraps $$ wrapped math environments into parseable form', () {
    const input = r'''
$$\begin{align}
x + y &= 5 \\
2x - y &= 1
\end{align}$$
''';

    final output = converter.convert(input);

    expect(output, contains(r'\begin{align}'));
    expect(output, contains(r'\end{align}'));
    expect(output, isNot(contains(r'$$\begin{align}')));
    expect(output, isNot(contains(r'\end{align}$$')));
  });

  test(r'handles $$% document wrappers while preserving content', () {
    const input = r'''
给我发了一个Latex块的代码

$$% LaTeX 文档示例
\documentclass{article}
\title{数学公式示例}
\begin{document}
\maketitle
正文
\end{document}
$$
''';

    final output = converter.convert(input);

    expect(output, contains('给我发了一个Latex块的代码'));
    expect(output, contains('LaTeX 文档示例'));
    expect(output, contains('# 数学公式示例'));
    expect(output, contains('正文'));
    expect(output, isNot(contains(r'$$%')));
    expect(output, isNot(contains('\n\$\$\n')));
  });

  test('normalizes escaped command prefixes and trailing comments', () {
    const input = r'''
\\documentclass{article} % class
\\usepackage{amsmath} % math
\\title{数学公式示例} % title
\\author{小龙虾}
\\date{\\today}
\\begin{document} % begin
\\maketitle % render title
\\section{方程组}
$$\\begin{align}
x + y &= 5 \\
2x - y &= 1
\\end{align}$$
\\end{document}
''';

    final output = converter.convert(input);

    expect(output, isNot(contains(r'\documentclass')));
    expect(output, isNot(contains(r'\\documentclass')));
    expect(output, isNot(contains(r'\usepackage')));
    expect(output, isNot(contains(r'\\usepackage')));
    expect(output, isNot(contains(r'\maketitle')));
    expect(output, isNot(contains(r'\\maketitle')));
    expect(output, contains(RegExp(r'> 小龙虾 · \d{4}-\d{2}-\d{2}')));
    expect(output, isNot(contains(r'\today')));
    expect(output, contains('# 数学公式示例'));
    expect(output, contains('## 方程组'));
    expect(output, contains(r'\begin{align}'));
    expect(output, contains(r'\end{align}'));
    expect(output, isNot(contains(r'$$\begin{align}')));
    expect(output, isNot(contains(r'\end{align}$$')));
  });
}
