part of 'chat_controller.dart';

class _ChatPageStateController {
  _ChatPageStateController(this.owner);

  final ChatController owner;
  int _wheelPointerSignalGeneration = 0;

  bool get shouldAutoFollowBottomUpdates =>
      owner._initialBottomAnchoring || owner._autoFollowBottom;

  bool get _isOwnerClosed => owner.isClosed;
  bool get _hasAnyUserScrollInteractionActive =>
      owner._userScrollInteractionActive ||
      owner._pointerSignalScrollInteractionActive;

  void bindFlutterView(FlutterView view) {
    if (identical(owner._boundFlutterView, view)) {
      return;
    }
    owner._boundFlutterView = view;
  }

  void onInit() {
    WidgetsBinding.instance.addObserver(owner);
    final initialKeyboardInsetBottom = _readKeyboardInsetBottom();
    owner._lastKeyboardInsetBottom = initialKeyboardInsetBottom;
    owner._messageListKeyboardInsetBottom.value = initialKeyboardInsetBottom;
    if (initialKeyboardInsetBottom > 0) {
      owner._lastVisibleKeyboardInsetBottom = initialKeyboardInsetBottom;
    }
    owner._chatInputController.syncInputLayoutKeyboardInset(
      rawKeyboardInsetBottom: initialKeyboardInsetBottom,
    );
    final explicitArgs = owner.routeArguments;
    final rawArgs = explicitArgs ?? Get.arguments;
    final args = rawArgs is Map<String, dynamic>
        ? rawArgs
        : const <String, dynamic>{};
    final params = explicitArgs != null
        ? const <String, String>{}
        : Get.parameters;

    owner.sessionId = owner
        ._readRoutingValue(args: args, params: params, key: 'session_id')
        .trim();
    owner.chatTitle = owner._readRoutingValue(
      args: args,
      params: params,
      key: 'title',
    );
    owner.chatType = owner._readRoutingValue(
      args: args,
      params: params,
      key: 'type',
      fallback: 'private',
    );
    owner._initialGroupAvatarMembers = owner._readInitialGroupAvatarMembers(
      args,
    );
    final openPerfArg = args[PrivateChatOpenPerfLogger.argumentKey];
    owner._privateChatOpenPerfTrace = openPerfArg is Map
        ? Map<String, dynamic>.from(openPerfArg)
        : null;
    PrivateChatOpenPerfLogger.mark(
      owner._privateChatOpenPerfTrace,
      'chat_controller_init',
      data: {'session_id': owner.sessionId, 'route_type': owner.chatType},
    );
    owner._chatInputController.restoreDraftFromMemoryCache();
    owner._chatInputController.restoreInitialDraft(
      args['initial_draft']?.toString() ?? '',
    );
  }

