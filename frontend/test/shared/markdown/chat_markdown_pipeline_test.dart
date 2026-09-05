import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/markdown/chat_markdown_ast.dart';
import 'package:grix/shared/markdown/chat_markdown_dialect.dart';
import 'package:grix/shared/markdown/chat_markdown_normalizer.dart';
import 'package:grix/shared/markdown/chat_markdown_parser_adapter.dart';
import 'package:grix/shared/markdown/chat_markdown_pipeline.dart';
import 'package:grix/shared/markdown/chat_markdown_semantics.dart';
import 'package:grix/shared/widgets/chat_markdown_render_strategy.dart';

void main() {
  final pipeline = ChatMarkdownPipeline(
    normalizer: const ChatMarkdownNormalizer(),
    parser: ChatMarkdownDialect.buildParserAdapter(),
  );

  test('preview mode normalizes but never enables markdown rendering', () {
    const raw = '```markdown\n| a | b |\n|---|---|\n| 1 | 2 |\n```';

    final result = pipeline.preparePreview(raw);

    expect(result.normalizedText, raw);
    expect(result.shouldUseMarkdown, isFalse);
    expect(result.document, isNull);
  });

  test('preview mode applies lightweight fixes (fence closure)', () {
    const raw = '**bold';

    final result = pipeline.preparePreview(raw);

    expect(result.normalizedText, '**bold**');
    expect(result.originalText, raw);
    expect(result.shouldUseMarkdown, isFalse);
  });

  test('final render enables markdown for rich formatted content', () {
    final result = pipeline.prepareFinalRender('**bold**');

    expect(result.shouldUseMarkdown, isTrue);
    expect(result.document, isNotNull);
    expect(result.semantics, isNotNull);
    expect(result.semantics!.hasInlineFormatting, isTrue);
  });

  test('final render keeps plain text on lightweight path', () {
    final result = pipeline.prepareFinalRender('just a plain message');

    expect(result.shouldUseMarkdown, isFalse);
    expect(result.document, isNotNull);
    expect(result.semantics, isNotNull);
    expect(result.semantics!.requiresRichRendering, isFalse);
  });

  test('trusted final render keeps raw streamed text unchanged', () {
    const raw = '好，那用流程图：```mermaid\nflowchart TD\nA[开始] --> B[结束]\n```';

    final result = pipeline.prepareFinalRenderFromTrustedSource(raw);

    expect(result.normalizedText, raw);
    expect(result.shouldUseMarkdown, isFalse);
  });

  test(
    'final render does not trip parser stall on unmatched table delimiter',
    () {
      final result = pipeline.prepareFinalRender(
        'plain text\n'
        'plain text\n'
        '| --- | --- |',
      );

      expect(result.document, isNotNull);
      expect(result.shouldUseMarkdown, isFalse);
      expect(result.normalizedText, contains('| --- | --- |'));
    },
  );

  test('final render degrades to plain text when parser throws', () {
    final brokenPipeline = ChatMarkdownPipeline(
      normalizer: const ChatMarkdownNormalizer(),
      parser: _ThrowingParserAdapter(),
    );

    final result = brokenPipeline.prepareFinalRender('**bold**');

    expect(result.shouldUseMarkdown, isFalse);
    expect(result.document, isNull);
    expect(result.semantics, isNull);
    expect(result.normalizedText, '**bold**');
  });

  test('final render degrades oversized input to plain text', () {
    final boundedPipeline = ChatMarkdownPipeline(
      normalizer: const ChatMarkdownNormalizer(),
      parser: ChatMarkdownDialect.buildParserAdapter(),
      maxRenderableCharacters: 7,
    );

    final result = boundedPipeline.prepareFinalRender('**bold**');

    expect(result.shouldUseMarkdown, isFalse);
    expect(result.document, isNull);
    expect(result.semantics, isNull);
  });

  test(
    'final render strips empty code blocks but preserves other formatting',
    () {
      const raw = '```json\n\n```';

      final result = pipeline.prepareFinalRender(raw);

      // Empty-only message: no rich content remains after stripping
      expect(result.document, isNotNull);
      expect(
        _countNodes(result.document!, ChatMarkdownNodeType.codeBlock),
        equals(0),
      );
    },
  );

  test(
    'final render keeps formatting when message has empty code block + rich content',
    () {
      const raw = '**bold text**\n\n```json\n\n```\n\nMore text';

      final result = pipeline.prepareFinalRender(raw);

      expect(result.shouldUseMarkdown, isTrue);
      expect(result.document, isNotNull);
      expect(
        _countNodes(result.document!, ChatMarkdownNodeType.strong),
        greaterThan(0),
      );
      expect(
        _countNodes(result.document!, ChatMarkdownNodeType.codeBlock),
        equals(0),
      );
    },
  );

  test('final render repairs loose ai table formats into rich table ast', () {
    const raw = '｜ 名称 ｜ 数量 ｜\n｜ -- ｜ == ｜\n｜ 苹果 ｜ 3 ｜';

    final result = pipeline.prepareFinalRender(raw);

    expect(result.shouldUseMarkdown, isTrue);
    expect(result.semantics!.hasTables, isTrue);
    expect(
      _countNodes(result.document!, ChatMarkdownNodeType.table),
      equals(1),
    );
    expect(result.normalizedText, contains('| --- | --- |'));
  });

  test('final render repairs table separator rows with dangling fragments', () {
    const raw =
        '| 功能 | 状态 | 优先级 | 负责人 | 预计完成 |\n'
        '|:-----|:----:|:------:|:-------:|--------:|-\n'
        '| 用户登录 | ✅ 已完成 | 🔴 高 | 张三 | 2025-01-15 |\n'
        '| 支付集成 | 🔄 开发中 | 🔴 高 | 李四 | 2025-02-01 |\n'
        '\n'
        '**要点：**\n'
        '- 支持内联 emoji、链接、加粗等格式';

    final result = pipeline.prepareFinalRender(raw);

    expect(result.shouldUseMarkdown, isTrue);
    expect(result.semantics!.hasTables, isTrue);
    expect(
      _countNodes(result.document!, ChatMarkdownNodeType.table),
      equals(1),
      reason: result.normalizedText,
    );
    expect(
      result.normalizedText,
      contains('| :--- | :---: | :---: | :---: | ---: |'),
    );
    expect(
      _findParagraphContainingText(result.document!, '要点：'),
      isNotNull,
      reason: result.normalizedText,
    );
    expect(
      _findListNode(result.document!, ordered: false),
      isNotNull,
      reason: result.normalizedText,
    );
  });

  test('final render restores table after accidental fence-wrapped header', () {
    const raw =
        '**常见方案：**\n\n'
        '```\n'
        '| 方案 | 原理 |\n'
        '| --- | --- |\n'
        '```| 内网穿透 | frp |\n'
        '| 云端中继 | relay |---\n'
        '后续说明\n';

    final result = pipeline.prepareFinalRender(raw);

    expect(result.shouldUseMarkdown, isTrue);
    expect(result.semantics!.hasTables, isTrue);
    expect(
      _countNodes(result.document!, ChatMarkdownNodeType.table),
      equals(1),
    );
    expect(result.normalizedText, isNot(contains('```')));
  });

  test('final render keeps trailing prose outside repaired table block', () {
    const raw =
        '```\n'
        '| 方案 | 原理 |\n'
        '| --- | --- |\n'
        '```| 内网穿透 | frp |\n'
        '| 云端中继 | relay |\n'
        '后续说明';

    final result = pipeline.prepareFinalRender(raw);

    expect(result.normalizedText, contains('| 云端中继 | relay |\n\n后续说明'));
    final table = _findFirst(result.document!, ChatMarkdownNodeType.table);
    expect(table, isNotNull);
    expect(
      _findParagraphWithText(result.document!, '后续说明'),
      isNotNull,
      reason: result.normalizedText,
    );
  });

  test('final render keeps shell pipelines outside markdown tables', () {
    const raw =
        '| 实例 | 版本 |\n'
        '| --- | --- |\n'
        '| main | 0.4.27 |\n'
        "grep -A1 'Grix' | grep '|' | head -1 | awk -F'|' '{print \$7}'\n"
        "tr -d ' ' | sed -n '1p'";

    final result = pipeline.prepareFinalRender(raw);

    expect(result.shouldUseMarkdown, isTrue);
    expect(result.semantics?.hasTables, isTrue);
    expect(
      _countNodes(result.document!, ChatMarkdownNodeType.table),
      equals(1),
      reason: result.normalizedText,
    );
    expect(
      result.normalizedText,
      contains(
        "| main | 0.4.27 |\n\n"
        "grep -A1 'Grix' | grep '|' | head -1 | awk -F'|' '{print \$7}'",
      ),
    );
    expect(
      _findParagraphContainingText(
        result.document!,
        "grep -A1 'Grix' | grep '|' | head -1 | awk -F'|' '{print \$7}'",
      ),
      isNotNull,
      reason: result.normalizedText,
    );
    expect(
      _findParagraphContainingText(result.document!, "tr -d ' ' | sed -n '1p'"),
      isNotNull,
      reason: result.normalizedText,
    );
  });

  test('final render falls back to plain text for suspicious fence tails', () {
    const raw = '''本地 Ollama 共 12 个模型：

**对话模型**

```
| 模型           | 大小     |
| ------------ | ------ |
| qwen3:latest | 5.2 GB |
|              |        |
````qwen3.5:4b` | 3.4 GB |
| `qwen3:4b` | 2.5 GB |
| `qwen3:4b-instruct` | 2.5 GB || `gemma3:4b` | 3.3 GB |
| `gemma-3-excel-finetune:latest` | 4.1 GB |
| `functiongemma:latest`| 0.3 GB |

**Embedding 模型**

```
| 模型                     | 大小 |
| ---------------------- | --- |
| qwen3-embedding:latest | 4. |
```7 GB |
| `quentinz/bge-large-zh-v1.5` | 0.7 GB |
| `mxbai-embed-large:latest` | 0.7 GB |
| `embeddinggemma:latest` | 0.6 GB |
| `nomic-embed-text:latest` | 0.3 GB |
''';

    final result = pipeline.prepareFinalRender(raw);

    expect(result.shouldUseMarkdown, isFalse);
    expect(result.document, isNull);
    expect(result.normalizedText, raw);
  });

  test('final render does not promote collapsed table text into heading', () {
    const raw =
        '找到主要数据： | 目录 | 大小 || CoreSimulator | 2.2GB || 总计 | 9.5GB |\n---\n后续说明';

    final result = pipeline.prepareFinalRender(raw);

    expect(result.normalizedText, contains('\n\n---\n'));
    expect(
      _countNodes(result.document!, ChatMarkdownNodeType.heading),
      equals(0),
    );
    expect(
      result.semantics!.hasFeature(ChatMarkdownFeature.thematicBreak),
      isTrue,
    );
  });

  test('final render marks raw html as fallback-only rich content', () {
    final result = pipeline.prepareFinalRender('<div>unsafe html</div>');

    expect(result.shouldUseMarkdown, isTrue);
    expect(result.semantics, isNotNull);
    expect(result.semantics!.hasFeature(ChatMarkdownFeature.html), isTrue);
  });

  test('parser adapter maps mermaid fenced blocks into mermaid nodes', () {
    final result = pipeline.prepareFinalRender(
      '```mermaid\ngraph TD\nA --> B\n```',
    );

    expect(result.shouldUseMarkdown, isTrue);
    expect(result.semantics!.hasMermaidBlocks, isTrue);
    final mermaidNode = _findFirst(
      result.document!,
      ChatMarkdownNodeType.mermaidBlock,
    );
    expect(mermaidNode, isNotNull);
    expect(mermaidNode!.attrs['language'], 'mermaid');
  });

  test('final render repairs inline mermaid fence after prose', () {
    final result = pipeline.prepareFinalRender(
      '好，那用流程图：```mermaid\nflowchart TD\nA[开始] --> B[结束]\n```',
    );

    expect(result.normalizedText, contains('好，那用流程图：\n```mermaid'));
    expect(result.shouldUseMarkdown, isTrue);
    expect(result.semantics!.hasMermaidBlocks, isTrue);
    expect(
      _findFirst(result.document!, ChatMarkdownNodeType.mermaidBlock),
      isNotNull,
    );
  });

  test('final render rewrites single-line fenced code after heading', () {
    final result = pipeline.prepareFinalRender(
      '### 1. 安装CLI```bash npm i -g clawhub ```',
    );

    expect(
      result.normalizedText,
      contains('### 1. 安装CLI\n```bash\nnpm i -g clawhub\n```'),
    );
    final codeBlock = _findFirst(
      result.document!,
      ChatMarkdownNodeType.codeBlock,
    );
    expect(codeBlock, isNotNull);
    expect(codeBlock!.attrs['language'], 'bash');
    expect(codeBlock.attrs['text'], 'npm i -g clawhub\n');
  });

  test('final render unwraps markdown-wrapped mermaid fences', () {
    final result = pipeline.prepareFinalRender(
      '````markdown\n```mermaid\nflowchart TD\nA[开始] --> B[结束]\n```\n````',
    );

    expect(result.shouldUseMarkdown, isTrue);
    expect(result.semantics!.hasMermaidBlocks, isTrue);
    expect(
      _findFirst(result.document!, ChatMarkdownNodeType.mermaidBlock),
      isNotNull,
    );
  });

  test('final render unwraps markdown-wrapped mermaid fences with info args', () {
    final result = pipeline.prepareFinalRender(
      '````markdown\n```Mermaid title="登录流程"\nflowchart TD\nA[开始] --> B[结束]\n```\n````',
    );

    expect(result.shouldUseMarkdown, isTrue);
    expect(result.semantics!.hasMermaidBlocks, isTrue);
    expect(
      _findFirst(result.document!, ChatMarkdownNodeType.mermaidBlock),
      isNotNull,
    );
  });

  test('final render decodes escaped mermaid fence markers', () {
    final result = pipeline.prepareFinalRender(
      r'好，那用流程图：\`\`\`mermaid'
      '\nflowchart TD'
      '\nA[开始] --> B[结束]'
      '\n```',
    );

    expect(result.shouldUseMarkdown, isTrue);
    expect(result.semantics!.hasMermaidBlocks, isTrue);
  });

  test('final render repairs echoed fenced fragments into one code block', () {
    final result = pipeline.prepareFinalRender(
      '###1. 安装CLI```bash\n'
      'n\n'
      '```bash\n'
      'p\n'
      '```bash\n'
      'm\n'
      '```bash\n'
      ' \n'
      '```bash\n'
      'i\n'
      '```bash\n'
      ' \n'
      '```bash\n'
      '-\n'
      '```bash\n'
      'g\n'
      '```bash\n'
      ' \n'
      '```bash\n'
      'c\n'
      '```bash\n'
      'l\n'
      '```bash\n'
      'a\n'
      '```bash\n'
      'w\n'
      '```bash\n'
      'h\n'
      '```bash\n'
      'u\n'
      '```bash\n'
      'b',
    );

    final codeBlock = _findFirst(
      result.document!,
      ChatMarkdownNodeType.codeBlock,
    );
    expect(codeBlock, isNotNull);
    expect(codeBlock!.attrs['language'], 'bash');
    expect(codeBlock.attrs['text'], 'npm i -g clawhub\n');
    expect(result.normalizedText, contains('```bash\nnpm i -g clawhub\n```'));
  });

  test(
    'final render preserves embedded formulas inside latex source fences',
    () {
      const raw = r'''```latex
$$% LaTeX 文档示例
\documentclass{article}
\usepackage{amsmath}
\title{数学公式示例}
\author{小龙虾}
\date{\today}
\begin{document}
\maketitle
行内公式：质能方程 $E = mc^2$
\section{基础公式}
\begin{itemize}
\item 项目一
\item 项目二
\end{itemize}

独立公式：
\[
\int_{0}^{\infty} e^{-x^2} \, dx = \frac{\sqrt{\pi}}{2}
\]
\begin{align}
x + y &= 5 \\
2x - y &= 1
\end{align}
\end{document}
$$
```''';

      final result = pipeline.prepareFinalRender(raw);

      expect(result.normalizedText, isNot(contains('```latex')));
      expect(result.normalizedText, contains('LaTeX 文档示例'));
      expect(result.normalizedText, contains('# 数学公式示例'));
      expect(
        result.normalizedText,
        contains(RegExp(r'> 小龙虾 · \d{4}-\d{2}-\d{2}')),
      );
      expect(
        result.normalizedText,
        isNot(contains(r'\documentclass{article}')),
      );
      expect(result.normalizedText, isNot(contains(r'\usepackage{amsmath}')));
      expect(result.normalizedText, isNot(contains(r'\maketitle')));
      expect(result.normalizedText, isNot(contains(r'\begin{document}')));
      expect(result.normalizedText, isNot(contains(r'\end{document}')));
      expect(result.normalizedText, contains('## 基础公式'));
      expect(result.normalizedText, contains('- 项目一'));
      expect(result.normalizedText, contains(r'\begin{align}'));
      expect(result.normalizedText, contains(r'\end{align}'));
      expect(result.normalizedText, isNot(contains(r'$$\begin{align}')));
      expect(result.normalizedText, isNot(contains(r'\end{align}$$')));
      expect(
        _countNodes(result.document!, ChatMarkdownNodeType.mathInline),
        greaterThan(0),
      );
      expect(
        _countNodes(result.document!, ChatMarkdownNodeType.mathBlock),
        greaterThan(1),
      );
      expect(result.semantics!.hasFeature(ChatMarkdownFeature.heading), isTrue);
      expect(result.semantics!.hasFeature(ChatMarkdownFeature.list), isTrue);
    },
  );

  test('final render extracts align environment inside latex fences', () {
    const raw = r'''```latex
\begin{align}
x + y &= 5 \\
2x - y &= 1
\end{align}
```''';

    final result = pipeline.prepareFinalRender(raw);
    final mathBlocks = _findAll(
      result.document!,
      ChatMarkdownNodeType.mathBlock,
    );

    expect(result.normalizedText, isNot(contains('```latex')));
    expect(mathBlocks, isNotEmpty);
    expect(mathBlocks.first.attrs['tex'], contains(r'\begin{align}'));
  });

  test('final render converts non-fenced latex document snippets', () {
    const raw = r'''给我发了一个Latex块的代码

$$% LaTeX 文档示例
\documentclass{article}
\title{数学公式示例}
\begin{document}
\maketitle
\section{基础公式}
行内公式：$E = mc^2$
\begin{align}
x + y &= 5 \\
2x - y &= 1
\end{align}
\end{document}
$$''';

    final result = pipeline.prepareFinalRender(raw);

    expect(result.normalizedText, contains('# 数学公式示例'));
    expect(result.normalizedText, contains('## 基础公式'));
    expect(result.normalizedText, isNot(contains(r'\documentclass{article}')));
    expect(result.normalizedText, isNot(contains(r'\begin{document}')));
    expect(result.normalizedText, isNot(contains(r'\end{document}')));
    expect(
      _countNodes(result.document!, ChatMarkdownNodeType.mathInline),
      greaterThan(0),
    );
    expect(
      _countNodes(result.document!, ChatMarkdownNodeType.mathBlock),
      greaterThan(0),
    );
  });

  test('final render keeps quoted strong emphasis before cjk suffix', () {
    const raw =
        '如果你想让我处理这个视频，请这样呼唤我：\n'
        '**"视频文案编辑员，处理这个视频：Z:\\clawVideo\\deepseek咨询.mp4"**这样我就会按照标准流程为你生成视频分析文案啦~\n';

    final result = pipeline.prepareFinalRender(raw);

    expect(
      _countNodes(result.document!, ChatMarkdownNodeType.strong),
      greaterThan(0),
      reason: result.normalizedText,
    );
  });

  test('final render includes validation result', () {
    final result = pipeline.prepareFinalRender('**bold**');

    expect(result.validation, isNotNull);
    expect(result.validation!.hasErrors, isFalse);
  });

  test('final render falls back to original text on parser exception', () {
    final brokenPipeline = ChatMarkdownPipeline(
      normalizer: const ChatMarkdownNormalizer(),
      parser: _ThrowingParserAdapter(),
    );

    final result = brokenPipeline.prepareFinalRender('just plain');

    expect(result.shouldUseMarkdown, isFalse);
    expect(result.normalizedText, 'just plain');
  });

  test('final render keeps quoted strong emphasis before numbered list', () {
    const raw =
        '我的身份是**"视频文案编辑员"**，不是"视频分析员"哦。\n\n'
        '如果你需要我处理这个视频，请这样呼唤我：\n\n'
        '**"视频文案编辑员，处理这个视频：Z:\\clawVideo\\deepseek咨询.mp4"**这样我就会按照标准工作流程为你：\n'
        '1. 分析视频内容\n'
        '2. 生成精确字幕\n';

    final result = pipeline.prepareFinalRender(raw);

    expect(
      _countNodes(result.document!, ChatMarkdownNodeType.strong),
      greaterThan(1),
      reason: result.normalizedText,
    );
  });

  test('final render restores inline ordered lists after clause prefixes', () {
    const raw =
        '我先收集更多信息：1. 是 iOS 还是 Android？\n'
        '2. 是所有消息都没有角标，还是特定情况？\n'
        '3. App 在前台还是后台时有这个问题？';

    final result = pipeline.prepareFinalRender(raw);

    expect(result.shouldUseMarkdown, isTrue);
    expect(
      _findListNode(result.document!, ordered: true),
      isNotNull,
      reason: result.normalizedText,
    );
    expect(result.normalizedText, contains('我先收集更多信息：\n1. 是 iOS 还是 Android？'));
  });

  test('final render restores unordered list after inline fence closer', () {
    const raw =
        '根据 `generate-video-subtitles` skill 的定义，要生成带音频文件需要：\n\n'
        '**基本格式：**\n'
        '```\n'
        '视频分析员 分析视频：[视频路径]\n'
        '```**生成音频的触发方式：**\n\n'
        '1. **明确指定生成音频：**\n'
        '   ```\n'
        '   视频分析员 分析视频并生成语音：Z:\\clawVideo\\newVideo\\腾讯新闻.mp4\n'
        '   ```2. **或者指定 TTS 参数：**\n'
        '   ```\n'
        '   视频分析员 分析视频：Z:\\clawVideo\\newVideo\\腾讯新闻.mp4\n'
        '   生成语音，音色：tongtong，语速：1.0\n'
        '   ```\n\n'
        '**可用的 TTS 参数：**\n'
        '- **音色**：tongtong（默认）、chuichui、male、female、xiaofei、wanchuan\n'
        '- **语速**：0.5-2.0（默认 1.0）\n';

    final result = pipeline.prepareFinalRender(raw);

    expect(result.normalizedText, contains('```\n**生成音频的触发方式：**'));
    expect(result.normalizedText, contains('```\n2. **或者指定 TTS 参数：**'));
    expect(
      _findListNode(result.document!, ordered: false),
      isNotNull,
      reason: result.normalizedText,
    );
  });

  test(
    'final render restores nested unordered list after inline fence closer',
    () {
      const raw =
          '我来修改 skill 文件，明确禁止压缩方案：✅ **已更新 skill 文件**\n\n'
          '**修改内容：**\n\n'
          '1. **核心约束与规则 - 新增第6条**\n'
          '   ```\n'
          '   6. 大文件处理：视频文件 >= 8MB 时，必须上传到腾讯云 COS。\n'
          '      如果上传失败，禁止使用压缩方案，必须返回错误并终止流程。\n'
          '   ```2. **第一步执行步骤 - 添加上传失败处理**\n'
          '   - 明确说明：如果上传失败（SSL错误、超时、网络错误等），**必须立即终止流程**\n'
          '   - **禁止**使用ffmpeg 压缩方案\n'
          '   - 提供标准错误信息格式：\n';

      final result = pipeline.prepareFinalRender(raw);

      expect(result.normalizedText, contains('```\n2. **第一步执行步骤 - 添加上传失败处理**'));
      expect(
        _findListNode(result.document!, ordered: false),
        isNotNull,
        reason: result.normalizedText,
      );
    },
  );

  test('angle-bracket placeholders keep the whole message on native ast', () {
    const message =
        '两张单已派给 Claude开发员，并行做：\n'
        '\n'
        '- **grix 前端（会话 930e9ac4）**：主机组按钮三分支。有能力候选走原逻辑；'
        '只有在线 hermes 时按钮变「让 <hermes 名> 安装连接器」，确认后打开会话并以你的身份'
        '发引导消息（含主机名和技能名）；……11 语种 i18n。\n'
        '- **grix-hermes（会话 9d7ac329）**：新增默认技能 `grix-connector-bootstrap`，'
        '六步：查已装 → 查 Node ≥ 20 → npm 装包……版本 bump 1.16.7。\n'
        '\n'
        '两边回写后我验收，通过就合 main；hermes 插件发版和 iOS 910 再一起等你定。';

    final result = pipeline.prepareFinalRender(message);

    expect(result.shouldUseMarkdown, isTrue);
    expect(result.semantics, isNotNull);
    expect(result.semantics!.hasFeature(ChatMarkdownFeature.html), isFalse);
    expect(
      const ChatMarkdownRenderStrategy().select(
        document: result.document,
        semantics: result.semantics,
      ),
      ChatMarkdownRenderMode.nativeAst,
    );
  });

  test('placeholder-only angle brackets never add the html feature', () {
    for (final input in <String>[
      'see <name> here',
      'open <文件路径> now',
      'visit <https://example.com>',
      'mail <user@example.com>',
    ]) {
      final result = pipeline.prepareFinalRender(input);

      expect(
        result.semantics?.hasFeature(ChatMarkdownFeature.html) ?? false,
        isFalse,
        reason: '$input must not be treated as html',
      );
    }
  });

  test('real html still falls back to plain text in chat', () {
    final result = pipeline.prepareFinalRender('<div>unsafe html</div>');

    expect(result.semantics!.hasFeature(ChatMarkdownFeature.html), isTrue);
    expect(
      const ChatMarkdownRenderStrategy().select(
        document: result.document,
        semantics: result.semantics,
      ),
      ChatMarkdownRenderMode.fallbackPlainText,
    );
  });
}

