part of 'chat_controller.dart';

class _ChatInputController {
  _ChatInputController(this.owner);

  static const Duration _draftPersistDebounceDuration = Duration(
    milliseconds: 180,
  );
  // Keep the latest draft in memory so back-navigation can restore it
  // immediately even if persistent storage is still catching up.
  static final Map<String, String> _draftMemoryCache = <String, String>{};
  static final Map<String, List<PendingAttachmentUpload>>
      _attachmentDraftMemoryCache =
      <String, List<PendingAttachmentUpload>>{};
  static const _draftAttachDirName = 'chat_draft_attach';
  static final Map<String, String> _replyDraftMemoryCache =
      <String, String>{};
  static final Map<String, String> _pinnedMentionDraftMemoryCache =
      <String, String>{};

  final ChatController owner;
  String? _pendingReplyDraftMsgId;

  void bind() {
    owner.inputController.addListener(owner._onInputTextChanged);
    owner._hasInputFocus.value = owner.focusNode.hasFocus;
    owner.focusNode.addListener(owner._handleInputFocusChanged);
  }

  void onClose() {
    saveDraft(immediate: true);
    cancelPendingInputFocusRetention();
    owner._pendingInputEdits.clear();
    owner._deferredInputEditFlushScheduled = false;
    owner._flushingDeferredInputEdits = false;
    owner.inputController.removeListener(owner._onInputTextChanged);
    owner.focusNode.removeListener(owner._handleInputFocusChanged);
    owner._keyboardMetricsSettledTimer?.cancel();
    owner._retainInputLayoutKeyboardInsetTimer?.cancel();
    owner._iOSKeyboardDropHysteresisTimer?.cancel();
    owner._restoreInputFocusTimer?.cancel();
    owner._imeResumeRebuildTimer?.cancel();
    owner._composingDebounce?.cancel();
    owner._draftPersistDebounce?.cancel();
    owner._draftPersistDebounce = null;
    _detachInputFocusBeforeDispose();
    owner.inputController.dispose();
    owner.focusNode.dispose();
  }

  void _detachInputFocusBeforeDispose() {
    owner._retainInputLayoutKeyboardInset = false;
    owner._inputLayoutKeyboardInsetBottom.value = 0;
    owner._hasInputFocus.value = false;
    owner._deactivateManagedInput(ChatController._composerManagedInputId);
    owner.closeAttachmentMenu();
    final primaryFocus = FocusManager.instance.primaryFocus;
    final shouldUnfocus =
        owner.focusNode.hasFocus || identical(primaryFocus, owner.focusNode);
    if (!shouldUnfocus) {
      return;
    }
    owner.focusNode.unfocus();
    primaryFocus?.unfocus();
  }

  void setReplyingToMessage(MessageModel msg) {
    owner.replyingToMessage.value = msg;
    owner.focusNode.requestFocus();
  }

  void cancelReply() {
    owner.replyingToMessage.value = null;
  }

  void handleInputFocusChanged() {
    owner._hasInputFocus.value = owner.focusNode.hasFocus;
    if (!owner.focusNode.hasFocus) {
      owner._deactivateManagedInput(ChatController._composerManagedInputId);
      cancelPendingInputFocusRetention();
      if (!owner._suppressMenuCloseOnFocusLoss) {
        owner.closeAttachmentMenu();
      }
      owner._suppressMenuCloseOnFocusLoss = false;
    } else {
      owner._activateManagedInput(
        ChatController._composerManagedInputId,
        ChatManagedInputPolicy.composer,
      );
    }
    // iOS 专项：焦点丢失时若键盘槽仍有高度（如搜狗切语音模式取走焦点但键盘区仍占位），
    // 用物理键盘 inset 代替 _currentInputViewportInsetBottom（shouldFollow=false 时为 0），
    // 避免输入栏在键盘还在时就提前下落被 IME 语音 UI 遮盖。
    // dismissInputInteraction() 会在 unfocus 后再次调用 syncInputLayoutKeyboardInset()
    // （不带 rawValue），使输入栏随键盘正常回落，主动收起流程不受影响。
    final rawInset =
        (!owner.focusNode.hasFocus &&
            owner.keyboardPlatformBehavior
                .shouldApplyFocusedZeroInsetHysteresis &&
            owner._lastKeyboardInsetBottom > 0)
        ? owner._lastKeyboardInsetBottom
        : null;
    syncInputLayoutKeyboardInset(rawKeyboardInsetBottom: rawInset);
    owner._pageStateController.onBottomDockLayoutChanged();
  }

  // iOS 第三方输入法（搜狗/微信）专项：App 切到后台时记录输入框是否正在打字。
  // 只在真正进入后台（paused）时记录，inactive（控制中心/通知栏下拉等瞬态）不算，
  // 避免切回来时无谓地重建连接、键盘闪烁。
  void handleAppPausedForIme(AppLifecycleState state) {
    if (owner.keyboardPlatformBehavior.kind != ChatKeyboardPlatformKind.ios) {
      return;
    }
    if (state != AppLifecycleState.paused) {
      return;
    }
    owner._imeWasFocusedBeforeBackground = owner.focusNode.hasFocus;
  }

