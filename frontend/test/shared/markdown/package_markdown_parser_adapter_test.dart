import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/markdown/chat_markdown_ast.dart';
import 'package:grix/shared/markdown/chat_markdown_dialect.dart';
import 'package:grix/shared/markdown/chat_markdown_latex_syntax.dart';
import 'package:grix/shared/markdown/package_markdown_parser_adapter.dart';

void main() {
  test('maps headings, paragraphs and links into canonical nodes', () {
    final adapter = PackageMarkdownParserAdapter();

    final document = adapter.parse('# Title\n\nA [link](https://example.com).');

    final heading = _findFirst(document, ChatMarkdownNodeType.heading);
    final paragraph = _findFirst(document, ChatMarkdownNodeType.paragraph);

    expect(heading, isNotNull);
    expect(heading!.attrs['level'], 1);
    expect(paragraph, isNotNull);
    expect(
      _findFirst(document, ChatMarkdownNodeType.link)?.attrs['href'],
      'https://example.com',
    );
  });

  test('maps tables and fenced code blocks', () {
    final adapter = PackageMarkdownParserAdapter();

    final document = adapter.parse(
      '| a | b |\n|:---|---:|\n| 1 | 2 |\n\n```json\n{"a":1}\n```',
    );

    final table = _findFirst(document, ChatMarkdownNodeType.table);
    expect(table, isNotNull);
    final cells = _findAll(document, ChatMarkdownNodeType.tableCell);
    expect(cells.first.attrs['align'], 'left');
    expect(cells[1].attrs['align'], 'right');
    final codeBlock = _findFirst(document, ChatMarkdownNodeType.codeBlock);
    expect(codeBlock, isNotNull);
    expect(codeBlock!.attrs['language'], 'json');
  });

  test(
    'parses 4-space indented tables without treating them as code blocks',
    () {
      final adapter = ChatMarkdownDialect.buildParserAdapter();

      final document = adapter.parse(
        '    | a | b |\n'
        '    | --- | --- |\n'
        '    | 1 | 2 |',
      );

      expect(_findFirst(document, ChatMarkdownNodeType.table), isNotNull);
      expect(
        _findAll(document, ChatMarkdownNodeType.tableCell).length,
        greaterThanOrEqualTo(4),
      );
    },
  );

  test('does not stall on paragraph followed by unmatched table delimiter', () {
    final adapter = ChatMarkdownDialect.buildParserAdapter();

    final document = adapter.parse(
      'plain text\n'
      'plain text\n'
      '| --- | --- |',
    );

    expect(_findFirst(document, ChatMarkdownNodeType.table), isNull);
    final paragraphs = _findAll(document, ChatMarkdownNodeType.paragraph);
    expect(paragraphs, isNotEmpty);
  });

  test('maps custom latex syntax into canonical math nodes', () {
    final adapter = PackageMarkdownParserAdapter(
      blockSyntaxes: const [LatexBlockSyntax()],
      inlineSyntaxes: [LatexInlineSyntax()],
    );

    final document = adapter.parse(r'Inline $E = mc^2$ and $$a^2+b^2=c^2$$');

    expect(_findFirst(document, ChatMarkdownNodeType.mathInline), isNotNull);
    expect(_findFirst(document, ChatMarkdownNodeType.mathBlock), isNotNull);
  });

  test(r'maps $$\begin{align}...\end{align}$$ into canonical math block', () {
    final adapter = PackageMarkdownParserAdapter(
      blockSyntaxes: const [LatexBlockSyntax()],
      inlineSyntaxes: [LatexInlineSyntax()],
    );

    final document = adapter.parse(
      r'$$\begin{align}'
      '\n'
      r'x + y &= 5 \\'
      '\n'
      r'2x - y &= 1'
      '\n'
      r'\end{align}$$',
    );

    final mathBlock = _findFirst(document, ChatMarkdownNodeType.mathBlock);
    expect(mathBlock, isNotNull);
    expect(mathBlock!.attrs['tex'], contains(r'\begin{align}'));
    expect(mathBlock.attrs['tex'], contains(r'\end{align}'));
  });

  test(
    r'maps \[...\] and \(...\) latex delimiters into canonical math nodes',
    () {
      final adapter = PackageMarkdownParserAdapter(
        blockSyntaxes: const [
          LatexEnvironmentBlockSyntax(),
          LatexBlockSyntax(),
        ],
        inlineSyntaxes: [LatexInlineSyntax()],
      );

      final document = adapter.parse(
        r'Inline \(\alpha+\beta\)'
        '\n\n'
        r'\['
        '\n'
        r'\int_0^1 x^2 dx'
        '\n'
        r'\]',
      );

      expect(_findFirst(document, ChatMarkdownNodeType.mathInline), isNotNull);
      expect(_findFirst(document, ChatMarkdownNodeType.mathBlock), isNotNull);
    },
  );

  test('maps latex math environments into canonical math block nodes', () {
    final adapter = PackageMarkdownParserAdapter(
      blockSyntaxes: const [LatexEnvironmentBlockSyntax(), LatexBlockSyntax()],
      inlineSyntaxes: [LatexInlineSyntax()],
    );

    final document = adapter.parse(
      r'\begin{align}'
      '\n'
      r'x + y &= 5 \\'
      '\n'
      r'2x - y &= 1'
      '\n'
      r'\end{align}',
    );

    final mathBlock = _findFirst(document, ChatMarkdownNodeType.mathBlock);
    expect(mathBlock, isNotNull);
    expect(mathBlock!.attrs['tex'], contains(r'\begin{align}'));
    expect(mathBlock.attrs['tex'], contains(r'\end{align}'));
  });

  test('maps task list items into semantic task nodes', () {
    final adapter = PackageMarkdownParserAdapter();

    final document = adapter.parse('- [x] done\n- [ ] todo');

    final taskItems = _findAll(document, ChatMarkdownNodeType.taskItem);
    expect(taskItems, hasLength(2));
    expect(taskItems.first.attrs['checked'], isTrue);
    expect(taskItems.last.attrs['checked'], isFalse);
  });

  test('maps bare url links into autolink nodes', () {
    final adapter = PackageMarkdownParserAdapter();

    final document = adapter.parse('Visit https://example.com');

    final autolink = _findFirst(document, ChatMarkdownNodeType.autolink);
    expect(autolink, isNotNull);
    expect(autolink!.attrs['href'], 'https://example.com');
  });

  test('maps footnote references and definitions into canonical nodes', () {
    final adapter = PackageMarkdownParserAdapter();

    final document = adapter.parse('[^1] note\n\n[^1]: footnote');

    final ref = _findFirst(document, ChatMarkdownNodeType.footnoteRef);
    final def = _findFirst(document, ChatMarkdownNodeType.footnoteDef);
    expect(ref, isNotNull);
    expect(ref!.attrs['href'], '#fn-1');
    expect(ref.attrs['id'], 'fnref-1');
    expect(def, isNotNull);
    expect(def!.attrs['id'], 'fn-1');
  });

  test('decodes escaped html entities in parser text payloads', () {
    final adapter = PackageMarkdownParserAdapter();

    final document = adapter.parse(
      'A > B & C\n\n'
      '`X > Y & Z`\n\n'
      '```mermaid\nA --> B\n```\n\n'
      '![alt > text & desc](https://example.com/a.png)',
    );

    expect(
      _findFirst(document, ChatMarkdownNodeType.text)?.attrs['text'],
      'A > B & C',
    );
    expect(
      _findFirst(document, ChatMarkdownNodeType.inlineCode)?.attrs['text'],
      'X > Y & Z',
    );
    expect(
      _findFirst(document, ChatMarkdownNodeType.mermaidBlock)?.attrs['text'],
      'A --> B\n',
    );
    expect(
      _findFirst(document, ChatMarkdownNodeType.image)?.attrs['alt'],
      'alt > text & desc',
    );
  });

  test('maps uppercase mermaid info strings into mermaid nodes', () {
    final adapter = PackageMarkdownParserAdapter();

    final document = adapter.parse('```Mermaid\nflowchart TD\nA --> B\n```');

    final mermaid = _findFirst(document, ChatMarkdownNodeType.mermaidBlock);
    expect(mermaid, isNotNull);
    expect(mermaid!.attrs['language'], 'mermaid');
    expect(mermaid.attrs['text'], 'flowchart TD\nA --> B\n');
  });

  test('maps quoted strong emphasis when followed by adjacent cjk text', () {
    final adapter = ChatMarkdownDialect.buildParserAdapter();

    final document = adapter.parse('**"视频文案编辑员"**这样我就会按照标准流程为你生成视频分析文案啦~');

    final strong = _findFirst(document, ChatMarkdownNodeType.strong);
    expect(strong, isNotNull);
    expect(
      _findFirst(document, ChatMarkdownNodeType.text)?.attrs['text'],
      contains('"视频文案编辑员"'),
    );
  });

  test('parses <video> tag into a video node with src/width', () {
    final adapter = ChatMarkdownDialect.buildParserAdapter();

    final document = adapter.parse(
      '<video src="http://100.64.0.1:56107/d/6cc29a81" controls width="640">'
      '</video>',
    );

    final video = _findFirst(document, ChatMarkdownNodeType.video);
    expect(video, isNotNull);
    expect(video!.attrs['src'], 'http://100.64.0.1:56107/d/6cc29a81');
    expect(video.attrs['width'], '640');
  });

  test('parses self-closing <video> tag', () {
    final adapter = ChatMarkdownDialect.buildParserAdapter();

    final document = adapter.parse('<video src="https://e.com/a.mp4" />');

    final video = _findFirst(document, ChatMarkdownNodeType.video);
    expect(video, isNotNull);
    expect(video!.attrs['src'], 'https://e.com/a.mp4');
  });

  test('reads src from nested <source> child', () {
    final adapter = ChatMarkdownDialect.buildParserAdapter();

    final document = adapter.parse(
      '<video controls poster="https://e.com/p.jpg">'
      '<source src="https://e.com/b.mp4" type="video/mp4"></video>',
    );

    final video = _findFirst(document, ChatMarkdownNodeType.video);
    expect(video, isNotNull);
    expect(video!.attrs['src'], 'https://e.com/b.mp4');
    expect(video.attrs['poster'], 'https://e.com/p.jpg');
  });

  test('does not treat ordinary angle-bracket text as video', () {
    final adapter = ChatMarkdownDialect.buildParserAdapter();

    final document = adapter.parse('a < b and c > d');

    expect(_findFirst(document, ChatMarkdownNodeType.video), isNull);
  });

  test('parses <audio> tag into an audio node with src/title', () {
    final adapter = ChatMarkdownDialect.buildParserAdapter();

    final document = adapter.parse(
      '<audio src="https://e.com/a.mp3" title="Demo" controls></audio>',
    );

    final audio = _findFirst(document, ChatMarkdownNodeType.audio);
    expect(audio, isNotNull);
    expect(audio!.attrs['src'], 'https://e.com/a.mp3');
    expect(audio.attrs['title'], 'Demo');
  });

  test('parses self-closing <audio> tag', () {
    final adapter = ChatMarkdownDialect.buildParserAdapter();

    final document = adapter.parse('<audio src="https://e.com/a.m4a" />');

    final audio = _findFirst(document, ChatMarkdownNodeType.audio);
    expect(audio, isNotNull);
    expect(audio!.attrs['src'], 'https://e.com/a.m4a');
  });

  test('reads audio src from nested <source> child', () {
    final adapter = ChatMarkdownDialect.buildParserAdapter();

    final document = adapter.parse(
      '<audio controls>'
      '<source src="https://e.com/b.mp3" type="audio/mpeg"></audio>',
    );

    final audio = _findFirst(document, ChatMarkdownNodeType.audio);
    expect(audio, isNotNull);
    expect(audio!.attrs['src'], 'https://e.com/b.mp3');
  });

  test('does not treat ordinary angle-bracket text as audio', () {
    final adapter = ChatMarkdownDialect.buildParserAdapter();

    final document = adapter.parse('a < b and c > d');

    expect(_findFirst(document, ChatMarkdownNodeType.audio), isNull);
  });
}

ChatMarkdownNode? _findFirst(ChatMarkdownNode node, ChatMarkdownNodeType type) {
  if (node.type == type) {
    return node;
  }
  for (final child in node.children) {
    final result = _findFirst(child, type);
    if (result != null) {
      return result;
    }
  }
  return null;
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
