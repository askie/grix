import 'dart:async';
import 'dart:convert';
import 'dart:io' show Directory, File, Platform;
import 'dart:ui' show FlutterView;

import 'package:file_picker/file_picker.dart';
import 'package:flutter/foundation.dart';
import 'package:path_provider/path_provider.dart';
import '../../../shared/services/native_clipboard_service.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';
import 'package:sentry_flutter/sentry_flutter.dart';
import '../../../data/providers/im_service.dart';
import '../../../data/providers/auth_service.dart';
import '../../../data/providers/agent_service.dart';
import '../../../data/providers/user_settings_service.dart';
import '../../../data/providers/friend_service.dart';
import '../../../data/providers/session_service.dart';
import '../../../data/providers/oss_service.dart';
import '../../../app/routes/app_routes.dart';
import '../../account_info/services/account_info_navigator.dart';
import '../../../app/routes/root_route_navigator.dart';
import '../../../app/settings/chat_background_service.dart';
import '../../../shared/models/chat_message_attachment.dart';
import '../../../shared/models/session_avatar_member.dart';
import '../../../shared/utils/chat_bind_directory_message.dart';
import '../../../shared/utils/chat_draft_index.dart';
import '../../../shared/utils/chat_numeric_mention_resolver.dart';
import '../../../shared/utils/toast_util.dart';
import '../../../data/models/message_model.dart';
import '../../../data/models/session_model.dart';
import '../../../modules/call/call_controller.dart';
import '../../../data/models/session_activity_model.dart';
import '../message_cards/models/chat_conversation_card_data.dart';
import '../message_cards/models/chat_agent_open_session_card_data.dart';
import '../message_cards/models/chat_agent_question_card_data.dart';
import '../message_cards/models/chat_call_owner_card_data.dart';
import '../message_cards/models/chat_exec_approval_card_data.dart';
import '../message_cards/models/chat_exec_status_card_data.dart';
import '../message_cards/models/chat_message_card_action.dart';
import '../message_cards/models/chat_message_card_data.dart';
import '../message_cards/models/chat_user_profile_card_data.dart';
import '../message_cards/services/chat_agent_card_action_encoder.dart';
import '../message_cards/services/chat_message_card_codec.dart';
import '../bindings/chat_binding.dart';
import '../models/chat_forward_dispatch_mode.dart';
import '../models/chat_forward_message_item.dart';
import '../models/chat_forward_target_option.dart';
import '../models/chat_attachment_type.dart';
import '../models/chat_message_list_snapshot.dart';
import '../models/chat_prepared_attachment_upload.dart';
import '../models/chat_message_identity.dart';
import '../services/chat_attachment_payload_builder.dart';
import '../services/chat_attachment_limit_policy.dart';
import '../services/agent_remote_file_node_mapper.dart';
import '../services/chat_bottom_obstruction_observer.dart';
import '../services/chat_file_interceptor.dart';
import '../services/chat_image_compression_service.dart';
import '../services/chat_forward_content_builder.dart';
import '../services/chat_forward_mention_sanitizer.dart';
import '../services/chat_managed_input.dart';
import '../services/chat_managed_input_registry.dart';
import '../services/chat_message_list_snapshot_builder.dart';
import '../services/chat_message_window_owners.dart';
import '../services/chat_message_owner_classifier.dart';
import '../services/chat_recent_bind_directory_store.dart';
import '../services/conversation_audit_preference_service.dart';
import '../services/chat_keyboard_platform_behavior.dart';
import '../services/chat_forward_target_option_resolver.dart';
import '../services/chat_route_navigator.dart';
import '../services/chat_voice_command_response_filter.dart';
import '../services/chat_voice_command_support.dart';
import '../services/chat_viewport_intent.dart';
import '../services/system_voice_command_io.dart';
import '../services/voice_command_io.dart';
import '../services/private_chat_open_perf_logger.dart';
import '../widgets/chat_image_editor_page.dart';
import 'chat_input_coordinator.dart';
import 'chat_voice_command_controller.dart';
import '../../../shared/utils/hardware_facade.dart';
import '../../../shared/widgets/remote_file_picker/remote_file_picker.dart';
import '../../../data/providers/user_favorite_path_service.dart';
import '../../../data/providers/feature_flag_service.dart';
import '../services/chat_pane_host.dart';

part 'chat_attachment_controller.dart';
part 'chat_voice_command_adapter.dart';
part 'chat_delegate_controller.dart';
part 'chat_group_controller.dart';
part 'chat_identity_controller.dart';
part 'chat_forward_controller.dart';
part 'chat_input_controller.dart';
part 'chat_mention_controller.dart';
part 'chat_navigation_controller.dart';
part 'chat_page_state_controller.dart';
part 'chat_status_controller.dart';

class _PendingMention {
  const _PendingMention({required this.memberId, required this.displayName});

  final String memberId;
  final String displayName;
}

class PinnedMention {
  const PinnedMention({required this.memberId, required this.displayName});

  final String memberId;
  final String displayName;
}

const String _mentionAllSyntheticMemberId = '__mention_all__';
String get _mentionAllDisplayName {
  final translated = 'chat_mention_all'.tr;
  return translated == 'chat_mention_all' ? '所有人' : translated;
}

const String _mentionAllExtraKey = 'mention_all';
const String _mentionBuiltinKindKey = 'builtin_kind';
const String _mentionBuiltinKindAll = 'mention_all';

class _ResolvedMentionTarget {
  const _ResolvedMentionTarget({
    required this.memberId,
    required this.displayName,
  });

  final String memberId;
  final String displayName;
}

typedef _ChatInputValueTransformer =
    TextEditingValue Function(TextEditingValue currentValue);

class _DeferredChatInputEdit {
  const _DeferredChatInputEdit({
    required this.transformer,
    this.requestFocus = false,
  });

  final _ChatInputValueTransformer transformer;
  final bool requestFocus;
}

class _FriendDisplayDigest {
  const _FriendDisplayDigest({
    required this.nickname,
    required this.username,
    required this.remarkName,
    required this.avatarUrl,
  });

  factory _FriendDisplayDigest.fromFriend(FriendItem friend) {
    return _FriendDisplayDigest(
      nickname: friend.nickname.trim(),
      username: friend.username.trim(),
      remarkName: friend.remarkName.trim(),
      avatarUrl: friend.avatarUrl.trim(),
    );
  }

  final String nickname;
  final String username;
  final String remarkName;
  final String avatarUrl;

  @override
  bool operator ==(Object other) {
    if (identical(this, other)) return true;
    return other is _FriendDisplayDigest &&
        other.nickname == nickname &&
        other.username == username &&
        other.remarkName == remarkName &&
        other.avatarUrl == avatarUrl;
  }

  @override
  int get hashCode => Object.hash(nickname, username, remarkName, avatarUrl);
}

class ChatController extends GetxController with WidgetsBindingObserver {
  ChatController({
    this.routeArguments,
    ChatImageCompressionService? imageCompressionService,
    ChatBottomObstructionObserver? bottomObstructionObserver,
    ChatKeyboardPlatformBehavior? keyboardPlatformBehavior,
    ConversationAuditPreferenceService? conversationAuditPreferenceService,
  }) : _imageCompressionService =
           imageCompressionService ?? const ChatImageCompressionService(),
       _bottomObstructionObserver =
           bottomObstructionObserver ?? createChatBottomObstructionObserver(),
       _conversationAuditPreferenceService =
           conversationAuditPreferenceService ??
           _findOrRegisterConversationAuditPreferenceService(),
       keyboardPlatformBehavior =
           keyboardPlatformBehavior ?? ChatKeyboardPlatformBehavior.resolve();

  /// Explicit route arguments for controllers created outside GetX routing
  /// (desktop chat pane). Null means read from `Get.arguments`.
  final Map<String, dynamic>? routeArguments;

  static ConversationAuditPreferenceService
  _findOrRegisterConversationAuditPreferenceService() {
    if (Get.isRegistered<ConversationAuditPreferenceService>()) {
      return Get.find<ConversationAuditPreferenceService>();
    }
    return Get.put<ConversationAuditPreferenceService>(
      ConversationAuditPreferenceService(),
      permanent: true,
    );
  }

  static const int delegateMinRounds = 1;
  static const int delegateMaxRounds = 50;
  static const int delegateDefaultRounds =
      ImService.defaultDelegateMaxConsecutiveReplies;
  static const double _bottomSnapThreshold = 120;
  static const double _bottomResumeThreshold = 4;
  static const double _historyLoadTriggerThreshold = 200;
  static const double _topPinnedHistoryLoadThreshold = 1;
  static const Duration _initialMessageLoadDelay = Duration(
    milliseconds: AppRoutes.defaultPageTransitionMilliseconds + 80,
  );
  static const Duration _inputSubmitStabilizationDuration = Duration(
    milliseconds: 300,
  );
  static const Duration _bottomViewportFollowMinDuration = Duration(
    milliseconds: 180,
  );
  static const Duration _bottomViewportFollowMaxDuration = Duration(
    milliseconds: 420,
  );
  static const int _bottomViewportFollowStableFrames = 2;
  static const double _initialBottomScrollOffset = 1e8;
  static const int _maxInputRunes = 100000;
  static const Duration _sendCooldown = Duration(milliseconds: 1200);

  static const Duration _agentDeliveryErrorDisplayGrace = Duration(seconds: 60);
  static const ChatManagedInputId _composerManagedInputId = ChatManagedInputId(
    kind: ChatManagedInputKind.composer,
    instanceId: 'main',
  );

  final ImService imService = Get.find<ImService>();
  final AuthService authService = Get.find<AuthService>();
  final AgentService agentService = Get.find<AgentService>();
  final SessionService sessionService = Get.find<SessionService>();
  final OssService ossService = Get.find<OssService>();
  final FriendService? _friendService = Get.isRegistered<FriendService>()
      ? Get.find<FriendService>()
      : null;
  final ChatImageCompressionService _imageCompressionService;
  late final ChatForwardTargetOptionResolver _forwardTargetOptionResolver =
      ChatForwardTargetOptionResolver(
        imService: imService,
        friendService: _friendService,
        agentService: agentService,
      );
  late final _ChatPageStateController _pageStateController =
      _ChatPageStateController(this);
  late final _ChatIdentityController _chatIdentityController =
      _ChatIdentityController(this);
  late final _ChatDelegateController _chatDelegateController =
      _ChatDelegateController(this);
  late final _ChatStatusController _chatStatusController =
      _ChatStatusController(this);
  late final _ChatGroupController _chatGroupController = _ChatGroupController(
    this,
  );
  late final _ChatNavigationController _chatNavigationController =
      _ChatNavigationController(this);
  late final _ChatInputController _chatInputController = _ChatInputController(
    this,
  );
  late final _ChatForwardController _chatForwardController =
      _ChatForwardController(this);
  late final _ChatAttachmentController _chatAttachmentController =
      _ChatAttachmentController(this);
  late final _ChatMentionController _chatMentionController =
      _ChatMentionController(this);
  late final _ChatVoiceCommandAdapter _chatVoiceCommandAdapter =
      _ChatVoiceCommandAdapter(this);
  late final ChatVoiceCommandController _chatVoiceCommandController =
      ChatVoiceCommandController(
        chat: _chatVoiceCommandAdapter,
        transcriber: SystemVoiceCommandIO.transcriber,
        speaker: SystemVoiceCommandIO.speaker,
        notice: (message, {isError = false}) {
          CustomToast.show(message, isError: isError);
        },
      );
  final ChatKeyboardPlatformBehavior keyboardPlatformBehavior;

  final TextEditingController inputController = TextEditingController();
  final ScrollController scrollController = ScrollController(
    initialScrollOffset: _initialBottomScrollOffset,
    keepScrollOffset: false,
  );
  final Map<String, GlobalKey> _messageViewportItemKeys = {};
  final ChatMessageListSnapshotBuilder _messageListSnapshotBuilder =
      ChatMessageListSnapshotBuilder();
  ChatMessageListSnapshot _messageListSnapshot = ChatMessageListSnapshot.empty;
  final RxInt _messageListSnapshotRevision = 0.obs;
  int _messageListSnapshotBuildCount = 0;
  final ChatManagedInputRegistry _managedInputRegistry =
      ChatManagedInputRegistry();
  final ChatInputCoordinator _managedInputCoordinator = ChatInputCoordinator();
  final FocusNode focusNode = FocusNode();

  /// 全屏编辑器打开时由其注册自己的焦点节点，接管"编辑后回焦"目标
  /// （如 @提及插入、草稿回填），避免焦点被抢回底部小输入框。
  FocusNode? expandedInputFocusNodeOverride;
  FocusNode get activeInputFocusNode =>
      expandedInputFocusNodeOverride ?? focusNode;
  bool isExpandedInputEditorOpen = false;
  final RxBool showInputExpandButton = false.obs;

  /// Ctrl+Enter 与 Cmd+Enter 在所有平台都提交消息。状态直接读
  /// [HardwareKeyboard]：引擎在分发每个按键前都会按平台修饰键标志同步它；
  /// 只靠输入框聚焦期间收到的事件自行跟踪，会在切换应用等场景漏掉
  /// 修饰键的按下/抬起而失步。
  bool get isSendModifierPressed =>
      HardwareKeyboard.instance.isControlPressed ||
      HardwareKeyboard.instance.isMetaPressed;

