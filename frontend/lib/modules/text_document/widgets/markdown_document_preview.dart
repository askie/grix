import 'package:flutter/material.dart';

import '../../../shared/markdown/chat_markdown_engine.dart';
import '../../../shared/widgets/chat_markdown_render_strategy.dart';
import '../../../shared/widgets/chat_markdown_view.dart';

class MarkdownDocumentPreview extends StatelessWidget {
  const MarkdownDocumentPreview({super.key, required this.source});

  final String source;

  @override
  Widget build(BuildContext context) {
    final renderState = ChatMarkdownEngine.pipeline.prepareFinalRender(source);
    return ChatMarkdownView(
      data: renderState.normalizedText,
      textColor: Theme.of(context).colorScheme.onSurface,
      isMine: false,
      document: renderState.document,
      semantics: renderState.semantics,
      renderStrategy: const ChatMarkdownRenderStrategy(
        renderHtmlAsPlainText: false,
      ),
      sourceMessageId: '',
      selectionEnabled: true,
    );
  }
}
