import 'chat_markdown_ast.dart';

abstract class ChatMarkdownParserAdapter {
  ChatMarkdownDocument parse(String markdown);
}