  // App 切回前台：若切出去前输入框在打字，文本框的原生输入连接此时可能已失效
  // （Flutter 以为还连着、实际断了），第三方输入法候选词栏会卡死。
  // 两头覆盖修复：① unfocus→requestFocus 强制重建输入连接（治连接丢失）；
  // ② 重新同步一次键盘 inset（治生命周期路径下 inset 脏状态导致的输入栏错位）。
  void handleAppResumedForIme() {
    if (owner.keyboardPlatformBehavior.kind != ChatKeyboardPlatformKind.ios) {
      return;
    }
    if (!owner._imeWasFocusedBeforeBackground) {
      return;
    }
    owner._imeWasFocusedBeforeBackground = false;
    if (owner.isClosed || !owner.focusNode.canRequestFocus) {
      return;
    }
    // 先丢焦点，关闭已失效的旧连接；再延迟重新请求焦点，让原生侧重新挂载 IME。
    owner.focusNode.unfocus();
    owner._imeResumeRebuildTimer?.cancel();
    owner._imeResumeRebuildTimer = Timer(
      const Duration(milliseconds: 100),
      () {
        owner._imeResumeRebuildTimer = null;
        if (owner.isClosed || !owner.focusNode.canRequestFocus) {
          return;
        }
        owner.focusNode.requestFocus();
        syncInputLayoutKeyboardInset();
      },
    );
  }

  void retainInputLayoutKeyboardInsetDuringSubmit() {
    if (!owner.focusNode.hasFocus) {
      return;
    }
    final visibleInsetBottom = owner._currentInputViewportInsetBottom;
    if (visibleInsetBottom <= 0) {
      return;
    }
    final text = owner.inputController.text.trim();
    if (text.isEmpty || owner.isInputComposing) {
      return;
    }
    owner._lastVisibleKeyboardInsetBottom = visibleInsetBottom;
    owner._retainInputLayoutKeyboardInset = true;
    owner._retainInputLayoutKeyboardInsetTimer?.cancel();
    final delay = owner.keyboardPlatformBehavior.submitInsetRetentionDuration;
    owner._retainInputLayoutKeyboardInsetTimer = Timer(delay, () {
      owner._retainInputLayoutKeyboardInset = false;
      syncInputLayoutKeyboardInset();
    });
  }

  void syncInputLayoutKeyboardInset({double? rawKeyboardInsetBottom}) {
    final keyboardInsetBottom =
        rawKeyboardInsetBottom ?? owner._currentInputViewportInsetBottom;
    if (keyboardInsetBottom > 0) {
      // 键盘可见：立即更新，同时取消所有待定的 drop 操作
      owner._retainInputLayoutKeyboardInset = false;
      owner._retainInputLayoutKeyboardInsetTimer?.cancel();
      owner._iOSKeyboardDropHysteresisTimer?.cancel();
      owner._iOSKeyboardDropHysteresisTimer = null;
      owner._inputLayoutKeyboardInsetBottom.value = keyboardInsetBottom;
      return;
    }
    // 键盘 inset 降为 0
    // 优先：显式 retain（发送稳定化逻辑设置）
    if (owner._retainInputLayoutKeyboardInset &&
        owner._lastVisibleKeyboardInsetBottom > 0) {
      owner._inputLayoutKeyboardInsetBottom.value =
          owner._lastVisibleKeyboardInsetBottom;
      return;
    }
    // iOS 专项：当输入框仍有焦点时，iOS 键盘 action（发送键）会在 onSubmitted
    // 回调触发前就改变 viewInsets（IME 状态清理/suggestion bar 动画），导致布局
    // 瞬间跳动。此处加 150ms 缓冲：若键盘在此期间恢复，自动取消 drop；若键盘
    // 真正消失（用户主动收起），dismissInputInteraction 会立即清除此 timer。
    if (owner.keyboardPlatformBehavior.shouldApplyFocusedZeroInsetHysteresis &&
        owner.focusNode.hasFocus &&
        owner._lastVisibleKeyboardInsetBottom > 0) {
      owner._inputLayoutKeyboardInsetBottom.value =
          owner._lastVisibleKeyboardInsetBottom;
      owner._iOSKeyboardDropHysteresisTimer?.cancel();
      owner._iOSKeyboardDropHysteresisTimer = Timer(
        owner.keyboardPlatformBehavior.focusedZeroInsetHysteresisDuration,
        () {
          owner._iOSKeyboardDropHysteresisTimer = null;
          // 若键盘确实消失且输入框已失焦，才执行真正的 drop。
          // 若仍有焦点（如搜狗等输入法切语音模式时键盘 inset 瞬降但焦点保持），
          // 保留布局高度，待焦点真正丢失时由 handleInputFocusChanged 触发回落。
          if (owner._lastKeyboardInsetBottom <= 0 &&
              !owner.focusNode.hasFocus) {
            owner._retainInputLayoutKeyboardInset = false;
            owner._inputLayoutKeyboardInsetBottom.value = 0;
          }
        },
      );
      return;
    }
    owner._retainInputLayoutKeyboardInset = false;
    owner._retainInputLayoutKeyboardInsetTimer?.cancel();
    owner._inputLayoutKeyboardInsetBottom.value = 0;
  }

  void insertInputLineBreak() {
    replaceInputSelection('\n');
  }