  Worker? _messageWorker;
  Worker? _messageSnapshotWorker;
  Worker? _delegateStateWorker;
  Worker? _agentOutputStateWorker;
  Worker? _sessionMemberEventWorker;
  Worker? _sessionAccessRevokedWorker;
  Worker? _sessionsWorker;
  Worker? _friendListWorker;
  Worker? _profileCacheWorker;
  Worker? _agentsWorker;
  Worker? _sharedAgentsWorker;
  DateTime? _lastSendAt;

  Worker? _sessionActivityWorker;
  Map<String, dynamic>? _privateChatOpenPerfTrace;
  bool _privateChatOpenPerfEnterSessionLogged = false;
  bool _privateChatOpenPerfFirstMessagesLogged = false;
  int _lastSessionMemberEventVersion = 0;
  int _lastSessionAccessRevokedVersion = 0;
  bool _groupAccessLostHandled = false;
  bool _isLoadingHistory = false;
  bool _autoFollowBottom = true;
  bool _userScrollInteractionActive = false;
  bool _pointerSignalScrollInteractionActive = false;
  bool _scrollTaskScheduled = false;
  int _scrollToLoadedTopGeneration = 0;
  bool _scrollToLoadedTopInProgress = false;
  ChatViewportAnchor? _lastUserViewportAnchor;
  DateTime? _lastUserViewportAnchorCapturedAt;
  int _userViewportAnchorGeneration = 0;
  int _metricsAnchorRestoreGeneration = 0;

  /// Timestamp of the last user scroll end. During the cooldown window
  /// [onScrollMetricsChanged] will not auto-anchor to bottom, preventing
  /// jitter when deferred markdown renders change item heights right after
  /// the user lifts their finger.
  DateTime? _lastUserScrollEndTime;
  double? _lastUserScrollEndDistanceToBottom;
  static const Duration _userScrollCooldownDuration = Duration(
    milliseconds: 400,
  );
  static const Duration _userScrollCooldownDurationFar = Duration(
    milliseconds: 2000,
  );
  static const Duration _userViewportAnchorFreshness = Duration(
    milliseconds: 2500,
  );
  static const double _farFromBottomThreshold = 300;
  bool get _userScrollCooldown {
    final end = _lastUserScrollEndTime;
    if (end == null) return false;
    final elapsed = DateTime.now().difference(end);
    final cooldown =
        (_lastUserScrollEndDistanceToBottom ?? 0) > _farFromBottomThreshold
        ? _userScrollCooldownDurationFar
        : _userScrollCooldownDuration;
    return elapsed < cooldown;
  }

  bool _initialBottomAnchoring = true;
  bool _hasObservedScrollMetrics = false;
  double _lastObservedMaxScrollExtent = 0;
  double _lastKeyboardInsetBottom = 0;
  double _lastVisibleKeyboardInsetBottom = 0;
  bool _suppressNextInputSubmit = false;
  bool _suppressMetricsAnchorWhileKeyboardAnimating = false;
  int _inputFocusRetentionVersion = 0;
  Timer? _keyboardMetricsSettledTimer;
  Timer? _retainInputLayoutKeyboardInsetTimer;
  Timer? _iOSKeyboardDropHysteresisTimer;
  Timer? _restoreInputFocusTimer;
  // iOS 第三方输入法（搜狗/微信）切到后台再切回时，文本框输入连接会失效，
  // 候选词栏卡死。记录切后台前输入框是否在打字，resumed 时据此重建连接。
  bool _imeWasFocusedBeforeBackground = false;
  Timer? _imeResumeRebuildTimer;
  final List<_DeferredChatInputEdit> _pendingInputEdits =
      <_DeferredChatInputEdit>[];
  bool _deferredInputEditFlushScheduled = false;
  bool _flushingDeferredInputEdits = false;
  bool _restoreInputFocusPending = false;
  int _viewportIntentExecutionGeneration = 0;
  int _keyboardViewportChangeEpoch = 0;
  int _scheduledKeyboardViewportChangeEpoch = -1;
  int _executedKeyboardViewportChangeEpoch = -1;
  Timer? _composingDebounce;
  Timer? _draftPersistDebounce;
  bool? _lastComposingActive;
  final RxBool isInputOverLengthLimit = false.obs;
  String? _lastDraftSnapshot;
  DateTime? _lastScrollSyncTime;
  final _inflightProfileLoads = <String>{};
  final Map<String, String> _memberDisplayNameCache = {};
  final RxDouble _inputLayoutKeyboardInsetBottom = 0.0.obs;
  final RxDouble _platformViewportObstructionBottom = 0.0.obs;
  final RxDouble _messageListKeyboardInsetBottom = 0.0.obs;
  final RxBool _hasInputFocus = false.obs;
  bool _retainInputLayoutKeyboardInset = false;
  FlutterView? _boundFlutterView;
  final ChatBottomObstructionObserver _bottomObstructionObserver;
  final ConversationAuditPreferenceService _conversationAuditPreferenceService;
  String _privateConversationAuditAgentId = '';
  StreamSubscription<double>? _bottomObstructionSubscription;
  List<SessionAvatarMember> _initialGroupAvatarMembers =
      const <SessionAvatarMember>[];

  final RxBool _isUploadingAttachment = false.obs;
  RxBool get isUploadingImage => _isUploadingAttachment;
  bool get isUploadingAttachment => _isUploadingAttachment.value;

  /// 审计开关由后端 Feature Gate 控制；服务未注册或 Gate 未开启时均不显示/不发送。
  bool get isConversationAuditAvailable =>
      Get.isRegistered<FeatureFlagService>() &&
      Get.find<FeatureFlagService>().isEnabled('conversation_audit');

  String get conversationAuditAgentId {
    final toolbarAgentId = imService.getAgentToolbar(sessionId)?.agentId.trim();
    if (isGroupChat) {
      final targetAgentId = groupToolbarTargetAgentId.trim();
      return targetAgentId.isNotEmpty ? targetAgentId : '';
    }
    if (_privateConversationAuditAgentId.isNotEmpty) {
      return _privateConversationAuditAgentId;
    }
    final session = imService.findSessionById(sessionId);
    if (session?.peerType == 2) {
      final peerAgentId = session!.peerId.trim();
      if (peerAgentId.isNotEmpty) {
        _privateConversationAuditAgentId = peerAgentId;
        return _privateConversationAuditAgentId;
      }
    }
    if (toolbarAgentId?.isNotEmpty == true) {
      _privateConversationAuditAgentId = toolbarAgentId!;
    }
    return _privateConversationAuditAgentId;
  }

  RxBool get conversationAuditEnabled => _conversationAuditPreferenceService
      .stateForAgent(agentId: conversationAuditAgentId);

  bool get canToggleConversationAudit {
    // 访客会话工具栏只有访客管理项，不应追加对话审计开关。
    // session.isVisitor 可在详情加载前拦截；isVisitorSession 覆盖详情确认后的状态。
    final sessionIsVisitor =
        imService.findSessionById(sessionId)?.isVisitor == true;
    if (sessionIsVisitor || isVisitorSession) {
      return false;
    }
    return isConversationAuditAvailable &&
        (authService.userId?.trim().isNotEmpty ?? false) &&
        conversationAuditAgentId.isNotEmpty;
  }

  void toggleConversationAudit() {
    final agentId = conversationAuditAgentId;
    final userId = authService.userId?.trim() ?? '';
    if (!isConversationAuditAvailable || userId.isEmpty || agentId.isEmpty) {
      return;
    }
    unawaited(
      _conversationAuditPreferenceService.toggle(
        agentId: agentId,
        sessionId: sessionId,
      ),
    );
  }

  final RxBool isAttachmentMenuOpen = false.obs;
  bool _suppressMenuCloseOnFocusLoss = false;
  final RxList<PendingAttachmentUpload> stagedAttachments =
      <PendingAttachmentUpload>[].obs;
  bool get hasStagedAttachments => stagedAttachments.isNotEmpty;
  final ChatFileInterceptor _fileInterceptor = createChatFileInterceptor();
  final RxBool isFileDragOver = false.obs;
  double get inputLayoutKeyboardInsetBottom =>
      _inputLayoutKeyboardInsetBottom.value;
  bool get hasManagedInputFocus =>
      _managedInputCoordinator.hasManagedInputFocus;
  bool get shouldFollowKeyboardForInputDock =>
      _hasInputFocus.value ||
      _restoreInputFocusPending ||
      _retainInputLayoutKeyboardInset;
  double get messageListViewportObstructionBottom {
    if (shouldFollowKeyboardForInputDock) {
      return 0;
    }
    final platformObstruction = _platformViewportObstructionBottom.value;
    final keyboardInset = _messageListKeyboardInsetBottom.value;
    return keyboardInset > platformObstruction
        ? keyboardInset
        : platformObstruction;
  }

  bool get _shouldApplyPlatformViewportObstruction =>
      shouldFollowKeyboardForInputDock;
  double get _currentInputViewportInsetBottom {
    if (!shouldFollowKeyboardForInputDock) {
      return 0;
    }
    final double platformObstruction = _shouldApplyPlatformViewportObstruction
        ? _platformViewportObstructionBottom.value
        : 0.0;
    return _lastKeyboardInsetBottom > platformObstruction
        ? _lastKeyboardInsetBottom
        : platformObstruction;
  }

  double get platformViewportObstructionBottom =>
      _shouldApplyPlatformViewportObstruction
      ? _platformViewportObstructionBottom.value
      : 0;

  final Rx<MessageModel?> replyingToMessage = Rx<MessageModel?>(null);

  final RxString revokedMessageContent = ''.obs;

  void clearRevokedMessageContent() {
    revokedMessageContent.value = '';
  }

  void restoreRevokedMessage() {
    final content = revokedMessageContent.value;
    if (content.isEmpty) return;
    revokedMessageContent.value = '';
    inputController.text = content;
    inputController.selection = TextSelection.collapsed(offset: content.length);
  }

  // --- 排队任务编辑模式（event_hold + queue_edit，队列权威在 connector） ---

  /// 正在编辑的排队任务 event_id（空=非编辑态）。会话维度即本控制器 sessionId。
  final RxString editingQueueTaskEventId = ''.obs;
  String _queueEditStashedDraft = '';
  Timer? _queueEditHoldRenewTimer;

  /// 编辑期间 hold 续期间隔：TTL 缺省 10min，每 2min 重发 hold=true 重置 TTL，
  /// App 被杀/断网时由 connector 侧 TTL 到期自动放行兜底。
  static const Duration _queueEditHoldRenewInterval = Duration(minutes: 2);

  bool get isEditingQueueTask => editingQueueTaskEventId.value.isNotEmpty;

  /// 进入排队任务编辑模式：先发 event_hold(reason=editing) 并等回执 ok
  /// （杜绝"还没编辑完任务就被执行了"的竞态），成功后暂存当前输入框草稿、
  /// 将任务全文填入输入框（光标置尾）。返回是否成功进入；失败/超时已 toast。
  Future<bool> startQueueTaskEdit(EventLifecycleQueueItem item) async {
    if (editingQueueTaskEventId.value == item.eventId) {
      return true;
    }
    final result = await imService.sendEventHold(
      sessionId: sessionId,
      eventId: item.eventId,
      hold: true,
      reason: 'editing',
    );
    if (isClosed) {
      return false;
    }
    if (!result.ok) {
      // timedOut=老服务端不支持新命令；not_found=任务已开跑/已消失。均不进编辑模式。
      CustomToast.show(
        result.timedOut
            ? 'chat_queue_edit_unsupported'.tr
            : 'chat_queue_edit_failed_started'.tr,
      );
      return false;
    }
    if (isEditingQueueTask) {
      // 正在编辑别的任务：先无损退出（解除其 hold 并还原草稿）
      cancelQueueTaskEdit();
    }
    _queueEditStashedDraft = inputController.text;
    editingQueueTaskEventId.value = item.eventId;
    final content = item.fullContent;
    inputController.text = content;
    inputController.selection = TextSelection.collapsed(offset: content.length);
    _queueEditHoldRenewTimer?.cancel();
    _queueEditHoldRenewTimer = Timer.periodic(_queueEditHoldRenewInterval, (_) {
      final eventId = editingQueueTaskEventId.value;
      if (eventId.isEmpty) {
        return;
      }
      // 续期：重复 hold=true 即重置 TTL，结果无需等待
      imService.sendEventHold(
        sessionId: sessionId,
        eventId: eventId,
        hold: true,
        reason: 'editing',
      );
    });
    return true;
  }

  /// 退出编辑（提示条点 × / 会话销毁）：发 hold:false 尽力解除
  /// （发不出去也有 connector 侧 TTL 兜底），还原进入前的草稿。
  void cancelQueueTaskEdit() {
    final eventId = editingQueueTaskEventId.value;
    if (eventId.isEmpty) {
      return;
    }
    imService.sendEventHold(
      sessionId: sessionId,
      eventId: eventId,
      hold: false,
      reason: 'editing',
    );
    _exitQueueTaskEditMode(restoreDraft: true);
  }

