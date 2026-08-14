import 'package:flutter/foundation.dart';

import 'chat_managed_input.dart';

enum ChatViewportIntentType {
  noop,
  stickBottom,
  revealActiveInput,
  restoreAnchor,
}

@immutable
class ChatViewportAnchor {
  const ChatViewportAnchor({
    required this.itemKey,
    required this.leadingOffset,
  });

  final String itemKey;
  final double leadingOffset;
}

@immutable
class ChatViewportIntent {
  const ChatViewportIntent._({required this.type, this.inputId, this.anchor});

  const ChatViewportIntent.noop() : this._(type: ChatViewportIntentType.noop);

  const ChatViewportIntent.stickBottom()
    : this._(type: ChatViewportIntentType.stickBottom);

  const ChatViewportIntent.revealActiveInput(ChatManagedInputId inputId)
    : this._(type: ChatViewportIntentType.revealActiveInput, inputId: inputId);

  const ChatViewportIntent.restoreAnchor(ChatViewportAnchor anchor)
    : this._(type: ChatViewportIntentType.restoreAnchor, anchor: anchor);

  final ChatViewportIntentType type;
  final ChatManagedInputId? inputId;
  final ChatViewportAnchor? anchor;
}