  void dismissInputInteraction() {
    cancelPendingInputFocusRetention();
    owner._retainInputLayoutKeyboardInset = false;
    owner._retainInputLayoutKeyboardInsetTimer?.cancel();
    // 用户主动收起键盘：立即取消 iOS hysteresis，确保布局即时跟随键盘收起
    owner._iOSKeyboardDropHysteresisTimer?.cancel();
    owner._iOSKeyboardDropHysteresisTimer = null;
    owner.closeAttachmentMenu();
    FocusManager.instance.primaryFocus?.unfocus();
    syncInputLayoutKeyboardInset();
  }

  void replaceInputSelection(String replacement) {
    updateInputValue((value) {
      final selection = normalizeSelection(value.selection, value.text.length);
      final prefix = value.text.substring(0, selection.start);
      final suffix = value.text.substring(selection.end);
      final nextText = '$prefix$replacement$suffix';
      final nextCursorOffset = prefix.length + replacement.length;
      return TextEditingValue(
        text: nextText,
        selection: TextSelection.collapsed(offset: nextCursorOffset),
        composing: TextRange.empty,
      );
    }, requestFocus: true);
  }

  TextSelection normalizeSelection(TextSelection selection, int textLength) {
    if (!selection.isValid) {
      return TextSelection.collapsed(offset: textLength);
    }

    var baseOffset = selection.baseOffset;
    var extentOffset = selection.extentOffset;
    if (baseOffset < 0 || extentOffset < 0) {
      return TextSelection.collapsed(offset: textLength);
    }
    if (baseOffset > textLength) {
      baseOffset = textLength;
    }
    if (extentOffset > textLength) {
      extentOffset = textLength;
    }
    return TextSelection(
      baseOffset: baseOffset,
      extentOffset: extentOffset,
      affinity: selection.affinity,
      isDirectional: selection.isDirectional,
    );
  }

  TextRange normalizeComposingRange(TextRange composing, int textLength) {
    if (!composing.isValid || composing.isCollapsed) {
      return TextRange.empty;
    }

    var start = composing.start;
    var end = composing.end;
    if (start < 0) {
      start = 0;
    }
    if (end < 0) {
      end = 0;
    }
    if (start > textLength) {
      start = textLength;
    }
    if (end > textLength) {
      end = textLength;
    }
    if (start >= end) {
      return TextRange.empty;
    }
    return TextRange(start: start, end: end);
  }

  TextEditingValue sanitizeEditingValue(TextEditingValue value) {
    final selection = normalizeSelection(value.selection, value.text.length);
    final composing = normalizeComposingRange(
      value.composing,
      value.text.length,
    );
    if (selection == value.selection && composing == value.composing) {
      return value;
    }
    return value.copyWith(selection: selection, composing: composing);
  }

  void updateInputValue(
    _ChatInputValueTransformer transformer, {
    bool requestFocus = false,
  }) {
    if (hasActiveInputComposition(owner.inputController.value)) {
      owner._pendingInputEdits.add(
        _DeferredChatInputEdit(
          transformer: transformer,
          requestFocus: requestFocus,
        ),
      );
      return;
    }
    if (owner._pendingInputEdits.isNotEmpty) {
      owner._pendingInputEdits.add(
        _DeferredChatInputEdit(
          transformer: transformer,
          requestFocus: requestFocus,
        ),
      );
      flushDeferredInputEdits();
      return;
    }
    _applyInputValueUpdate(transformer, requestFocus: requestFocus);
  }

  void scheduleDeferredInputEditsFlush() {
    if (owner._pendingInputEdits.isEmpty ||
        owner._deferredInputEditFlushScheduled ||
        owner._flushingDeferredInputEdits ||
        hasActiveInputComposition(owner.inputController.value)) {
      return;
    }
    owner._deferredInputEditFlushScheduled = true;
    scheduleMicrotask(() {
      owner._deferredInputEditFlushScheduled = false;
      flushDeferredInputEdits();
    });
  }

  void flushDeferredInputEdits() {
    if (owner._pendingInputEdits.isEmpty ||
        owner._flushingDeferredInputEdits ||
        hasActiveInputComposition(owner.inputController.value)) {
      return;
    }

    final edits = List<_DeferredChatInputEdit>.from(owner._pendingInputEdits);
    owner._pendingInputEdits.clear();
    owner._flushingDeferredInputEdits = true;
    try {
      var nextValue = sanitizeEditingValue(owner.inputController.value);
      var shouldRequestFocus = false;
      for (final edit in edits) {
        nextValue = sanitizeEditingValue(edit.transformer(nextValue));
        shouldRequestFocus = shouldRequestFocus || edit.requestFocus;
      }
      final currentValue = owner.inputController.value;
      if (nextValue != currentValue) {
        owner.inputController.value = nextValue;
      }
      final focusTarget = owner.activeInputFocusNode;
      if (shouldRequestFocus &&
          focusTarget.canRequestFocus &&
          !focusTarget.hasFocus) {
        focusTarget.requestFocus();
      }
    } finally {
      owner._flushingDeferredInputEdits = false;
    }
  }

  void _applyInputValueUpdate(
    _ChatInputValueTransformer transformer, {
    bool requestFocus = false,
  }) {
    final currentValue = sanitizeEditingValue(owner.inputController.value);
    final nextValue = sanitizeEditingValue(transformer(currentValue));
    if (nextValue != owner.inputController.value) {
      owner.inputController.value = nextValue;
    }
    final focusTarget = owner.activeInputFocusNode;
    if (requestFocus &&
        focusTarget.canRequestFocus &&
        !focusTarget.hasFocus) {
      focusTarget.requestFocus();
    }
  }