  /// 编辑态下点发送：改发 queue_edit（不发新消息）。成功清输入框、退出
  /// 编辑并还原草稿；失败 toast 且文字保留输入框（退出编辑态，用户可选择
  /// 当新消息直接发送）。
  Future<void> submitQueueTaskEdit() async {
    final eventId = editingQueueTaskEventId.value;
    if (eventId.isEmpty) {
      return;
    }
    final text = inputController.text.trim();
    if (text.isEmpty) {
      return;
    }
    final result = await imService.sendQueueEdit(
      sessionId: sessionId,
      eventId: eventId,
      content: text,
    );
    if (isClosed || editingQueueTaskEventId.value != eventId) {
      return;
    }
    if (result.ok) {
      _exitQueueTaskEditMode(restoreDraft: true);
    } else {
      CustomToast.show('chat_queue_edit_failed_gone'.tr);
      _exitQueueTaskEditMode(restoreDraft: false);
    }
  }

  void _exitQueueTaskEditMode({required bool restoreDraft}) {
    editingQueueTaskEventId.value = '';
    _queueEditHoldRenewTimer?.cancel();
    _queueEditHoldRenewTimer = null;
    if (restoreDraft) {
      final draft = _queueEditStashedDraft;
      inputController.text = draft;
      inputController.selection = TextSelection.collapsed(offset: draft.length);
    }
    _queueEditStashedDraft = '';
  }

  void setReplyingToMessage(MessageModel msg) {
    _chatInputController.setReplyingToMessage(msg);
  }

  void cancelReply() {
    _chatInputController.cancelReply();
  }

  void toggleAttachmentMenu() {
    _chatAttachmentController.toggleAttachmentMenu();
  }

  void closeAttachmentMenu() {
    _chatAttachmentController.closeAttachmentMenu();
  }

  // --- Visible-to management ---

  void toggleVisibleToPicker() {
    showVisibleToPicker.value = !showVisibleToPicker.value;
  }

  void toggleVisibleToMember(String memberId) {
    final normalizedMemberId = memberId.trim();
    if (normalizedMemberId.isEmpty) {
      return;
    }
    final idx = visibleToUserIds.indexOf(normalizedMemberId);
    if (idx >= 0) {
      visibleToUserIds.removeAt(idx);
      visibleToSelectionFlagByMemberId(normalizedMemberId).value = false;
    } else {
      visibleToUserIds.add(normalizedMemberId);
      visibleToSelectionFlagByMemberId(normalizedMemberId).value = true;
    }
  }

  bool isMemberSelectedForVisibleTo(String memberId) {
    final normalizedMemberId = memberId.trim();
    if (normalizedMemberId.isEmpty) {
      return false;
    }
    return visibleToSelectionFlagByMemberId(normalizedMemberId).value;
  }

  RxBool visibleToSelectionFlagByMemberId(String memberId) {
    final normalizedMemberId = memberId.trim();
    if (normalizedMemberId.isEmpty) {
      return false.obs;
    }
    return _visibleToSelectionFlagsByMemberId.putIfAbsent(
      normalizedMemberId,
      () => visibleToUserIds.contains(normalizedMemberId).obs,
    );
  }

  void clearVisibleTo() {
    for (final memberId in visibleToUserIds) {
      visibleToSelectionFlagByMemberId(memberId).value = false;
    }
    visibleToUserIds.clear();
    showVisibleToPicker.value = false;
  }

  List<String> _resolveVisibleToDisplayNames(Iterable<String> ids) {
    final names = <String>[];
    for (final id in ids) {
      final member = _groupMembers.cast<Map<String, dynamic>?>().firstWhere(
        (m) => m?['member_id']?.toString() == id,
        orElse: () => null,
      );
      if (member != null) {
        names.add(resolveGroupMemberDisplayName(member));
      } else {
        names.add(id);
      }
    }
    return names;
  }

  String get visibleToDisplaySummary {
    if (visibleToUserIds.isEmpty) return '';
    return _joinVisibleToNames(_resolveVisibleToDisplayNames(visibleToUserIds));
  }

  String visibleToSummaryForMessage(
    List<String>? visibleToIds, {
    String? ownerId,
  }) {
    final orderedIds = <String>[];
    final seen = <String>{};

    void addId(String? id) {
      final normalized = id?.trim() ?? '';
      if (normalized.isEmpty || seen.contains(normalized)) return;
      seen.add(normalized);
      orderedIds.add(normalized);
    }

    addId(ownerId);
    if (visibleToIds != null) {
      for (final id in visibleToIds) {
        addId(id);
      }
    }

    if (orderedIds.isEmpty) return '';
    final names = _resolveVisibleToDisplayNames(orderedIds);
    if (names.isEmpty) return '';
    return _joinVisibleToNames(names);
  }

  String _joinVisibleToNames(List<String> names) {
    if (names.length <= 3) return names.join('、');
    return 'chat_visible_to_overflow'.trParams({
      'names': names.take(3).join('、'),
      'count': '${names.length}',
    });
  }

  final RxBool delegatePanelOpen = false.obs;
  final RxInt _delegateRoundsDraft = delegateDefaultRounds.obs;
  final RxBool _delegateRoundsDirty = false.obs;
  final RxInt _groupMemberCount = 0.obs;
  final RxList<Map<String, dynamic>> _groupMembers =
      <Map<String, dynamic>>[].obs;
  final RxBool _allMembersMuted = false.obs;
  final RxBool _allowMemberInvite = false.obs;
  final RxInt _memberInviteThreshold = 0.obs;
  final Set<String> _groupAgentMemberIds = <String>{};
  final RxString _chatType = 'private'.obs;
  final RxString _privatePeerNickname = ''.obs;
  final RxString _privatePeerUserId = ''.obs;
  final RxString _privatePeerAvatarUrl = ''.obs;
  final Map<String, RxInt> _senderProfileVersionsByKey = <String, RxInt>{};
  bool _isResolvingPrivatePeerId = false;
  final RxBool _isLoadingOlderHistory = false.obs;
  final RxBool _hasOlderHistory = true.obs;
  final RxBool _isForwardSelectionMode = false.obs;
  final RxSet<String> _selectedForwardMessageKeys = <String>{}.obs;
  final Map<String, RxBool> _forwardSelectionFlagsByKey = <String, RxBool>{};

  // @ Mention state
  final RxBool showMentionList = false.obs;
  final RxString mentionSearchQuery = ''.obs;
  final RxList<Map<String, dynamic>> filteredMentionList =
      <Map<String, dynamic>>[].obs;
  final RxInt mentionSelectedIndex = 0.obs;
  final RxString _groupToolbarTargetAgentId = ''.obs;
  final Set<String> _pendingExecApprovalIds = <String>{};
  int _mentionStartIndex = -1;
  final List<_PendingMention> _pendingMentions = <_PendingMention>[];
  final RxList<PinnedMention> _pinnedMentions = <PinnedMention>[].obs;
  String _lastInviteToGroupErrorMessage = '';

  // Visible-to (private message in group) state
  final RxList<String> visibleToUserIds = <String>[].obs;
  final RxBool showVisibleToPicker = false.obs;
  final RxBool _isVisitorSession = false.obs;
  final RxMap<String, dynamic> _visitorInfo = <String, dynamic>{}.obs;
  final Map<String, RxBool> _visibleToSelectionFlagsByMemberId =
      <String, RxBool>{};
  final Map<String, _FriendDisplayDigest> _friendDisplayDigestByUserId =
      <String, _FriendDisplayDigest>{};
  final Map<String, _FriendDisplayDigest>
  _activeHumanSenderDisplayDigestByUserId = <String, _FriendDisplayDigest>{};

  late String sessionId;
  late String chatTitle;
  String get chatType => _chatType.value;
  set chatType(String value) => _chatType.value = _normalizeChatType(value);
  bool get isGroupChat => chatType == 'group';

  /// 供外部（如通话按钮）获取私聊对端用户 ID
  String resolvePrivatePeerUserId() => _resolvePrivatePeerUserId();

  /// 供外部（如通话按钮）获取私聊对端昵称
  String resolvePrivatePeerName() => _resolvePrivatePeerNameFromSession();
  bool get isAgentPrivateChat {
    if (chatType != 'private') return false;
    final session = imService.findSessionById(sessionId);
    return session?.peerType == 2;
  }

  bool get isVisitorSession => _isVisitorSession.value;
  Map<String, dynamic> get visitorInfo =>
      Map<String, dynamic>.from(_visitorInfo);
  String get visitorSiteName =>
      (visitorInfo['site_name'] ?? '').toString().trim();
  String get visitorName =>
      (visitorInfo['visitor_name'] ?? '').toString().trim();
  String get visitorEmail =>
      (visitorInfo['visitor_email'] ?? '').toString().trim();
  String get visitorLastPageUrl =>
      (visitorInfo['last_page_url'] ?? '').toString().trim();

  String get groupToolbarTargetAgentId => _groupToolbarTargetAgentId.value;

  bool get canReportGroup {
    return _chatGroupController.canReportGroup;
  }

  bool get canForwardConversationCard {
    return sessionId.trim().isNotEmpty && displayChatTitle.trim().isNotEmpty;
  }

  void bindFlutterView(FlutterView view) {
    _pageStateController.bindFlutterView(view);
  }

  String get myDisplayName {
    return _chatIdentityController.myDisplayName;
  }

  String get peerDisplayName {
    return _chatIdentityController.peerDisplayName;
  }

  String get privatePeerNickname => _chatIdentityController.privatePeerNickname;

  String get privatePeerAvatarUrl {
    return _chatIdentityController.privatePeerAvatarUrl;
  }

  String get chatSubtitle {
    return _chatIdentityController.chatSubtitle;
  }

  bool get isChatSubtitleOnline {
    return _chatIdentityController.isChatSubtitleOnline;
  }

  bool get isChatSubtitleOffline {
    return _chatIdentityController.isChatSubtitleOffline;
  }

  String get displayChatTitle {
    return _chatIdentityController.displayChatTitle;
  }

  String get headerAvatarTitle {
    return _chatIdentityController.headerAvatarTitle;
  }

  String get headerAvatarColorSeed {
    return _chatIdentityController.headerAvatarColorSeed;
  }

  bool get shouldShowHeaderAvatar {
    return _chatIdentityController.shouldShowHeaderAvatar;
  }

  String get delegatedAgentId {
    return _chatDelegateController.delegatedAgentId;
  }

  String get delegatedAgentName {
    return _chatDelegateController.delegatedAgentName;
  }

  String get voiceDelegatedAgentId =>
      imService.voiceDelegateStates[sessionId] ?? '';

  String get voiceDelegatedAgentName {
    final id = voiceDelegatedAgentId;
    if (id.isEmpty) return '';
    final n = _resolveKnownAgentName(id);
    return n.isNotEmpty ? n : 'Agent $id';
  }

  String get _userVoiceDefaultAgentId => Get.isRegistered<UserSettingsService>()
      ? Get.find<UserSettingsService>().voiceAutoDelegateAgentId.value.trim()
      : '';

  /// 本会话是否为会话级语音托管（vs 仅用户级默认）
  bool get voiceDelegateIsSessionLevel => voiceDelegatedAgentId.isNotEmpty;

  /// 本会话有效的语音托管 agent：会话级优先，否则用户级默认（群聊不适用）
  String get effectiveVoiceDelegateAgentId {
    final sid = voiceDelegatedAgentId;
    if (sid.isNotEmpty) return sid;
    if (isGroupChat) return '';
    return _userVoiceDefaultAgentId;
  }

  String voiceAgentName(String id) {
    if (id.isEmpty) return '';
    final n = _resolveKnownAgentName(id);
    return n.isNotEmpty ? n : 'Agent $id';
  }

  int get delegatedMaxConsecutiveReplies {
    return _chatDelegateController.delegatedMaxConsecutiveReplies;
  }

  int get delegateRoundsDraft => _chatDelegateController.delegateRoundsDraft;
  bool get delegateRoundsDirty => _chatDelegateController.delegateRoundsDirty;
  int get groupMemberCount => _chatGroupController.groupMemberCount;
  List<Map<String, dynamic>> get groupMembers =>
      _chatGroupController.groupMembers;
  bool get allMembersMuted => _chatGroupController.allMembersMuted;
  bool get allowMemberInvite => _chatGroupController.allowMemberInvite;
  int get memberInviteThreshold => _chatGroupController.memberInviteThreshold;
  String get lastInviteToGroupErrorMessage =>
      _chatGroupController.lastInviteToGroupErrorMessage;
  int senderProfileVersionFor({
    required String senderId,
    required int senderType,
    required bool isMine,
  }) {
    final key = _senderProfileVersionKey(
      senderId: senderId,
      senderType: senderType,
      isMine: isMine,
    );
    if (key.isEmpty) {
      return 0;
    }
    return _senderProfileVersionsByKey.putIfAbsent(key, () => 0.obs).value;
  }

  String get myGroupNickname => _chatIdentityController.myGroupNickname;

  List<SessionAvatarMember> get groupAvatarMembers {
    return _chatIdentityController.groupAvatarMembers;
  }

