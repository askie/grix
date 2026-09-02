import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/markdown/chat_markdown_ast.dart';
import 'package:grix/shared/markdown/chat_markdown_dialect.dart';

void main() {
  test('links a bare file URI in prose', () {
    final document = _parse('打开 file:///workspace/README.md 看看');

    final links = _findLinks(document);
    expect(links, hasLength(1));
    expect(links.single.attrs['href'], 'file:///workspace/README.md');
  });

  test('keeps trailing punctuation and brackets out of the link', () {
    expect(_hrefs('见 file:///workspace/a.md。'), ['file:///workspace/a.md']);
    expect(_hrefs('见 (file:///workspace/a.md)'), ['file:///workspace/a.md']);
    expect(_hrefs('见 file:///workspace/a.md, 以及别的'), [
      'file:///workspace/a.md',
    ]);
  });

  test('leaves markdown links and code spans untouched', () {
    expect(_hrefs('[README](file:///workspace/README.md)'), [
      'file:///workspace/README.md',
    ]);
    expect(_hrefs('`file:///workspace/README.md`'), isEmpty);
  });

  test('does not link file URIs that point at another host', () {
    expect(_hrefs('见 file://example.com/share/a.md'), isEmpty);
  });

  test('keeps an ampersand in the path intact', () {
    final document = _parse('见 file:///workspace/a&b.md');
    final links = _findLinks(document);
    expect(links, hasLength(1));
    expect(links.single.attrs['href'], 'file:///workspace/a&b.md');
    expect(
      links.single.children.single.attrs['text'],
      'file:///workspace/a&b.md',
    );
  });

  test('still autolinks http URLs', () {
    expect(_hrefs('见 https://example.com/a'), ['https://example.com/a']);
  });
}

ChatMarkdownNode _parse(String source) =>
    ChatMarkdownDialect.buildParserAdapter().parse(source);

List<String> _hrefs(String source) => _findLinks(
  _parse(source),
).map((node) => node.attrs['href']?.toString() ?? '').toList();

List<ChatMarkdownNode> _findLinks(ChatMarkdownNode node) {
  final nodes = <ChatMarkdownNode>[];
  if (node.type == ChatMarkdownNodeType.link ||
      node.type == ChatMarkdownNodeType.autolink) {
    nodes.add(node);
  }
  for (final child in node.children) {
    nodes.addAll(_findLinks(child));
  }
  return nodes;
}