  TextSelection? resolveInputSelectionWithinBounds(
    TextSelection selection,
    int textLength,
  ) {
    if (!selection.isValid) {
      return null;
    }
    final baseOffset = selection.baseOffset;
    final extentOffset = selection.extentOffset;
    if (baseOffset < 0 || extentOffset < 0) {
      return null;
    }
    if (baseOffset > textLength || extentOffset > textLength) {
      return null;
    }
    return selection;
  }

  bool dispatchCurrentInputMessage() {
    flushDeferredInputEdits();
    if (owner.sessionId.isEmpty || owner.isInputComposing) {
      return false;
    }
    if (owner.isUploadingAttachment) {
      return false;
    }
    final text = owner.inputController.text.trim();
    final hasStaged = owner.hasStagedAttachments;
    final hasPinned = owner._pinnedMentions.isNotEmpty;
    if (text.isEmpty && !hasStaged && !hasPinned) {
      return false;
    }
    if ((text.isNotEmpty || hasPinned) && !ensureCurrentUserCanSpeak()) {
      return false;
    }

    final previewDispatchContent = (text.isNotEmpty || hasPinned)
        ? owner._chatMentionController.buildDispatchContent(text)
        : '';
    if (previewDispatchContent.isNotEmpty) {
      final blockedReason = validateOutgoingText(previewDispatchContent);
      if (blockedReason != null) {
        CustomToast.show(blockedReason);
        return false;
      }
    }

    owner.closeAttachmentMenu();
    final replyId = owner.replyingToMessage.value?.msgId;
    final visibleTo = owner.visibleToUserIds.isNotEmpty
        ? owner.visibleToUserIds.toList()
        : null;

    if (hasStaged) {
      final capturedStaged = List<PendingAttachmentUpload>.from(
        owner.stagedAttachments,
      );
      final mentionExtra = (text.isNotEmpty || hasPinned)
          ? owner._chatMentionController.buildMentionExtraWithPinned(text)
          : null;
      final normalizedText = previewDispatchContent;
      owner.clearVisibleTo();
      cancelReply();
      _uploadAndSendAttachments(
        capturedStaged,
        normalizedText,
        mentionExtra,
        replyId,
        visibleTo,
      );
    } else {
      final mentionExtra = owner._chatMentionController
          .buildMentionExtraWithPinned(text);
      final sendContent = previewDispatchContent;

      if (sendContent.runes.length > ChatController._maxInputRunes) {
        CustomToast.show(
          'chat_send_too_long'.trParams({
            'count': '${ChatController._maxInputRunes}',
          }),
        );
        return false;
      }
      owner.imService.sendMessage(
        sendContent,
        owner.sessionId,
        extra: mentionExtra,
        quotedMessageId: replyId,
        visibleTo: visibleTo,
      );
      if (sendContent.isNotEmpty) {
        rememberSuccessfulLocalSend(sendContent);
      }
      updateInputValue((_) => TextEditingValue.empty);
      clearDraft();
      owner._chatMentionController.clearAfterMessageDispatched();
      owner.clearVisibleTo();
      cancelReply();
    }

    if (owner.keyboardPlatformBehavior.shouldRestoreComposerFocusAfterSubmit) {
      if (!owner.focusNode.hasFocus) {
        owner.focusNode.requestFocus();
      }
    }
    Future.delayed(
      const Duration(milliseconds: 100),
      () => owner.scrollToBottom(
        animated: true,
        force: true,
        resumeAutoFollow: true,
      ),
    );
    return true;
  }

  Future<void> _uploadAndSendAttachments(
    List<PendingAttachmentUpload> staged,
    String normalizedText,
    Map<String, dynamic>? mentionExtra,
    String? quotedMessageId,
    List<String>? visibleTo,
  ) async {
    owner._isUploadingAttachment.value = true;
    try {
      final uploaded = await owner._chatAttachmentController
          ._uploadRawAttachments(staged);
      if (uploaded.isEmpty) {
        CustomToast.show('oss_upload_failed'.tr, isError: true);
        return;
      }

      final attachmentContent =
          ChatAttachmentPayloadBuilder.buildMessageContent(uploaded);
      final content = normalizedText.isNotEmpty
          ? '$normalizedText\n$attachmentContent'
          : attachmentContent;
      final attachmentExtra = ChatAttachmentPayloadBuilder.buildMessageExtra(
        uploaded,
      );
      final mergedExtra = <String, dynamic>{};
      if (mentionExtra != null) mergedExtra.addAll(mentionExtra);
      mergedExtra.addAll(attachmentExtra);

      owner.imService.sendMessage(
        content,
        owner.sessionId,
        extra: mergedExtra.isEmpty ? null : mergedExtra,
        quotedMessageId: quotedMessageId,
        visibleTo: visibleTo,
      );
      if (normalizedText.isNotEmpty) {
        rememberSuccessfulLocalSend(normalizedText);
      }
      updateInputValue((_) => TextEditingValue.empty);
      clearDraft();
      owner.stagedAttachments.clear();
      owner._chatMentionController.clearAfterMessageDispatched();
      owner.clearVisibleTo();
      cancelReply();
    } catch (e) {
      debugPrint('_uploadAndSendAttachments error: $e');
      CustomToast.show('oss_upload_error'.tr, isError: true);
    } finally {
      owner._isUploadingAttachment.value = false;
    }
  }