  List<SessionActivityModel> get currentSessionActivities =>
      _chatDelegateController.currentSessionActivities;
  Map<String, dynamic>? get currentAgentOutputState =>
      _chatDelegateController.currentAgentOutputState;
  bool get hasActiveAgentOutput => _chatDelegateController.hasActiveAgentOutput;
  bool get canStopCurrentAgentOutput =>
      _chatDelegateController.canStopCurrentAgentOutput;
  bool get isCurrentAgentOutputStopping =>
      _chatDelegateController.isCurrentAgentOutputStopping;
  bool get hasRunningExecutionForSession =>
      _chatDelegateController.hasRunningExecutionForSession;
  String get currentAgentOutputRunId =>
      _chatDelegateController.currentAgentOutputRunId;
  String get currentAgentOutputStreamMsgId =>
      _chatDelegateController.currentAgentOutputStreamMsgId;
  bool get hasSessionActivity => _chatDelegateController.hasSessionActivity;
  bool get hasChatStatusIndicator => _chatStatusController.current != null;

  /// 首屏历史已加载完毕后，空白页才展示快捷绑定目录。
  bool get isInitialHistoryReady => imService.isInitialHistoryReady;
  bool get isLoadingOlderHistory => _isLoadingOlderHistory.value;
  bool get hasOlderHistory => _hasOlderHistory.value;
  bool get isForwardSelectionMode => _isForwardSelectionMode.value;
  int get selectedForwardMessageCount => _selectedForwardMessageKeys.length;
  RxBool forwardSelectionFlagByKey(String selectionKey) {
    final normalized = selectionKey.trim();
    return _forwardSelectionFlagsByKey.putIfAbsent(normalized, () => false.obs);
  }

  void setForwardSelectionState(String selectionKey, bool selected) {
    final flag = forwardSelectionFlagByKey(selectionKey);
    if (flag.value == selected) {
      return;
    }
    flag.value = selected;
  }

  void clearAllForwardSelectionFlags() {
    for (final flag in _forwardSelectionFlagsByKey.values) {
      if (flag.value) {
        flag.value = false;
      }
    }
  }

  bool get isInputComposing =>
      _hasActiveInputComposition(inputController.value);
  String get sessionActivityLabel {
    return _chatDelegateController.sessionActivityLabel;
  }

  String get agentOutputLabel {
    return _chatDelegateController.agentOutputLabel;
  }

  String get chatStatusLabel => _chatStatusController.current?.label ?? '';

  int get myGroupRole => _chatGroupController.myGroupRole;
  bool get isGroupOwner => _chatGroupController.isGroupOwner;
  bool get canDissolveGroup => _chatGroupController.canDissolveGroup;
  bool get canLeaveGroup => _chatGroupController.canLeaveGroup;
  bool get canInviteGroupMembers => _chatGroupController.canInviteGroupMembers;
  bool get canManageGroupMembers => _chatGroupController.canManageGroupMembers;
  bool get canManageGroupSpeaking =>
      _chatGroupController.canManageGroupSpeaking;
  bool get canCurrentUserSpeak => _chatGroupController.canCurrentUserSpeak;
  String get currentUserSpeakingBlockedReason =>
      _chatGroupController.currentUserSpeakingBlockedReason;
  String get groupMemberInviteRestrictionReason =>
      _chatGroupController.groupMemberInviteRestrictionReason;

  String? agentDeliveryLabelForMessage(
    MessageModel msg, {
    bool hasPeerReplyAfter = false,
  }) {
    if (!_shouldShowAgentDeliveryForMessage(
      msg,
      hasPeerReplyAfter: hasPeerReplyAfter,
    )) {
      return null;
    }
    return imService.describeAgentDeliveryStatus(msg.agentDeliveryStatus);
  }

  bool isAgentDeliveryErrorForMessage(
    MessageModel msg, {
    bool hasPeerReplyAfter = false,
  }) {
    if (!_shouldShowAgentDeliveryForMessage(
      msg,
      hasPeerReplyAfter: hasPeerReplyAfter,
    )) {
      return false;
    }
    return imService.isAgentDeliveryStatusError(msg.agentDeliveryStatus);
  }

  bool _shouldShowAgentDeliveryForMessage(
    MessageModel msg, {
    bool hasPeerReplyAfter = false,
  }) {
    if (msg.senderType != 1) {
      return false;
    }
    final status = msg.agentDeliveryStatus?.trim() ?? '';
    if (status.isEmpty) {
      return false;
    }
    if (_shouldSuppressTransientAgentDeliveryError(msg, status)) {
      return false;
    }
    // 私聊中，如果对方已回复，则不再显示 received / failed / timeout 状态
    if (!isGroupChat) {
      if (hasPeerReplyAfter &&
          (status == 'received' || status == 'failed' || status == 'timeout')) {
        return false;
      }
      return true;
    }
    return _hasMentionedAgentInMessage(msg);
  }

  bool _shouldSuppressTransientAgentDeliveryError(
    MessageModel msg,
    String status,
  ) {
    // channel_unavailable is a permanent offline error, not transient — always show.
    if (status == 'failed:channel_unavailable') {
      return false;
    }
    if (status != 'failed' && status != 'timeout') {
      return false;
    }
    if (_hasActiveAgentOutputForMessage(msg)) {
      return true;
    }
    final createdAt = msg.createdAt;
    if (createdAt <= 0) {
      return false;
    }
    final elapsed = DateTime.now().millisecondsSinceEpoch - createdAt;
    return elapsed >= 0 &&
        elapsed < _agentDeliveryErrorDisplayGrace.inMilliseconds;
  }

  bool _hasActiveAgentOutputForMessage(MessageModel msg) {
    final sessionId = msg.sessionId.trim();
    final msgId = msg.msgId.trim();
    if (sessionId.isEmpty || msgId.isEmpty) {
      return false;
    }
    final state = imService.agentOutputStateFor(sessionId);
    if (state == null) {
      return false;
    }
    final triggerMsgId = state['trigger_msg_id']?.toString().trim() ?? '';
    if (triggerMsgId.isNotEmpty && triggerMsgId != msgId) {
      return false;
    }
    switch (state['state']?.toString().trim() ?? '') {
      case 'queued':
      case 'received':
      case 'streaming':
      case 'stopping':
        return true;
      default:
        return false;
    }
  }

  bool _hasMentionedAgentInMessage(MessageModel msg) {
    final rawMentions = msg.extra['mention_user_ids'];
    if (rawMentions is! List || _groupAgentMemberIds.isEmpty) {
      return false;
    }
    for (final raw in rawMentions) {
      final mentionId = raw?.toString().trim() ?? '';
      if (mentionId.isEmpty) {
        continue;
      }
      if (_groupAgentMemberIds.contains(mentionId)) {
        return true;
      }
    }
    return false;
  }

  String resolveSenderName({
    required String senderId,
    required bool isMine,
    required bool isGroup,
    int senderType = 1,
  }) {
    return _chatIdentityController.resolveSenderName(
      senderId: senderId,
      isMine: isMine,
      isGroup: isGroup,
      senderType: senderType,
    );
  }

  String resolveSenderAvatarUrl({
    required String senderId,
    required bool isMine,
    required bool isGroup,
    int senderType = 1,
  }) {
    return _chatIdentityController.resolveSenderAvatarUrl(
      senderId: senderId,
      isMine: isMine,
      isGroup: isGroup,
      senderType: senderType,
    );
  }

  String formatMessageContentForDisplay(String rawContent) {
    // 目录绑定指令消息（grix://open/session）在气泡/引用里显示友好文案。
    final bindDirectory = ChatBindDirectoryMessage.friendlyText(rawContent);
    if (bindDirectory.isNotEmpty) {
      return bindDirectory;
    }
    return _chatIdentityController.formatMessageContentForDisplay(rawContent);
  }

  bool isInternalDirectiveMessage(String rawContent) {
    return ChatMessageCardCodec.isInternalDirectiveMessage(rawContent);
  }

  bool tryLockExecApprovalAction(ChatExecApprovalCardData card) {
    final approvalId = card.approvalId.trim();
    if (approvalId.isEmpty) {
      return false;
    }
    if (_pendingExecApprovalIds.contains(approvalId)) {
      CustomToast.show(
        'chat_message_card_exec_approval_already_submitted'.tr,
        isError: false,
      );
      return false;
    }
    _pendingExecApprovalIds.add(approvalId);
    return true;
  }

  void rollbackExecApprovalAction(ChatExecApprovalCardData card) {
    final approvalId = card.approvalId.trim();
    if (approvalId.isEmpty) {
      return;
    }
    _pendingExecApprovalIds.remove(approvalId);
  }

  bool isExecApprovalActionPending(String approvalId) {
    final normalizedApprovalId = approvalId.trim();
    if (normalizedApprovalId.isEmpty) {
      return false;
    }
    return _pendingExecApprovalIds.contains(normalizedApprovalId);
  }

  void syncExecApprovalActionLocks() {
    if (_pendingExecApprovalIds.isEmpty) {
      return;
    }
    final resolvedApprovalIds = <String>{};
    for (final message in imService.currentMessages) {
      final card = ChatMessageCardCodec.decodeFromMessage(
        content: message.content,
      );
      if (card is ChatExecApprovalCardData) {
        if (!card.isResolved) {
          continue;
        }
        final approvalId = card.approvalId.trim();
        if (approvalId.isNotEmpty) {
          resolvedApprovalIds.add(approvalId);
        }
        continue;
      }
      if (card is! ChatExecStatusCardData) {
        continue;
      }
      if (!_isExecApprovalResolvedStatus(card.displayStatus)) {
        continue;
      }
      final approvalId = card.approvalId.trim();
      if (approvalId.isNotEmpty) {
        resolvedApprovalIds.add(approvalId);
      }
    }
    _pendingExecApprovalIds.removeAll(resolvedApprovalIds);
  }

  bool _isExecApprovalResolvedStatus(String status) {
    return status == 'approval-expired' ||
        status == 'approval-forwarded' ||
        status == 'approval-unavailable' ||
        status == 'resolved-allow-once' ||
        status == 'resolved-allow-always' ||
        status == 'resolved-allow-rule' ||
        status == 'resolved-deny';
  }

  String? _parseUserId(String raw) {
    final normalized = raw.trim();
    if (normalized.isEmpty) return null;
    return normalized;
  }

  Set<String> _consumeFriendListChangedUserIds() {
    final fs = _friendService;
    if (fs == null) {
      _friendDisplayDigestByUserId.clear();
      return <String>{};
    }

    final nextDigestByUserId = <String, _FriendDisplayDigest>{};
    final changedUserIds = <String>{};
    for (final friend in fs.friendList) {
      final userId = friend.userId.trim();
      if (userId.isEmpty) {
        continue;
      }
      final nextDigest = _FriendDisplayDigest.fromFriend(friend);
      nextDigestByUserId[userId] = nextDigest;
      final previousDigest = _friendDisplayDigestByUserId[userId];
      if (previousDigest != nextDigest) {
        changedUserIds.add(userId);
      }
    }

    for (final previousUserId in _friendDisplayDigestByUserId.keys) {
      if (!nextDigestByUserId.containsKey(previousUserId)) {
        changedUserIds.add(previousUserId);
      }
    }

    _friendDisplayDigestByUserId
      ..clear()
      ..addAll(nextDigestByUserId);
    return changedUserIds;
  }

  Set<String> _activeHumanSenderIdsFromCurrentMessages() {
    final activeHumanSenderIds = <String>{};
    for (final message in imService.currentMessages) {
      if (message.senderType != 1) {
        continue;
      }
      final senderId = message.senderId.trim();
      if (senderId.isEmpty) {
        continue;
      }
      activeHumanSenderIds.add(senderId);
    }
    return activeHumanSenderIds;
  }

  _FriendDisplayDigest _friendDisplayDigestFromCache(String userId) {
    final fs = _friendService;
    if (fs == null) {
      return const _FriendDisplayDigest(
        nickname: '',
        username: '',
        remarkName: '',
        avatarUrl: '',
      );
    }
    return _FriendDisplayDigest(
      nickname: fs.getUserNickname(userId)?.trim() ?? '',
      username: fs.getUserUsername(userId)?.trim() ?? '',
      remarkName: fs.getFriendRemarkName(userId)?.trim() ?? '',
      avatarUrl: fs.getUserAvatarUrl(userId)?.trim() ?? '',
    );
  }

  Set<String> _consumeActiveHumanSenderProfileChangedUserIds() {
    final activeUserIds = _activeHumanSenderIdsFromCurrentMessages();
    if (activeUserIds.isEmpty) {
      _activeHumanSenderDisplayDigestByUserId.clear();
      return <String>{};
    }

    final changedUserIds = <String>{};
    final nextDigestByUserId = <String, _FriendDisplayDigest>{};
    for (final userId in activeUserIds) {
      final nextDigest = _friendDisplayDigestFromCache(userId);
      nextDigestByUserId[userId] = nextDigest;
      final previousDigest = _activeHumanSenderDisplayDigestByUserId[userId];
      if (previousDigest != nextDigest) {
        changedUserIds.add(userId);
      }
    }

    _activeHumanSenderDisplayDigestByUserId
      ..clear()
      ..addAll(nextDigestByUserId);
    return changedUserIds;
  }

  void _refreshGroupMemberDisplayState({
    bool refreshMembers = false,
    Iterable<String>? changedHumanSenderIds,
  }) {
    if (changedHumanSenderIds == null) {
      _refreshActiveSenderProfileVersions();
    } else {
      _refreshHumanSenderProfileVersions(changedHumanSenderIds);
    }
    _rebuildGroupAgentMemberIds();
    if (_groupMembers.isEmpty) {
      return;
    }
    if (refreshMembers) {
      _groupMembers.refresh();
    }
    _pruneVisibleToSelectionFlags();
    _rebuildMemberDisplayNameCache();
    _refreshMentionSuggestionState();
    _syncGroupToolbarTargetAgent();
  }

