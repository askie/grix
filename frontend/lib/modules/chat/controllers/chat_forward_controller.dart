part of 'chat_controller.dart';

class _ChatForwardController {
  const _ChatForwardController(this.owner);

  final ChatController owner;

  bool canForwardMessage(MessageModel message) {
    if (message.msgType == 3) {
      return false;
    }
    if (owner.imService.isMessageStreaming(message.msgId)) {
      return false;
    }
    final status = message.status?.trim() ?? '';
    if (status.startsWith('sending')) {
      return false;
    }
    return message.content.trim().isNotEmpty;
  }

  bool isForwardMessageSelected(MessageModel message) {
    final selectionKey = ChatMessageIdentity.selectionKey(message);
    return owner.forwardSelectionFlagByKey(selectionKey).value;
  }

  void beginForwardSelection(MessageModel message) {
    if (!canForwardMessage(message)) {
      return;
    }

    final selectionKey = ChatMessageIdentity.selectionKey(message);
    owner.clearAllForwardSelectionFlags();
    owner._isForwardSelectionMode.value = true;
    owner._selectedForwardMessageKeys
      ..clear()
      ..add(selectionKey);
    owner.setForwardSelectionState(selectionKey, true);

    owner.showMentionList.value = false;
    owner.mentionSelectedIndex.value = 0;
    owner.cancelReply();
    owner.dismissInputInteraction();
  }

  void toggleForwardMessageSelection(MessageModel message) {
    if (!canForwardMessage(message)) {
      return;
    }

    final selectionKey = ChatMessageIdentity.selectionKey(message);
    if (owner._selectedForwardMessageKeys.contains(selectionKey)) {
      owner._selectedForwardMessageKeys.remove(selectionKey);
      owner.setForwardSelectionState(selectionKey, false);
    } else {
      owner._selectedForwardMessageKeys.add(selectionKey);
      owner.setForwardSelectionState(selectionKey, true);
    }

    if (owner._selectedForwardMessageKeys.isEmpty) {
      owner._isForwardSelectionMode.value = false;
      return;
    }
    owner._isForwardSelectionMode.value = true;
  }

  void exitForwardSelectionMode() {
    owner.clearAllForwardSelectionFlags();
    owner._selectedForwardMessageKeys.clear();
    owner._isForwardSelectionMode.value = false;
  }

  List<MessageModel> collectSelectedForwardMessages() {
    if (owner._selectedForwardMessageKeys.isEmpty) {
      return const <MessageModel>[];
    }

    return owner.imService.currentMessages
        .where((message) {
          if (!canForwardMessage(message)) {
            return false;
          }
          final key = ChatMessageIdentity.selectionKey(message);
          return owner._selectedForwardMessageKeys.contains(key);
        })
        .toList(growable: false)
      ..sort((a, b) => a.createdAt.compareTo(b.createdAt));
  }

  List<ChatForwardTargetOption> buildForwardTargetOptions() {
    return owner._forwardTargetOptionResolver.resolveAll();
  }

  Future<int> forwardConversationCard({
    required String targetSessionId,
    String accompanyingMessage = '',
  }) async {
    if (!owner.canForwardConversationCard) {
      return 0;
    }

    final sid = targetSessionId.trim();
    if (sid.isEmpty) {
      return 0;
    }

    final peerId = owner.isGroupChat ? '' : owner._resolvePrivatePeerUserId();
    final cardEnvelope = ChatMessageCardCodec.buildConversationCard(
      sessionId: owner.sessionId.trim(),
      sessionType: owner.chatType,
      title: owner.displayChatTitle,
      peerId: peerId,
      peerNickname: owner.isGroupChat ? '' : owner.privatePeerNickname.trim(),
      avatarUrl: owner.isGroupChat ? '' : owner.privatePeerAvatarUrl.trim(),
    );
    final content = ChatForwardContentBuilder.buildConversationCardContent(
      cardContent: cardEnvelope.content,
      accompanyingMessage: accompanyingMessage,
    );
    await owner.imService.sendMessage(
      content,
      sid,
      extra: cardEnvelope.extra,
    );
    return 1;
  }