  String? validateOutgoingText(String text) {
    if (text.isEmpty) {
      return null;
    }

    final now = DateTime.now();
    if (owner._lastSendAt != null &&
        now.difference(owner._lastSendAt!) < ChatController._sendCooldown) {
      return 'chat_send_too_fast'.tr;
    }

    return null;
  }

  bool ensureCurrentUserCanSpeak() {
    final reason = owner.currentUserSpeakingBlockedReason;
    if (reason.isEmpty) {
      return true;
    }
    CustomToast.show(reason);
    return false;
  }

  void rememberSuccessfulLocalSend(String text) {
    owner._lastSendAt = DateTime.now();
  }

  void suppressNextInputSubmit() {
    owner._suppressNextInputSubmit = true;
  }

  void submitMessageFromHardwareEnter() {
    suppressNextInputSubmit();
    submitMessageWhileStabilizingInput();
  }

  void submitMessageFromInputAction() {
    if (consumePendingInputSubmitSuppression()) {
      return;
    }
    if (owner.isInputComposing) {
      suppressNextInputSubmit();
      return;
    }
    insertInputLineBreak();
  }

  void submitMessageWhileStabilizingInput() {
    retainInputLayoutKeyboardInsetDuringSubmit();
    final sent = owner.dispatchCurrentInputMessage();
    if (!sent) {
      return;
    }
    retainInputFocusAfterSubmit();
  }

