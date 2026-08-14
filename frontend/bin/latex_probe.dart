import 'dart:io';

import 'package:grix/shared/markdown/chat_markdown_ast.dart';
import 'package:grix/shared/markdown/chat_markdown_dialect.dart';
import 'package:grix/shared/markdown/chat_markdown_normalizer.dart';

void main() {
  const raw = r'''给我发了一个Latex块的代码

$$% LaTeX 文档示例
\documentclass{article}
\usepackage{amsmath}
\usepackage{amssymb}

\title{数学公式示例}
\author{小龙虾}
\date{\today}

\begin{document}

\maketitle

\section{基础公式}

行内公式：质能方程 $E = mc^2$

独立公式：
\[
\int_{0}^{\infty} e^{-x^2} \, dx = \frac{\sqrt{\pi}}{2}
\]
''';

  const normalizer = ChatMarkdownNormalizer();
  final normalized = normalizer.normalizeForRendering(raw);
  stdout.writeln('--- normalized ---');
  stdout.writeln(normalized.text);

  final parser = ChatMarkdownDialect.buildParserAdapter();
  final doc = parser.parse(normalized.text);

  int mathBlock = 0;
  int mathInline = 0;
  int paragraph = 0;

  void walk(ChatMarkdownNode n) {
    if (n.type == ChatMarkdownNodeType.mathBlock) mathBlock++;
    if (n.type == ChatMarkdownNodeType.mathInline) mathInline++;
    if (n.type == ChatMarkdownNodeType.paragraph) paragraph++;
    for (final c in n.children) {
      walk(c);
    }
  }

  walk(doc);
  stdout.writeln(
    'mathBlock=$mathBlock mathInline=$mathInline paragraph=$paragraph',
  );
}