  void onReady() {
    if (_isOwnerClosed) {
      return;
    }
    final initData = {
      'session_id': owner.sessionId,
      'title': owner.chatTitle,
      'type': owner.chatType,
      'route': Get.currentRoute,
    };
    Sentry.addBreadcrumb(
      Breadcrumb(
        category: 'chat_ctrl',
        message: 'onReady',
        data: initData,
        level: SentryLevel.info,
      ),
    );
    debugPrint('[ChatCtrl] onReady: $initData');

    if (!owner.authService.isLoggedIn) {
      RootRouteNavigator.toLogin();
      return;
    }
    if (owner.sessionId.isEmpty) {
      Sentry.captureMessage(
        'ChatCtrl onReady: sessionId is EMPTY, '
        'route=${Get.currentRoute} args=${Get.arguments} params=${Get.parameters}',
        level: SentryLevel.error,
      );
      if (Get.key.currentState != null && !AppRoutes.isCurrentHomePath) {
        RootRouteNavigator.toHome();
      }
      return;
    }
    PrivateChatOpenPerfLogger.mark(
      owner._privateChatOpenPerfTrace,
      'chat_controller_ready',
      data: {'session_id': owner.sessionId, 'route': Get.currentRoute},
    );

    if (Get.isRegistered<ChatBackgroundService>()) {
      Get.find<ChatBackgroundService>().ensureSyncedWithCurrentUser();
    }

    Future.microtask(() {
      if (_isOwnerClosed) {
        return;
      }
      owner._autoFollowBottom = true;
      owner._userScrollInteractionActive = false;
      owner._pointerSignalScrollInteractionActive = false;
      owner._lastUserScrollEndTime = null;
      owner._initialBottomAnchoring = true;
      owner._hasObservedScrollMetrics = false;
      owner._lastObservedMaxScrollExtent = 0;
      owner._isLoadingOlderHistory.value = false;
      syncHistoryFlagsFromService();
      owner.chatType = owner.imService.resolveSessionTypeById(
        owner.sessionId,
        fallback: owner.chatType,
      );
      final hasTypeHint = owner.imService.hasSessionTypeHint(owner.sessionId);
      final routeTitle = owner.chatTitle.trim();
      if (routeTitle.isNotEmpty &&
          !owner.imService.hasSessionDisplayTitleById(owner.sessionId)) {
        owner.imService.bindSessionDisplayTitle(
          owner.sessionId,
          routeTitle,
          type: owner.chatType,
        );
      }
      owner.imService.enterSession(
        owner.sessionId,
        initialLoadDelay: ChatController._initialMessageLoadDelay,
      );
      ChatMessageWindowOwners.enter(
        owner.sessionId,
        userId: owner.authService.userId ?? '',
      );
      PrivateChatOpenPerfLogger.mark(
        owner._privateChatOpenPerfTrace,
        'enter_session_called',
        data: {
          'initial_load_delay_ms':
              ChatController._initialMessageLoadDelay.inMilliseconds,
        },
      );
      owner._privateChatOpenPerfEnterSessionLogged = true;
      _logFirstMessageWindowIfNeeded();
      if (!owner.imService.isConnected) {
        owner.imService.ensureConnected();
      } else {
        owner.imService.refreshDelegateStates();
      }
      final currentSession = owner.imService.findSessionById(owner.sessionId);
      final isVisitorSession = currentSession?.isVisitor == true;
      if (owner.isGroupChat || !hasTypeHint || isVisitorSession) {
        unawaited(
          owner.refreshSessionDetail(forceTypeProbe: !owner.isGroupChat),
        );
      } else {
        owner._applyVisitorSessionDetail(null);
        owner._resetGroupSessionState();
      }
      if (!owner.isGroupChat && hasTypeHint) {
        owner._refreshPrivatePeerNickname();
        owner._refreshPrivatePeerAvatar();
      }
      owner.loadAgents();
      owner._prefetchCurrentMessageProfiles();
      scrollToBottom();
    });

    owner._messageSnapshotWorker = ever(owner.imService.currentMessages, (_) {
      _logFirstMessageWindowIfNeeded();
      owner.onMessageListWindowChanged();
    });
    _logFirstMessageWindowIfNeeded();
    owner.onMessageListWindowChanged();
    owner._messageWorker = debounce(owner.imService.currentMessages, (
      messages,
    ) {
      syncHistoryFlagsFromService();
      owner.syncExecApprovalActionLocks();
      owner._prefetchCurrentMessageProfiles();
      _clearOrphanedAgentOutputCapsule(messages);
      if (owner._initialBottomAnchoring) {
        scrollToBottom(force: true);
        return;
      }
      if (shouldAutoFollowBottomUpdates) {
        scrollToBottom();
      }
    }, time: const Duration(milliseconds: 80));
    owner._delegateStateWorker = ever(owner.imService.delegateStates, (_) {
      owner._syncDelegateRoundsDraftFromState();
    });
    // 节流：AI 流式输出/工具卡片状态会高频变动，使用 debounce 合并到一帧节奏内的
    // 最后一次调用，避免每次 chunk 都触发 scrollToBottom 引发 UI 线程持续唤醒。
    owner._agentOutputStateWorker = debounce(
      owner.imService.agentOutputStates,
      (_) {
        if (shouldAutoFollowBottomUpdates) {
          scrollToBottom();
        }
      },
      time: const Duration(milliseconds: 80),
    );
    owner._lastSessionMemberEventVersion = owner.imService
        .getSessionMemberEventVersion(owner.sessionId);
    owner._sessionMemberEventWorker = ever(
      owner.imService.sessionMemberEventVersions,
      (_) {
        final currentVersion = owner.imService.getSessionMemberEventVersion(
          owner.sessionId,
        );
        if (currentVersion <= owner._lastSessionMemberEventVersion) {
          return;
        }
        owner._lastSessionMemberEventVersion = currentVersion;
        unawaited(owner.refreshSessionDetail(forceTypeProbe: true));
      },
    );
    owner._lastSessionAccessRevokedVersion = owner.imService
        .getSessionAccessRevokedVersion(owner.sessionId);
    owner._sessionAccessRevokedWorker = ever(
      owner.imService.sessionAccessRevokedVersions,
      (_) {
        final currentVersion = owner.imService.getSessionAccessRevokedVersion(
          owner.sessionId,
        );
        if (currentVersion <= owner._lastSessionAccessRevokedVersion) {
          return;
        }
        owner._lastSessionAccessRevokedVersion = currentVersion;
        unawaited(owner._handleGroupAccessLost());
      },
    );
    owner._sessionsWorker = ever(owner.imService.sessions, (_) {
      owner._refreshPrivatePeerNickname(fetchIfMissing: false);
      owner._refreshPrivatePeerAvatar(fetchIfMissing: false);
    });
    // 节流：对方"正在输入"等活动状态会随服务端续期频繁刷新，
    // 用 debounce 合并避免高频滚动调用。
    owner._sessionActivityWorker = debounce(owner.imService.sessionActivities, (
      _,
    ) {
      if (shouldAutoFollowBottomUpdates) {
        scrollToBottom();
      }
    }, time: const Duration(milliseconds: 80));
    final fs = owner._friendService;
    if (fs != null) {
      owner._consumeFriendListChangedUserIds();
      owner._consumeActiveHumanSenderProfileChangedUserIds();
      owner._friendListWorker = debounce(fs.friendList, (_) {
        final changedUserIds = owner._consumeFriendListChangedUserIds();
        owner._refreshGroupMemberDisplayState(
          changedHumanSenderIds: changedUserIds,
        );
        owner._consumeActiveHumanSenderProfileChangedUserIds();
        owner._refreshPrivatePeerNickname(fetchIfMissing: false);
        owner._refreshPrivatePeerAvatar(fetchIfMissing: false);
      }, time: const Duration(milliseconds: 16));
      owner._profileCacheWorker = ever(fs.profileCacheVersion, (_) {
        final changedUserIds = owner
            ._consumeActiveHumanSenderProfileChangedUserIds();
        owner._refreshGroupMemberDisplayState(
          changedHumanSenderIds: changedUserIds,
        );
        owner._refreshPrivatePeerNickname(fetchIfMissing: false);
        owner._refreshPrivatePeerAvatar(fetchIfMissing: false);
      });
    }
    owner._agentsWorker = ever(owner.agentService.agents, (_) {
      owner._refreshPrivatePeerAvatar(fetchIfMissing: false);
      owner._refreshGroupMemberDisplayState();
    });
    // sharedAgents 常在 agents 之后异步就绪；冷启动草稿里已有 @共享 agent 时
    // 需重算群工具栏目标，否则会一直空直到用户再改输入。
    owner._sharedAgentsWorker = ever(owner.agentService.sharedAgents, (_) {
      owner._refreshPrivatePeerAvatar(fetchIfMissing: false);
      owner._refreshGroupMemberDisplayState();
    });
    owner._syncDelegateRoundsDraftFromState();

    owner._chatInputController.bind();
    unawaited(owner._chatInputController.restoreDraft());
    owner.scrollController.addListener(owner._onScroll);
    owner._platformViewportObstructionBottom.value =
        owner._bottomObstructionObserver.currentBottomObstruction;
    owner._bottomObstructionSubscription = owner
        ._bottomObstructionObserver
        .onChanged
        .listen(_handleBottomObstructionChanged);
  }

  void onClose() {
    _wheelPointerSignalGeneration++;
    WidgetsBinding.instance.removeObserver(owner);
    owner._messageSnapshotWorker?.dispose();
    owner._messageWorker?.dispose();
    owner.imService.leaveSession(owner.sessionId);
    ChatMessageWindowOwners.leave(owner.sessionId);
    // Deferred: this delete may run from a widget dispose, and re-entering a
    // session mutates the observed message list.
    scheduleMicrotask(ChatController.restoreSharedMessageWindow);
    owner._delegateStateWorker?.dispose();
    owner._agentOutputStateWorker?.dispose();
    owner._sessionMemberEventWorker?.dispose();
    owner._sessionAccessRevokedWorker?.dispose();
    owner._sessionsWorker?.dispose();
    owner._friendListWorker?.dispose();
    owner._profileCacheWorker?.dispose();
    owner._agentsWorker?.dispose();
    owner._sharedAgentsWorker?.dispose();
    owner._sessionActivityWorker?.dispose();
    owner._bottomObstructionSubscription?.cancel();
    owner._bottomObstructionObserver.dispose();
    owner.scrollController.removeListener(owner._onScroll);
    owner._chatInputController.onClose();
    owner._messageViewportItemKeys.clear();
    owner.scrollController.dispose();
  }

  void _logFirstMessageWindowIfNeeded() {
    if (owner._privateChatOpenPerfFirstMessagesLogged) {
      return;
    }
    if (!owner._privateChatOpenPerfEnterSessionLogged) {
      return;
    }
    if (owner._privateChatOpenPerfTrace == null ||
        owner._privateChatOpenPerfTrace!.isEmpty) {
      return;
    }
    final currentSid = owner.imService.currentSessionId?.trim() ?? '';
    if (currentSid.isNotEmpty && currentSid != owner.sessionId) {
      return;
    }
    owner._privateChatOpenPerfFirstMessagesLogged = true;
    PrivateChatOpenPerfLogger.mark(
      owner._privateChatOpenPerfTrace,
      'message_list_first_change_after_enter',
      data: {
        'session_id': owner.sessionId,
        'message_count': owner.imService.currentMessages.length,
      },
    );
  }

