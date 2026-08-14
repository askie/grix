import 'package:flutter/material.dart';

import 'chat_managed_input.dart';

@immutable
class ChatManagedInputRegistration {
  const ChatManagedInputRegistration({
    required this.inputId,
    required this.policy,
    required this.targetKey,
  });

  final ChatManagedInputId inputId;
  final ChatManagedInputPolicy policy;
  final GlobalKey targetKey;
}

class ChatManagedInputRegistry {
  final Map<ChatManagedInputId, ChatManagedInputRegistration> _registrations =
      <ChatManagedInputId, ChatManagedInputRegistration>{};

  ChatManagedInputRegistration? registrationOf(ChatManagedInputId inputId) {
    return _registrations[inputId];
  }

  ChatManagedInputPolicy? policyOf(ChatManagedInputId inputId) {
    return _registrations[inputId]?.policy;
  }

  BuildContext? currentContextOf(ChatManagedInputId inputId) {
    return _registrations[inputId]?.targetKey.currentContext;
  }

  void register({
    required ChatManagedInputId inputId,
    required ChatManagedInputPolicy policy,
    required GlobalKey targetKey,
  }) {
    _registrations[inputId] = ChatManagedInputRegistration(
      inputId: inputId,
      policy: policy,
      targetKey: targetKey,
    );
  }

  void updateTargetKey(ChatManagedInputId inputId, GlobalKey targetKey) {
    final registration = _registrations[inputId];
    if (registration == null) {
      return;
    }
    if (identical(registration.targetKey, targetKey)) {
      return;
    }
    _registrations[inputId] = ChatManagedInputRegistration(
      inputId: registration.inputId,
      policy: registration.policy,
      targetKey: targetKey,
    );
  }

  void unregister(ChatManagedInputId inputId) {
    _registrations.remove(inputId);
  }
}
