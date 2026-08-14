import 'package:markdown/markdown.dart' as md;

/// Parses quoted strong spans that are immediately followed by more prose.
///
/// The upstream markdown package follows strict delimiter-run rules, so a span
/// like `**"text"**后续内容` is left as plain text. In chat content this is a
/// common pattern, so we recover it as a strong node before the default
/// emphasis syntax runs.
class ChatMarkdownAdjacentStrongSyntax extends md.InlineSyntax {
  ChatMarkdownAdjacentStrongSyntax()
      : super(
          r'''\*\*((?:"[^"\n]+")|(?:'[^'\n]+')|(?:“[^”\n]+”)|(?:‘[^’\n]+’))\*\*(?=\S)''',
          startCharacter: 0x2A,
        );

  @override
  bool onMatch(md.InlineParser parser, Match match) {
    final text = match.group(1);
    if (text == null || text.isEmpty) {
      return false;
    }

    parser.addNode(md.Element('strong', <md.Node>[md.Text(text)]));
    return true;
  }
}
