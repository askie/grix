import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/markdown/chat_markdown_ast.dart';
import 'package:grix/shared/markdown/chat_markdown_dialect.dart';
import 'package:grix/shared/markdown/chat_markdown_normalizer.dart';
import 'package:grix/shared/markdown/chat_markdown_pipeline.dart';

/// 统计文档中的显示公式块数量
int _countMathBlock(ChatMarkdownNode node) {
  var count = node.type == ChatMarkdownNodeType.mathBlock ? 1 : 0;
  for (final child in node.children) {
    count += _countMathBlock(child);
  }
  return count;
}

/// 统计文档中的行内公式数量
int _countMathInline(ChatMarkdownNode node) {
  var count = node.type == ChatMarkdownNodeType.mathInline ? 1 : 0;
  for (final child in node.children) {
    count += _countMathInline(child);
  }
  return count;
}

void main() {
  final pipeline = ChatMarkdownPipeline(
    normalizer: const ChatMarkdownNormalizer(),
    parser: ChatMarkdownDialect.buildParserAdapter(),
  );

  ({int block, int inline}) parse(String input) {
    final result = pipeline.prepareFinalRender(input);
    var block = 0;
    var inline = 0;
    for (final node in result.document?.children ?? const <ChatMarkdownNode>[]) {
      block += _countMathBlock(node);
      inline += _countMathInline(node);
    }
    return (block: block, inline: inline);
  }

  group(r'\[ ... \] 显示公式解析', () {
    test('独占一行（带空格）识别为显示公式块', () {
      final counts = parse('eq\n\n\\[ y = \\frac{a}{b} \\]\n\nnext');
      expect(counts.block, 1);
    });

    test('独占一行（无空格）识别为显示公式块', () {
      final counts = parse('eq\n\n\\[y=\\frac{a}{b}\\]\n\nnext');
      expect(counts.block, 1);
    });

    test('开闭符各自独占一行的多行形式识别为显示公式块', () {
      final counts = parse('eq\n\n\\[\ny=\\frac{a}{b}\n\\]\n\nnext');
      expect(counts.block, 1);
    });

    test('行内位置的 \\[ ... \\] 识别为显示公式块', () {
      final counts = parse('see \\[ y=\\frac{a}{b} \\] here');
      expect(counts.block, 1);
    });
  });

  group('既有分隔符保持不变', () {
    test(r'$$ ... $$ 仍为显示公式块', () {
      final counts = parse('eq\n\n\$\$y=\\frac{a}{b}\$\$\n\nnext');
      expect(counts.block, 1);
    });

    test(r'$ ... $ 仍为行内公式', () {
      final counts = parse('val \$x_1\$ ok');
      expect(counts.inline, 1);
    });

    test(r'\( ... \) 仍为行内公式', () {
      final counts = parse('inline \\(x>0\\) here');
      expect(counts.inline, 1);
    });
  });

  group('混排与上下文兼容', () {
    test('多种分隔符同文档各自识别', () {
      final counts = parse('\$\$a=b\$\$ and \$c\$ and \\(d\\) and \\[e=f\\] done');
      expect(counts.block, 2);
      expect(counts.inline, 2);
    });

    test('列表项内的 \\[ ... \\] 与 \\( ... \\)', () {
      final counts = parse('- step \\[ y=\\frac{a}{b} \\]\n- next \\(x\\)');
      expect(counts.block, 1);
      expect(counts.inline, 1);
    });

    test('引用块内的 \\[ ... \\]', () {
      final counts = parse('> note \\[ y=x \\] end');
      expect(counts.block, 1);
    });

    test('开闭符独占行的多行环境（矩阵）', () {
      final counts = parse(
        'eq\n\n\\[\n\\begin{matrix} a & b \\\\ c & d \\end{matrix}\n\\]\n\nz',
      );
      expect(counts.block, 1);
    });
  });

  group('不误伤其它语法', () {
    test('围栏代码块内的 \\[ ... \\] 保持为代码', () {
      final r = pipeline.prepareFinalRender('```\n\\[ y = x \\]\n```');
      var block = 0;
      var code = 0;
      void walk(ChatMarkdownNode n) {
        if (n.type == ChatMarkdownNodeType.mathBlock) block++;
        if (n.type == ChatMarkdownNodeType.codeBlock) code++;
        for (final c in n.children) {
          walk(c);
        }
      }
      for (final n in r.document?.children ?? const <ChatMarkdownNode>[]) {
        walk(n);
      }
      expect(code, 1);
      expect(block, 0);
    });

    test('Markdown 链接不受影响', () {
      final r = pipeline.prepareFinalRender('see [text](https://e.com) link');
      var link = 0;
      void walk(ChatMarkdownNode n) {
        if (n.type == ChatMarkdownNodeType.link) link++;
        for (final c in n.children) {
          walk(c);
        }
      }
      for (final n in r.document?.children ?? const <ChatMarkdownNode>[]) {
        walk(n);
      }
      expect(link, 1);
    });
  });

  group('健壮性与流式路径', () {
    test('未闭合的 \\[ 不崩溃且降级为纯文本', () {
      final r = pipeline.prepareFinalRender('open \\[ y = x and no close');
      var block = 0;
      for (final n in r.document?.children ?? const <ChatMarkdownNode>[]) {
        block += _countMathBlock(n);
      }
      expect(block, 0);
    });

    test('流式可信来源路径同样支持 \\[ ... \\]', () {
      final r = pipeline
          .prepareFinalRenderFromTrustedSource('eq\n\n\\[ y=\\frac{a}{b} \\]\n\nnext');
      var block = 0;
      for (final n in r.document?.children ?? const <ChatMarkdownNode>[]) {
        block += _countMathBlock(n);
      }
      expect(block, 1);
    });
  });
}
