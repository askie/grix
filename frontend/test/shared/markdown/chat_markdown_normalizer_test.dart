import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/markdown/chat_markdown_normalizer.dart';

void main() {
  const normalizer = ChatMarkdownNormalizer();

  test('converts fenced latex blocks into block math', () {
    const raw = '```latex\nx = \\frac{1}{2}\n```';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, contains('\$\$'));
    expect(result.text, contains(r'\frac{1}{2}'));
    expect(result.text, isNot(contains('```latex')));
  });

  test('unwraps latex source fences with embedded formulas', () {
    const raw = r'''```latex
$$% LaTeX 文档示例
\documentclass{article}
\usepackage{amsmath}
\title{数学公式示例}
\author{小龙虾}
\date{\today}
\begin{document}
\maketitle
行内公式：$E = mc^2$
\section{基础公式}
文本 \textbf{加粗}
\begin{itemize}
\item 项目一
\item 项目二
\end{itemize}
\[
\int_{0}^{\infty} e^{-x^2} \, dx
\]
\begin{align}
x + y &= 5 \\
2x - y &= 1
\end{align}
\end{document}
$$
```''';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, isNot(contains('```latex')));
    expect(result.text.trimLeft().startsWith('LaTeX 文档示例'), isTrue);
    expect(result.text, contains('# 数学公式示例'));
    expect(result.text, contains(RegExp(r'> 小龙虾 · \d{4}-\d{2}-\d{2}')));
    expect(result.text, isNot(contains(r'\documentclass{article}')));
    expect(result.text, isNot(contains(r'\usepackage{amsmath}')));
    expect(result.text, isNot(contains(r'\maketitle')));
    expect(result.text, isNot(contains(r'\begin{document}')));
    expect(result.text, isNot(contains(r'\end{document}')));
    expect(result.text, contains(r'$E = mc^2$'));
    expect(result.text, contains('## 基础公式'));
    expect(result.text, contains('文本 **加粗**'));
    expect(result.text, contains('- 项目一'));
    expect(result.text, contains(r'\['));
    expect(result.text, isNot(contains(r'$$\begin{align}')));
    expect(result.text, isNot(contains(r'\end{align}$$')));
    expect(result.text, contains(r'\begin{align}'));
    expect(result.text, contains(r'\end{align}'));
  });

  test('converts real-world latex fenced message with trailing plain text', () {
    const raw = r'''```latex
% LaTeX 文档示例
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
  \int_{0}^{\infty} e^{-x^2} dx = \frac{\sqrt{\pi}}{2}
\]

\section{矩阵}

\[
  A = \begin{pmatrix}
    a_{11} & a_{12} & a_{13} \\
    a_{21} & a_{22} & a_{23} \\
    a_{31} & a_{32} & a_{33}
  \end{pmatrix}
\]

\section{求和与极限}

\[
  \sum_{i=1}^{n} i = \frac{n(n+1)}{2}
\]

\[
  \lim_{x \to 0} \frac{\sin x}{x} = 1
\]

\section{分段函数}

\[
  f(x) = \begin{cases}
    x^2 & \text{if } x \geq 0 \\
    -x & \text{if } x < 0
  \end{cases}
\]

\section{方程组}

\begin{align}
  x + y &= 5 \\
  2x - y &= 1
\end{align}

\end{document}
```

需要更具体的类型吗？比如表格、图形、定理环境等？''';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, contains('# 数学公式示例'));
    expect(result.text, contains('## 基础公式'));
    expect(result.text, contains('## 方程组'));
    expect(result.text, contains(r'\begin{align}'));
    expect(result.text, contains(r'\end{align}'));
    expect(result.text, contains('需要更具体的类型吗？'));
    expect(result.text, isNot(contains(r'\documentclass{article}')));
    expect(result.text, isNot(contains(r'\usepackage{amsmath}')));
    expect(result.text, isNot(contains(r'\maketitle')));
    expect(result.text, isNot(contains(r'\begin{document}')));
    expect(result.text, isNot(contains(r'\end{document}')));
    expect(result.text, isNot(contains('```latex')));
  });

  test('does not auto-close non-standalone \$\$ markers', () {
    const raw = '\$\$% LaTeX source\n\\documentclass{article}';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, contains('LaTeX source'));
    expect(result.text, isNot(contains(r'\documentclass{article}')));
    expect(result.text, isNot(endsWith('\n\$\$')));
  });

  test('converts non-fenced latex document snippets in text segments', () {
    const raw = r'''给我发了一个Latex块的代码

$$% LaTeX 文档示例
\documentclass{article}
\title{数学公式示例}
\begin{document}
\maketitle
\section{基础公式}
行内公式：$E = mc^2$
\end{document}
$$''';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, contains('给我发了一个Latex块的代码'));
    expect(result.text, contains('LaTeX 文档示例'));
    expect(result.text, contains('# 数学公式示例'));
    expect(result.text, contains('## 基础公式'));
    expect(result.text, contains(r'$E = mc^2$'));
    expect(result.text, isNot(contains(r'\documentclass{article}')));
    expect(result.text, isNot(contains(r'\begin{document}')));
    expect(result.text, isNot(contains(r'\end{document}')));
    expect(result.text, isNot(contains(r'$$%')));
  });

  test('converts escaped non-fenced latex document snippets', () {
    const raw = r'''给我发了一个Latex块的代码

$$% LaTeX 文档示例
\\documentclass{article} % comment
\\usepackage{amsmath}
\\title{数学公式示例}
\\author{小龙虾}
\\date{\\today}
\\begin{document}
\\maketitle
\\section{方程组}
$$\\begin{align}
x + y &= 5 \\
2x - y &= 1
\\end{align}$$
\\end{document}
$$''';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, contains('LaTeX 文档示例'));
    expect(result.text, contains('# 数学公式示例'));
    expect(result.text, contains(RegExp(r'> 小龙虾 · \d{4}-\d{2}-\d{2}')));
    expect(result.text, contains('## 方程组'));
    expect(result.text, contains(r'\begin{align}'));
    expect(result.text, contains(r'\end{align}'));
    expect(result.text, isNot(contains(r'\documentclass{article}')));
    expect(result.text, isNot(contains(r'\\documentclass{article}')));
    expect(result.text, isNot(contains(r'\usepackage{amsmath}')));
    expect(result.text, isNot(contains(r'\\usepackage{amsmath}')));
    expect(result.text, isNot(contains(r'\maketitle')));
    expect(result.text, isNot(contains(r'\\maketitle')));
    expect(result.text, isNot(contains(r'\today')));
    expect(result.text, isNot(contains(r'$$\begin{align}')));
    expect(result.text, isNot(contains(r'\end{align}$$')));
  });

  test('does not rewrite latex signals inside non-latex fenced code', () {
    const raw = '```bash\n\\documentclass{article}\necho ok\n```';

    final result = normalizer.normalizeForFinalRender(raw);

    expect(result.text, raw);
  });

  test('unwraps table-like markdown code blocks', () {
    const raw = '```markdown\n| a | b |\n|---|---|\n| 1 | 2 |\n```';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, contains('| a | b |'));
    expect(result.text, isNot(contains('```markdown')));
  });

  test(
    'repairs loose table separators and full-width pipes in markdown fences',
    () {
      const raw = '```markdown\n｜ 名称 ｜ 数量 ｜\n｜ -- ｜ == ｜\n｜ 苹果 ｜ 3 ｜\n```';

      final result = normalizer.normalizeForRendering(raw);

      expect(result.text, contains('| 名称 | 数量 |'));
      expect(result.text, contains('| --- | --- |'));
      expect(result.text, contains('| 苹果 | 3 |'));
      expect(result.text, isNot(contains('```markdown')));
      expect(result.text, isNot(contains('｜')));
    },
  );

  test('unwraps accidental table fences when the closer is glued to rows', () {
    const raw = '**常见方案：**\n\n'
        '```\n'
        '| 方案 | 原理 |\n'
        '| --- | --- |\n'
        '```| 内网穿透 | frp |\n'
        '| 云端中继 | relay |---\n'
        '后续说明\n';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, isNot(contains('```')));
    expect(result.text, contains('| 方案 | 原理 |'));
    expect(result.text, contains('| 内网穿透 | frp |'));
    expect(result.text, contains('| 云端中继 | relay |'));
    expect(result.text, contains('\n\n---\n后续说明'));
  });

  test('inserts blank line after normalized table before trailing prose', () {
    const raw = '```\n'
        '| 方案 | 原理 |\n'
        '| --- | --- |\n'
        '```| 内网穿透 | frp |\n'
        '| 云端中继 | relay |\n'
        '后续说明';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, contains('| 云端中继 | relay |\n\n后续说明'));
  });

  test('does not unwrap mermaid blocks as tables', () {
    const raw = '```mermaid\ngraph TD\nA --> B\n```';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, raw);
  });

  test('splits inline fence opener after prose into standalone block', () {
    const raw = '好，那用流程图：```mermaid\nflowchart TD\nA[开始] --> B[结束]\n```';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, contains('好，那用流程图：\n```mermaid'));
    expect(result.text, contains('flowchart TD'));
  });

  test('does not treat literal inline fence prose as a code block opener', () {
    const raw = '这里是字面量 ```json，不是代码块，只是说明。\n下一行还是正文';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, raw);
  });

  test('splits closing fence from trailing prose on the same line', () {
    const raw = '```text\nhello\n```后续说明';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, '```text\nhello\n```\n后续说明');
  });

  test('unwraps markdown fence that only wraps a mermaid block', () {
    const raw =
        '````markdown\n```mermaid\nflowchart TD\nA[开始] --> B[结束]\n```\n````';

    final result = normalizer.normalizeForRendering(raw);

    expect(
      result.text.trim(),
      '```mermaid\nflowchart TD\nA[开始] --> B[结束]\n```',
    );
  });

  test('recognizes escaped mermaid fence markers', () {
    const raw = r'好，那用流程图：\`\`\`mermaid'
        '\nflowchart TD'
        '\nA[开始] --> B[结束]'
        '\n```';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, contains('好，那用流程图：\n```mermaid'));
    expect(result.text.trimRight(), endsWith('```'));
  });

  test('unwraps quoted pure tables but preserves mixed blockquotes', () {
    const pureTable = '> | a | b |\n> |---|---|\n> | 1 | 2 |';
    const mixedQuote = '> summary\n> | a | b |\n> |---|---|';

    final pureResult = normalizer.normalizeForRendering(pureTable);
    final mixedResult = normalizer.normalizeForRendering(mixedQuote);

    expect(pureResult.text, isNot(contains('> | a | b |')));
    expect(mixedResult.text, contains('> summary'));
  });

  test('unwraps quoted tables with loose separators and normalizes syntax', () {
    const raw = '> ｜ a ｜ b ｜\n> ｜ -- ｜ == ｜\n> ｜ 1 ｜ 2 ｜';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, '| a | b |\n| --- | --- |\n| 1 | 2 |');
    expect(result.text, isNot(contains('>')));
  });

  test('normalizes loose separator alignment markers in plain table text', () {
    const raw = '| A | B |\n| -- | ==: |\n| 1 | 2 |';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, '| A | B |\n| --- | ---: |\n| 1 | 2 |');
  });

  test('trims dangling separator fragments at end of table delimiter rows', () {
    const raw = '| 功能 | 状态 | 优先级 | 负责人 | 预计完成 |\n'
        '|:-----|:----:|:------:|:-------:|--------:|-\n'
        '| 用户登录 | ✅ 已完成 | 🔴 高 | 张三 | 2025-01-15 |';

    final result = normalizer.normalizeForRendering(raw);

    expect(
      result.text,
      '| 功能 | 状态 | 优先级 | 负责人 | 预计完成 |\n'
      '| :--- | :---: | :---: | :---: | ---: |\n'
      '| 用户登录 | ✅ 已完成 | 🔴 高 | 张三 | 2025-01-15 |',
    );
  });

  test('synthesizes missing separator for high-confidence table blocks', () {
    const raw = '| name | score |\n| alice | 90 |\n| bob | 88 |';

    final result = normalizer.normalizeForRendering(raw);

    expect(
      result.text,
      '| name | score |\n| --- | --- |\n| alice | 90 |\n| bob | 88 |',
    );
  });

  test('keeps short pipe prose unchanged when separator is missing', () {
    const raw = 'A | B\nC | D';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, raw);
  });

  test('splits inline ordered lists after clause prefixes', () {
    const raw = '确认一下：1. 是所有浏览器都有这个问题，还是特定浏览器？\n'
        '2. "记住账号"有没有生效？';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, '确认一下：\n1. 是所有浏览器都有这个问题，还是特定浏览器？\n2. "记住账号"有没有生效？');
  });

  test('keeps fraction-like digits inside ordered list items intact', () {
    const raw = '**两条副本告警：**\n'
        '1. 08-20 09:33 — CN 集群 aibot-postgres-ro 副本异常(2/1)\n'
        '2. 08-25 02:43 — CN 集群 aibot-api 副本未满(1/2)，期望2个只有1个';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, raw);
  });

  test('keeps table row intact when cell contains time-like colon prefix', () {
    const raw = '| 项目 | 值 |\n'
        '|------|-----|\n'
        '| 版本 | **1.4.2** |\n'
        '| commit | `712b95e` (2026-06-23 17:48) |\n'
        '| 网关 | 已重启 |\n';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, contains('17:48) |'));
    expect(result.text, isNot(contains('17: |')));
    expect(result.text, isNot(contains('\n48)')));
  });

  test('keeps table row intact when cell contains Chinese lead-in colon', () {
    const raw = '| 项目 | 值 |\n'
        '|------|-----|\n'
        '| 备注 | 看下面：1. 重启 2. 重启 |\n'
        '| 网关 | 已重启 |\n';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, contains('看下面：1. 重启 2. 重启 |'));
    expect(result.text, contains('| 网关 | 已重启 |'));
    expect(result.text, isNot(contains('\n1. 重启')));
  });

  test('keeps table row intact when cell contains English lead-in colon', () {
    const raw = '| key | val |\n'
        '|-----|-----|\n'
        '| note | step: 1. reboot |\n'
        '| done | yes |\n';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, contains('step: 1. reboot |'));
    expect(result.text, contains('| done | yes |'));
    expect(result.text, isNot(contains('\n1. reboot')));
  });

  test('preserves alignment from short delimiter markers per GFM spec', () {
    const raw = '| a | b | c |\n|:--|--:|:-:|\n| 1 | 2 | 3 |\n';

    final result = normalizer.normalizeForRendering(raw);

    // Alignment should be preserved (normalizer may canonicalize length).
    expect(result.text, matches(RegExp(r':-+[^:]')));
    expect(result.text, matches(RegExp(r'[^:]-+:')));
    expect(result.text, matches(RegExp(r':-+:')));
    expect(result.text, isNot(contains('| --- | --- | --- |')));
  });

  test('recognizes single-dash delimiter row per GFM spec', () {
    const raw = '| a | b |\n|-|-|\n| 1 | 2 |\n';

    final result = normalizer.normalizeForRendering(raw);
    final lines = result.text.split('\n').where((l) => l.isNotEmpty).toList();

    expect(lines.length, 3);
    expect(lines[0], '| a | b |');
    expect(lines[2], '| 1 | 2 |');
  });

  test('keeps dash-only placeholder data row', () {
    const raw = '| 项目 | 状态 |\n|------|------|\n| - | - |\n| ok | done |\n';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, contains('| - | - |'));
    expect(result.text, contains('| ok | done |'));
  });

  test('keeps inline code pipe inside table cell unsplit', () {
    const raw = '| 用法 | 示例 | 说明 |\n'
        '|------|------|------|\n'
        '| 管道 | `a|b` | 选 a 或 b |\n'
        '| 普通 | `hi` | 普通行 |\n';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, contains('| 管道 | `a|b` | 选 a 或 b |'));
    expect(result.text, contains('| 普通 | `hi` | 普通行 |'));
    expect(result.text, isNot(contains('`a | b`')));
  });

  test('keeps double-backtick inline code containing single backtick', () {
    const raw = '| key | val | note |\n'
        '|-----|-----|------|\n'
        '| 双 | ``a `or` b|c`` | 备 |\n'
        '| 普 | x | y |\n';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, contains('``a `or` b|c``'));
  });

  test('keeps inline math pipe inside table cell unsplit', () {
    const raw = '| 公式 | 描述 |\n'
        '|------|------|\n'
        r'| $a|b$ | 并联 |' '\n'
        r'| $c$   | 单值 |' '\n';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, contains(r'$a|b$'));
    expect(result.text, isNot(contains(r'$a | b$')));
  });

  test('unmatched backtick in cell falls back to literal split', () {
    const raw = '| a | b |\n'
        '|---|---|\n'
        '| ` 没闭合 | x |\n'
        '| ok | y |\n';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, contains('| ` 没闭合 | x |'));
    expect(result.text, contains('| ok | y |'));
  });

  test('adds missing whitespace after ordered-list markers', () {
    const raw = '1.**删除这条记录** - 我帮你清理\n2.稍后重试上传';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, '1. **删除这条记录** - 我帮你清理\n2. 稍后重试上传');
  });

  test('inserts blank line before continued ordered-list runs', () {
    const raw = '生成确认\n2. 第二阶段\n3. 第三阶段';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, '生成确认\n\n2. 第二阶段\n3. 第三阶段');
  });

  test('inserts blank line before divider after collapsed table-like lines',
      () {
    const raw =
        '找到主要数据： | 目录 | 大小 || CoreSimulator | 2.2GB || 总计 | 9.5GB |\n---\n后续说明';

    final result = normalizer.normalizeForRendering(raw);

    expect(
      result.text,
      '找到主要数据： | 目录 | 大小 || CoreSimulator | 2.2GB || 总计 | 9.5GB |\n\n---\n后续说明',
    );
  });

  test('closes unbalanced fences for rendering copies', () {
    const raw = '```text\nhello';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, endsWith('```'));
  });

  test('closes unbalanced inline markers in text segments', () {
    const raw = '**bold';

    final result = normalizer.normalizeForRendering(raw);

    expect(result.text, '**bold**');
  });

  test('normalization is idempotent on stabilized output', () {
    const raw = '```markdown\n| a | b |\n|---|---|\n| 1 | 2 |\n```';

    final once = normalizer.normalizeForRendering(raw).text;
    final twice = normalizer.normalizeForRendering(once).text;

    expect(twice, once);
  });

  test('closes multiple unclosed fences with different markers', () {
    const raw = '```python\nprint("hi")\n~~~bash\necho "hi"';

    final result = normalizer.normalizeForFinalRender(raw);

    // Both fences should be closed
    expect(result.text, contains('~~~'));
    expect(result.text, contains('```'));
    // The text should end with closing markers
    expect(result.text.trimRight().endsWith('```'), isTrue);
  });

  test('skips escaped markers in unbalanced marker closure', () {
    const raw = r'This is \*\*not bold\*\* but **this is';

    final result = normalizer.normalizeForFinalRender(raw);

    // Should close the unbalanced **
    expect(result.text, endsWith('**'));
  });

  test('lightweight fixes only appends without modifying content', () {
    const raw = '**bold text and ```python\ncode here';

    final fixed = normalizer.applyLightweightFixes(raw);

    // Original content should be preserved as prefix
    expect(fixed, startsWith(raw));
    // Closing markers should be appended
    expect(fixed.length, greaterThan(raw.length));
  });

  test('idempotent on various markdown patterns', () {
    final samples = [
      '**bold** and *italic*',
      '# Heading\n\n```dart\ncode\n```',
      '| a | b |\n|---|---|\n| 1 | 2 |',
      r'$$x = 1$$',
      '> blockquote\n> **bold** inside',
      '1. ordered\n2. list\n   - nested',
    ];

    for (final raw in samples) {
      final once = normalizer.normalizeForRendering(raw).text;
      final twice = normalizer.normalizeForRendering(once).text;
      expect(twice, once, reason: 'Not idempotent for: $raw');
    }
  });
}
