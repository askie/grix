import 'chat_markdown_dialect.dart';
import 'chat_markdown_normalizer.dart';
import 'chat_markdown_pipeline.dart';

/// Shared construction point for the Markdown dialect used by chat messages
/// and standalone documents.
class ChatMarkdownEngine {
  ChatMarkdownEngine._();

  static const ChatMarkdownNormalizer normalizer = ChatMarkdownNormalizer();

  static final ChatMarkdownPipeline pipeline = ChatMarkdownPipeline(
    normalizer: normalizer,
    parser: ChatMarkdownDialect.buildParserAdapter(),
  );
}