  void _rebuildGroupAgentMemberIds() {
    _groupAgentMemberIds.clear();
    for (final member in _groupMembers) {
      if (_parseInt(member['member_type']) != 2) {
        continue;
      }
      final memberId = (member['member_id'] ?? '').toString().trim();
      if (memberId.isEmpty) {
        continue;
      }
      _groupAgentMemberIds.add(memberId);
    }
  }

  void _ensureProfileLoaded(String userId) {
    if (_friendService == null) return;
    if (_inflightProfileLoads.contains(userId)) return;

    _inflightProfileLoads.add(userId);
    _friendService.fetchUserProfile(userId).whenComplete(() {
      _inflightProfileLoads.remove(userId);
      final changedActiveSenderIds =
          _consumeActiveHumanSenderProfileChangedUserIds();
      _refreshGroupMemberDisplayState(
        refreshMembers: true,
        changedHumanSenderIds: changedActiveSenderIds.contains(userId)
            ? [userId]
            : const <String>[],
      );
      _refreshPrivatePeerNickname(fetchIfMissing: false);
      _refreshPrivatePeerAvatar(fetchIfMissing: false);
    });
  }

  String _senderProfileVersionKey({
    required String senderId,
    required int senderType,
    required bool isMine,
  }) {
    if (isMine) {
      return 'self';
    }
    final normalizedSenderId = senderId.trim();
    if (normalizedSenderId.isEmpty) {
      return '';
    }
    return '$senderType:$normalizedSenderId';
  }

  void _bumpSenderProfileVersionKey(String key) {
    final normalized = key.trim();
    if (normalized.isEmpty) {
      return;
    }
    final version = _senderProfileVersionsByKey.putIfAbsent(
      normalized,
      () => 0.obs,
    );
    version.value++;
  }

  void _refreshHumanSenderProfileVersions(Iterable<String> senderIds) {
    final myUserId = authService.userId?.trim() ?? '';
    final activeHumanSenderIds = _activeHumanSenderIdsFromCurrentMessages();
    for (final rawId in senderIds) {
      final senderId = rawId.trim();
      if (senderId.isEmpty) {
        continue;
      }
      if (!activeHumanSenderIds.contains(senderId)) {
        continue;
      }
      _bumpSenderProfileVersionKey(
        _senderProfileVersionKey(
          senderId: senderId,
          senderType: 1,
          isMine: senderId == myUserId,
        ),
      );
    }
  }

  void _refreshActiveSenderProfileVersions() {
    final myUserId = authService.userId?.trim() ?? '';
    final activeKeys = <String>{};
    for (final message in imService.currentMessages) {
      activeKeys.add(
        _senderProfileVersionKey(
          senderId: message.senderId,
          senderType: message.senderType,
          isMine: ChatMessageOwnerClassifier.isMineMessage(
            message,
            currentUserId: myUserId,
          ),
        ),
      );
    }
    for (final key in activeKeys) {
      _bumpSenderProfileVersionKey(key);
    }
  }

  void _prefetchCurrentMessageProfiles() {
    if (_friendService == null) {
      return;
    }

    final myUserId = authService.userId?.trim() ?? '';
    final candidateUserIds = <String>{};
    for (final message in imService.currentMessages) {
      for (final mentionedUserId
          in ChatNumericMentionResolver.extractNumericMentionUserIds(
            message.content,
          )) {
        if (mentionedUserId == myUserId ||
            _findGroupMember(mentionedUserId, memberType: 2) != null) {
          continue;
        }
        candidateUserIds.add(mentionedUserId);
      }

      if (message.senderType != 1) {
        continue;
      }
      final senderId = message.senderId.trim();
      if (senderId.isEmpty || senderId == 'me' || senderId == myUserId) {
        continue;
      }
      candidateUserIds.add(senderId);
    }

    if (!isGroupChat) {
      final peerId = _resolvePrivatePeerUserId();
      if (peerId.isNotEmpty && peerId != myUserId) {
        candidateUserIds.add(peerId);
      }
    }

    for (final userId in candidateUserIds) {
      _ensureProfileLoaded(userId);
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.paused ||
        state == AppLifecycleState.inactive) {
      persistDraftImmediately();
      _chatInputController.handleAppPausedForIme(state);
    }
    // Stop STT/TTS when leaving the foreground. Use paused/hidden/detached
    // only — inactive also fires for iOS permission sheets, and the press
    // gate already prevents recording after a first-auth release.
    if (state == AppLifecycleState.paused ||
        state == AppLifecycleState.hidden ||
        state == AppLifecycleState.detached) {
      _chatVoiceCommandController.deactivateForExternalAction();
    }
    if (state == AppLifecycleState.resumed) {
      imService.refreshAgentToolbar(sessionId);
      _chatInputController.handleAppResumedForIme();
    }
  }

  @override
  void onInit() {
    super.onInit();
    _pageStateController.onInit();
  }

  @override
  void onReady() {
    super.onReady();
    if (isClosed) return;
    _pageStateController.onReady();
    _fileInterceptor.setDragOverCallback((isOver) {
      isFileDragOver.value = isOver;
    });
    _fileInterceptor.register((bytes, fileName, contentType) {
      return stageFileFromBytes(
        bytes: bytes,
        fileName: fileName,
        contentType: contentType,
      );
    });
    _chatVoiceCommandController.bind();
  }

  @override
  void onClose() {
    // 仍处于排队任务编辑态时尽力发一次 hold:false 解除（TTL 兜底）
    cancelQueueTaskEdit();
    clearAllForwardSelectionFlags();
    imService.setGroupToolbarTargetAgent(sessionId, agentId: '');
    _friendDisplayDigestByUserId.clear();
    _activeHumanSenderDisplayDigestByUserId.clear();
    _fileInterceptor.unregister();
    _chatVoiceCommandController.dispose();
    _pageStateController.onClose();
    super.onClose();
  }

  void closeChatRoute() {
    _pageStateController.closeChatRoute();
  }

  void persistDraftImmediately() {
    _chatInputController.persistDraftImmediately();
  }

  void scrollToBottom({
    bool animated = false,
    bool force = false,
    bool resumeAutoFollow = false,
  }) {
    _pageStateController.scrollToBottom(
      animated: animated,
      force: force,
      resumeAutoFollow: resumeAutoFollow,
    );
  }

  void scrollToLoadedTop({bool animated = true}) {
    _pageStateController.scrollToLoadedTop(animated: animated);
  }

  void onScrollMetricsChanged(ScrollMetrics metrics) {
    _pageStateController.onScrollMetricsChanged(metrics);
  }

  void onMessageViewportLayoutChanged() {
    _pageStateController.onMessageViewportLayoutChanged();
  }

  @override
  void didChangeMetrics() {
    _pageStateController.didChangeMetrics();
  }

  void onBottomDockLayoutChanged() {
    _pageStateController.onBottomDockLayoutChanged();
  }

  void _handleInputFocusChanged() {
    _chatInputController.handleInputFocusChanged();
  }

  void insertInputLineBreak() {
    _chatInputController.insertInputLineBreak();
  }

  void insertText(String text) {
    _chatInputController.replaceInputSelection(text);
  }

  void dismissInputInteraction() {
    _chatInputController.dismissInputInteraction();
  }

  ChatManagedInputBinding createMessageCardManagedInputBinding(
    String messageId,
  ) {
    final inputId = ChatManagedInputId(
      kind: ChatManagedInputKind.messageCard,
      instanceId: messageId,
    );
    return ChatManagedInputBinding(
      inputId: inputId,
      policy: ChatManagedInputPolicy.messageCard,
      registerTarget: (targetKey) => _registerManagedInput(
        inputId: inputId,
        policy: ChatManagedInputPolicy.messageCard,
        targetKey: targetKey,
      ),
      unregister: () => _unregisterManagedInput(inputId),
      updateTargetKey: (targetKey) =>
          _managedInputRegistry.updateTargetKey(inputId, targetKey),
      reportFocusChange: (hasFocus) =>
          onManagedInputFocusChanged(inputId, hasFocus),
    );
  }

  void onManagedInputFocusChanged(ChatManagedInputId inputId, bool hasFocus) {
    final policy =
        _managedInputRegistry.policyOf(inputId) ??
        (inputId == _composerManagedInputId
            ? ChatManagedInputPolicy.composer
            : ChatManagedInputPolicy.messageCard);

    if (!hasFocus) {
      _deactivateManagedInput(inputId);
      _chatInputController.syncInputLayoutKeyboardInset();
      _pageStateController.onBottomDockLayoutChanged();
      return;
    }

    _chatInputController.cancelPendingInputFocusRetention();
    closeAttachmentMenu();
    if (inputId != _composerManagedInputId && focusNode.hasFocus) {
      focusNode.unfocus();
    }
    _activateManagedInput(inputId, policy);
    _chatInputController.syncInputLayoutKeyboardInset();
    _pageStateController.onBottomDockLayoutChanged();
  }

  bool dispatchCurrentInputMessage() {
    if (_chatVoiceCommandController.isCapturingSpeech) {
      unawaited(_flushVoiceDraftThenDispatch());
      return false;
    }
    // 编辑排队任务模式下，发送动作改发 queue_edit（不发新消息）
    if (isEditingQueueTask) {
      if (inputController.text.trim().isEmpty) {
        return false;
      }
      submitQueueTaskEdit();
      return true;
    }
    return _chatInputController.dispatchCurrentInputMessage();
  }

  bool _voiceFlushInFlight = false;

  Future<void> _flushVoiceDraftThenDispatch() async {
    if (_voiceFlushInFlight) return;
    _voiceFlushInFlight = true;
    try {
      await _chatVoiceCommandController.stopListeningAndSubmit();
      if (isClosed) return;
      // A nested stop (send + tap-outside) used to return before teardown
      // finished. Re-dispatching while still capturing spun the event loop
      // and froze the UI. Join the in-flight stop, then send only once idle.
      if (_chatVoiceCommandController.isCapturingSpeech) return;
      dispatchCurrentInputMessage();
    } finally {
      _voiceFlushInFlight = false;
    }
  }

  void sendMessage() {
    dispatchCurrentInputMessage();
  }

  bool get supportsVoiceCommand => isVoiceCommandEntrySupported(
    featureEnabled:
        Get.isRegistered<FeatureFlagService>() &&
        Get.find<FeatureFlagService>().isEnabled('voice_command'),
    platformSupported: _chatVoiceCommandController.isSupported,
  );
  RxBool get isVoiceCommandListening => _chatVoiceCommandController.isListening;
  RxBool get isVoiceCommandAwaitingResponse =>
      _chatVoiceCommandController.isAwaitingResponse;
  RxString get voiceCommandTranscriptPreview =>
      _chatVoiceCommandController.transcriptPreview;

  Future<void> startVoiceCommand() =>
      _chatVoiceCommandController.startListening();

  Future<void> stopVoiceCommandAndSubmit() =>
      _chatVoiceCommandController.stopListeningAndSubmit();

  Future<void> cancelVoiceCommand() =>
      _chatVoiceCommandController.cancelListening();

  void deactivateVoiceCommandForRouteChange() {
    _chatVoiceCommandController.deactivateForExternalAction();
  }

  void suppressNextInputSubmit() {
    _chatInputController.suppressNextInputSubmit();
  }

  void submitMessageFromHardwareEnter() {
    _chatInputController.submitMessageFromHardwareEnter();
  }

  void submitMessageFromInputAction() {
    _chatInputController.submitMessageFromInputAction();
  }

  bool canForwardMessage(MessageModel message) {
    return _chatForwardController.canForwardMessage(message);
  }

  bool _canRevokePrivateAgentMessage(MessageModel message) {
    if (isGroupChat) {
      return false;
    }
    if (message.senderType != 2) {
      return false;
    }

    final currentSession = imService.findSessionById(sessionId);
    if (currentSession == null || currentSession.type != 'private') {
      return false;
    }
    if (currentSession.peerType != 2) {
      return false;
    }

    final peerAgentId = currentSession.peerId.trim();
    if (peerAgentId.isEmpty) {
      return false;
    }
    return peerAgentId == message.senderId.trim();
  }

  bool canRevokeMessage({
    required MessageModel message,
    required bool isMine,
    required bool isSending,
    required bool isFailed,
    required bool isStreaming,
  }) {
    if (isSending || isFailed || isStreaming) {
      return false;
    }
    if (isMine) {
      return true;
    }
    if (_canRevokePrivateAgentMessage(message)) {
      return true;
    }
    return isGroupChat && canManageGroupMembers;
  }

  bool isForwardMessageSelected(MessageModel message) {
    return _chatForwardController.isForwardMessageSelected(message);
  }

  bool isForwardMessageSelectedByKey(String selectionKey) {
    return forwardSelectionFlagByKey(selectionKey).value;
  }

  void beginForwardSelection(MessageModel message) {
    _chatForwardController.beginForwardSelection(message);
  }

  void toggleForwardMessageSelection(MessageModel message) {
    _chatForwardController.toggleForwardMessageSelection(message);
  }

  void exitForwardSelectionMode() {
    _chatForwardController.exitForwardSelectionMode();
  }

  List<MessageModel> collectSelectedForwardMessages() {
    return _chatForwardController.collectSelectedForwardMessages();
  }