  void retainInputFocusAfterSubmit() {
    if (owner.isClosed || !owner.focusNode.canRequestFocus) {
      return;
    }

    if (!owner.keyboardPlatformBehavior.shouldRestoreComposerFocusAfterSubmit) {
      return;
    }

    owner._restoreInputFocusPending = true;
    final retentionVersion = ++owner._inputFocusRetentionVersion;
    requestInputFocusIfStillRetained(retentionVersion);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      requestInputFocusIfStillRetained(retentionVersion);
    });
    owner._restoreInputFocusTimer?.cancel();
    owner._restoreInputFocusTimer = Timer(
      ChatController._inputSubmitStabilizationDuration,
      () {
        owner._restoreInputFocusTimer = null;
        owner._restoreInputFocusPending = false;
        requestInputFocusIfStillRetained(retentionVersion);
      },
    );
  }

  void requestInputFocusIfStillRetained(int retentionVersion) {
    if (owner.isClosed ||
        retentionVersion != owner._inputFocusRetentionVersion ||
        !owner.focusNode.canRequestFocus) {
      return;
    }
    if (!owner.focusNode.hasFocus) {
      owner.focusNode.requestFocus();
    }
  }

  void cancelPendingInputFocusRetention() {
    owner._inputFocusRetentionVersion++;
    owner._restoreInputFocusTimer?.cancel();
    owner._restoreInputFocusTimer = null;
    owner._restoreInputFocusPending = false;
  }

  bool hasActiveInputComposition(TextEditingValue value) {
    final composing = value.composing;
    if (!composing.isValid || composing.isCollapsed) {
      return false;
    }
    return composing.start <= value.text.length &&
        composing.end <= value.text.length;
  }

  void clearPendingInputSubmitSuppressionForNewKeyPress() {
    if (!owner._suppressNextInputSubmit) {
      return;
    }
    clearPendingInputSubmitSuppression();
  }

  bool consumePendingInputSubmitSuppression() {
    if (!owner._suppressNextInputSubmit) {
      return false;
    }
    clearPendingInputSubmitSuppression();
    return true;
  }

  void clearPendingInputSubmitSuppression() {
    owner._suppressNextInputSubmit = false;
  }

  void onInputTextChanged() {
    final active = owner.inputController.text.trim().isNotEmpty;
    owner.isInputOverLengthLimit.value =
        owner.inputController.text.runes.length > ChatController._maxInputRunes;
    final inputText = owner.inputController.text;
    owner.showInputExpandButton.value =
        '\n'.allMatches(inputText).length >= 2 || inputText.runes.length > 90;
    if (active != owner._lastComposingActive) {
      owner._lastComposingActive = active;
      owner._composingDebounce?.cancel();
      owner.imService.updateSessionComposing(owner.sessionId, active: active);
    } else if (active) {
      // 每次原始击键都触达一次：续期中不会重复发包，只重置空闲超时，
      // 避免快速连续输入超过 60s 时（防抖迟迟不触发）被空闲兜底误掐。
      owner.imService.updateSessionComposing(owner.sessionId, active: active);
      owner._composingDebounce?.cancel();
      owner._composingDebounce = Timer(const Duration(milliseconds: 500), () {
        owner.imService.updateSessionComposing(owner.sessionId, active: active);
      });
    }
    owner._chatMentionController.handleInputTextChanged();
    owner._syncGroupToolbarTargetAgent();
    saveDraft();
    scheduleDeferredInputEditsFlush();
  }

  String _draftKey() {
    final userId = owner.authService.userId ?? '';
    return 'chat_draft_${userId}_${owner.sessionId}';
  }

  void saveDraft({bool immediate = false}) {
    if (owner.sessionId.isEmpty) return;
    final key = _draftKey();
    final text = owner.inputController.text;
    final shouldPersistImmediately = immediate || text.isEmpty;
    if (!shouldPersistImmediately && text == owner._lastDraftSnapshot) {
      return;
    }
    owner._lastDraftSnapshot = text;
    _cacheDraft(key, text);
    ChatDraftIndex.update(
      sessionId: owner.sessionId,
      hasDraft: text.trim().isNotEmpty,
    );
    owner._draftPersistDebounce?.cancel();
    if (shouldPersistImmediately) {
      owner._draftPersistDebounce = null;
      unawaited(_persistDraft(key, text));
      unawaited(_persistAttachmentDrafts());
      _saveReplyDraft();
      persistPinnedMentionsDraft();
      return;
    }
    owner._draftPersistDebounce = Timer(_draftPersistDebounceDuration, () {
      owner._draftPersistDebounce = null;
      unawaited(_persistDraft(key, text));
      persistPinnedMentionsDraft();
    });
  }

  void persistDraftImmediately() {
    saveDraft(immediate: true);
  }

  void restoreDraftFromMemoryCache() {
    if (owner.sessionId.isEmpty) return;
    final cachedDraft = _draftMemoryCache[_draftKey()];
    if (cachedDraft != null && cachedDraft.isNotEmpty) {
      _restoreDraftText(cachedDraft);
    }
    // 附件与回复草稿独立于文字恢复：纯粘贴图片（无文字）也能还原。
    _restoreAttachmentDraftsFromMemoryCache();
    _restoreReplyDraftFromMemoryCache();
    _restorePinnedMentionsDraftFromMemoryCache();
  }

  void restoreInitialDraft(String draft) {
    if (draft.isEmpty) return;
    _restoreDraftText(draft);
  }

  void clearDraft() {
    if (owner.sessionId.isEmpty) return;
    final key = _draftKey();
    owner._lastDraftSnapshot = '';
    owner._draftPersistDebounce?.cancel();
    owner._draftPersistDebounce = null;
    _cacheDraft(key, '');
    ChatDraftIndex.update(sessionId: owner.sessionId, hasDraft: false);
    unawaited(_persistDraft(key, ''));
    unawaited(_clearAttachmentDrafts());
    _clearReplyDraft();
    // 固定艾特跨发送保留：clearDraft（发完清输入）不清 pinned。
  }

  Future<void> restoreDraft() async {
    if (owner.sessionId.isEmpty) return;
    final key = _draftKey();
    // 附件草稿：优先内存缓存（全平台跨页内导航生效），内存为空时回落到
    // 文件持久化（仅原生有效）。与文字草稿是否存在无关。
    final cachedAttachments = _attachmentDraftMemoryCache[_attachmentDraftKey()];
    if (cachedAttachments != null && cachedAttachments.isNotEmpty) {
      _applyAttachmentDrafts(cachedAttachments);
    } else {
      unawaited(_restoreAttachmentDraftsFromPrefs());
    }
    // 固定艾特草稿与文字无关，始终尝试恢复。
    _restorePinnedMentionsDraft();
    // 文字与回复草稿。
    final cachedDraft = _draftMemoryCache[key];
    if (cachedDraft != null && cachedDraft.isNotEmpty) {
      _restoreDraftText(cachedDraft);
      _restoreReplyDraft();
      return;
    }
    final prefs = await SharedPreferences.getInstance();
    final draft = prefs.getString(key);
    if (draft != null && draft.isNotEmpty) {
      _cacheDraft(key, draft);
      ChatDraftIndex.update(
        sessionId: owner.sessionId,
        hasDraft: draft.trim().isNotEmpty,
      );
      _restoreDraftText(draft);
    }
    unawaited(_restoreReplyDraftFromPrefs());
  }

  Future<void> _persistDraft(String key, String text) async {
    if (text != owner._lastDraftSnapshot) {
      return;
    }
    final prefs = await SharedPreferences.getInstance();
    if (text != owner._lastDraftSnapshot) {
      return;
    }
    if (text.isEmpty) {
      await prefs.remove(key);
      return;
    }
    await prefs.setString(key, text);
  }

  void _cacheDraft(String key, String text) {
    if (text.isEmpty) {
      _draftMemoryCache.remove(key);
      return;
    }
    _draftMemoryCache[key] = text;
  }

  void _restoreDraftText(String draft) {
    owner._lastDraftSnapshot = draft;
    updateInputValue((currentValue) {
      if (currentValue.text.isNotEmpty) {
        return currentValue;
      }
      return TextEditingValue(
        text: draft,
        selection: TextSelection.collapsed(offset: draft.length),
      );
    });
  }

  // ---- Attachment draft persistence ----

  String _attachmentDraftKey() => '${_draftKey()}_attach';

  Future<void> _persistAttachmentDrafts() async {
    // 内存缓存全平台生效，保证页内导航离开后再回来能恢复（含 Web 粘贴图片）。
    // 仅缓存带完整字节的附件：stream-only（如 Web 选取的视频）跨控制器实例后
    // 流已失效，且原生文件持久化本就只存有 bytes 的附件。
    // 文件持久化依赖临时目录，仅在原生平台执行。
    final key = _attachmentDraftKey();
    final cacheable = owner.stagedAttachments
        .where((u) => u.bytes != null && u.bytes!.isNotEmpty)
        .toList();
    if (cacheable.isNotEmpty) {
      _attachmentDraftMemoryCache[key] = cacheable;
      if (!kIsWeb) {
        await _writeAttachmentDraftFiles(key, cacheable);
      }
    } else {
      _attachmentDraftMemoryCache.remove(key);
      if (!kIsWeb) {
        await _deleteAttachmentDraftFiles(key);
      }
    }
  }

  Future<void> _writeAttachmentDraftFiles(
    String key,
    List<PendingAttachmentUpload> attachments,
  ) async {
    try {
      final tempDir = await getTemporaryDirectory();
      final dir = Directory('${tempDir.path}/$_draftAttachDirName');
      if (!dir.existsSync()) {
        await dir.create(recursive: true);
      }
      final metaList = <Map<String, dynamic>>[];
      for (var i = 0; i < attachments.length; i++) {
        final upload = attachments[i];
        if (upload.bytes == null || upload.bytes!.isEmpty) continue;
        final ext = _fileExt(upload.fileName);
        final fileName = '${key}_$i${ext.isNotEmpty ? '.$ext' : ''}';
        final file = File('${dir.path}/$fileName');
        await file.writeAsBytes(upload.bytes!);
        metaList.add({
          'type': upload.type.name,
          'fileName': upload.fileName,
          'contentType': upload.contentType,
          'tempPath': file.path,
          'contentLength': upload.contentLength,
        });
      }
      final prefs = await SharedPreferences.getInstance();
      if (metaList.isEmpty) {
        await prefs.remove(key);
      } else {
        await prefs.setString(key, jsonEncode(metaList));
      }
    } catch (e) {
      debugPrint('[DraftAttach] write error: $e');
    }
  }

  void _restoreAttachmentDraftsFromMemoryCache() {
    final key = _attachmentDraftKey();
    final cached = _attachmentDraftMemoryCache[key];
    if (cached == null || cached.isEmpty) return;
    _applyAttachmentDrafts(cached);
  }

  Future<void> _restoreAttachmentDraftsFromPrefs() async {
    if (kIsWeb) return;
    final key = _attachmentDraftKey();
    if (_attachmentDraftMemoryCache.containsKey(key)) return;
    try {
      final prefs = await SharedPreferences.getInstance();
      final metaJson = prefs.getString(key);
      if (metaJson == null || metaJson.isEmpty) return;
      final metaList = jsonDecode(metaJson) as List;
      final result = <PendingAttachmentUpload>[];
      for (final meta in metaList) {
        if (meta is! Map<String, dynamic>) continue;
        final tempPath = meta['tempPath'] as String? ?? '';
        if (tempPath.isEmpty) continue;
        final file = File(tempPath);
        if (!await file.exists()) continue;
        final bytes = await file.readAsBytes();
        if (bytes.isEmpty) continue;
        final typeName = meta['type'] as String? ?? 'file';
        final type = ChatAttachmentType.values.firstWhere(
          (t) => t.name == typeName,
          orElse: () => ChatAttachmentType.file,
        );
        result.add(PendingAttachmentUpload(
          type: type,
          fileName: meta['fileName'] as String? ?? '',
          contentType: meta['contentType'] as String? ?? '',
          bytes: bytes,
          contentLength: meta['contentLength'] as int?,
        ));
      }
      if (result.isNotEmpty) {
        _attachmentDraftMemoryCache[key] = result;
        _applyAttachmentDrafts(result);
      }
    } catch (e) {
      debugPrint('[DraftAttach] restore error: $e');
    }
  }

  void _applyAttachmentDrafts(List<PendingAttachmentUpload> attachments) {
    if (owner.stagedAttachments.isNotEmpty) return;
    owner.stagedAttachments.addAll(attachments);
  }

  Future<void> _clearAttachmentDrafts() async {
    final key = _attachmentDraftKey();
    _attachmentDraftMemoryCache.remove(key);
    if (!kIsWeb) {
      await _deleteAttachmentDraftFiles(key);
    }
  }

  Future<void> _deleteAttachmentDraftFiles(String key) async {
    try {
      final prefs = await SharedPreferences.getInstance();
      final metaJson = prefs.getString(key);
      await prefs.remove(key);
      if (metaJson == null || metaJson.isEmpty) return;
      final metaList = jsonDecode(metaJson) as List;
      for (final meta in metaList) {
        if (meta is! Map<String, dynamic>) continue;
        final tempPath = meta['tempPath'] as String? ?? '';
        if (tempPath.isEmpty) continue;
        final file = File(tempPath);
        if (await file.exists()) {
          await file.delete();
        }
      }
    } catch (_) {}
  }

  static String _fileExt(String fileName) {
    final dot = fileName.lastIndexOf('.');
    if (dot < 0 || dot == fileName.length - 1) return '';
    return fileName.substring(dot + 1).toLowerCase();
  }

  // ---- Reply draft persistence ----

  String _replyDraftKey() => '${_draftKey()}_reply';

  void _saveReplyDraft() {
    final replyMsgId = owner.replyingToMessage.value?.msgId ?? '';
    final key = _replyDraftKey();
    if (replyMsgId.isNotEmpty) {
      _replyDraftMemoryCache[key] = replyMsgId;
    } else {
      _replyDraftMemoryCache.remove(key);
    }
    unawaited(_persistReplyDraft(key, replyMsgId));
  }

  Future<void> _persistReplyDraft(String key, String msgId) async {
    try {
      final prefs = await SharedPreferences.getInstance();
      if (msgId.isEmpty) {
        await prefs.remove(key);
      } else {
        await prefs.setString(key, msgId);
      }
    } catch (_) {}
  }

  void _restoreReplyDraftFromMemoryCache() {
    final key = _replyDraftKey();
    final msgId = _replyDraftMemoryCache[key];
    if (msgId == null || msgId.isEmpty) return;
    _pendingReplyDraftMsgId = msgId;
    _tryApplyPendingReplyDraft();
  }

  void _restoreReplyDraft() {
    final key = _replyDraftKey();
    final msgId = _replyDraftMemoryCache[key];
    if (msgId != null && msgId.isNotEmpty) {
      _pendingReplyDraftMsgId = msgId;
      _tryApplyPendingReplyDraft();
      return;
    }
    unawaited(_restoreReplyDraftFromPrefs());
  }

  Future<void> _restoreReplyDraftFromPrefs() async {
    try {
      final key = _replyDraftKey();
      if (_replyDraftMemoryCache.containsKey(key)) return;
      final prefs = await SharedPreferences.getInstance();
      final msgId = prefs.getString(key);
      if (msgId == null || msgId.isEmpty) return;
      _replyDraftMemoryCache[key] = msgId;
      _pendingReplyDraftMsgId = msgId;
      _tryApplyPendingReplyDraft();
    } catch (_) {}
  }

  void _tryApplyPendingReplyDraft() {
    final msgId = _pendingReplyDraftMsgId;
    if (msgId == null || msgId.isEmpty) return;
    final messages = owner.imService.currentMessages;
    final msg = messages.where((m) => m.msgId == msgId).firstOrNull;
    if (msg != null) {
      owner.replyingToMessage.value = msg;
      _pendingReplyDraftMsgId = null;
      return;
    }
    // Messages not loaded yet — retry after first frame.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _tryApplyPendingReplyDraft();
    });
  }

  void _clearReplyDraft() {
    _pendingReplyDraftMsgId = null;
    final key = _replyDraftKey();
    _replyDraftMemoryCache.remove(key);
    unawaited(_persistReplyDraft(key, ''));
  }

  // ---- Pinned mention draft persistence ----

  String _pinnedMentionDraftKey() => '${_draftKey()}_pinned';

  void persistPinnedMentionsDraft() {
    if (owner.sessionId.isEmpty) return;
    final key = _pinnedMentionDraftKey();
    // 空串也写入内存，避免 prefs 异步 restore 把已取消的固定「复活」。
    final payload = owner._pinnedMentions.isEmpty
        ? ''
        : jsonEncode(
            owner._pinnedMentions
                .map(
                  (m) => <String, String>{
                    'memberId': m.memberId,
                    'displayName': m.displayName,
                  },
                )
                .toList(growable: false),
          );
    _pinnedMentionDraftMemoryCache[key] = payload;
    unawaited(_persistPinnedMentionsDraft(key, payload));
  }

  Future<void> _persistPinnedMentionsDraft(String key, String payload) async {
    try {
      final prefs = await SharedPreferences.getInstance();
      if (payload.isEmpty) {
        await prefs.remove(key);
      } else {
        await prefs.setString(key, payload);
      }
    } catch (_) {}
  }

  void _restorePinnedMentionsDraftFromMemoryCache() {
    final key = _pinnedMentionDraftKey();
    if (!_pinnedMentionDraftMemoryCache.containsKey(key)) return;
    _applyPinnedMentionsDraft(_pinnedMentionDraftMemoryCache[key] ?? '');
  }

  void _restorePinnedMentionsDraft() {
    final key = _pinnedMentionDraftKey();
    if (_pinnedMentionDraftMemoryCache.containsKey(key)) {
      _applyPinnedMentionsDraft(_pinnedMentionDraftMemoryCache[key] ?? '');
      return;
    }
    unawaited(_restorePinnedMentionsDraftFromPrefs());
  }

  Future<void> _restorePinnedMentionsDraftFromPrefs() async {
    try {
      final key = _pinnedMentionDraftKey();
      if (_pinnedMentionDraftMemoryCache.containsKey(key)) return;
      final prefs = await SharedPreferences.getInstance();
      final payload = prefs.getString(key);
      // await 后再看一眼：期间若用户 unpin，内存已有权威空串。
      if (_pinnedMentionDraftMemoryCache.containsKey(key)) return;
      if (payload == null || payload.isEmpty) return;
      _pinnedMentionDraftMemoryCache[key] = payload;
      _applyPinnedMentionsDraft(payload);
    } catch (_) {}
  }

  void _applyPinnedMentionsDraft(String payload) {
    if (payload.isEmpty) {
      owner._pinnedMentions.clear();
      owner._syncGroupToolbarTargetAgent();
      return;
    }
    try {
      final decoded = jsonDecode(payload);
      if (decoded is! List) return;
      final restored = <PinnedMention>[];
      for (final item in decoded) {
        if (item is! Map) continue;
        final memberId = (item['memberId'] ?? '').toString().trim();
        final displayName = (item['displayName'] ?? '').toString().trim();
        if (memberId.isEmpty || displayName.isEmpty) continue;
        restored.add(
          PinnedMention(memberId: memberId, displayName: displayName),
        );
      }
      owner._pinnedMentions
        ..clear()
        ..addAll(restored);
      owner._syncGroupToolbarTargetAgent();
    } catch (_) {}
  }
}
