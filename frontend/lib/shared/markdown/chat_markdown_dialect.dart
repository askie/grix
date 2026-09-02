import 'package:markdown/markdown.dart' as md;

import 'chat_markdown_adjacent_strong_syntax.dart';
import 'chat_markdown_audio_syntax.dart';
import 'chat_markdown_file_uri_syntax.dart';
import 'chat_markdown_latex_syntax.dart';
import 'chat_markdown_syntax.dart';
import 'chat_markdown_video_syntax.dart';
import 'package_markdown_parser_adapter.dart';

class ChatMarkdownDialect {
  const ChatMarkdownDialect._();

  static md.ExtensionSet buildExtensionSet() {
    return md.ExtensionSet(
      List.of(md.ExtensionSet.gitHubFlavored.blockSyntaxes)
        ..insert(0, const IndentedTableSyntax())
        ..insert(0, const LatexEnvironmentBlockSyntax())
        ..insert(0, const LatexBlockSyntax())
        ..insert(0, const ChatMarkdownVideoBlockSyntax())
        ..insert(0, const ChatMarkdownAudioBlockSyntax()),
      List.of(md.ExtensionSet.gitHubFlavored.inlineSyntaxes)
        ..insert(0, ChatMarkdownAdjacentStrongSyntax())
        ..insert(0, ChatMarkdownFileUriSyntax())
        ..insert(0, ChatMarkdownVideoInlineSyntax())
        ..insert(0, ChatMarkdownAudioInlineSyntax())
        ..insert(0, LatexInlineSyntax()),
    );
  }

  static PackageMarkdownParserAdapter buildParserAdapter() {
    return PackageMarkdownParserAdapter(
      extensionSet: buildExtensionSet(),
    );
  }
}