  List<ChatForwardTargetOption> buildForwardTargetOptions() {
    return _chatForwardController.buildForwardTargetOptions();
  }

  Future<int> forwardConversationCard({
    required String targetSessionId,
    String accompanyingMessage = '',
  }) async {
    return _chatForwardController.forwardConversationCard(
      targetSessionId: targetSessionId,
      accompanyingMessage: accompanyingMessage,
    );
  }

  Future<int> forwardMessages({
    required List<MessageModel> messages,
    required String targetSessionId,
    required ChatForwardDispatchMode mode,
  }) async {
    return _chatForwardController.forwardMessages(
      messages: messages,
      targetSessionId: targetSessionId,
      mode: mode,
    );
  }

  /// 转发目标选择 sheet 上 "+" 入口使用：把待转发消息构建成
  /// "发给 Agent"对话框的预填文本（内容与 merged 转发一致）。
  String buildForwardAgentDraft(List<MessageModel> messages) {
    return _chatForwardController.buildAgentForwardDraft(messages);
  }

  Future<void> pickAndSendImage() {
    return _chatAttachmentController.pickAndSendImage();
  }

  Future<void> pickAndSendImageFromCamera() {
    return _chatAttachmentController.pickAndSendImageFromCamera();
  }

  Future<void> pickAndSendVideo() {
    return _chatAttachmentController.pickAndSendVideo();
  }

  Future<void> pickAndSendVideoFromCamera() {
    return _chatAttachmentController.pickAndSendVideoFromCamera();
  }

  Future<void> pickAndSendFile() async {
    await _chatAttachmentController.pickAndSendFile();
  }

  Future<void> stageFileFromBytes({
    required Uint8List bytes,
    required String fileName,
    required String contentType,
  }) async {
    await _chatAttachmentController.stageFileFromBytes(
      bytes: bytes,
      fileName: fileName,
      contentType: contentType,
    );
  }

  /// 桌面端 & iOS 粘贴处理：优先尝试粘贴图片，如果剪贴板没有图片则手动粘贴文本。
  /// iOS 端通过 NativeClipboardService 读取缓存剪贴板，减少系统权限弹窗。
  Future<void> handleDesktopPaste() async {
    // 附件粘贴失败只提示、不中断：剪贴板里的文本仍应能正常粘进输入框。
    bool hasImage = false;
    try {
      hasImage = await _fileInterceptor.handlePasteIntent();
    } catch (e) {
      debugPrint('粘贴剪贴板附件失败: $e');
      CustomToast.show('chat_attachment_paste_failed'.tr, isError: true);
    }
    if (hasImage) return;

    // 剪贴板没有图片，手动粘贴文本到输入框
    final String? text;
    if (Platform.isIOS) {
      text = await NativeClipboardService.getText();
    } else {
      final clipboardData = await Clipboard.getData(Clipboard.kTextPlain);
      text = clipboardData?.text;
    }
    if (text == null || text.isEmpty) return;

    final currentValue = inputController.value;
    final selection = currentValue.selection;
    if (!selection.isValid) {
      // 没有有效选区时追加到末尾
      inputController.text = currentValue.text + text;
      inputController.selection = TextSelection.collapsed(
        offset: inputController.text.length,
      );
    } else {
      final newText = currentValue.text.replaceRange(
        selection.start,
        selection.end,
        text,
      );
      inputController.value = TextEditingValue(
        text: newText,
        selection: TextSelection.collapsed(
          offset: selection.start + text.length,
        ),
      );
    }
  }

  Future<void> pickRemoteFiles() async {
    await _chatAttachmentController.pickRemoteFiles();
  }

  void removeStagedAttachment(int index) {
    _chatAttachmentController.removeStagedAttachment(index);
  }

  void clearStagedAttachments() {
    _chatAttachmentController.clearStagedAttachments();
  }

  Future<void> editStagedImage(int index) {
    return _chatAttachmentController.editStagedImage(index);
  }

  @visibleForTesting
  Future<void> uploadPreparedAttachmentsForTest(
    List<ChatPreparedAttachmentUpload> uploads,
  ) {
    return _chatAttachmentController.uploadAttachmentsForTest(uploads);
  }

  void _onScroll() {
    _pageStateController.onScroll();
  }

  bool _hasActiveInputComposition(TextEditingValue value) {
    return _chatInputController.hasActiveInputComposition(value);
  }

  void clearPendingInputSubmitSuppressionForNewKeyPress() {
    _chatInputController.clearPendingInputSubmitSuppressionForNewKeyPress();
  }

  void _onInputTextChanged() {
    _chatInputController.onInputTextChanged();
  }

  void _rebuildMemberDisplayNameCache() {
    _chatMentionController.rebuildMemberDisplayNameCache();
  }

  void _refreshMentionSuggestionState() {
    _chatMentionController.refreshSuggestionState();
  }

  void _syncGroupToolbarTargetAgent() {
    if (!isGroupChat) {
      _groupToolbarTargetAgentId.value = '';
      imService.setGroupToolbarTargetAgent(sessionId, agentId: '');
      return;
    }
    // 输入框显式 @ + 固定艾特合并：仍要求最终只指向唯一可访问 agent 才出工具栏。
    final targetIds = <String>[];
    final seen = <String>{};
    void addId(String raw) {
      final id = raw.trim();
      if (id.isEmpty ||
          id == _mentionAllSyntheticMemberId ||
          seen.contains(id)) {
        return;
      }
      seen.add(id);
      targetIds.add(id);
    }

    for (final id in _chatMentionController.resolveExplicitMentionUserIds(
      inputController.text,
    )) {
      addId(id);
    }
    for (final pinned in _pinnedMentions) {
      addId(pinned.memberId);
    }

    if (targetIds.length != 1) {
      _groupToolbarTargetAgentId.value = '';
      imService.setGroupToolbarTargetAgent(sessionId, agentId: '');
      return;
    }
    final candidateId = targetIds.first;
    if (!_groupAgentMemberIds.contains(candidateId)) {
      _groupToolbarTargetAgentId.value = '';
      imService.setGroupToolbarTargetAgent(sessionId, agentId: '');
      return;
    }
    // 与后端 CanUseAgent 对齐：主人持有的 + 共享给我的都可出工具栏；
    // 群里别人的 agent（我无权使用）不出。最终仍由后端鉴权兜底。
    final canUse = agentService.allAccessibleAgents.any(
      (item) => item.id.trim() == candidateId,
    );
    if (!canUse) {
      _groupToolbarTargetAgentId.value = '';
      imService.setGroupToolbarTargetAgent(sessionId, agentId: '');
      return;
    }
    _groupToolbarTargetAgentId.value = candidateId;
    imService.setGroupToolbarTargetAgent(sessionId, agentId: candidateId);
  }

  void mentionMoveUp() {
    _chatMentionController.mentionMoveUp();
  }

  void mentionMoveDown() {
    _chatMentionController.mentionMoveDown();
  }

  bool mentionSelectCurrent() {
    return _chatMentionController.mentionSelectCurrent();
  }

  void mentionSenderFromMessage({
    required String senderId,
    required int senderType,
    required bool isMine,
    required String senderName,
  }) {
    _chatMentionController.mentionSenderFromMessage(
      senderId: senderId,
      senderType: senderType,
      isMine: isMine,
      senderName: senderName,
    );
  }

  void insertMention(Map<String, dynamic> member) {
    _chatMentionController.insertMention(member);
  }

  bool isPinnedMention(String memberId) {
    return _chatMentionController.isPinnedMention(memberId);
  }

  RxList<PinnedMention> get pinnedMentions {
    return _pinnedMentions;
  }

  void togglePinnedMention(Map<String, dynamic> member) {
    _chatMentionController.togglePinnedMention(member);
  }

  void removePinnedMention(String memberId) {
    _chatMentionController.removePinnedMention(memberId);
  }

  void retryMessage(String? clientMsgId, {String? msgId}) {
    _chatNavigationController.retryMessage(clientMsgId, msgId: msgId);
  }

  Future<void> revokeMessage(String msgId, {String? originalContent}) async {
    await _chatNavigationController.revokeMessage(msgId);
    if (originalContent != null && originalContent.isNotEmpty) {
      revokedMessageContent.value = originalContent;
    }
  }

  void onStreamingMessageUpdated(String msgId) {
    _pageStateController.onStreamingMessageUpdated(msgId);
  }

  void onMessageListWindowChanged() {
    _messageListSnapshot = _messageListSnapshotBuilder.build(
      messages: imService.currentMessages,
      currentUserId: authService.userId?.toString(),
      isInternalDirectiveMessage: isInternalDirectiveMessage,
    );
    _messageListSnapshotBuildCount++;
    _messageListSnapshotRevision.value++;
    _pruneForwardSelectionState();
    _pruneMessageViewportItemKeys();
    _pruneSenderProfileVersionKeys();
    _pruneActiveHumanSenderDisplayDigestCache();
  }

  /// 消息列表快照版本号，在 Obx 内读取时注册响应式依赖。
  int get messageListSnapshotRevision => _messageListSnapshotRevision.value;

  ChatMessageListSnapshot get messageListSnapshot {
    _messageListSnapshotRevision.value;
    return _messageListSnapshot;
  }

  void onUserScrollStart(ScrollMetrics metrics) {
    _pageStateController.onUserScrollStart(metrics);
  }

  void onUserScrollActive(ScrollMetrics metrics) {
    _pageStateController.onUserScrollActive(metrics);
  }

  void onUserScrollEnd(ScrollMetrics metrics) {
    _pageStateController.onUserScrollEnd(metrics);
  }

  void onUserScrollInteractionReset() {
    _pageStateController.onUserScrollInteractionReset();
  }

  void onWheelScrollActive(ScrollMetrics metrics) {
    _pageStateController.onWheelScrollActive(metrics);
  }

  void onWheelScrollEnd(ScrollMetrics metrics) {
    _pageStateController.onWheelScrollEnd(metrics);
  }

  void onPointerSignalScroll() {
    _pageStateController.onPointerSignalScroll();
  }

  void onNestedScrollableUserDragStart() {
    _pageStateController.onNestedScrollableUserDragStart();
  }

  void onNestedScrollableUserDragActive() {
    _pageStateController.onNestedScrollableUserDragActive();
  }

  void onNestedScrollableUserDragEnd() {
    _pageStateController.onNestedScrollableUserDragEnd();
  }

  GlobalKey messageViewportItemGlobalKey(String itemKey) {
    final normalized = itemKey.trim();
    return _messageViewportItemKeys.putIfAbsent(
      normalized,
      () => GlobalKey(debugLabel: 'chat_message_$normalized'),
    );
  }

  GlobalKey? peekMessageViewportItemGlobalKey(String itemKey) {
    final normalized = itemKey.trim();
    if (normalized.isEmpty) {
      return null;
    }
    return _messageViewportItemKeys[normalized];
  }

  void _pruneMessageViewportItemKeys() {
    if (_messageViewportItemKeys.isEmpty) {
      return;
    }
    final activeKeys = <String>{};
    for (final message in imService.currentMessages) {
      activeKeys.add(ChatMessageIdentity.selectionKey(message));
    }
    _messageViewportItemKeys.removeWhere((itemKey, _) {
      return !activeKeys.contains(itemKey);
    });
  }

  void _pruneActiveHumanSenderDisplayDigestCache() {
    if (_activeHumanSenderDisplayDigestByUserId.isEmpty) {
      return;
    }
    final activeUserIds = _activeHumanSenderIdsFromCurrentMessages();
    _activeHumanSenderDisplayDigestByUserId.removeWhere((userId, _) {
      return !activeUserIds.contains(userId);
    });
  }

  void _pruneForwardSelectionState() {
    if (_selectedForwardMessageKeys.isEmpty &&
        _forwardSelectionFlagsByKey.isEmpty) {
      return;
    }
    final activeKeys = <String>{};
    for (final message in imService.currentMessages) {
      activeKeys.add(ChatMessageIdentity.selectionKey(message));
    }
    _selectedForwardMessageKeys.removeWhere((selectionKey) {
      if (activeKeys.contains(selectionKey)) {
        return false;
      }
      final flag = _forwardSelectionFlagsByKey[selectionKey];
      if (flag != null && flag.value) {
        flag.value = false;
      }
      return true;
    });
    _forwardSelectionFlagsByKey.removeWhere((selectionKey, flag) {
      return !activeKeys.contains(selectionKey) && !flag.value;
    });
    if (_selectedForwardMessageKeys.isEmpty) {
      _isForwardSelectionMode.value = false;
    }
  }

  void _pruneSenderProfileVersionKeys() {
    if (_senderProfileVersionsByKey.isEmpty) {
      return;
    }
    final myUserId = authService.userId?.trim() ?? '';
    final activeKeys = <String>{};
    for (final message in imService.currentMessages) {
      activeKeys.add(
        _senderProfileVersionKey(
          senderId: message.senderId,
          senderType: message.senderType,
          isMine: ChatMessageOwnerClassifier.isMineMessage(
            message,
            currentUserId: myUserId,
          ),
        ),
      );
    }
    _senderProfileVersionsByKey.removeWhere((key, _) {
      return key != 'self' && !activeKeys.contains(key);
    });
  }

