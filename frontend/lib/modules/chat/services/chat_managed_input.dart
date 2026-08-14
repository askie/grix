import 'package:flutter/material.dart';

enum ChatManagedInputKind { composer, messageCard }

enum ChatManagedInputRevealMode { stickBottom, revealOnly }

enum ChatManagedInputRestoreMode { keepBottom, restoreAnchor, none }

@immutable
class ChatManagedInputId {
  const ChatManagedInputId({required this.kind, required this.instanceId});

  final ChatManagedInputKind kind;
  final String instanceId;

  @override
  bool operator ==(Object other) {
    if (identical(this, other)) {
      return true;
    }
    return other is ChatManagedInputId &&
        other.kind == kind &&
        other.instanceId == instanceId;
  }

  @override
  int get hashCode => Object.hash(kind, instanceId);
}

@immutable
class ChatManagedInputPolicy {
  const ChatManagedInputPolicy({
    required this.revealMode,
    required this.restoreMode,
  });

  static const composer = ChatManagedInputPolicy(
    revealMode: ChatManagedInputRevealMode.stickBottom,
    restoreMode: ChatManagedInputRestoreMode.keepBottom,
  );

  static const messageCard = ChatManagedInputPolicy(
    revealMode: ChatManagedInputRevealMode.revealOnly,
    restoreMode: ChatManagedInputRestoreMode.restoreAnchor,
  );

  final ChatManagedInputRevealMode revealMode;
  final ChatManagedInputRestoreMode restoreMode;
}

@immutable
class ChatManagedInputBinding {
  const ChatManagedInputBinding({
    required this.inputId,
    required this.policy,
    required this.registerTarget,
    required this.unregister,
    required this.updateTargetKey,
    required this.reportFocusChange,
  });

  final ChatManagedInputId inputId;
  final ChatManagedInputPolicy policy;
  final void Function(GlobalKey targetKey) registerTarget;
  final VoidCallback unregister;
  final void Function(GlobalKey targetKey) updateTargetKey;
  final ValueChanged<bool> reportFocusChange;
}
