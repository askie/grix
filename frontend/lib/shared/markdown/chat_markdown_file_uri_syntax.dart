import 'package:markdown/markdown.dart' as md;

import 'chat_markdown_uri_policy.dart';

/// Turns a bare `file://` URI in prose into a link node.
///
/// GitHub flavored autolinking only covers `http`, `https`, `ftp`, `www.` and
/// email addresses, so an agent that writes a plain `file:///workspace/a.md`
/// would otherwise render as unclickable text. Only URIs that
/// [ChatMarkdownUriPolicy.resolveAgentFilePath] accepts become links; anything
/// else stays literal text.
class ChatMarkdownFileUriSyntax extends md.InlineSyntax {
  ChatMarkdownFileUriSyntax() : super(_pattern, caseSensitive: false);

  /// Stops at whitespace, quoting and closing brackets so a URI inside prose
  /// such as `(见 file:///tmp/a.md)` does not swallow the surrounding text, and
  /// refuses to end on sentence punctuation.
  static const String _pattern =
      r"""file://[^\s<>"'`)\]}\\]*[^\s<>"'`)\]}\\.,;:!?。，、；：！？]""";

  @override
  bool onMatch(md.InlineParser parser, Match match) {
    final href = match[0]!;
    // Text nodes are HTML encoded when the parser encodes HTML, and the AST
    // adapter decodes them again. `&` is the only character the match can
    // contain that survives that round trip unescaped; the href itself is
    // carried through verbatim.
    final label = parser.encodeHtml ? href.replaceAll('&', '&amp;') : href;
    if (ChatMarkdownUriPolicy.resolveAgentFilePath(href) == null) {
      parser.addNode(md.Text(label));
      return true;
    }
    final anchor = md.Element.text('a', label);
    anchor.attributes['href'] = href;
    parser.addNode(anchor);
    return true;
  }
}
