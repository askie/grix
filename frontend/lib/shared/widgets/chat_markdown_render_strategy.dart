import '../markdown/chat_markdown_ast.dart';
import '../markdown/chat_markdown_semantics.dart';
import 'chat_markdown_ast_view.dart';

enum ChatMarkdownRenderMode {
  fallbackPlainText,
  fallbackMarkdownWidget,
  nativeAst,
}

class ChatMarkdownRenderStrategy {
  const ChatMarkdownRenderStrategy({this.renderHtmlAsPlainText = true});

  /// Chat messages keep HTML-bearing content in plain text for safety. Trusted
  /// document previews can disable this and use the Markdown widget fallback,
  /// which renders surrounding Markdown without executing raw HTML.
  final bool renderHtmlAsPlainText;

  ChatMarkdownRenderMode select({
    required ChatMarkdownDocument? document,
    required ChatMarkdownSemanticSummary? semantics,
  }) {
    if (renderHtmlAsPlainText &&
        (semantics?.hasFeature(ChatMarkdownFeature.html) ?? false)) {
      return ChatMarkdownRenderMode.fallbackPlainText;
    }
    if (!_shouldUseNativeAst(document: document, semantics: semantics)) {
      return ChatMarkdownRenderMode.fallbackMarkdownWidget;
    }
    return ChatMarkdownRenderMode.nativeAst;
  }

  bool _shouldUseNativeAst({
    required ChatMarkdownDocument? document,
    required ChatMarkdownSemanticSummary? semantics,
  }) {
    if (document == null || semantics == null) {
      return false;
    }
    return ChatMarkdownAstView.supportsDocument(document);
  }
}
