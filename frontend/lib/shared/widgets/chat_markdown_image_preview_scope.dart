import 'package:flutter/material.dart';

import '../markdown/chat_markdown_ast.dart';
import '../markdown/chat_markdown_uri_policy.dart';

class ChatMarkdownImagePreviewItem {
  const ChatMarkdownImagePreviewItem({
    required this.imageUri,
    this.alt,
  });

  final Uri imageUri;
  final String? alt;
}

class ChatMarkdownImagePreviewCollection {
  ChatMarkdownImagePreviewCollection._({
    required this.items,
    required Map<ChatMarkdownNode, int> indexes,
  }) : _indexes = indexes;

  final List<ChatMarkdownImagePreviewItem> items;
  final Map<ChatMarkdownNode, int> _indexes;

  int? indexOf(ChatMarkdownNode node) => _indexes[node];
}

class ChatMarkdownImagePreviewScope extends InheritedWidget {
  const ChatMarkdownImagePreviewScope({
    super.key,
    required this.items,
    required super.child,
  });

  final List<ChatMarkdownImagePreviewItem> items;

  static ChatMarkdownImagePreviewScope? maybeOf(BuildContext context) {
    return context
        .dependOnInheritedWidgetOfExactType<ChatMarkdownImagePreviewScope>();
  }

  static ChatMarkdownImagePreviewCollection collect(
    ChatMarkdownDocument document,
  ) {
    final items = <ChatMarkdownImagePreviewItem>[];
    final indexes = Map<ChatMarkdownNode, int>.identity();

    void visit(ChatMarkdownNode node) {
      if (node.fallbackReason != null) {
        return;
      }
      if (node.type == ChatMarkdownNodeType.image) {
        final src = node.attrs['src']?.toString() ?? '';
        final imageUri = ChatMarkdownUriPolicy.resolveSafeImageUri(src);
        if (imageUri != null) {
          indexes[node] = items.length;
          items.add(
            ChatMarkdownImagePreviewItem(
              imageUri: imageUri,
              alt: node.attrs['alt']?.toString(),
            ),
          );
        }
      }
      for (final child in node.children) {
        visit(child);
      }
    }

    visit(document);
    return ChatMarkdownImagePreviewCollection._(
      items: List<ChatMarkdownImagePreviewItem>.unmodifiable(items),
      indexes: indexes,
    );
  }

  @override
  bool updateShouldNotify(ChatMarkdownImagePreviewScope oldWidget) {
    return oldWidget.items != items;
  }
}