  void _pruneVisibleToSelectionFlags() {
    if (_visibleToSelectionFlagsByMemberId.isEmpty &&
        visibleToUserIds.isEmpty) {
      return;
    }
    final activeMemberIds = <String>{};
    for (final member in _groupMembers) {
      final memberId = (member['member_id'] ?? '').toString().trim();
      if (memberId.isEmpty) {
        continue;
      }
      activeMemberIds.add(memberId);
    }

    visibleToUserIds.removeWhere((memberId) {
      final normalizedMemberId = memberId.trim();
      if (normalizedMemberId.isEmpty ||
          !activeMemberIds.contains(normalizedMemberId)) {
        final flag = _visibleToSelectionFlagsByMemberId[normalizedMemberId];
        if (flag != null && flag.value) {
          flag.value = false;
        }
        return true;
      }
      return false;
    });

    _visibleToSelectionFlagsByMemberId.removeWhere((memberId, flag) {
      return !activeMemberIds.contains(memberId) && !flag.value;
    });
  }

  @visibleForTesting
  ChatViewportAnchor? get debugLastUserViewportAnchor =>
      _lastUserViewportAnchor;

  @visibleForTesting
  int get debugMessageListSnapshotBuildCount => _messageListSnapshotBuildCount;

  void _registerManagedInput({
    required ChatManagedInputId inputId,
    required ChatManagedInputPolicy policy,
    required GlobalKey targetKey,
  }) {
    _managedInputRegistry.register(
      inputId: inputId,
      policy: policy,
      targetKey: targetKey,
    );
  }

  void _unregisterManagedInput(ChatManagedInputId inputId) {
    _managedInputRegistry.unregister(inputId);
    _deactivateManagedInput(inputId);
  }

  void _activateManagedInput(
    ChatManagedInputId inputId,
    ChatManagedInputPolicy policy,
  ) {
    final anchor =
        policy.restoreMode == ChatManagedInputRestoreMode.restoreAnchor
        ? _pageStateController.captureViewportAnchor()
        : null;
    _managedInputCoordinator.activate(
      inputId: inputId,
      policy: policy,
      startedAtBottom:
          _pageStateController.shouldPreserveBottomViewportOnInputActivation,
      anchor: anchor,
    );
  }

  void _deactivateManagedInput(ChatManagedInputId inputId) {
    _managedInputCoordinator.deactivate(inputId);
  }

  @visibleForTesting
  Future<void> loadOlderHistoryPreservingOffsetForTest() {
    return _pageStateController.loadOlderHistoryPreservingOffset();
  }

  @visibleForTesting
  Future<void> loadNewerHistoryPreservingOffsetForTest() {
    return _pageStateController.loadNewerHistoryPreservingOffset();
  }

  void stopDelegate() {
    _chatDelegateController.stopDelegate();
  }

  bool stopAgentOutput() {
    return _chatDelegateController.stopAgentOutput();
  }

  bool shouldShowAgentOutputStopForMessage(
    MessageModel msg, {
    bool? isStreaming,
  }) {
    return _chatDelegateController.shouldShowAgentOutputStopForMessage(
      msg,
      isStreaming: isStreaming,
    );
  }

  bool canStopAgentOutputForMessage(MessageModel msg, {bool? isStreaming}) {
    return _chatDelegateController.canStopAgentOutputForMessage(
      msg,
      isStreaming: isStreaming,
    );
  }

  bool isCurrentAgentOutputMessage(MessageModel msg) {
    return _chatDelegateController.isCurrentAgentOutputMessage(msg);
  }

  bool stopAgentOutputForMessage(
    MessageModel msg, {
    String source = 'message',
    bool? isStreaming,
  }) {
    return _chatDelegateController.stopAgentOutputForMessage(
      msg,
      source: source,
      isStreaming: isStreaming,
    );
  }

  void startDelegate(String agentId) {
    _chatDelegateController.startDelegate(agentId);
  }

  void increaseDelegateRounds() {
    _chatDelegateController.increaseDelegateRounds();
  }

  void decreaseDelegateRounds() {
    _chatDelegateController.decreaseDelegateRounds();
  }

  void saveDelegateRounds() {
    _chatDelegateController.saveDelegateRounds();
  }

  void loadAgents() {
    agentService.loadAgents();
  }

  /// 为当前 agent 私聊发起语音通话。
  /// 仅 providerType==4 的语音大模型 agent 有效，其他 agent 静默忽略。
  void startVoiceCallForCurrentAgent() {
    if (!isAgentPrivateChat) return;
    final session = imService.findSessionById(sessionId);
    final agentId = session?.peerId.trim() ?? '';
    if (agentId.isEmpty) return;
    final agent = agentService.agents.firstWhereOrNull((a) => a.id == agentId);
    if (agent == null || agent.providerType != 4) return;
    final im = Get.find<ImService>();
    Get.find<CallController>().directCallAgent(
      agentId,
      agent.agentName,
      im.sendCallPacket,
    );
  }

  /// 语音大脑通话：在与文字 agent 的私聊里，用用户级语音大脑当语音通道呼出。
  /// 仅文字 agent（providerType!=4）有效；语音通道由后端按用户设置解析。
  void startVoiceBrainCall() {
    if (!isAgentPrivateChat) return;
    final session = imService.findSessionById(sessionId);
    final agentId = session?.peerId.trim() ?? '';
    if (agentId.isEmpty) return;
    final agent = agentService.agents.firstWhereOrNull((a) => a.id == agentId);
    if (agent == null || agent.providerType == 4) return;
    final im = Get.find<ImService>();
    Get.find<CallController>().voiceBrainCallAgent(
      agentId,
      agent.agentName,
      sessionId,
      im.sendCallPacket,
    );
  }

  Future<void> ensureFriendListLoaded() async {
    await _chatGroupController.ensureFriendListLoaded();
  }

  List<FriendItem> get invitableFriends {
    return _chatGroupController.invitableFriends;
  }

  List<AgentModel> get invitableAgents {
    return _chatGroupController.invitableAgents;
  }

  Future<int> inviteToGroup({
    List<String> userIds = const [],
    List<String> agentIds = const [],
  }) async {
    return _chatGroupController.inviteToGroup(
      userIds: userIds,
      agentIds: agentIds,
    );
  }

  Future<bool> updateGroupInviteSetting(bool allowMemberInvite) async {
    return _chatGroupController.updateGroupInviteSetting(allowMemberInvite);
  }

  Future<bool> updateGroupAllMembersMuted(bool allMembersMuted) async {
    return _chatGroupController.updateGroupAllMembersMuted(allMembersMuted);
  }

  bool canRemoveGroupMember(Map<String, dynamic> member) {
    return _chatGroupController.canRemoveGroupMember(member);
  }

  bool canPromoteGroupMember(Map<String, dynamic> member) {
    return _chatGroupController.canPromoteGroupMember(member);
  }

  bool canDemoteGroupMember(Map<String, dynamic> member) {
    return _chatGroupController.canDemoteGroupMember(member);
  }

  bool canTransferGroupOwner(Map<String, dynamic> member) {
    return _chatGroupController.canTransferGroupOwner(member);
  }

  bool canUpdateGroupMemberSpeaking(Map<String, dynamic> member) {
    return _chatGroupController.canUpdateGroupMemberSpeaking(member);
  }

  bool canUpdateGroupMemberAgentReceive(Map<String, dynamic> member) {
    return _chatGroupController.canUpdateGroupMemberAgentReceive(member);
  }

  int groupMemberAgentReceiveMode(Map<String, dynamic> member) {
    return _chatGroupController.groupMemberAgentReceiveMode(member);
  }

  bool canToggleGroupMemberSpeakWhitelist(Map<String, dynamic> member) {
    return _chatGroupController.canToggleGroupMemberSpeakWhitelist(member);
  }

  bool isGroupMemberSpeakMuted(Map<String, dynamic> member) {
    return _chatGroupController.isGroupMemberSpeakMuted(member);
  }

  bool canGroupMemberSpeakWhenAllMuted(Map<String, dynamic> member) {
    return _chatGroupController.canGroupMemberSpeakWhenAllMuted(member);
  }

  Future<bool> updateGroupMemberSpeaking(
    Map<String, dynamic> member, {
    bool? isSpeakMuted,
    bool? canSpeakWhenAllMuted,
  }) async {
    return _chatGroupController.updateGroupMemberSpeaking(
      member,
      isSpeakMuted: isSpeakMuted,
      canSpeakWhenAllMuted: canSpeakWhenAllMuted,
    );
  }

  Future<bool> updateGroupMemberAgentReceive(
    Map<String, dynamic> member, {
    required int mode,
  }) async {
    return _chatGroupController.updateGroupMemberAgentReceive(
      member,
      mode: mode,
    );
  }

  Future<int> removeGroupMember(Map<String, dynamic> member) async {
    return _chatGroupController.removeGroupMember(member);
  }

  bool canLeaveGroupMember(Map<String, dynamic> member) {
    return _chatGroupController.canLeaveGroupMember(member);
  }

  Future<bool> leaveGroup() async {
    return _chatGroupController.leaveGroup();
  }

  Future<bool> updateGroupMemberRole(
    Map<String, dynamic> member, {
    required int role,
  }) async {
    return _chatGroupController.updateGroupMemberRole(member, role: role);
  }

  Future<bool> transferGroupOwner(Map<String, dynamic> member) async {
    return _chatGroupController.transferGroupOwner(member);
  }

  Future<bool> dissolveGroup() async {
    return _chatGroupController.dissolveGroup();
  }

  /// 将当前私聊会话原地转换为群聊。成功后刷新会话详情以切换为群聊视图。
  Future<bool> convertToGroup() async {
    final sid = sessionId.trim();
    if (sid.isEmpty || isGroupChat) return false;
    final result = await sessionService.convertToGroup(
      sessionId: sid,
      name: displayChatTitle,
    );
    if (result == null) return false;
    await refreshSessionDetail(forceTypeProbe: true);
    return true;
  }

  Future<bool> setMyGroupNickname(String rawNickname) async {
    return _chatGroupController.setMyGroupNickname(rawNickname);
  }

  Future<bool> renameCurrentSession(String rawTitle) async {
    return _chatGroupController.renameCurrentSession(rawTitle);
  }

  void onHeaderAvatarTap() {
    _chatNavigationController.onHeaderAvatarTap();
  }

  void onMessageAvatarTap({
    required String senderId,
    required int senderType,
    required bool isMine,
    required String senderName,
    required String senderAvatarUrl,
  }) {
    _chatNavigationController.onMessageAvatarTap(
      senderId: senderId,
      senderType: senderType,
      isMine: isMine,
      senderName: senderName,
      senderAvatarUrl: senderAvatarUrl,
    );
  }

  void onMessageCardTap(ChatMessageCardData card) {
    _chatNavigationController.onMessageCardTap(card);
  }

  Future<ChatMessageCardActionResult> onMessageCardAction(
    ChatMessageCardAction action,
  ) {
    return _chatNavigationController.onMessageCardAction(action);
  }

  /// 最近绑定目录的本地缓存（跨会话共享，纯前端）。
  static final ChatRecentBindDirectoryStore recentBindDirectoryStore =
      ChatRecentBindDirectoryStore();

  /// 当前私聊对端是需要绑定目录的 agent（非 hermes/openclaw）时返回其 agentId，
  /// 否则空串。空白聊天页据此决定是否展示快捷绑定目录组件。
  String get directoryBoundAgentId {
    if (isGroupChat) return '';
    final session = imService.findSessionById(sessionId);
    if (session?.peerType != 2) return '';
    final agentId = session!.peerId.trim();
    if (agentId.isEmpty || agentId == '0') return '';
    final agent = agentService.agents.firstWhereOrNull((a) => a.id == agentId);
    if (agent == null) return '';
    if (!isDirectoryBoundAgentClientType(agent.agentClientType)) return '';
    return agentId;
  }

  /// 某个 agent 所在的宿主机名（connector 上报），未知时空串。
  /// 目录路径只在同一台机器上有效，快捷绑定的缓存记录与跨 agent 补位都以此为界。
  String agentHostnameOf(String agentId) {
    final normalized = agentId.trim();
    if (normalized.isEmpty) return '';
    return agentService.agents
            .firstWhereOrNull((a) => a.id == normalized)
            ?.hostname
            .trim() ??
        '';
  }

  Future<bool> sendQuickBindDirectory(String path) {
    return _chatNavigationController.sendQuickBindDirectory(path);
  }

  void onUserProfileCardTap(ChatUserProfileCardData card) {
    _chatNavigationController.onUserProfileCardTap(card);
  }

  Future<void> onConversationCardTap(ChatConversationCardData card) async {
    await _chatNavigationController.onConversationCardTap(card);
  }

  void openGroupReportPage() {
    _chatNavigationController.openGroupReportPage();
  }

  Future<void> deleteCurrentConversation() async {
    await _chatNavigationController.deleteCurrentConversation();
  }

  Future<bool> closeCurrentVisitorSession() async {
    final sid = sessionId.trim();
    if (sid.isEmpty || !isVisitorSession) {
      CustomToast.show('chat_visitor_close_failed'.tr);
      return false;
    }
    final result = await sessionService.closeVisitorSession(sid);
    if (!result.success) {
      CustomToast.show(
        result.message.isNotEmpty
            ? result.message
            : 'chat_visitor_close_failed'.tr,
      );
      return false;
    }
    // 先给出成功反馈，再做会话刷新；刷新失败不应吞掉提示或向调用方抛异常。
    CustomToast.show('chat_visitor_close_success'.tr, isError: false);
    try {
      await imService.refreshSessionsNow();
      await refreshSessionDetail();
    } catch (e) {
      debugPrint('closeCurrentVisitorSession refresh error: $e');
    }
    return true;
  }