  Future<int> forwardMessages({
    required List<MessageModel> messages,
    required String targetSessionId,
    required ChatForwardDispatchMode mode,
  }) async {
    final sid = targetSessionId.trim();
    if (sid.isEmpty) {
      return 0;
    }

    final forwardable =
        messages.where(canForwardMessage).toList(growable: false)
          ..sort((a, b) => a.createdAt.compareTo(b.createdAt));
    if (forwardable.isEmpty) {
      return 0;
    }

    switch (mode) {
      case ChatForwardDispatchMode.merged:
        final mergedContent = ChatForwardContentBuilder.buildMergedContent(
          messages: forwardable.map(buildForwardMessageItem).toList(),
          title: 'chat_forward_merged_header'.tr,
          senderLabel: 'chat_forward_sender'.tr,
          timeLabel: 'chat_forward_time'.tr,
          emptyContentPlaceholder: 'chat_forward_empty_content'.tr,
        );
        if (mergedContent.trim().isEmpty) {
          return 0;
        }
        await owner.imService.sendMessage(
          mergedContent,
          sid,
          updateCurrentSessionUi: false,
        );
        return forwardable.length;
      case ChatForwardDispatchMode.separate:
        var sentCount = 0;
        for (final message in forwardable) {
          final sanitizedContent = buildForwardSafeContent(message.content);
          if (sanitizedContent.trim().isEmpty) {
            continue;
          }
          await owner.imService.sendMessage(
            sanitizedContent,
            sid,
            extra: buildForwardSendExtra(message.extra),
            updateCurrentSessionUi: false,
          );
          sentCount++;
        }
        return sentCount;
    }
  }

  /// 构建"转发给 Agent"的预填文本：与 merged 转发给人的内容保持一致。
  /// 没有可转发消息时返回空串。
  String buildAgentForwardDraft(List<MessageModel> messages) {
    final forwardable =
        messages.where(canForwardMessage).toList(growable: false)
          ..sort((a, b) => a.createdAt.compareTo(b.createdAt));
    if (forwardable.isEmpty) {
      return '';
    }
    return ChatForwardContentBuilder.buildMergedContent(
      messages: forwardable.map(buildForwardMessageItem).toList(
        growable: false,
      ),
      title: 'chat_forward_merged_header'.tr,
      senderLabel: 'chat_forward_sender'.tr,
      timeLabel: 'chat_forward_time'.tr,
      emptyContentPlaceholder: 'chat_forward_empty_content'.tr,
    );
  }

  ChatForwardMessageItem buildForwardMessageItem(MessageModel message) {
    final isMine = isMineMessage(message);
    final senderName = owner.resolveSenderName(
      senderId: message.senderId,
      isMine: isMine,
      isGroup: owner.isGroupChat,
      senderType: message.senderType,
    );
    return ChatForwardMessageItem(
      senderName: senderName,
      content: buildForwardSafeContent(message.content),
      createdAt: message.createdAt,
    );
  }

  String buildForwardSafeContent(String rawContent) {
    final normalized = owner.formatMessageContentForDisplay(rawContent);
    return ChatForwardMentionSanitizer.neutralizeNumericMentions(normalized);
  }

  Map<String, dynamic>? buildForwardSendExtra(Map<String, dynamic> extra) {
    if (extra.isEmpty) {
      return null;
    }
    final forwarded = Map<String, dynamic>.from(extra)
      ..remove('mention_user_ids')
      ..remove('delegate_origin')
      // 审计是目标 Agent 的本地偏好，不能沿用源消息的逐条审计标记。
      ..remove('audit');
    if (forwarded.isEmpty) {
      return null;
    }
    return forwarded;
  }

  bool isMineMessage(MessageModel message) {
    return ChatMessageOwnerClassifier.isMineMessage(
      message,
      currentUserId: owner.authService.userId,
    );
  }
}
