part of 'chat_controller.dart';

class _ChatVoiceCommandAdapter implements VoiceCommandChatPort {
  _ChatVoiceCommandAdapter(this.owner);

  final ChatController owner;
  final List<Worker> _workers = <Worker>[];

  @override
  String get sessionId => owner.sessionId.trim();

  @override
  bool get isEligibleSession => owner.isAgentPrivateChat;

  @override
  bool get isBusy {
    if (owner.imService.hasStreamingAgentOutputForSession(owner.sessionId)) {
      return true;
    }
    final state = owner.imService.agentOutputStateFor(owner.sessionId);
    final status = state?['state']?.toString().trim() ?? '';
    return status == 'queued' ||
        status == 'received' ||
        status == 'streaming' ||
        status == 'stopping';
  }

  @override
  bool get hasConflictingComposerState =>
      owner.isEditingQueueTask ||
      owner.isUploadingAttachment ||
      owner.hasStagedAttachments ||
      owner.replyingToMessage.value != null ||
      owner.visibleToUserIds.isNotEmpty ||
      owner._pinnedMentions.isNotEmpty;

  @override
  String get draftText => owner.inputController.text;

  @override
  String? get speechLocaleId {
    final locale = Get.locale;
    if (locale == null) return null;
    final country = locale.countryCode?.trim() ?? '';
    return country.isEmpty
        ? locale.languageCode
        : '${locale.languageCode}_$country';
  }

  @override
  String? get speechLanguageTag {
    final locale = Get.locale;
    if (locale == null) return null;
    final country = locale.countryCode?.trim() ?? '';
    return country.isEmpty
        ? locale.languageCode
        : '${locale.languageCode}-$country';
  }

  @override
  VoiceCommandAgentState agentStateFor(VoiceCommandDispatch dispatch) {
    final normalizedSessionId = dispatch.sessionId.trim();
    if (normalizedSessionId.isEmpty) return VoiceCommandAgentState.idle;
    if (owner.imService.hasStreamingAgentOutputForSession(
      normalizedSessionId,
    )) {
      return VoiceCommandAgentState.busy;
    }
    final state = owner.imService.agentOutputStateFor(normalizedSessionId);
    final stateTriggerMsgId = state?['trigger_msg_id']?.toString().trim() ?? '';
    final dispatchTriggerMsgId = _resolveTriggerMessageId(dispatch);
    if (stateTriggerMsgId.isNotEmpty &&
        dispatchTriggerMsgId != null &&
        stateTriggerMsgId != dispatchTriggerMsgId) {
      return VoiceCommandAgentState.idle;
    }
    final status = state?['state']?.toString().trim() ?? '';
    switch (status) {
      case 'queued':
      case 'received':
      case 'streaming':
      case 'stopping':
        return VoiceCommandAgentState.busy;
      case 'completed':
        return VoiceCommandAgentState.completed;
      case 'failed':
        return VoiceCommandAgentState.failed;
      case 'stopped':
        return VoiceCommandAgentState.stopped;
      default:
        return VoiceCommandAgentState.idle;
    }
  }

  @override
  Future<VoiceCommandDispatch?> dispatchFinalTranscript(String text) async {
    final normalized = text.trim();
    final targetSessionId = sessionId;
    if (normalized.isEmpty ||
        targetSessionId.isEmpty ||
        isBusy ||
        hasConflictingComposerState ||
        draftText.trim().isNotEmpty) {
      return null;
    }
    if (!owner._chatInputController.ensureCurrentUserCanSpeak()) {
      return null;
    }
    final blockedReason = owner._chatInputController.validateOutgoingText(
      normalized,
    );
    if (blockedReason != null) {
      CustomToast.show(blockedReason);
      return null;
    }
    if (normalized.runes.length > ChatController._maxInputRunes) {
      CustomToast.show(
        'chat_send_too_long'.trParams({
          'count': '${ChatController._maxInputRunes}',
        }),
      );
      return null;
    }
    final messagesBeforeSend = owner.imService.currentMessages
        .where((message) => message.sessionId == targetSessionId)
        .toList();
    final baseline = messagesBeforeSend
        .map((message) => message.msgId)
        .where((id) => id.isNotEmpty)
        .toSet();
    final clientMessageId = await owner.imService.sendMessageWithClientId(
      normalized,
      targetSessionId,
    );
    if (clientMessageId == null || clientMessageId.trim().isEmpty) return null;
    owner._chatInputController.rememberSuccessfulLocalSend(normalized);
    owner._chatInputController.updateInputValue((_) => TextEditingValue.empty);
    owner._chatInputController.clearDraft();
    Future<void>.delayed(
      const Duration(milliseconds: 100),
      () => owner.scrollToBottom(
        animated: true,
        force: true,
        resumeAutoFollow: true,
      ),
    );
    return VoiceCommandDispatch(
      sessionId: targetSessionId,
      clientMessageId: clientMessageId,
      messageIdsBeforeSend: baseline,
    );
  }

  @override
  VoiceCommandResponse? latestCompletedResponseAfter(
    VoiceCommandDispatch dispatch,
  ) {
    final triggerMessageId = _resolveTriggerMessageId(dispatch);
    if (triggerMessageId == null) return null;
    final candidates = owner.imService.currentMessages.where((message) {
      return message.sessionId == dispatch.sessionId &&
          message.senderType == 2 &&
          message.msgType == 1 &&
          !message.isDeleted &&
          !message.isRevoked &&
          ChatVoiceCommandResponseFilter.isSpeakablePlainText(
            message.content,
          ) &&
          !dispatch.messageIdsBeforeSend.contains(message.msgId) &&
          message.quotedMessageId?.trim() == triggerMessageId;
    }).toList();
    if (candidates.isEmpty) return null;
    candidates.sort((a, b) {
      final byTime = a.createdAt.compareTo(b.createdAt);
      return byTime != 0 ? byTime : a.msgId.compareTo(b.msgId);
    });
    final response = candidates.last;
    return VoiceCommandResponse(text: response.content.trim());
  }

  String? _resolveTriggerMessageId(VoiceCommandDispatch dispatch) {
    for (final message in owner.imService.currentMessages) {
      if (message.sessionId != dispatch.sessionId ||
          message.clientMsgId?.trim() != dispatch.clientMessageId) {
        continue;
      }
      final msgId = message.msgId.trim();
      if (msgId.isEmpty || msgId.startsWith('temp_')) return null;
      return msgId;
    }
    return null;
  }

  @override
  VoiceCommandObserverDisposer observe({required void Function() onChanged}) {
    _disposeWorkers();
    _workers.add(
      debounce(
        owner.imService.currentMessages,
        (_) => onChanged(),
        time: const Duration(milliseconds: 120),
      ),
    );
    _workers.add(ever(owner.imService.agentOutputStates, (_) => onChanged()));
    _workers.add(
      ever(owner.imService.streamingSessionPreviewTickRx, (_) => onChanged()),
    );
    return _disposeWorkers;
  }

  void _disposeWorkers() {
    for (final worker in _workers) {
      worker.dispose();
    }
    _workers.clear();
  }
}