  Future<bool> banCurrentVisitorSession() async {
    final sid = sessionId.trim();
    if (sid.isEmpty || !isVisitorSession) {
      CustomToast.show('chat_visitor_ban_failed'.tr);
      return false;
    }
    final result = await sessionService.banVisitorSession(sid);
    if (!result.success) {
      CustomToast.show(
        result.message.isNotEmpty
            ? result.message
            : 'chat_visitor_ban_failed'.tr,
      );
      return false;
    }
    // 先给出成功反馈，再做会话刷新；刷新失败不应吞掉提示或向调用方抛异常。
    CustomToast.show('chat_visitor_ban_success'.tr, isError: false);
    try {
      await imService.refreshSessionsNow();
      await refreshSessionDetail();
    } catch (e) {
      debugPrint('banCurrentVisitorSession refresh error: $e');
    }
    return true;
  }

  bool get isCurrentSessionMuted {
    return _chatNavigationController.isCurrentSessionMuted;
  }

  Future<bool> setCurrentSessionMuted(bool isMuted) async {
    return _chatNavigationController.setCurrentSessionMuted(isMuted);
  }

  void _syncDelegateRoundsDraftFromState() {
    _chatDelegateController.syncDelegateRoundsDraftFromState();
  }

  Future<void> refreshSessionDetail({bool forceTypeProbe = false}) async {
    await _chatGroupController.refreshSessionDetail(
      forceTypeProbe: forceTypeProbe,
    );
  }

  Map<String, dynamic>? _findGroupMember(
    String rawMemberId, {
    int? memberType,
  }) {
    final memberId = rawMemberId.trim();
    if (memberId.isEmpty) {
      return null;
    }
    for (final member in _groupMembers) {
      if (memberType != null &&
          _parseInt(member['member_type']) != memberType) {
        continue;
      }
      final currentMemberId = (member['member_id'] ?? '').toString().trim();
      if (currentMemberId == memberId) {
        return member;
      }
    }
    return null;
  }

  Map<String, dynamic>? _findGroupHumanMember(String rawUserId) {
    return _findGroupMember(rawUserId, memberType: 1);
  }

  String resolveGroupMemberDisplayName(Map<String, dynamic> member) {
    return _chatIdentityController.resolveGroupMemberDisplayName(member);
  }

  String resolveGroupMemberAccount(Map<String, dynamic> member) {
    return _chatIdentityController.resolveGroupMemberAccount(member);
  }

  String resolveGroupMemberAvatarUrl(Map<String, dynamic> member) {
    return _resolveGroupMemberAvatarUrl(member);
  }

  String _resolveKnownAgentName(String rawAgentId) {
    final agentId = rawAgentId.trim();
    if (agentId.isEmpty) {
      return '';
    }

    final memberNickname = _resolveAgentNameFromGroupMembers(agentId);
    if (memberNickname.isNotEmpty) {
      return memberNickname;
    }

    final idx = agentService.agents.indexWhere((a) => a.id == agentId);
    if (idx != -1) {
      return agentService.agents[idx].agentName.trim();
    }
    final sharedIdx = agentService.sharedAgents.indexWhere(
      (a) => a.id == agentId,
    );
    if (sharedIdx != -1) {
      return agentService.sharedAgents[sharedIdx].agentName.trim();
    }
    return '';
  }

  String _resolveAgentNameFromGroupMembers(String agentId) {
    for (final member in _groupMembers) {
      if (_parseInt(member['member_type']) != 2) {
        continue;
      }
      final memberId = (member['member_id'] ?? '').toString().trim();
      if (memberId != agentId) {
        continue;
      }
      return (member['nickname'] ?? '').toString().trim();
    }
    return '';
  }

  String _resolveGroupMemberAvatarUrl(Map<String, dynamic> member) {
    final memberId = (member['member_id'] ?? '').toString().trim();
    final memberType = _parseInt(member['member_type']);
    if (memberId.isEmpty) return '';

    if (memberType == 2) {
      final idx = agentService.agents.indexWhere((a) => a.id == memberId);
      if (idx != -1) {
        return agentService.agents[idx].avatarUrl.trim();
      }
      return '';
    }

    final myId = authService.userId?.trim() ?? '';
    if (myId.isNotEmpty && memberId == myId) {
      return authService.user?.avatarUrl?.trim() ?? '';
    }

    final fs = _friendService;
    if (fs != null) {
      return fs.getUserAvatarUrl(memberId)?.trim() ?? '';
    }
    return '';
  }

  int _parseDelegateRounds(dynamic v) {
    if (v is int) return v;
    if (v is num) return v.toInt();
    if (v is String) return int.tryParse(v.trim()) ?? 0;
    return int.tryParse(v?.toString() ?? '') ?? 0;
  }

  int _parseInt(dynamic v) {
    if (v is int) return v;
    if (v is num) return v.toInt();
    if (v is String) return int.tryParse(v.trim()) ?? 0;
    return int.tryParse(v?.toString() ?? '') ?? 0;
  }

  bool _parseBool(dynamic v) {
    if (v is bool) return v;
    if (v is num) return v != 0;
    final normalized = v?.toString().trim().toLowerCase() ?? '';
    return normalized == 'true' || normalized == '1';
  }

  void _resetGroupSessionState() {
    _groupMemberCount.value = 0;
    _initialGroupAvatarMembers = const <SessionAvatarMember>[];
    _groupMembers.clear();
    _groupAgentMemberIds.clear();
    _allMembersMuted.value = false;
    _allowMemberInvite.value = false;
    _memberInviteThreshold.value = 0;
    _memberDisplayNameCache.clear();
  }

  void _applyVisitorSessionDetail(Map<String, dynamic>? detail) {
    if (detail == null) {
      _isVisitorSession.value = false;
      _visitorInfo.clear();
      return;
    }
    final isVisitor = _parseBool(detail['is_visitor']);
    final info = detail['visitor_info'];
    if (!isVisitor || info is! Map) {
      _isVisitorSession.value = false;
      _visitorInfo.clear();
      return;
    }
    _isVisitorSession.value = true;
    _visitorInfo.assignAll(Map<String, dynamic>.from(info));
  }

  List<SessionAvatarMember> _readInitialGroupAvatarMembers(
    Map<String, dynamic> args,
  ) {
    final raw = args['initial_group_avatar_members'];
    if (raw is! List) {
      return const <SessionAvatarMember>[];
    }

    final members = <SessionAvatarMember>[];
    for (final item in raw) {
      if (item is! Map) {
        continue;
      }
      final member = SessionAvatarMember.fromJson(
        Map<String, dynamic>.from(item),
      );
      if (member.memberId.isEmpty) {
        continue;
      }
      members.add(member);
      if (members.length >= 9) {
        break;
      }
    }
    return List<SessionAvatarMember>.unmodifiable(members);
  }

  Future<void> _handleGroupAccessLost() async {
    if (_groupAccessLostHandled) return;
    _groupAccessLostHandled = true;

    _resetGroupSessionState();
    await imService.revokeSessionAccess(sessionId);
    final reason = imService.getSessionAccessRevokedReason(sessionId);
    final toastKey = reason == 'group_banned'
        ? 'chat_group_banned'
        : 'chat_removed_from_group';
    CustomToast.show(toastKey.tr, isError: false);

    if (ChatPaneHost.closeIfActive(sessionId)) return;
    if (Get.key.currentState == null) return;
    if (Get.currentRoute == AppRoutes.chat &&
        (Get.key.currentState?.canPop() ?? false)) {
      Get.back();
      return;
    }
    if (!AppRoutes.isCurrentHomePath) {
      RootRouteNavigator.toHome();
    }
  }

  String _normalizeChatType(String rawType) {
    final normalized = rawType.trim().toLowerCase();
    if (normalized == 'group') return 'group';
    return 'private';
  }

  void _refreshPrivatePeerNickname({bool fetchIfMissing = true}) {
    if (isGroupChat) {
      _privatePeerNickname.value = '';
      _privatePeerUserId.value = '';
      _privatePeerAvatarUrl.value = '';
      return;
    }

    final fromSession = _resolvePrivatePeerNameFromSession();
    if (fromSession.isNotEmpty) {
      _privatePeerNickname.value = fromSession;
      return;
    }

    final peerId = _resolvePrivatePeerUserId();
    if (peerId.isEmpty) {
      if (fetchIfMissing) {
        unawaited(_probePrivatePeerUserIdFromSessionDetail());
      }
      _privatePeerNickname.value = '';
      return;
    }

    final fs = _friendService;
    if (fs == null) {
      _privatePeerNickname.value = '';
      return;
    }

    final cached = fs.getUserNickname(peerId)?.trim() ?? '';
    if (cached.isNotEmpty) {
      _privatePeerNickname.value = cached;
      return;
    }

    if (!fetchIfMissing) {
      return;
    }

    _ensureProfileLoaded(peerId);
  }

  void _refreshPrivatePeerAvatar({bool fetchIfMissing = true}) {
    if (isGroupChat) {
      _privatePeerAvatarUrl.value = '';
      return;
    }

    final currentSession = imService.findSessionById(sessionId);
    if (currentSession?.type == 'private' && currentSession?.peerType == 2) {
      final agentId = currentSession?.peerId.trim() ?? '';
      if (agentId.isEmpty) {
        _privatePeerAvatarUrl.value = '';
        return;
      }
      final idx = agentService.agents.indexWhere(
        (agent) => agent.id == agentId,
      );
      _privatePeerAvatarUrl.value = idx == -1
          ? ''
          : agentService.agents[idx].avatarUrl.trim();
      return;
    }

    final peerId = _resolvePrivatePeerUserId();
    if (peerId.isEmpty) {
      _privatePeerAvatarUrl.value = '';
      if (fetchIfMissing) {
        unawaited(_probePrivatePeerUserIdFromSessionDetail());
      }
      return;
    }

    final fs = _friendService;
    if (fs == null) {
      _privatePeerAvatarUrl.value = '';
      return;
    }

    final avatarUrl = fs.getUserAvatarUrl(peerId)?.trim() ?? '';
    if (avatarUrl.isNotEmpty) {
      _privatePeerAvatarUrl.value = avatarUrl;
      return;
    }

    _privatePeerAvatarUrl.value = '';
    if (fetchIfMissing) {
      _ensureProfileLoaded(peerId);
    }
  }

  String _resolvePrivatePeerUserId() {
    final cached = _privatePeerUserId.value.trim();
    if (cached.isNotEmpty) {
      return cached;
    }

    final sid = sessionId.trim();
    if (sid.isEmpty) return '';
    final idx = imService.sessions.indexWhere((s) => s.sessionId == sid);
    if (idx < 0) return '';
    final session = imService.sessions[idx];
    if (session.type != 'private') return '';
    if (session.peerType == 2) return '';
    final peerId = session.peerId.trim();
    if (peerId.isNotEmpty) {
      _privatePeerUserId.value = peerId;
    }
    return peerId;
  }

  String _resolvePrivatePeerNameFromSession() {
    final sid = sessionId.trim();
    if (sid.isEmpty) return '';
    final idx = imService.sessions.indexWhere((s) => s.sessionId == sid);
    if (idx < 0) return '';
    final session = imService.sessions[idx];
    if (session.type != 'private') return '';

    final nickname = session.peerNickname.trim();
    if (nickname.isNotEmpty) return nickname;
    final username = session.peerUsername.trim();
    if (username.isNotEmpty) return username;
    return '';
  }

  Future<void> _probePrivatePeerUserIdFromSessionDetail() async {
    if (isGroupChat || _isResolvingPrivatePeerId) return;
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    _isResolvingPrivatePeerId = true;
    try {
      final detailResult = await sessionService.fetchSessionDetailResult(sid);
      final detail = detailResult.data;
      if (detail == null) return;
      final sessionType = _parseInt(detail['session_type']);
      if (sessionType != 1) return;

      final myId = authService.userId?.trim() ?? '';
      final membersRaw = detail['members'];
      if (membersRaw is! List) return;
      for (final item in membersRaw) {
        if (item is! Map) continue;
        final memberType = _parseInt(item['member_type']);
        if (memberType != 1) continue;
        final memberId = (item['member_id'] ?? '').toString().trim();
        if (memberId.isEmpty || memberId == myId) continue;
        _privatePeerUserId.value = memberId;
        _refreshPrivatePeerNickname(fetchIfMissing: false);
        _refreshPrivatePeerAvatar(fetchIfMissing: false);
        return;
      }
    } catch (e) {
      debugPrint('probe private peer id from session detail error: $e');
    } finally {
      _isResolvingPrivatePeerId = false;
    }
  }

  String _readRoutingValue({
    required Map<String, dynamic> args,
    required Map<String, String?> params,
    required String key,
    String fallback = '',
  }) {
    final argValue = (args[key] ?? '').toString();
    if (argValue.trim().isNotEmpty) return argValue;

    final paramValue = (params[key] ?? '').toString();
    if (paramValue.trim().isNotEmpty) return paramValue;

    return fallback;
  }
}