  void onPointerSignalScroll() {
    if (!owner.scrollController.hasClients) {
      return;
    }
    onWheelScrollActive(owner.scrollController.position);
    final signalGeneration = ++_wheelPointerSignalGeneration;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_isOwnerClosed || signalGeneration != _wheelPointerSignalGeneration) {
        return;
      }
      if (!owner.scrollController.hasClients) {
        owner._pointerSignalScrollInteractionActive = false;
        return;
      }
      onWheelScrollEnd(owner.scrollController.position);
    });
    WidgetsBinding.instance.scheduleFrame();
  }

  void closeChatRoute() {
    owner.persistDraftImmediately();
    if (ChatPaneHost.closeIfActive(owner.sessionId)) return;
    final navigatorState = Get.key.currentState;
    if (navigatorState != null && navigatorState.canPop()) {
      Get.back<void>();
      return;
    }
    if (!AppRoutes.isCurrentHomePath) {
      RootRouteNavigator.toHome();
    }
  }

  void scrollToBottom({
    bool animated = false,
    bool force = false,
    bool resumeAutoFollow = false,
  }) {
    if (resumeAutoFollow) {
      owner._autoFollowBottom = true;
    }
    // Never steal scroll while the user is actively dragging, even on force.
    if (_hasAnyUserScrollInteractionActive) {
      return;
    }
    if (!force && !canExecuteBottomFollow()) {
      return;
    }
    if (owner._scrollTaskScheduled) {
      return;
    }
    owner._scrollTaskScheduled = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      owner._scrollTaskScheduled = false;
      _executeBottomFollowOnCurrentFrame(animated: animated, force: force);
    });
  }

  void scrollToLoadedTop({bool animated = true}) {
    if (!owner.scrollController.hasClients) {
      return;
    }
    owner._initialBottomAnchoring = false;
    owner._autoFollowBottom = false;

    final position = owner.scrollController.position;
    final target = position.minScrollExtent;
    final distance = (position.pixels - target).abs();
    if (distance < ChatController._topPinnedHistoryLoadThreshold) {
      return;
    }

    final requestGeneration = ++owner._scrollToLoadedTopGeneration;
    owner._scrollToLoadedTopInProgress = true;

    try {
      if (animated) {
        unawaited(
          owner.scrollController
              .animateTo(
                target,
                duration: const Duration(milliseconds: 180),
                curve: Curves.easeOut,
              )
              .whenComplete(() {
                if (owner._scrollToLoadedTopGeneration == requestGeneration) {
                  owner._scrollToLoadedTopInProgress = false;
                }
              })
              .catchError((Object e) {
                debugPrint(
                  '⚠️ scrollToLoadedTop animation ignored: ScrollPosition is not ready. $e',
                );
              }),
        );
      } else {
        owner.scrollController.jumpTo(target);
        owner._scrollToLoadedTopInProgress = false;
      }
    } catch (e) {
      owner._scrollToLoadedTopInProgress = false;
      debugPrint(
        '⚠️ scrollToLoadedTop ignored: ScrollPosition is not ready. $e',
      );
    }
  }

  void onScrollMetricsChanged(ScrollMetrics metrics) {
    if (owner._suppressMetricsAnchorWhileKeyboardAnimating) {
      _scheduleSettledViewportIntentExecution();
      return;
    }
    // 回前台恢复窗口内的度量变化来自布局抖动而不是用户意图：跳过自动贴底
    // 与差值补偿，统一交给逐帧恢复循环按锚点校正。
    if (owner._resumeViewportRestorePending &&
        !_hasAnyUserScrollInteractionActive) {
      owner._hasObservedScrollMetrics = true;
      owner._lastObservedMaxScrollExtent = metrics.maxScrollExtent;
      _driveResumeViewportRestore();
      return;
    }
    final nextMaxExtent = metrics.maxScrollExtent;
    if (!owner._hasObservedScrollMetrics) {
      owner._hasObservedScrollMetrics = true;
      owner._lastObservedMaxScrollExtent = nextMaxExtent;
      return;
    }

    final previousMaxExtent = owner._lastObservedMaxScrollExtent;
    owner._lastObservedMaxScrollExtent = nextMaxExtent;

    // maxScrollExtent grew — may need to auto-anchor to bottom.
    if (nextMaxExtent > previousMaxExtent + 1) {
      final previousDistanceToBottom = (previousMaxExtent - metrics.pixels)
          .abs();
      final shouldAnchorToBottom =
          owner._initialBottomAnchoring ||
          (owner._autoFollowBottom &&
              previousDistanceToBottom <= ChatController._bottomSnapThreshold);
      if (!shouldAnchorToBottom) {
        _scheduleRecentUserViewportAnchorRestore();
        return;
      }
      if (_hasAnyUserScrollInteractionActive) {
        _scheduleRecentUserViewportAnchorRestore();
        return;
      }
      scrollToBottom();
      return;
    }

    // maxScrollExtent shrank — preserve viewport position so the user
    // doesn't see content jump downward (e.g. when card projection hides
    // adjacent tool-execution items above the visible area).
    if (nextMaxExtent < previousMaxExtent - 1) {
      if (owner._autoFollowBottom || owner._initialBottomAnchoring) {
        return;
      }
      if (_hasAnyUserScrollInteractionActive) {
        _scheduleRecentUserViewportAnchorRestore();
        return;
      }
      if (_scheduleRecentUserViewportAnchorRestore()) {
        return;
      }
      final delta = previousMaxExtent - nextMaxExtent;
      final currentPixels = metrics.pixels;
      if (currentPixels > 0) {
        final adjustedPixels = (currentPixels - delta).clamp(
          0.0,
          metrics.maxScrollExtent,
        );
        if ((adjustedPixels - currentPixels).abs() > 0.5) {
          _addScrollBreadcrumb('shrink_compensate', {
            'from': currentPixels,
            'to': adjustedPixels,
            'prevMax': previousMaxExtent,
            'nextMax': nextMaxExtent,
          });
          owner.scrollController.jumpTo(adjustedPixels);
        }
      }
    }
  }

  void onBottomDockLayoutChanged() {
    _scheduleSettledViewportIntentExecution();
  }

  void onMessageViewportLayoutChanged() {
    _scheduleRecentUserViewportAnchorRestore();
  }

  void didChangeMetrics() {
    final hadVisibleInputInset =
        owner._currentInputViewportInsetBottom > 0 ||
        owner.messageListViewportObstructionBottom > 0;
    final nextKeyboardInsetBottom = _readKeyboardInsetBottom();
    if ((nextKeyboardInsetBottom - owner._lastKeyboardInsetBottom).abs() <
        0.5) {
      return;
    }
    owner._lastKeyboardInsetBottom = nextKeyboardInsetBottom;
    owner._messageListKeyboardInsetBottom.value = nextKeyboardInsetBottom;
    if (nextKeyboardInsetBottom > 0) {
      owner._lastVisibleKeyboardInsetBottom = nextKeyboardInsetBottom;
    }
    owner._chatInputController.syncInputLayoutKeyboardInset();
    final hasVisibleInputInset =
        owner._currentInputViewportInsetBottom > 0 ||
        owner.messageListViewportObstructionBottom > 0;
    _scheduleBottomAnchorAfterInputViewportChange(
      hadVisibleInputInset: hadVisibleInputInset,
      hasVisibleInputInset: hasVisibleInputInset,
    );
  }

  void _handleBottomObstructionChanged(double nextBottomObstruction) {
    final hadVisibleInputInset =
        owner._currentInputViewportInsetBottom > 0 ||
        owner.messageListViewportObstructionBottom > 0;
    final previousBottomObstruction =
        owner._platformViewportObstructionBottom.value;
    if ((nextBottomObstruction - previousBottomObstruction).abs() < 0.5) {
      return;
    }
    owner._platformViewportObstructionBottom.value = nextBottomObstruction;
    owner._chatInputController.syncInputLayoutKeyboardInset();
    final hasVisibleInputInset =
        owner._currentInputViewportInsetBottom > 0 ||
        owner.messageListViewportObstructionBottom > 0;
    _scheduleBottomAnchorAfterInputViewportChange(
      hadVisibleInputInset: hadVisibleInputInset,
      hasVisibleInputInset: hasVisibleInputInset,
    );
  }

  void _scheduleBottomAnchorAfterInputViewportChange({
    required bool hadVisibleInputInset,
    required bool hasVisibleInputInset,
  }) {
    if (!owner.focusNode.hasFocus &&
        !hadVisibleInputInset &&
        !hasVisibleInputInset) {
      return;
    }
    owner._suppressMetricsAnchorWhileKeyboardAnimating = true;
    owner._keyboardMetricsSettledTimer?.cancel();
    owner._keyboardMetricsSettledTimer = Timer(
      const Duration(milliseconds: 90),
      () {
        owner._suppressMetricsAnchorWhileKeyboardAnimating = false;
        owner._keyboardViewportChangeEpoch++;
        _scheduleSettledViewportIntentExecution(
          sourceKeyboardEpoch: owner._keyboardViewportChangeEpoch,
        );
      },
    );
  }

  void onScroll() {
    if (!owner.scrollController.hasClients) {
      return;
    }
    final position = owner.scrollController.position;
    if (owner._initialBottomAnchoring) {
      return;
    }
    // 回前台后的短暂窗口内，列表高度抖动产生的滚动不是用户意图：既不改
    // 跟随底部状态，也不触发顶部历史加载（否则会被钉在最上面）。
    if (owner._resumeViewportRestorePending &&
        !owner._userScrollInteractionActive) {
      return;
    }

    final now = DateTime.now();
    if (owner._lastScrollSyncTime == null ||
        now.difference(owner._lastScrollSyncTime!) >
            const Duration(milliseconds: 33)) {
      owner._lastScrollSyncTime = now;
      syncBottomFollowState(
        distanceToBottom: distanceToBottom(position),
        fromUserInteraction: owner._userScrollInteractionActive,
      );
    }

    if (owner._isLoadingHistory) {
      return;
    }
    if (position.pixels <= ChatController._historyLoadTriggerThreshold &&
        owner._hasOlderHistory.value) {
      owner._isLoadingHistory = true;
      owner._isLoadingOlderHistory.value = true;
      loadOlderHistoryPreservingOffset().whenComplete(() {
        owner._isLoadingOlderHistory.value = false;
        syncHistoryFlagsFromService();
        owner._isLoadingHistory = false;
      });
      return;
    }

    if (owner.imService.hasNewerMessages &&
        position.maxScrollExtent - position.pixels <=
            ChatController._historyLoadTriggerThreshold) {
      owner._isLoadingHistory = true;
      loadNewerHistoryPreservingOffset().whenComplete(() {
        syncHistoryFlagsFromService();
        owner._isLoadingHistory = false;
      });
    }
  }

  void syncHistoryFlagsFromService() {
    final nextHasOlder = owner.imService.hasOlderMessages;
    if (owner._hasOlderHistory.value == nextHasOlder) {
      return;
    }
    owner._hasOlderHistory.value = nextHasOlder;
  }

  Future<void> loadOlderHistoryPreservingOffset() async {
    final hadClients = owner.scrollController.hasClients;
    final beforePixels = hadClients
        ? owner.scrollController.position.pixels
        : 0.0;
    final beforeMinExtent = hadClients
        ? owner.scrollController.position.minScrollExtent
        : 0.0;
    final beforeMaxExtent = hadClients
        ? owner.scrollController.position.maxScrollExtent
        : 0.0;
    final scrollToLoadedTopGeneration = owner._scrollToLoadedTopGeneration;
    final startedDuringLoadedTopScroll = owner._scrollToLoadedTopInProgress;
    final previousFirstItemKey = owner.imService.currentMessages.isEmpty
        ? ''
        : ChatMessageIdentity.selectionKey(
            owner.imService.currentMessages.first,
          );
    final shouldPinToLoadedTop =
        hadClients &&
        beforePixels <=
            beforeMinExtent + ChatController._topPinnedHistoryLoadThreshold;
    final anchor = shouldPinToLoadedTop
        ? null
        : _captureLeadingVisibleMessageAnchor();

    await owner.imService.loadOlderForCurrentSession();

    final completer = Completer<void>();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      completer.complete();
    });
    await completer.future;

    if (!hadClients || !owner.scrollController.hasClients) {
      return;
    }
    final position = owner.scrollController.position;
    final shouldForceLoadedTop =
        startedDuringLoadedTopScroll ||
        owner._scrollToLoadedTopGeneration != scrollToLoadedTopGeneration;
    if (shouldPinToLoadedTop || shouldForceLoadedTop) {
      final top = position.minScrollExtent;
      final distanceToTop = (position.pixels - top).abs();
      if (distanceToTop > ChatController._topPinnedHistoryLoadThreshold) {
        _addScrollBreadcrumb('history_top_pin', {
          'from': position.pixels,
          'to': top,
          'forced': shouldForceLoadedTop,
        });
        owner.scrollController.jumpTo(top);
      }
      return;
    }

    // Fall back to anchoring the first visible message. This helps when the
    // inserted block contains complex widgets whose final height is not fully
    // measurable in the first post-load frame.
    if (anchor != null && _restoreLeadingVisibleMessageAnchor(anchor)) {
      return;
    }

    final insertedTopExtent = previousFirstItemKey.isEmpty
        ? null
        : _measureInsertedTopExtent(beforeFirstItemKey: previousFirstItemKey);
    if (insertedTopExtent != null && insertedTopExtent > 0) {
      final target = (beforePixels + insertedTopExtent)
          .clamp(position.minScrollExtent, position.maxScrollExtent)
          .toDouble();
      if ((target - position.pixels).abs() >= 0.5) {
        owner.scrollController.jumpTo(target);
      }
      return;
    }

    final delta = position.maxScrollExtent - beforeMaxExtent;
    if (delta <= 0) {
      return;
    }

    final target = (beforePixels + delta)
        .clamp(position.minScrollExtent, position.maxScrollExtent)
        .toDouble();
    owner.scrollController.jumpTo(target);
  }

  Future<void> loadNewerHistoryPreservingOffset() async {
    final hadClients = owner.scrollController.hasClients;
    final beforeDistanceToBottom = hadClients
        ? distanceToBottom(owner.scrollController.position)
        : 0.0;

    await owner.imService.loadNewerForCurrentSession();

    final completer = Completer<void>();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      completer.complete();
    });
    await completer.future;

    if (!hadClients || !owner.scrollController.hasClients) {
      return;
    }
    final position = owner.scrollController.position;

    final keepWindowBottom =
        beforeDistanceToBottom <= ChatController._historyLoadTriggerThreshold;
    final target = keepWindowBottom
        ? position.maxScrollExtent.toDouble()
        : (position.maxScrollExtent - beforeDistanceToBottom)
              .clamp(position.minScrollExtent, position.maxScrollExtent)
              .toDouble();
    if ((target - position.pixels).abs() < 0.5) {
      return;
    }
    owner.scrollController.jumpTo(target);
  }

  void onStreamingMessageUpdated(String msgId) {
    owner.imService.onStreamUiUpdated(msgId);
    if (shouldAutoFollowBottomUpdates) {
      scrollToBottom();
    }
  }

  /// App 切后台前记住首个可见消息的位置。回前台时消息卡片重建、键盘 inset
  /// 变化会让 maxScrollExtent 抖动；若没有新鲜锚点，收缩补偿会按差值把位置
  /// 往上推（甚至钳到 0），随后顶部历史加载再把它钉死在最上面。
  void onAppEnteredBackground() {
    if (owner._backgroundViewportAnchor != null) {
      return;
    }
    if (!owner.scrollController.hasClients) {
      return;
    }
    if (shouldAutoFollowBottomUpdates) {
      return;
    }
    final anchor = _captureLeadingVisibleMessageAnchor();
    owner._backgroundViewportAnchor = anchor;
    if (anchor != null) {
      _addScrollBreadcrumb('bg_capture', {
        'anchor': anchor.itemKey,
        'leading': anchor.leadingOffset,
        'pixels': owner.scrollController.position.pixels,
        'max': owner.scrollController.position.maxScrollExtent,
      });
    }
  }

  /// 回前台：用切走前的锚点刷新"最近用户锚点"，并在窗口内冻结度量抖动的
  /// 自动补偿，由逐帧恢复循环把首个可见消息校正回原位；锚点卡片未构建时
  /// 先按方向分步跳近再精确恢复。用户真实滚动会立即结束窗口。
  void onAppResumed() {
    final anchor = owner._backgroundViewportAnchor;
    owner._backgroundViewportAnchor = null;
    if (anchor == null || _isOwnerClosed) {
      return;
    }
    _addScrollBreadcrumb('resume_restore_start', {
      'anchor': anchor.itemKey,
      'leading': anchor.leadingOffset,
      'pixels': owner.scrollController.hasClients
          ? owner.scrollController.position.pixels
          : null,
    });
    owner._lastUserViewportAnchor = anchor;
    owner._lastUserViewportAnchorCapturedAt = DateTime.now();
    owner._userViewportAnchorGeneration++;
    owner._resumeViewportRestoreAnchor = anchor;
    owner._resumeViewportRestorePending = true;
    owner._resumeViewportRestoreTimer?.cancel();
    owner._resumeViewportRestoreTimer = Timer(
      ChatController._resumeViewportRestoreWindow,
      () {
        owner._resumeViewportRestoreTimer = null;
        _endResumeViewportRestore('window_elapsed');
      },
    );
    _driveResumeViewportRestore();
  }

  void _endResumeViewportRestore(String reason) {
    if (!owner._resumeViewportRestorePending) {
      return;
    }
    owner._resumeViewportRestorePending = false;
    owner._resumeViewportRestoreAnchor = null;
    owner._resumeViewportRestoreGeneration++;
    owner._resumeViewportRestoreTimer?.cancel();
    owner._resumeViewportRestoreTimer = null;
    _addScrollBreadcrumb('resume_restore_end', {
      'reason': reason,
      'pixels': owner.scrollController.hasClients
          ? owner.scrollController.position.pixels
          : null,
    });
  }

  /// 逐帧尝试把回前台锚点对应的消息恢复到原可见位置。锚点卡片当前未构建
  /// （被 Flutter 的 extent 钳制或补偿甩出构建范围）时，恢复会静默失败，
  /// 因此每帧先向锚点方向跳近一个视口高度，直到卡片构建出来再精确校正。
  void _driveResumeViewportRestore() {
    if (!owner._resumeViewportRestorePending) {
      return;
    }
    final generation = ++owner._resumeViewportRestoreGeneration;
    void step(Duration _) {
      if (_isOwnerClosed ||
          generation != owner._resumeViewportRestoreGeneration ||
          !owner._resumeViewportRestorePending) {
        return;
      }
      final anchor = owner._resumeViewportRestoreAnchor;
      if (anchor == null) {
        _endResumeViewportRestore('anchor_missing');
        return;
      }
      if (_hasAnyUserScrollInteractionActive) {
        _endResumeViewportRestore('user_scroll');
        return;
      }
      if (shouldAutoFollowBottomUpdates) {
        _endResumeViewportRestore('auto_follow');
        return;
      }
      if (!owner.scrollController.hasClients) {
        WidgetsBinding.instance.addPostFrameCallback(step);
        WidgetsBinding.instance.scheduleFrame();
        return;
      }
      if (_restoreLeadingVisibleMessageAnchor(anchor)) {
        // 恢复成功：刷新最近用户锚点，窗口保持开启，后续抖动再触发。
        _rememberCurrentUserViewportAnchor();
        return;
      }
      if (!_stepTowardUnbuiltAnchorItem(anchor)) {
        _endResumeViewportRestore('anchor_unreachable');
        return;
      }
      WidgetsBinding.instance.addPostFrameCallback(step);
      WidgetsBinding.instance.scheduleFrame();
    }

    WidgetsBinding.instance.addPostFrameCallback(step);
    WidgetsBinding.instance.scheduleFrame();
  }

  /// 锚点消息卡片未构建时，向它所在方向跳近一个视口高度。返回 false 表示
  /// 锚点已不存在或无法再靠近（应结束恢复窗口）。
  bool _stepTowardUnbuiltAnchorItem(ChatViewportAnchor anchor) {
    final messages = owner.imService.currentMessages;
    var anchorIndex = -1;
    for (var i = 0; i < messages.length; i++) {
      if (ChatMessageIdentity.selectionKey(messages[i]) == anchor.itemKey) {
        anchorIndex = i;
        break;
      }
    }
    if (anchorIndex < 0) {
      return false;
    }
    int? nearestBuiltIndex;
    for (var i = 0; i < messages.length; i++) {
      final globalKey = owner.peekMessageViewportItemGlobalKey(
        ChatMessageIdentity.selectionKey(messages[i]),
      );
      final context = globalKey?.currentContext;
      if (context == null) {
        continue;
      }
      final renderObject = context.findRenderObject();
      if (renderObject is! RenderBox || !renderObject.attached) {
        continue;
      }
      if (nearestBuiltIndex == null ||
          (i - anchorIndex).abs() < (nearestBuiltIndex - anchorIndex).abs()) {
        nearestBuiltIndex = i;
      }
    }
    if (nearestBuiltIndex == null) {
      return false;
    }
    final viewport = _resolveScrollableViewportRenderBox();
    final stepExtent = ((viewport?.size.height ?? 400.0)).clamp(200.0, 1200.0);
    final position = owner.scrollController.position;
    final direction = anchorIndex < nearestBuiltIndex ? -1.0 : 1.0;
    final target = (position.pixels + direction * stepExtent)
        .clamp(position.minScrollExtent, position.maxScrollExtent)
        .toDouble();
    if ((target - position.pixels).abs() < 0.5) {
      return false;
    }
    _addScrollBreadcrumb('resume_step', {
      'from': position.pixels,
      'to': target,
      'anchorIndex': anchorIndex,
      'nearestBuilt': nearestBuiltIndex,
    });
    owner.scrollController.jumpTo(target);
    return true;
  }

  void _addScrollBreadcrumb(String event, Map<String, Object?> data) {
    Sentry.addBreadcrumb(
      Breadcrumb(
        category: 'chat_scroll',
        message: event,
        data: {
          for (final entry in data.entries)
            if (entry.value != null) entry.key: entry.value!,
        },
        level: SentryLevel.info,
      ),
    );
  }

  void onUserScrollStart(ScrollMetrics metrics) {
    // 桌面端失焦（inactive）后仍能滚动：用户一动就作废后台锚点与恢复窗口。
    owner._backgroundViewportAnchor = null;
    _endResumeViewportRestore('user_scroll');
    owner._userScrollInteractionActive = true;
    owner._initialBottomAnchoring = false;
    owner._metricsAnchorRestoreGeneration++;
    owner._managedInputCoordinator.onUserScrollTakeover();
    syncBottomFollowState(
      distanceToBottom: distanceToBottom(metrics),
      fromUserInteraction: true,
    );
    _rememberCurrentUserViewportAnchor();
  }

  void onUserScrollActive(ScrollMetrics metrics) {
    owner._backgroundViewportAnchor = null;
    _endResumeViewportRestore('user_scroll');
    owner._userScrollInteractionActive = true;
    owner._initialBottomAnchoring = false;
    owner._metricsAnchorRestoreGeneration++;
    owner._managedInputCoordinator.onUserScrollTakeover();
    syncBottomFollowState(
      distanceToBottom: distanceToBottom(metrics),
      fromUserInteraction: true,
    );
    _rememberCurrentUserViewportAnchor();
  }

  void onUserScrollEnd(ScrollMetrics metrics) {
    owner._userScrollInteractionActive = false;
    owner._initialBottomAnchoring = false;
    _startUserScrollCooldown();
    syncBottomFollowState(
      distanceToBottom: distanceToBottom(metrics),
      fromUserInteraction: true,
    );
    _rememberCurrentUserViewportAnchor();
  }

  /// Safety net: clear stuck user scroll interaction state.
  /// Called from UserScrollNotification(direction: idle) which Flutter
  /// guarantees to dispatch when the user stops scrolling, even when
  /// ScrollEndNotification.dragDetails is null (drag cancelled/reassigned).
  void onUserScrollInteractionReset() {
    owner._userScrollInteractionActive = false;
    owner._pointerSignalScrollInteractionActive = false;
  }

  /// Mouse-wheel/trackpad pointer scroll should temporarily block competing
  /// viewport intents, but it should not disable bottom follow or start
  /// cooldown logic like an explicit drag takeover does.
  void onWheelScrollActive(ScrollMetrics metrics) {
    owner._backgroundViewportAnchor = null;
    _endResumeViewportRestore('user_scroll');
    owner._pointerSignalScrollInteractionActive = true;
    owner._initialBottomAnchoring = false;
    owner._metricsAnchorRestoreGeneration++;
    owner._managedInputCoordinator.onUserScrollTakeover();
    syncBottomFollowState(
      distanceToBottom: distanceToBottom(metrics),
      fromUserInteraction: true,
    );
    _rememberCurrentUserViewportAnchor();
  }

  void onWheelScrollEnd(ScrollMetrics metrics) {
    owner._pointerSignalScrollInteractionActive = false;
    owner._initialBottomAnchoring = false;
    syncBottomFollowState(
      distanceToBottom: distanceToBottom(metrics),
      fromUserInteraction: true,
    );
    _rememberCurrentUserViewportAnchor();
  }

  void onNestedScrollableUserDragStart() {
    _pauseAutoFollowForNestedDrag();
  }

  void onNestedScrollableUserDragActive() {
    _pauseAutoFollowForNestedDrag();
  }

  void _pauseAutoFollowForNestedDrag() {
    owner._initialBottomAnchoring = false;
    owner._userScrollInteractionActive = true;
    owner._autoFollowBottom = false;
    owner._managedInputCoordinator.onUserScrollTakeover();
  }

  void onNestedScrollableUserDragEnd() {
    owner._userScrollInteractionActive = false;
    _startUserScrollCooldown();
  }

  /// Record the end of user scroll. During the cooldown window
  /// [onScrollMetricsChanged] will not auto-anchor to bottom, preventing
  /// jitter when deferred markdown renders change item heights right after
  /// the user lifts their finger.
  void _startUserScrollCooldown() {
    owner._lastUserScrollEndTime = DateTime.now();
    if (owner.scrollController.hasClients) {
      owner._lastUserScrollEndDistanceToBottom = distanceToBottom(
        owner.scrollController.position,
      );
    }
  }

  bool canExecuteBottomFollow() {
    if (!shouldAutoFollowBottomUpdates) {
      return false;
    }
    if (!owner.scrollController.hasClients) {
      return true;
    }
    if (!_hasAnyUserScrollInteractionActive) {
      return true;
    }
    return distanceToBottom(owner.scrollController.position) <= 0.5;
  }

  void syncBottomFollowState({
    required double distanceToBottom,
    required bool fromUserInteraction,
  }) {
    if (owner._initialBottomAnchoring) {
      return;
    }
    if (distanceToBottom <= ChatController._bottomResumeThreshold) {
      owner._autoFollowBottom = true;
      return;
    }
    if (fromUserInteraction) {
      owner._autoFollowBottom = false;
    }
  }

  double distanceToBottom(ScrollMetrics metrics) {
    return normalizeDistanceToBottom(metrics.maxScrollExtent - metrics.pixels);
  }

  double normalizeDistanceToBottom(double rawDistance) {
    if (rawDistance.isNaN || rawDistance.isInfinite) {
      return 0;
    }
    return rawDistance <= 0 ? 0 : rawDistance;
  }

  bool get shouldPreserveBottomViewportOnInputActivation =>
      _shouldKeepBottomAnchorAfterBottomViewportChange();

  ChatViewportAnchor? captureViewportAnchor() {
    return _captureLeadingVisibleMessageAnchor();
  }

  void _rememberCurrentUserViewportAnchor() {
    if (!owner.scrollController.hasClients) {
      return;
    }
    if (shouldAutoFollowBottomUpdates) {
      return;
    }
    final anchor = _captureLeadingVisibleMessageAnchor();
    if (anchor == null) {
      return;
    }
    owner._lastUserViewportAnchor = anchor;
    owner._lastUserViewportAnchorCapturedAt = DateTime.now();
    owner._userViewportAnchorGeneration++;
  }

  bool _scheduleRecentUserViewportAnchorRestore() {
    if (!owner.scrollController.hasClients) {
      return false;
    }
    if (_isUserScrollTakingPriority()) {
      return false;
    }
    if (shouldAutoFollowBottomUpdates) {
      return false;
    }
    final anchor = owner._lastUserViewportAnchor;
    final capturedAt = owner._lastUserViewportAnchorCapturedAt;
    if (anchor == null || capturedAt == null) {
      return false;
    }
    final elapsed = DateTime.now().difference(capturedAt);
    if (elapsed > ChatController._userViewportAnchorFreshness) {
      return false;
    }

    final anchorGeneration = owner._userViewportAnchorGeneration;
    final restoreGeneration = ++owner._metricsAnchorRestoreGeneration;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_isOwnerClosed) {
        return;
      }
      if (!owner.scrollController.hasClients) {
        return;
      }
      if (restoreGeneration != owner._metricsAnchorRestoreGeneration ||
          anchorGeneration != owner._userViewportAnchorGeneration) {
        return;
      }
      if (_isUserScrollTakingPriority()) {
        return;
      }
      if (shouldAutoFollowBottomUpdates) {
        return;
      }
      if (_restoreLeadingVisibleMessageAnchor(anchor)) {
        _rememberCurrentUserViewportAnchor();
      }
    });
    WidgetsBinding.instance.scheduleFrame();
    return true;
  }

  bool _isUserScrollTakingPriority() {
    return _hasAnyUserScrollInteractionActive || owner._userScrollCooldown;
  }

  double? _measureInsertedTopExtent({required String beforeFirstItemKey}) {
    var totalHeight = 0.0;
    for (final message in owner.imService.currentMessages) {
      final itemKey = ChatMessageIdentity.selectionKey(message);
      if (itemKey == beforeFirstItemKey) {
        return totalHeight;
      }
      final globalKey = owner.peekMessageViewportItemGlobalKey(itemKey);
      final context = globalKey?.currentContext;
      if (context == null) {
        return null;
      }
      final renderObject = context.findRenderObject();
      if (renderObject is! RenderBox || !renderObject.attached) {
        return null;
      }
      totalHeight += renderObject.size.height;
    }
    return null;
  }

  ChatViewportAnchor? _captureLeadingVisibleMessageAnchor() {
    if (!owner.scrollController.hasClients) {
      return null;
    }
    final viewport = _resolveScrollableViewportRenderBox();
    if (viewport == null || viewport.size.height <= 0) {
      return null;
    }

    ChatViewportAnchor? bestAnchor;
    for (final message in owner.imService.currentMessages) {
      final itemKey = ChatMessageIdentity.selectionKey(message);
      final globalKey = owner.peekMessageViewportItemGlobalKey(itemKey);
      final context = globalKey?.currentContext;
      if (context == null) {
        continue;
      }
      final renderObject = context.findRenderObject();
      if (renderObject is! RenderBox || !renderObject.attached) {
        continue;
      }
      final topLeft = renderObject.localToGlobal(
        Offset.zero,
        ancestor: viewport,
      );
      final top = topLeft.dy;
      final bottom = top + renderObject.size.height;
      if (bottom <= 0 || top >= viewport.size.height) {
        continue;
      }
      if (bestAnchor == null || top < bestAnchor.leadingOffset) {
        bestAnchor = ChatViewportAnchor(itemKey: itemKey, leadingOffset: top);
      }
    }
    return bestAnchor;
  }

  bool _restoreLeadingVisibleMessageAnchor(ChatViewportAnchor anchor) {
    if (!owner.scrollController.hasClients) {
      return false;
    }
    final viewport = _resolveScrollableViewportRenderBox();
    if (viewport == null) {
      return false;
    }
    final globalKey = owner.peekMessageViewportItemGlobalKey(anchor.itemKey);
    final context = globalKey?.currentContext;
    if (context == null) {
      return false;
    }
    final renderObject = context.findRenderObject();
    if (renderObject is! RenderBox || !renderObject.attached) {
      return false;
    }

    final topLeft = renderObject.localToGlobal(Offset.zero, ancestor: viewport);
    final delta = topLeft.dy - anchor.leadingOffset;
    if (delta.abs() < 0.5) {
      return true;
    }

    final position = owner.scrollController.position;
    final target = (position.pixels + delta)
        .clamp(position.minScrollExtent, position.maxScrollExtent)
        .toDouble();
    if ((target - position.pixels).abs() < 0.5) {
      return true;
    }
    owner.scrollController.jumpTo(target);
    return true;
  }

  RenderBox? _resolveScrollableViewportRenderBox() {
    final context = owner.scrollController.position.context.notificationContext;
    if (context == null) {
      return null;
    }
    final renderObject = context.findRenderObject();
    if (renderObject is! RenderBox || !renderObject.attached) {
      return null;
    }
    return renderObject;
  }

  void _revealManagedInputInVisibleViewport(ChatManagedInputId inputId) {
    if (!owner.scrollController.hasClients) {
      return;
    }
    final context = owner._managedInputRegistry.currentContextOf(inputId);
    if (context == null || !context.mounted) {
      return;
    }
    final renderObject = context.findRenderObject();
    if (renderObject is! RenderBox || !renderObject.attached) {
      return;
    }
    final viewport = _resolveScrollableViewportRenderBox();
    if (viewport == null || viewport.size.height <= 0) {
      return;
    }

    final targetTopLeft = renderObject.localToGlobal(
      Offset.zero,
      ancestor: viewport,
    );
    final targetTop = targetTopLeft.dy;
    final targetBottom = targetTop + renderObject.size.height;
    final visibleBottom =
        viewport.size.height - owner.messageListViewportObstructionBottom;

    double? delta;
    if (targetBottom > visibleBottom) {
      delta = targetBottom - visibleBottom;
    } else if (targetTop < 0) {
      delta = targetTop;
    }
    if (delta == null || delta.abs() < 0.5) {
      return;
    }

    final position = owner.scrollController.position;
    final targetPixels = (position.pixels + delta)
        .clamp(position.minScrollExtent, position.maxScrollExtent)
        .toDouble();
    if ((targetPixels - position.pixels).abs() < 0.5) {
      return;
    }
    owner.scrollController.jumpTo(targetPixels);
  }

  void _executeBottomFollowOnCurrentFrame({
    required bool animated,
    required bool force,
  }) {
    if (!owner.scrollController.hasClients) {
      return;
    }
    // Re-check after frame: user may have started dragging since scheduling.
    if (_isBottomViewportFollowBlockedByUserScroll()) {
      return;
    }
    if (!force && !canExecuteBottomFollow()) {
      return;
    }

    final position = owner.scrollController.position;
    final target = position.maxScrollExtent;
    final distance = (target - position.pixels).abs();
    if (distance < 1) {
      _markBottomAnchored();
      return;
    }

    try {
      if (animated) {
        owner.scrollController.animateTo(
          target,
          duration: const Duration(milliseconds: 180),
          curve: Curves.easeOut,
        );
      } else {
        owner.scrollController.jumpTo(target);
      }
      _markBottomAnchored();
    } catch (e) {
      debugPrint('⚠️ scrollToBottom ignored: ScrollPosition is not ready. $e');
    }
  }

  void _markBottomAnchored() {
    if (owner._initialBottomAnchoring &&
        owner.imService.currentMessages.isNotEmpty) {
      owner._initialBottomAnchoring = false;
    }
  }

  void _scheduleSettledViewportIntentExecution({
    Duration minDuration = ChatController._bottomViewportFollowMinDuration,
    Duration maxDuration = ChatController._bottomViewportFollowMaxDuration,
    int stableFrames = ChatController._bottomViewportFollowStableFrames,
    int? sourceKeyboardEpoch,
  }) {
    if (minDuration < Duration.zero) {
      minDuration = Duration.zero;
    }
    if (maxDuration < minDuration) {
      maxDuration = minDuration;
    }
    if (stableFrames < 1) {
      stableFrames = 1;
    }
    final initialIntent = _resolveViewportIntent();
    if (initialIntent.type == ChatViewportIntentType.noop) {
      return;
    }
    if (sourceKeyboardEpoch != null) {
      if (owner._executedKeyboardViewportChangeEpoch == sourceKeyboardEpoch ||
          owner._scheduledKeyboardViewportChangeEpoch == sourceKeyboardEpoch) {
        return;
      }
      owner._scheduledKeyboardViewportChangeEpoch = sourceKeyboardEpoch;
    }
    final executionGeneration = ++owner._viewportIntentExecutionGeneration;
    double? lastMaxScrollExtent;
    var stableFrameCount = 0;
    Duration? executionStartedAt;

    void executeAfterFrame(Duration timestamp) {
      if (owner.isClosed ||
          executionGeneration != owner._viewportIntentExecutionGeneration) {
        return;
      }
      final intent = _resolveViewportIntent();
      if (intent.type == ChatViewportIntentType.noop) {
        return;
      }
      executionStartedAt ??= timestamp;
      final elapsed = timestamp - executionStartedAt!;
      if (_isViewportIntentBlockedByUserScroll(intent)) {
        if (elapsed < maxDuration) {
          WidgetsBinding.instance.addPostFrameCallback(executeAfterFrame);
        } else if (sourceKeyboardEpoch != null &&
            owner._scheduledKeyboardViewportChangeEpoch ==
                sourceKeyboardEpoch) {
          owner._scheduledKeyboardViewportChangeEpoch = -1;
        }
        return;
      }

      _executeViewportIntentOnCurrentFrame(intent);
      if (sourceKeyboardEpoch != null &&
          intent.type != ChatViewportIntentType.noop) {
        owner._executedKeyboardViewportChangeEpoch = sourceKeyboardEpoch;
      }

      if (!owner.scrollController.hasClients) {
        if (elapsed < maxDuration) {
          WidgetsBinding.instance.addPostFrameCallback(executeAfterFrame);
        }
        return;
      }

      final currentMaxScrollExtent =
          owner.scrollController.position.maxScrollExtent;
      if (lastMaxScrollExtent != null &&
          (currentMaxScrollExtent - lastMaxScrollExtent!).abs() < 0.5) {
        stableFrameCount++;
      } else {
        stableFrameCount = 0;
      }
      lastMaxScrollExtent = currentMaxScrollExtent;
      final reachedStableViewport =
          elapsed >= minDuration && stableFrameCount >= stableFrames;
      if (reachedStableViewport || elapsed >= maxDuration) {
        if (sourceKeyboardEpoch != null &&
            owner._scheduledKeyboardViewportChangeEpoch ==
                sourceKeyboardEpoch) {
          owner._scheduledKeyboardViewportChangeEpoch = -1;
        }
        if (intent.type == ChatViewportIntentType.restoreAnchor ||
            owner._managedInputCoordinator.shouldRestoreBottom) {
          owner._managedInputCoordinator.clearPendingRestore();
        }
        return;
      }
      WidgetsBinding.instance.addPostFrameCallback(executeAfterFrame);
    }

    WidgetsBinding.instance.addPostFrameCallback(executeAfterFrame);
  }

  ChatViewportIntent _resolveViewportIntent() {
    final activeInputId = owner._managedInputCoordinator.activeInputId;
    final activePolicy = owner._managedInputCoordinator.activeInputPolicy;
    if (activeInputId != null && activePolicy != null) {
      if (activePolicy.revealMode == ChatManagedInputRevealMode.revealOnly) {
        return ChatViewportIntent.revealActiveInput(activeInputId);
      }
      if (_shouldKeepBottomAnchorAfterBottomViewportChange()) {
        return const ChatViewportIntent.stickBottom();
      }
    }

    final pendingRestoreAnchor =
        owner._managedInputCoordinator.pendingRestoreAnchor;
    if (pendingRestoreAnchor != null) {
      return ChatViewportIntent.restoreAnchor(pendingRestoreAnchor);
    }

    if (owner._managedInputCoordinator.shouldRestoreBottom ||
        _shouldKeepBottomAnchorAfterBottomViewportChange()) {
      return const ChatViewportIntent.stickBottom();
    }

    return const ChatViewportIntent.noop();
  }

  void _executeViewportIntentOnCurrentFrame(ChatViewportIntent intent) {
    switch (intent.type) {
      case ChatViewportIntentType.noop:
        return;
      case ChatViewportIntentType.stickBottom:
        _executeBottomFollowOnCurrentFrame(animated: false, force: true);
        return;
      case ChatViewportIntentType.revealActiveInput:
        final inputId = intent.inputId;
        if (inputId == null) {
          return;
        }
        _revealManagedInputInVisibleViewport(inputId);
        return;
      case ChatViewportIntentType.restoreAnchor:
        final anchor = intent.anchor;
        if (anchor == null) {
          return;
        }
        _restoreLeadingVisibleMessageAnchor(anchor);
        return;
    }
  }

  bool get _hasFocusDrivenBottomViewportFollowIntent =>
      owner.focusNode.hasFocus ||
      owner.hasManagedInputFocus ||
      owner._managedInputCoordinator.shouldRestoreBottom ||
      owner._restoreInputFocusPending;

  bool get _canKeepBottomAnchorForFocusDrivenViewportChange =>
      owner._autoFollowBottom && _hasFocusDrivenBottomViewportFollowIntent;

  bool _isBottomViewportFollowBlockedByUserScroll() {
    if (!_hasAnyUserScrollInteractionActive) {
      return false;
    }
    return true;
  }

  bool _isViewportIntentBlockedByUserScroll(ChatViewportIntent intent) {
    if (_isBottomViewportFollowBlockedByUserScroll()) {
      return true;
    }
    if (intent.type == ChatViewportIntentType.stickBottom &&
        owner._userScrollCooldown &&
        !_canKeepBottomAnchorForFocusDrivenViewportChange &&
        !owner._managedInputCoordinator.shouldRestoreBottom) {
      return true;
    }
    return false;
  }

  bool _shouldKeepBottomAnchorAfterBottomViewportChange() {
    if (owner._initialBottomAnchoring) {
      return true;
    }
    if (owner._autoFollowBottom) {
      return true;
    }
    return _canKeepBottomAnchorForFocusDrivenViewportChange;
  }

  double _readKeyboardInsetBottom() {
    final view =
        owner._boundFlutterView ??
        WidgetsBinding.instance.platformDispatcher.implicitView;
    if (view == null) {
      return 0;
    }
    return view.viewInsets.bottom / view.devicePixelRatio;
  }

  // Clears a stale agent-output capsule when a newer message that is unrelated
  // to the capsule's triggering event has arrived.  This handles the case where
  // the agent never reaches a terminal state (e.g. timeout, connection drop)
  // so the capsule would otherwise linger until the 90-second pruner fires.
  //
  // Conditions to clear:
  //   1. There is an active capsule for the current session.
  //   2. The newest visible message is NOT the capsule's trigger message.
  //   3. The newest message was created at least 5 seconds after the capsule's
  //      last update, indicating the agent is no longer making progress.
  void _clearOrphanedAgentOutputCapsule(List<MessageModel> messages) {
    if (messages.isEmpty) return;
    final capsule = owner.imService.agentOutputStateFor(owner.sessionId);
    if (capsule == null) return;
    final triggerMsgId = capsule['trigger_msg_id']?.toString().trim() ?? '';
    final updatedAt = capsule['updated_at'] as int? ?? 0;
    if (updatedAt <= 0) return;
    final latest = messages.last;
    if (latest.msgId == triggerMsgId) return;
    const staleThresholdMs = 5000;
    if (latest.createdAt - updatedAt >= staleThresholdMs) {
      owner.imService.agentOutputStates.remove(owner.sessionId);
    }
  }
}