class _ThrowingParserAdapter implements ChatMarkdownParserAdapter {
  @override
  ChatMarkdownDocument parse(String markdown) {
    throw StateError('boom');
  }
}

ChatMarkdownNode? _findFirst(ChatMarkdownNode node, ChatMarkdownNodeType type) {
  if (node.type == type) {
    return node;
  }
  for (final child in node.children) {
    final found = _findFirst(child, type);
    if (found != null) {
      return found;
    }
  }
  return null;
}

int _countNodes(ChatMarkdownNode node, ChatMarkdownNodeType type) {
  var count = node.type == type ? 1 : 0;
  for (final child in node.children) {
    count += _countNodes(child, type);
  }
  return count;
}

List<ChatMarkdownNode> _findAll(
  ChatMarkdownNode node,
  ChatMarkdownNodeType type,
) {
  final nodes = <ChatMarkdownNode>[];
  if (node.type == type) {
    nodes.add(node);
  }
  for (final child in node.children) {
    nodes.addAll(_findAll(child, type));
  }
  return nodes;
}

ChatMarkdownNode? _findListNode(
  ChatMarkdownNode node, {
  required bool ordered,
}) {
  if (node.type == ChatMarkdownNodeType.list &&
      node.attrs['ordered'] == ordered) {
    return node;
  }
  for (final child in node.children) {
    final found = _findListNode(child, ordered: ordered);
    if (found != null) {
      return found;
    }
  }
  return null;
}

ChatMarkdownNode? _findParagraphWithText(ChatMarkdownNode node, String text) {
  if (node.type == ChatMarkdownNodeType.paragraph) {
    final found = _findFirst(node, ChatMarkdownNodeType.text);
    if (found?.attrs['text'] == text) {
      return node;
    }
  }
  for (final child in node.children) {
    final found = _findParagraphWithText(child, text);
    if (found != null) {
      return found;
    }
  }
  return null;
}

ChatMarkdownNode? _findParagraphContainingText(
  ChatMarkdownNode node,
  String text,
) {
  if (node.type == ChatMarkdownNodeType.paragraph) {
    final content = _collectPlainText(node);
    if (content.contains(text)) {
      return node;
    }
  }
  for (final child in node.children) {
    final found = _findParagraphContainingText(child, text);
    if (found != null) {
      return found;
    }
  }
  return null;
}

String _collectPlainText(ChatMarkdownNode node) {
  final buffer = StringBuffer();
  final text = node.attrs['text']?.toString();
  if (text != null) {
    buffer.write(text);
  }
  for (final child in node.children) {
    buffer.write(_collectPlainText(child));
  }
  return buffer.toString();
}
