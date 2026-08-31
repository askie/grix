import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/friend_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/data/providers/oss_service.dart';
import 'package:grix/data/models/message_model.dart';
import 'package:grix/data/models/session_activity_model.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/modules/chat/chat_view.dart';
import 'package:grix/modules/chat/bindings/chat_binding.dart';
import 'package:grix/modules/chat/controllers/chat_controller.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_open_session_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_question_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_conversation_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_exec_approval_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_exec_status_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_message_card_action.dart';
import 'package:grix/modules/chat/message_cards/models/chat_user_profile_card_data.dart';
import 'package:grix/modules/chat/message_cards/services/chat_agent_card_action_encoder.dart';
import 'package:grix/modules/chat/message_cards/services/chat_message_card_codec.dart';
import 'package:grix/modules/chat/models/chat_forward_dispatch_mode.dart';
import 'package:grix/modules/chat/models/chat_attachment_type.dart';
import 'package:grix/modules/chat/models/chat_message_identity.dart';
import 'package:grix/modules/chat/models/chat_prepared_attachment_upload.dart';
import 'package:grix/modules/chat/services/chat_bottom_obstruction_observer.dart';
import 'package:grix/modules/chat/services/chat_keyboard_platform_behavior.dart';
import 'package:grix/modules/chat/services/chat_pane_host.dart';
import 'package:grix/modules/chat/services/chat_route_navigator.dart';
import 'package:grix/shared/models/session_avatar_member.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeImService extends ImService {
  bool connected = false;
  bool authenticated = false;
  int enterSessionCalls = 0;
  int connectCalls = 0;
  int refreshDelegateStatesCalls = 0;
  int loadMoreCalls = 0;
  int loadNewerCalls = 0;
  int sendCalls = 0;
  int retryCalls = 0;
  int delegateStartCalls = 0;
  int delegateStopCalls = 0;
  int agentOutputStopCalls = 0;
  int localStreamingStopCalls = 0;
  int deleteConversationCalls = 0;
  int revokeSessionAccessCalls = 0;
  int applyLocalMessageRevokeCalls = 0;

  String? enteredSessionId;
  String? connectUrl;
  String? sentContent;
  String? sentSessionId;
  bool? sentUpdateCurrentSessionUi;
  Map<String, dynamic>? sentExtra;
  String? retriedClientMsgId;
  String? retriedMsgId;
  String? startedAgentId;
  String? startedSessionId;
  int? startedMaxConsecutiveReplies;
  String? stoppedSessionId;
  String? stoppedOutputSessionId;
  String? stoppedOutputRunId;
  String? locallyStoppedStreamSessionId;
  String? locallyStoppedStreamMsgId;
  String? deletedSessionId;
  String? accessRevokedSessionId;
  String? revokedMessageId;
  String? revokedSessionId;
  VoidCallback? onLoadOlder;
  VoidCallback? onLoadNewer;
  bool hasOlder = true;
  bool hasNewer = false;

  Completer<void>? loadMoreCompleter;
  Completer<void>? loadNewerCompleter;

  @override
  bool get isConnected => connected;

  @override
  bool get isAuthenticated => authenticated;

  @override
  bool get hasOlderMessages => hasOlder;

  @override
  bool get hasNewerMessages => hasNewer;

  @override
  void enterSession(
    String sessionId, {
    Duration initialLoadDelay = Duration.zero,
  }) {
    enterSessionCalls++;
    enteredSessionId = sessionId;
  }

  @override
  void leaveSession([String? explicitSessionId]) {}

  @override
  void connect(String wsUrl) {
    connectCalls++;
    connectUrl = wsUrl;
    connected = true;
    authenticated = true;
  }

  @override
  void ensureConnected() {
    // Production ensureConnected() dispatches through the private _connectImpl,
    // which bypasses the connect() override above and would attempt a real
    // socket. Route it through the recorded connect() so the fake records the
    // call with the default WS endpoint, matching the onReady contract.
    connect(ImService.defaultWsUrl);
  }

  @override
  void refreshDelegateStates() {
    refreshDelegateStatesCalls++;
  }

  @override
  void updateSessionComposing(String sessionId, {required bool active}) {}

  @override
  Future<void> loadOlderForCurrentSession() async {
    loadMoreCalls++;
    onLoadOlder?.call();
    final completer = loadMoreCompleter;
    if (completer != null) {
      await completer.future;
    }
  }

  @override
  Future<void> loadNewerForCurrentSession() async {
    loadNewerCalls++;
    onLoadNewer?.call();
    final completer = loadNewerCompleter;
    if (completer != null) {
      await completer.future;
    }
  }

  @override
  Future<void> sendMessage(
    String content,
    String sessionId, {
    Map<String, dynamic>? extra,
    String? quotedMessageId,
    List<String>? visibleTo,
    bool updateCurrentSessionUi = true,
  }) async {
    sendCalls++;
    sentContent = content;
    sentSessionId = sessionId;
    sentUpdateCurrentSessionUi = updateCurrentSessionUi;
    sentExtra = extra == null ? null : Map<String, dynamic>.from(extra);
  }

  @override
  Future<void> retryMessage(String? clientMsgId, {String? msgId}) async {
    retryCalls++;
    retriedClientMsgId = clientMsgId;
    retriedMsgId = msgId;
  }

  @override
  void delegateStart(
    String sessionId,
    String agentId, {
    int? maxConsecutiveReplies,
  }) {
    delegateStartCalls++;
    startedSessionId = sessionId;
    startedAgentId = agentId;
    startedMaxConsecutiveReplies = maxConsecutiveReplies;
  }

  @override
  void delegateStop(String sessionId) {
    delegateStopCalls++;
    stoppedSessionId = sessionId;
  }

  @override
  bool stopAgentOutput(String sessionId, {String? runId}) {
    agentOutputStopCalls++;
    stoppedOutputSessionId = sessionId;
    stoppedOutputRunId = runId;
    return true;
  }

  @override
  bool stopStreamingMessageLocally(String sessionId, String msgId) {
    localStreamingStopCalls++;
    locallyStoppedStreamSessionId = sessionId;
    locallyStoppedStreamMsgId = msgId;
    return true;
  }

  @override
  Future<void> deleteConversation(String sessionId) async {
    deleteConversationCalls++;
    deletedSessionId = sessionId;
  }

  @override
  Future<void> revokeSessionAccess(String sessionId) async {
    revokeSessionAccessCalls++;
    accessRevokedSessionId = sessionId;
    sessions.removeWhere((s) => s.sessionId == sessionId);
  }

  @override
  Future<void> applyLocalMessageRevoke({
    required String sessionId,
    required String msgId,
    String dbOpLabel = 'deleteMessage(local_revoke)',
    bool reloadSessions = true,
    int? authoritativeUnreadCount,
  }) async {
    applyLocalMessageRevokeCalls++;
    revokedSessionId = sessionId;
    revokedMessageId = msgId;
  }
}

class _FakeAuthService extends AuthService {
  _FakeAuthService({required this.loggedIn, required this.id});

  final bool loggedIn;
  final String? id;

  @override
  bool get isLoggedIn => loggedIn;

  @override
  String? get userId => id;
}

class _FakeAgentService extends AgentService {
  int loadCalls = 0;

  @override
  Future<void> loadAgents({String? categoryId}) async {
    loadCalls++;
  }
}

class _FakeSessionService extends SessionService {
  int detailCalls = 0;
  int deleteMessageCalls = 0;
  int addMembersCalls = 0;
  int removeMembersCalls = 0;
  int leaveGroupCalls = 0;
  int updateRoleCalls = 0;
  int transferOwnerCalls = 0;
  int dissolveGroupCalls = 0;
  int setGroupNicknameCalls = 0;
  int updateInviteSettingCalls = 0;
  int updateAllMembersMutedCalls = 0;
  int updateMemberSpeakingCalls = 0;
  String? addMembersSessionId;
  String? removeMembersSessionId;
  String? leaveGroupSessionId;
  String? updateRoleSessionId;
  String? transferOwnerSessionId;
  String? dissolveGroupSessionId;
  String? setGroupNicknameSessionId;
  String? updateInviteSettingSessionId;
  String? updateAllMembersMutedSessionId;
  String? updateMemberSpeakingSessionId;
  String? updateMemberAgentReceiveSessionId;
  String? setGroupNicknameValue;
  String? updateRoleMemberId;
  String? transferOwnerMemberId;
  String? updateMemberSpeakingMemberId;
  String? updateMemberAgentReceiveMemberId;
  bool? updateInviteSettingAllowMemberInvite;
  bool? updateAllMembersMutedValue;
  bool? updateMemberSpeakingIsMuted;
  bool? updateMemberSpeakingCanSpeakWhenAllMuted;
  bool deleteMessageResult = true;
  int? updateRoleMemberType;
  int? updateRoleValue;
  int? updateMemberSpeakingMemberType;
  int? updateMemberAgentReceiveMemberType;
  int? updateMemberAgentReceiveMode;
  List<String> addMembersIds = const [];
  List<String> removeMembersIds = const [];
  List<int> addMembersTypes = const [];
  List<int> removeMembersTypes = const [];
  Map<String, dynamic>? detailResp;
  SessionDetailResult? detailResult;
  Map<String, dynamic>? addMembersResp;
  SessionAddMembersResult? addMembersResult;
  Map<String, dynamic>? removeMembersResp;
  SessionLeaveResult leaveGroupResp = const SessionLeaveResult(
    code: 0,
    sessionId: 'session-left',
    left: true,
  );
  Map<String, dynamic>? updateRoleResp;
  Map<String, dynamic>? transferOwnerResp;
  Map<String, dynamic>? dissolveGroupResp;
  SessionInviteSettingResult updateInviteSettingResult =
      const SessionInviteSettingResult(code: 0, allowMemberInvite: true);
  SessionAllMembersMutedResult updateAllMembersMutedResult =
      const SessionAllMembersMutedResult(code: 0, allMembersMuted: true);
  SessionMemberSpeakingResult updateMemberSpeakingResult =
      const SessionMemberSpeakingResult(code: 0);
  SessionMemberAgentReceiveResult updateMemberAgentReceiveResult =
      const SessionMemberAgentReceiveResult(code: 0);
  SessionMemberNicknameResult setGroupNicknameResp =
      const SessionMemberNicknameResult(code: 0, groupNickname: '');
  String? deleteMessageSessionId;
  String? deleteMessageMsgId;

  @override
  Future<Map<String, dynamic>?> fetchSessionDetail(String sessionId) async {
    final result = await fetchSessionDetailResult(sessionId);
    return result.data;
  }

  @override
  Future<SessionDetailResult> fetchSessionDetailResult(String sessionId) async {
    detailCalls++;
    if (detailResult != null) return detailResult!;
    final detail = detailResp;
    if (detail == null) {
      return const SessionDetailResult(
        code: 50001,
        message: 'mock detail missing',
      );
    }
    return SessionDetailResult(data: Map<String, dynamic>.from(detail));
  }

  @override
  Future<Map<String, dynamic>?> addGroupMembers({
    required String sessionId,
    required List<String> memberIds,
    List<int> memberTypes = const [],
  }) async {
    final result = await addGroupMembersResult(
      sessionId: sessionId,
      memberIds: memberIds,
      memberTypes: memberTypes,
    );
    return result.data;
  }

  @override
  Future<SessionAddMembersResult> addGroupMembersResult({
    required String sessionId,
    required List<String> memberIds,
    List<int> memberTypes = const [],
  }) async {
    addMembersCalls++;
    addMembersSessionId = sessionId;
    addMembersIds = List<String>.from(memberIds);
    addMembersTypes = List<int>.from(memberTypes);
    if (addMembersResult != null) {
      return addMembersResult!;
    }
    return SessionAddMembersResult(data: addMembersResp);
  }

  @override
  Future<Map<String, dynamic>?> removeGroupMembers({
    required String sessionId,
    required List<String> memberIds,
    List<int> memberTypes = const [],
  }) async {
    removeMembersCalls++;
    removeMembersSessionId = sessionId;
    removeMembersIds = List<String>.from(memberIds);
    removeMembersTypes = List<int>.from(memberTypes);
    return removeMembersResp;
  }

  @override
  Future<SessionLeaveResult> leaveGroupResult({
    required String sessionId,
  }) async {
    leaveGroupCalls++;
    leaveGroupSessionId = sessionId;
    return leaveGroupResp;
  }

  @override
  Future<Map<String, dynamic>?> updateGroupMemberRole({
    required String sessionId,
    required String memberId,
    int memberType = 1,
    required int role,
  }) async {
    updateRoleCalls++;
    updateRoleSessionId = sessionId;
    updateRoleMemberId = memberId;
    updateRoleMemberType = memberType;
    updateRoleValue = role;
    return updateRoleResp;
  }

  @override
  Future<Map<String, dynamic>?> transferGroupOwner({
    required String sessionId,
    required String memberId,
  }) async {
    transferOwnerCalls++;
    transferOwnerSessionId = sessionId;
    transferOwnerMemberId = memberId;
    return transferOwnerResp;
  }

  @override
  Future<Map<String, dynamic>?> dissolveGroup({
    required String sessionId,
  }) async {
    dissolveGroupCalls++;
    dissolveGroupSessionId = sessionId;
    return dissolveGroupResp;
  }

  @override
  Future<SessionMemberNicknameResult> setGroupNicknameResult(
    String sessionId,
    String nickname,
  ) async {
    setGroupNicknameCalls++;
    setGroupNicknameSessionId = sessionId;
    setGroupNicknameValue = nickname;
    return setGroupNicknameResp;
  }

  @override
  Future<SessionInviteSettingResult> updateGroupInviteSettingResult({
    required String sessionId,
    required bool allowMemberInvite,
  }) async {
    updateInviteSettingCalls++;
    updateInviteSettingSessionId = sessionId;
    updateInviteSettingAllowMemberInvite = allowMemberInvite;
    return updateInviteSettingResult;
  }

  @override
  Future<SessionAllMembersMutedResult> updateGroupAllMembersMutedResult({
    required String sessionId,
    required bool allMembersMuted,
  }) async {
    updateAllMembersMutedCalls++;
    updateAllMembersMutedSessionId = sessionId;
    updateAllMembersMutedValue = allMembersMuted;
    return updateAllMembersMutedResult;
  }

  @override
  Future<SessionMemberSpeakingResult> updateGroupMemberSpeakingResult({
    required String sessionId,
    required String memberId,
    int memberType = 1,
    bool? isSpeakMuted,
    bool? canSpeakWhenAllMuted,
  }) async {
    updateMemberSpeakingCalls++;
    updateMemberSpeakingSessionId = sessionId;
    updateMemberSpeakingMemberId = memberId;
    updateMemberSpeakingMemberType = memberType;
    updateMemberSpeakingIsMuted = isSpeakMuted;
    updateMemberSpeakingCanSpeakWhenAllMuted = canSpeakWhenAllMuted;
    return updateMemberSpeakingResult;
  }

  @override
  Future<SessionMemberAgentReceiveResult> updateGroupMemberAgentReceiveResult({
    required String sessionId,
    required String memberId,
    required int agentReceiveMode,
    int? agentReceiveBacklogCount,
    int memberType = 1,
  }) async {
    updateMemberAgentReceiveSessionId = sessionId;
    updateMemberAgentReceiveMemberId = memberId;
    updateMemberAgentReceiveMemberType = memberType;
    updateMemberAgentReceiveMode = agentReceiveMode;
    return updateMemberAgentReceiveResult;
  }

  @override
  Future<bool> deleteMessage({
    required String sessionId,
    required String msgId,
  }) async {
    deleteMessageCalls++;
    deleteMessageSessionId = sessionId;
    deleteMessageMsgId = msgId;
    return deleteMessageResult;
  }
}

class _FakeOssService extends OssService {
  int presignCalls = 0;
  int uploadCalls = 0;
  int uploadStreamCalls = 0;
  final List<String> requestedFileNames = <String>[];
  final List<String> requestedContentTypes = <String>[];
  final List<String?> uploadedContentTypes = <String?>[];
  final List<int> uploadedByteLengths = <int>[];
  final List<String> uploadedUrls = <String>[];
  final List<String> deletedObjectKeys = <String>[];
  final Set<int> failUploadAtCalls = <int>{};

  @override
  Future<Map<String, String>?> getPresignedUrl(
    String filename,
    String contentType,
  ) async {
    presignCalls++;
    requestedFileNames.add(filename);
    requestedContentTypes.add(contentType);
    return <String, String>{
      'uploadUrl': 'https://upload.example.com/$presignCalls',
      'accessUrl': 'https://cdn.example.com/$filename',
      'objectKey': 'aibot/media/user/42/$filename',
    };
  }

  @override
  Future<bool> uploadToOss(
    String uploadUrl,
    Uint8List fileBytes, {
    String? contentType,
  }) async {
    uploadCalls++;
    uploadedUrls.add(uploadUrl);
    uploadedContentTypes.add(contentType);
    uploadedByteLengths.add(fileBytes.length);
    return !failUploadAtCalls.contains(uploadCalls);
  }

  @override
  Future<bool> uploadStreamToOss(
    String uploadUrl,
    Stream<List<int>> stream, {
    required int contentLength,
    String? contentType,
  }) async {
    uploadStreamCalls++;
    uploadedUrls.add(uploadUrl);
    return !failUploadAtCalls.contains(uploadCalls + uploadStreamCalls);
  }

  @override
  Future<bool> deleteObjects(List<String> objectKeys) async {
    deletedObjectKeys.addAll(objectKeys);
    return true;
  }
}

class _SpyChatController extends ChatController {
  int dispatchCurrentInputMessageInvocations = 0;

  @override
  bool dispatchCurrentInputMessage() {
    dispatchCurrentInputMessageInvocations++;
    return super.dispatchCurrentInputMessage();
  }
}

class _FakeFriendService extends FriendService {
  final Map<String, String> remarks = <String, String>{};
  final Map<String, String> nicknames = <String, String>{};
  final Map<String, String> usernames = <String, String>{};
  final Map<String, Completer<String?>> _pendingProfiles =
      <String, Completer<String?>>{};

  @override
  String? getFriendRemarkName(String userId) {
    return remarks[userId];
  }

  @override
  String? getUserNickname(String userId) {
    return nicknames[userId];
  }

  @override
  String? getUserUsername(String userId) {
    return usernames[userId];
  }

  @override
  Future<String?> fetchUserProfile(String userId) async {
    return _pendingProfiles.putIfAbsent(userId, Completer<String?>.new).future;
  }

  void completeProfile(String userId, {String? nickname, String? username}) {
    if (nickname != null) {
      nicknames[userId] = nickname;
    }
    if (username != null) {
      usernames[userId] = username;
    }
    final completer = _pendingProfiles.putIfAbsent(
      userId,
      Completer<String?>.new,
    );
    if (!completer.isCompleted) {
      completer.complete(userId);
    }
  }
}

class _FakeChatBottomObstructionObserver
    implements ChatBottomObstructionObserver {
  _FakeChatBottomObstructionObserver({double initialBottomObstruction = 0})
    : _currentBottomObstruction = initialBottomObstruction;

  final StreamController<double> _controller =
      StreamController<double>.broadcast();
  double _currentBottomObstruction;

  @override
  double get currentBottomObstruction => _currentBottomObstruction;

  @override
  Stream<double> get onChanged => _controller.stream;

  void emit(double nextBottomObstruction) {
    _currentBottomObstruction = nextBottomObstruction;
    if (_controller.isClosed) {
      return;
    }
    _controller.add(nextBottomObstruction);
  }

  @override
  void dispose() {
    _controller.close();
  }
}

class _DismissSpyChatController extends ChatController {
  bool dismissInputInteractionCalled = false;

  @override
  void dismissInputInteraction() {
    dismissInputInteractionCalled = true;
    super.dismissInputInteraction();
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Future<void> pumpChatRouteApp(WidgetTester tester) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        initialRoute: AppRoutes.home,
        getPages: [
          GetPage(name: AppRoutes.home, page: () => const SizedBox.shrink()),
          GetPage(
            name: AppRoutes.chat,
            page: () =>
                ChatView(controllerTag: ChatBinding.currentControllerTag()),
            binding: ChatBinding(),
          ),
        ],
      ),
    );
    await tester.pumpAndSettle();
  }

  late _FakeImService imService;
  late _FakeAuthService authService;
  late _FakeAgentService agentService;
  late _FakeSessionService sessionService;
  late _FakeOssService ossService;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    imService = _FakeImService();
    authService = _FakeAuthService(loggedIn: true, id: '42');
    agentService = _FakeAgentService();
    sessionService = _FakeSessionService();
    ossService = _FakeOssService();

    Get.put<ImService>(imService);
    Get.put<AuthService>(authService);
    Get.put<AgentService>(agentService);
    Get.put<SessionService>(sessionService);
    Get.put<OssService>(ossService);
  });

  tearDown(() async {
    Get.reset();
  });

  testWidgets('sendMessage trims content and clears input', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_1';
    controller.chatTitle = 'session_test_1';
    controller.chatType = 'private';
    controller.inputController.text = '  hello world  ';

    controller.sendMessage();
    await tester.pump(const Duration(milliseconds: 120));

    expect(imService.sendCalls, 1);
    expect(imService.sentContent, 'hello world');
    expect(imService.sentSessionId, 'session_test_1');
    expect(controller.inputController.text, isEmpty);
  });

  testWidgets('input draft persists while typing before controller closes', (
    WidgetTester tester,
  ) async {
    final controller = ChatController();
    addTearDown(() {
      if (!controller.isClosed) {
        controller.onClose();
      }
    });
    controller.sessionId = 'session_draft_live';
    controller.chatTitle = 'session_draft_live';
    controller.chatType = 'private';
    controller.onReady();

    controller.inputController.text = 'draft in progress';
    await tester.pump(const Duration(milliseconds: 220));

    final prefs = await SharedPreferences.getInstance();
    expect(
      prefs.getString('chat_draft_42_session_draft_live'),
      'draft in progress',
    );
  });

  testWidgets('draft restores immediately after recreating chat controller', (
    WidgetTester tester,
  ) async {
    final controller = ChatController();
    var controllerClosed = false;
    addTearDown(() {
      if (!controllerClosed) {
        controller.onClose();
      }
    });
    controller.sessionId = 'session_draft_restore';
    controller.chatTitle = 'session_draft_restore';
    controller.chatType = 'private';
    controller.onReady();

    controller.inputController.text = 'keep me';
    await tester.pump();
    controller.onClose();
    controllerClosed = true;

    final reopened = ChatController();
    addTearDown(() {
      if (!reopened.isClosed) {
        reopened.onClose();
      }
    });
    reopened.sessionId = 'session_draft_restore';
    reopened.chatTitle = 'session_draft_restore';
    reopened.chatType = 'private';
    reopened.onReady();
    await tester.pump();

    expect(reopened.inputController.text, 'keep me');
  });

  testWidgets(
    'staged attachment draft restores after recreating chat controller',
    (WidgetTester tester) async {
      final controller = ChatController();
      var controllerClosed = false;
      addTearDown(() {
        if (!controllerClosed) {
          controller.onClose();
        }
      });
      controller.sessionId = 'session_attach_draft_restore';
      controller.chatTitle = 'session_attach_draft_restore';
      controller.chatType = 'private';
      controller.onReady();

      // 仅粘贴图片、不输入文字
      controller.stagedAttachments.add(
        PendingAttachmentUpload(
          type: ChatAttachmentType.image,
          fileName: 'clipboard.png',
          contentType: 'image/png',
          bytes: Uint8List.fromList(<int>[1, 2, 3, 4]),
        ),
      );
      await tester.pump();
      controller.onClose();
      controllerClosed = true;

      final reopened = ChatController();
      addTearDown(() {
        if (!reopened.isClosed) {
          reopened.onClose();
        }
      });
      reopened.sessionId = 'session_attach_draft_restore';
      reopened.chatTitle = 'session_attach_draft_restore';
      reopened.chatType = 'private';
      reopened.onReady();
      await tester.pump();

      expect(reopened.stagedAttachments.length, 1);
      expect(reopened.stagedAttachments.first.fileName, 'clipboard.png');
      expect(reopened.inputController.text, isEmpty);
    },
  );

  testWidgets(
    'stream-only attachment draft is skipped across controller restart',
    (WidgetTester tester) async {
      final controller = ChatController();
      var controllerClosed = false;
      addTearDown(() {
        if (!controllerClosed) {
          controller.onClose();
        }
      });
      controller.sessionId = 'session_attach_stream_skip';
      controller.chatTitle = 'session_attach_stream_skip';
      controller.chatType = 'private';
      controller.onReady();

      // 一张带字节的图片 + 一个仅有流的视频（如 Web 选取的视频）
      controller.stagedAttachments.addAll(<PendingAttachmentUpload>[
        PendingAttachmentUpload(
          type: ChatAttachmentType.image,
          fileName: 'pic.png',
          contentType: 'image/png',
          bytes: Uint8List.fromList(<int>[1, 2, 3]),
        ),
        PendingAttachmentUpload(
          type: ChatAttachmentType.video,
          fileName: 'clip.mp4',
          contentType: 'video/mp4',
          stream: Stream<List<int>>.fromIterable(<List<int>>[
            <int>[1],
          ]),
          contentLength: 1,
        ),
      ]);
      await tester.pump();
      controller.onClose();
      controllerClosed = true;

      final reopened = ChatController();
      addTearDown(() {
        if (!reopened.isClosed) {
          reopened.onClose();
        }
      });
      reopened.sessionId = 'session_attach_stream_skip';
      reopened.chatTitle = 'session_attach_stream_skip';
      reopened.chatType = 'private';
      reopened.onReady();
      await tester.pump();

      // 仅恢复带字节的图片，流式视频被跳过
      expect(reopened.stagedAttachments.length, 1);
      expect(reopened.stagedAttachments.first.fileName, 'pic.png');
    },
  );

  testWidgets('sending message clears any queued draft persistence', (
    WidgetTester tester,
  ) async {
    final controller = ChatController();
    addTearDown(() {
      if (!controller.isClosed) {
        controller.onClose();
      }
    });
    controller.sessionId = 'session_draft_send_clear';
    controller.chatTitle = 'session_draft_send_clear';
    controller.chatType = 'private';
    controller.onReady();

    controller.inputController.text = 'send and clear';
    controller.sendMessage();
    await tester.pump(const Duration(milliseconds: 260));

    final prefs = await SharedPreferences.getInstance();
    expect(prefs.getString('chat_draft_42_session_draft_send_clear'), isNull);
  });

  testWidgets('draft restores after leaving chat route and reopening it', (
    WidgetTester tester,
  ) async {
    Get.testMode = false;
    sessionService.detailResult = const SessionDetailResult(
      data: {'session_type': 1},
    );

    await pumpChatRouteApp(tester);

    unawaited(
      ChatRouteNavigator.toChat(
        sessionId: 'session_route_draft_a',
        title: 'A',
        type: 'private',
      ),
    );
    await tester.pumpAndSettle();

    await tester.enterText(find.byType(TextField), 'route draft text');
    await tester.pump(const Duration(milliseconds: 220));

    unawaited(
      ChatRouteNavigator.toChat(
        sessionId: 'session_route_draft_b',
        title: 'B',
        type: 'private',
      ),
    );
    await tester.pumpAndSettle();

    unawaited(
      ChatRouteNavigator.toChat(
        sessionId: 'session_route_draft_a',
        title: 'A',
        type: 'private',
      ),
    );
    await tester.pumpAndSettle();

    final controller = Get.find<ChatController>(
      tag: ChatBinding.controllerTagForSession('session_route_draft_a'),
    );
    expect(controller.sessionId, 'session_route_draft_a');
    expect(controller.inputController.text, 'route draft text');

    expect(
      await Get.delete<ChatController>(
        tag: ChatBinding.controllerTagForSession('session_route_draft_a'),
        force: true,
      ),
      isTrue,
    );
    await tester.pumpAndSettle();
  });

  testWidgets('draft restores after immediate back to list and reopen', (
    WidgetTester tester,
  ) async {
    Get.testMode = false;
    sessionService.detailResult = const SessionDetailResult(
      data: {'session_type': 1},
    );

    await pumpChatRouteApp(tester);

    unawaited(
      ChatRouteNavigator.toChat(
        sessionId: 'session_route_draft_back',
        title: 'Back',
        type: 'private',
      ),
    );
    await tester.pumpAndSettle();

    await tester.enterText(find.byType(TextField), 'back draft text');
    await tester.pump();

    final controller = Get.find<ChatController>(
      tag: ChatBinding.controllerTagForSession('session_route_draft_back'),
    );
    controller.closeChatRoute();
    await tester.pumpAndSettle();

    unawaited(
      ChatRouteNavigator.toChat(
        sessionId: 'session_route_draft_back',
        title: 'Back',
        type: 'private',
      ),
    );
    await tester.pumpAndSettle();

    final reopened = Get.find<ChatController>(
      tag: ChatBinding.controllerTagForSession('session_route_draft_back'),
    );
    expect(reopened.inputController.text, 'back draft text');

    expect(
      await Get.delete<ChatController>(
        tag: ChatBinding.controllerTagForSession('session_route_draft_back'),
        force: true,
      ),
      isTrue,
    );
    await tester.pumpAndSettle();
  });

  testWidgets('draft restores after tapping app bar back and reopening', (
    WidgetTester tester,
  ) async {
    Get.testMode = false;
    sessionService.detailResult = const SessionDetailResult(
      data: {'session_type': 1},
    );

    await pumpChatRouteApp(tester);

    unawaited(
      ChatRouteNavigator.toChat(
        sessionId: 'session_route_draft_back_button',
        title: 'BackButton',
        type: 'private',
      ),
    );
    await tester.pumpAndSettle();

    await tester.enterText(find.byType(TextField), 'button draft text');
    await tester.pump();

    await tester.tap(find.byIcon(Icons.arrow_back_ios_rounded));
    await tester.pumpAndSettle();

    unawaited(
      ChatRouteNavigator.toChat(
        sessionId: 'session_route_draft_back_button',
        title: 'BackButton',
        type: 'private',
      ),
    );
    await tester.pumpAndSettle();

    final reopened = Get.find<ChatController>(
      tag: ChatBinding.controllerTagForSession(
        'session_route_draft_back_button',
      ),
    );
    expect(reopened.inputController.text, 'button draft text');

    expect(
      await Get.delete<ChatController>(
        tag: ChatBinding.controllerTagForSession(
          'session_route_draft_back_button',
        ),
        force: true,
      ),
      isTrue,
    );
    await tester.pumpAndSettle();
  });

  testWidgets('drafts stay isolated between different chat sessions', (
    WidgetTester tester,
  ) async {
    Get.testMode = false;
    sessionService.detailResult = const SessionDetailResult(
      data: {'session_type': 1},
    );

    await pumpChatRouteApp(tester);

    unawaited(
      ChatRouteNavigator.toChat(
        sessionId: 'session_route_draft_isolated_a',
        title: 'IsolatedA',
        type: 'private',
      ),
    );
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'draft A');
    await tester.pump();

    unawaited(
      ChatRouteNavigator.toChat(
        sessionId: 'session_route_draft_isolated_b',
        title: 'IsolatedB',
        type: 'private',
      ),
    );
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'draft B');
    await tester.pump();

    unawaited(
      ChatRouteNavigator.toChat(
        sessionId: 'session_route_draft_isolated_a',
        title: 'IsolatedA',
        type: 'private',
      ),
    );
    await tester.pumpAndSettle();

    final reopenedA = Get.find<ChatController>(
      tag: ChatBinding.controllerTagForSession(
        'session_route_draft_isolated_a',
      ),
    );
    expect(reopenedA.inputController.text, 'draft A');

    unawaited(
      ChatRouteNavigator.toChat(
        sessionId: 'session_route_draft_isolated_b',
        title: 'IsolatedB',
        type: 'private',
      ),
    );
    await tester.pumpAndSettle();

    final reopenedB = Get.find<ChatController>(
      tag: ChatBinding.controllerTagForSession(
        'session_route_draft_isolated_b',
      ),
    );
    expect(reopenedB.inputController.text, 'draft B');

    expect(
      await Get.delete<ChatController>(
        tag: ChatBinding.controllerTagForSession(
          'session_route_draft_isolated_b',
        ),
        force: true,
      ),
      isTrue,
    );
    await tester.pumpAndSettle();
  });

  testWidgets('group chat route still shows mention-all after route replace', (
    WidgetTester tester,
  ) async {
    Get.testMode = false;
    sessionService.detailResult = const SessionDetailResult(
      data: {'session_type': 1},
    );

    await pumpChatRouteApp(tester);

    unawaited(
      ChatRouteNavigator.toChat(
        sessionId: 'session_route_private',
        title: 'private',
        type: 'private',
      ),
    );
    await tester.pumpAndSettle();

    sessionService.detailResult = const SessionDetailResult(
      data: {
        'session_type': 2,
        'member_count': 4,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 3, 'nickname': '我'},
          {'member_id': '1002', 'member_type': 1, 'role': 1, 'nickname': '成员甲'},
          {'member_id': '1003', 'member_type': 1, 'role': 1, 'nickname': '成员乙'},
          {'member_id': '9001', 'member_type': 2, 'role': 1},
        ],
      },
    );

    unawaited(
      ChatRouteNavigator.toChat(
        sessionId: 'session_route_group',
        title: 'group',
        type: 'group',
      ),
    );
    await tester.pumpAndSettle();

    final controller = Get.find<ChatController>(
      tag: ChatBinding.controllerTagForSession('session_route_group'),
    );
    expect(controller.sessionId, 'session_route_group');
    expect(controller.chatType, 'group');

    await tester.tap(find.byType(TextField));
    await tester.pump();
    await tester.enterText(find.byType(TextField), '@');
    await tester.pump();

    expect(find.text('所有人'), findsOneWidget);

    Get.back<void>();
    await tester.pumpAndSettle();
  });

  testWidgets(
    'uploadPreparedAttachmentsForTest uploads all attachments then stages them',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_upload_batch';
      controller.chatTitle = 'session_upload_batch';
      controller.chatType = 'private';

      await controller.uploadPreparedAttachmentsForTest(
        <ChatPreparedAttachmentUpload>[
          ChatPreparedAttachmentUpload(
            type: ChatAttachmentType.image,
            fileName: 'a.png',
            contentType: 'image/png',
            bytes: Uint8List.fromList(<int>[1, 2, 3]),
          ),
          ChatPreparedAttachmentUpload(
            type: ChatAttachmentType.file,
            fileName: 'b.pdf',
            contentType: 'application/pdf',
            bytes: Uint8List.fromList(<int>[4, 5, 6]),
          ),
        ],
      );
      await tester.pump(const Duration(milliseconds: 120));

      expect(ossService.presignCalls, 2);
      expect(ossService.uploadCalls, 2);
      expect(imService.sendCalls, 0);
      expect(ossService.deletedObjectKeys, isEmpty);
      expect(controller.stagedAttachments, hasLength(2));
      expect(controller.stagedAttachments[0].fileName, 'a.png');
      expect(controller.stagedAttachments[1].fileName, 'b.pdf');
      expect(controller.isUploadingAttachment, isFalse);
    },
  );

  testWidgets(
    'uploadPreparedAttachmentsForTest does not send message when one upload fails',
    (WidgetTester tester) async {
      ossService.failUploadAtCalls.add(2);
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_upload_batch_fail';
      controller.chatTitle = 'session_upload_batch_fail';
      controller.chatType = 'private';

      await controller.uploadPreparedAttachmentsForTest(
        <ChatPreparedAttachmentUpload>[
          ChatPreparedAttachmentUpload(
            type: ChatAttachmentType.image,
            fileName: 'a.png',
            contentType: 'image/png',
            bytes: Uint8List.fromList(<int>[1, 2, 3]),
          ),
          ChatPreparedAttachmentUpload(
            type: ChatAttachmentType.file,
            fileName: 'b.pdf',
            contentType: 'application/pdf',
            bytes: Uint8List.fromList(<int>[4, 5, 6]),
          ),
        ],
      );
      await tester.pump(const Duration(milliseconds: 120));

      expect(ossService.presignCalls, 2);
      expect(ossService.uploadCalls, 2);
      expect(imService.sendCalls, 0);
      expect(ossService.deletedObjectKeys, <String>[
        'aibot/media/user/42/a.png',
      ]);
      expect(controller.stagedAttachments, isEmpty);
      expect(controller.isUploadingAttachment, isFalse);
    },
  );

  testWidgets(
    'dispatchCurrentInputMessage sends text with staged attachments',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_staged_send';
      controller.chatTitle = 'session_staged_send';
      controller.chatType = 'private';
      controller.inputController.text = 'check this out';

      controller.stagedAttachments.add(
        PendingAttachmentUpload(
          type: ChatAttachmentType.image,
          fileName: 'photo.jpg',
          contentType: 'image/jpeg',
          bytes: Uint8List.fromList(<int>[1, 2, 3]),
        ),
      );

      final result = controller.dispatchCurrentInputMessage();
      expect(result, isTrue);

      // Upload+send is async; pump to let it complete.
      await tester.pump(const Duration(milliseconds: 120));

      expect(ossService.presignCalls, 1);
      expect(ossService.uploadCalls, 1);
      expect(imService.sendCalls, 1);
      expect(imService.sentContent, contains('photo.jpg'));
      expect(imService.sentContent, contains('check this out'));
      expect(controller.inputController.text, isEmpty);
      expect(controller.stagedAttachments, isEmpty);
      expect(controller.isUploadingAttachment, isFalse);
    },
  );

  testWidgets(
    'dispatchCurrentInputMessage sends staged attachments without text',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_staged_notext';
      controller.chatTitle = 'session_staged_notext';
      controller.chatType = 'private';
      controller.inputController.text = '';

      controller.stagedAttachments.add(
        PendingAttachmentUpload(
          type: ChatAttachmentType.file,
          fileName: 'doc.pdf',
          contentType: 'application/pdf',
          bytes: Uint8List.fromList(<int>[10, 20, 30]),
        ),
      );

      controller.dispatchCurrentInputMessage();
      await tester.pump(const Duration(milliseconds: 120));

      expect(ossService.presignCalls, 1);
      expect(ossService.uploadCalls, 1);
      expect(imService.sendCalls, 1);
      expect(imService.sentContent, contains('doc.pdf'));
      expect(controller.inputController.text, isEmpty);
      expect(controller.stagedAttachments, isEmpty);
    },
  );

  testWidgets(
    'dispatchCurrentInputMessage preserves content when upload fails',
    (WidgetTester tester) async {
      ossService.failUploadAtCalls.add(1);
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_staged_fail';
      controller.chatTitle = 'session_staged_fail';
      controller.chatType = 'private';
      controller.inputController.text = 'should survive';

      controller.stagedAttachments.add(
        PendingAttachmentUpload(
          type: ChatAttachmentType.image,
          fileName: 'fail.png',
          contentType: 'image/png',
          bytes: Uint8List.fromList(<int>[7, 8, 9]),
        ),
      );

      controller.dispatchCurrentInputMessage();
      await tester.pump(const Duration(milliseconds: 120));

      expect(imService.sendCalls, 0);
      // Content should still be present for retry.
      expect(controller.inputController.text, 'should survive');
      expect(controller.stagedAttachments, hasLength(1));
      expect(controller.stagedAttachments[0].fileName, 'fail.png');
      expect(controller.isUploadingAttachment, isFalse);
    },
  );

  testWidgets('dispatchCurrentInputMessage locks UI during upload', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_staged_lock';
    controller.chatTitle = 'session_staged_lock';
    controller.chatType = 'private';
    controller.inputController.text = 'hello';

    controller.stagedAttachments.add(
      PendingAttachmentUpload(
        type: ChatAttachmentType.image,
        fileName: 'lock.jpg',
        contentType: 'image/jpeg',
        bytes: Uint8List.fromList(<int>[1]),
      ),
    );

    // Kick off the async upload+send but do NOT pump yet.
    controller.dispatchCurrentInputMessage();

    // At this point the upload flag should be true (synchronous part of
    // the async method runs immediately until the first await).
    expect(controller.isUploadingAttachment, isTrue);

    // A second dispatch should be rejected while uploading.
    final secondResult = controller.dispatchCurrentInputMessage();
    expect(secondResult, isFalse);

    // Let upload complete.
    await tester.pump(const Duration(milliseconds: 120));
    expect(controller.isUploadingAttachment, isFalse);
    expect(imService.sendCalls, 1);
  });

  testWidgets(
    'dispatchCurrentInputMessage rollback on partial multi-upload failure',
    (WidgetTester tester) async {
      ossService.failUploadAtCalls.add(2);
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_staged_partial';
      controller.chatTitle = 'session_staged_partial';
      controller.chatType = 'private';
      controller.inputController.text = 'two files';

      controller.stagedAttachments.addAll(<PendingAttachmentUpload>[
        PendingAttachmentUpload(
          type: ChatAttachmentType.image,
          fileName: 'first.png',
          contentType: 'image/png',
          bytes: Uint8List.fromList(<int>[1]),
        ),
        PendingAttachmentUpload(
          type: ChatAttachmentType.file,
          fileName: 'second.pdf',
          contentType: 'application/pdf',
          bytes: Uint8List.fromList(<int>[2]),
        ),
      ]);

      controller.dispatchCurrentInputMessage();
      await tester.pump(const Duration(milliseconds: 120));

      expect(imService.sendCalls, 0);
      // First upload succeeded and should be rolled back.
      expect(ossService.deletedObjectKeys, <String>[
        'aibot/media/user/42/first.png',
      ]);
      // Content preserved for retry.
      expect(controller.inputController.text, 'two files');
      expect(controller.stagedAttachments, hasLength(2));
    },
  );

  testWidgets('sendMessage ignores empty content after trim', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_1';
    controller.chatTitle = 'session_test_1';
    controller.chatType = 'private';
    controller.inputController.text = '   ';

    controller.sendMessage();
    controller.onReady();
    await tester.pump();

    expect(imService.sendCalls, 0);
  });

  testWidgets('sendMessage ignores active input composition', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_composing_send';
    controller.chatTitle = 'session_test_composing_send';
    controller.chatType = 'private';
    controller.inputController.value = const TextEditingValue(
      text: 'hello',
      selection: TextSelection.collapsed(offset: 5),
      composing: TextRange(start: 0, end: 5),
    );

    controller.sendMessage();
    controller.onReady();
    await tester.pump();

    expect(controller.isInputComposing, isTrue);
    expect(imService.sendCalls, 0);
    expect(controller.inputController.text, 'hello');
  });

  testWidgets('insertInputLineBreak defers edits until composition ends', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_deferred_line_break';
    controller.chatTitle = 'session_test_deferred_line_break';
    controller.chatType = 'private';

    controller.onReady();
    await tester.pump();

    controller.inputController.value = const TextEditingValue(
      text: '需要mermaid',
      selection: TextSelection.collapsed(offset: 9),
      composing: TextRange(start: 2, end: 9),
    );
    await tester.pump();

    controller.insertInputLineBreak();
    await tester.pump();

    expect(controller.inputController.text, '需要mermaid');
    expect(
      controller.inputController.selection,
      const TextSelection.collapsed(offset: 9),
    );

    controller.inputController.value = const TextEditingValue(
      text: '需要mermaid',
      selection: TextSelection.collapsed(offset: 9),
    );
    await tester.pump();

    expect(controller.inputController.text, '需要mermaid\n');
    expect(
      controller.inputController.selection,
      const TextSelection.collapsed(offset: 10),
    );

    controller.inputController.clear();
    await tester.pump();
    Get.find<ImService>().updateSessionComposing(
      controller.sessionId,
      active: false,
    );
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets(
    'submitMessageFromInputAction inserts a line break after suppression clears',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_composing_submit';
      controller.chatTitle = 'session_test_composing_submit';
      controller.chatType = 'private';
      controller.inputController.value = const TextEditingValue(
        text: 'hello',
        selection: TextSelection.collapsed(offset: 5),
        composing: TextRange(start: 0, end: 5),
      );

      controller.suppressNextInputSubmit();
      controller.submitMessageFromInputAction();
      controller.onReady();
      await tester.pump();

      expect(imService.sendCalls, 0);
      expect(controller.inputController.text, 'hello');

      controller.inputController.value = const TextEditingValue(
        text: 'hello',
        selection: TextSelection.collapsed(offset: 5),
      );
      controller.clearPendingInputSubmitSuppressionForNewKeyPress();
      controller.onReady();
      await tester.pump();

      controller.submitMessageFromInputAction();
      await tester.pump();

      expect(imService.sendCalls, 0);
      expect(controller.inputController.text, 'hello\n');
      expect(
        controller.inputController.selection,
        const TextSelection.collapsed(offset: 6),
      );

      controller.inputController.clear();
      await tester.pump();
      Get.find<ImService>().updateSessionComposing(
        controller.sessionId,
        active: false,
      );
      await tester.pump(const Duration(milliseconds: 600));
    },
  );

  testWidgets(
    'submitMessageFromHardwareEnter suppresses the following input action',
    (WidgetTester tester) async {
      final controller = Get.put(_SpyChatController());
      controller.sessionId = 'session_test_hardware_enter_submit';
      controller.chatTitle = 'session_test_hardware_enter_submit';
      controller.chatType = 'private';
      controller.inputController.text = 'hello';

      controller.submitMessageFromHardwareEnter();
      await tester.pump(const Duration(milliseconds: 120));

      expect(controller.dispatchCurrentInputMessageInvocations, 1);
      expect(imService.sendCalls, 1);
      expect(imService.sentContent, 'hello');
      expect(controller.inputController.text, isEmpty);

      controller.submitMessageFromInputAction();
      await tester.pump();

      expect(controller.dispatchCurrentInputMessageInvocations, 1);
      expect(imService.sendCalls, 1);

      controller.inputController.text = 'next';
      controller.clearPendingInputSubmitSuppressionForNewKeyPress();
      await tester.pump(const Duration(milliseconds: 1300));
      controller.submitMessageFromInputAction();
      await tester.pump();

      expect(controller.dispatchCurrentInputMessageInvocations, 1);
      expect(imService.sendCalls, 1);
      expect(imService.sentContent, 'hello');
      expect(controller.inputController.text, 'next\n');
    },
  );

  testWidgets(
    'submit keeps retained keyboard inset when web obstruction briefly drops',
    (WidgetTester tester) async {
      final obstructionObserver = _FakeChatBottomObstructionObserver();
      final controller = Get.put(
        ChatController(bottomObstructionObserver: obstructionObserver),
      );
      controller.sessionId = 'session_test_submit_obstruction_drop';
      controller.chatTitle = 'session_test_submit_obstruction_drop';
      controller.chatType = 'private';
      controller.onReady();

      await tester.pumpWidget(
        GetMaterialApp(
          home: Material(
            child: TextField(
              controller: controller.inputController,
              focusNode: controller.focusNode,
            ),
          ),
        ),
      );
      await tester.pump();

      controller.focusNode.requestFocus();
      await tester.pump();

      obstructionObserver.emit(260);
      await tester.pump();

      controller.inputController.text = 'hello';
      controller.submitMessageFromHardwareEnter();
      await tester.pump(const Duration(milliseconds: 120));

      expect(imService.sendCalls, 1);
      expect(controller.inputLayoutKeyboardInsetBottom, 260);

      obstructionObserver.emit(0);
      await tester.pump();

      expect(controller.platformViewportObstructionBottom, 0);
      expect(controller.inputLayoutKeyboardInsetBottom, 260);

      await tester.pump(const Duration(milliseconds: 400));
      await tester.pump();

      // 前序测试在无 overlay 环境触发的 CustomToast 重试会在本测试的
      // GetMaterialApp 中落地并启动 3 秒消失计时器，这里排空避免 pending timer。
      await tester.pump(const Duration(seconds: 3));
      await tester.pump();
    },
  );

  testWidgets('keyboard dock tracking only follows the chat composer focus', (
    WidgetTester tester,
  ) async {
    addTearDown(tester.view.reset);
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1.0;

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_focus_scoped_keyboard_dock';
    controller.chatTitle = 'Chat';
    controller.chatType = 'private';
    controller.onReady();

    final externalFocusNode = FocusNode(debugLabel: 'card_input');
    addTearDown(externalFocusNode.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: Material(
          child: Column(
            children: [
              TextField(
                controller: controller.inputController,
                focusNode: controller.focusNode,
              ),
              TextField(focusNode: externalFocusNode),
            ],
          ),
        ),
      ),
    );
    await tester.pump();

    await tester.tap(find.byType(TextField).first);
    await tester.pump();
    expect(controller.focusNode.hasFocus, isTrue);

    tester.view.viewInsets = const FakeViewPadding(bottom: 260);
    await tester.pump();
    expect(controller.shouldFollowKeyboardForInputDock, isTrue);
    expect(controller.inputLayoutKeyboardInsetBottom, 260);

    await tester.tap(find.byType(TextField).last);
    await tester.pump();
    expect(controller.focusNode.hasFocus, isFalse);
    expect(externalFocusNode.hasFocus, isTrue);
    expect(controller.shouldFollowKeyboardForInputDock, isFalse);
    expect(controller.inputLayoutKeyboardInsetBottom, 0);

    await tester.pump(const Duration(milliseconds: 220));
  });

  testWidgets(
    'iOS keyboard policy keeps dock inset briefly when focused keyboard metrics drop to zero',
    (WidgetTester tester) async {
      addTearDown(tester.view.reset);
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1.0;

      final controller = Get.put(
        ChatController(
          keyboardPlatformBehavior: ChatKeyboardPlatformBehavior.resolve(
            isWeb: false,
            targetPlatform: TargetPlatform.iOS,
          ),
        ),
      );
      controller.sessionId = 'session_test_ios_keyboard_hysteresis';
      controller.chatTitle = 'Chat';
      controller.chatType = 'private';
      controller.onReady();

      await tester.pumpWidget(
        MaterialApp(
          home: Material(
            child: TextField(
              controller: controller.inputController,
              focusNode: controller.focusNode,
            ),
          ),
        ),
      );
      await tester.pump();

      await tester.tap(find.byType(TextField));
      await tester.pump();
      expect(controller.focusNode.hasFocus, isTrue);

      tester.view.viewInsets = const FakeViewPadding(bottom: 260);
      await tester.pump();
      expect(controller.inputLayoutKeyboardInsetBottom, 260);

      tester.view.viewInsets = FakeViewPadding.zero;
      await tester.pump();
      expect(controller.inputLayoutKeyboardInsetBottom, 260);

      // Hysteresis timer fires after 150ms, but since focus is still active
      // (Sogou voice-mode fix), the inset is kept until focus is lost.
      await tester.pump(const Duration(milliseconds: 170));
      expect(controller.inputLayoutKeyboardInsetBottom, 260);

      // Losing focus triggers the real drop.
      controller.focusNode.unfocus();
      await tester.pump();
      expect(controller.inputLayoutKeyboardInsetBottom, 0);
    },
  );

  testWidgets(
    'iOS IME rebuild: input refocuses after background-with-focus then resume',
    (WidgetTester tester) async {
      addTearDown(tester.view.reset);
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1.0;

      final controller = Get.put(
        ChatController(
          keyboardPlatformBehavior: ChatKeyboardPlatformBehavior.resolve(
            isWeb: false,
            targetPlatform: TargetPlatform.iOS,
          ),
        ),
      );
      controller.sessionId = 'session_test_ios_ime_rebuild';
      controller.chatTitle = 'Chat';
      controller.chatType = 'private';
      controller.onReady();

      await tester.pumpWidget(
        MaterialApp(
          home: Material(
            child: TextField(
              controller: controller.inputController,
              focusNode: controller.focusNode,
            ),
          ),
        ),
      );
      await tester.pump();

      await tester.tap(find.byType(TextField));
      await tester.pump();
      expect(controller.focusNode.hasFocus, isTrue);

      // 切到后台（记录当时在打字），再切回前台。
      controller.didChangeAppLifecycleState(AppLifecycleState.paused);
      controller.didChangeAppLifecycleState(AppLifecycleState.resumed);

      // resumed 立即丢焦点（关闭失效的旧连接）。
      await tester.pump();
      expect(controller.focusNode.hasFocus, isFalse);

      // 100ms 后重新请求焦点，重建输入连接。
      await tester.pump(const Duration(milliseconds: 120));
      expect(controller.focusNode.hasFocus, isTrue);
    },
  );

  testWidgets(
    'iOS IME rebuild: no refocus when input was not focused before background',
    (WidgetTester tester) async {
      addTearDown(tester.view.reset);
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1.0;

      final controller = Get.put(
        ChatController(
          keyboardPlatformBehavior: ChatKeyboardPlatformBehavior.resolve(
            isWeb: false,
            targetPlatform: TargetPlatform.iOS,
          ),
        ),
      );
      controller.sessionId = 'session_test_ios_ime_no_rebuild';
      controller.chatTitle = 'Chat';
      controller.chatType = 'private';
      controller.onReady();

      await tester.pumpWidget(
        MaterialApp(
          home: Material(
            child: TextField(
              controller: controller.inputController,
              focusNode: controller.focusNode,
            ),
          ),
        ),
      );
      await tester.pump();
      expect(controller.focusNode.hasFocus, isFalse);

      controller.didChangeAppLifecycleState(AppLifecycleState.paused);
      controller.didChangeAppLifecycleState(AppLifecycleState.resumed);

      await tester.pump(const Duration(milliseconds: 120));
      // 切出去前没在打字 → 不应主动抢焦点。
      expect(controller.focusNode.hasFocus, isFalse);
    },
  );

  testWidgets('Non-iOS: lifecycle resume does not touch input focus', (
    WidgetTester tester,
  ) async {
    addTearDown(tester.view.reset);
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1.0;

    final controller = Get.put(
      ChatController(
        keyboardPlatformBehavior: ChatKeyboardPlatformBehavior.resolve(
          isWeb: false,
          targetPlatform: TargetPlatform.android,
        ),
      ),
    );
    controller.sessionId = 'session_test_android_ime_noop';
    controller.chatTitle = 'Chat';
    controller.chatType = 'private';
    controller.onReady();

    await tester.pumpWidget(
      MaterialApp(
        home: Material(
          child: TextField(
            controller: controller.inputController,
            focusNode: controller.focusNode,
          ),
        ),
      ),
    );
    await tester.pump();

    await tester.tap(find.byType(TextField));
    await tester.pump();
    expect(controller.focusNode.hasFocus, isTrue);

    controller.didChangeAppLifecycleState(AppLifecycleState.paused);
    controller.didChangeAppLifecycleState(AppLifecycleState.resumed);

    // 非 iOS：不做 unfocus/refocus，焦点保持不变。
    await tester.pump(const Duration(milliseconds: 120));
    expect(controller.focusNode.hasFocus, isTrue);
  });

  testWidgets(
    'Android keyboard policy drops dock inset immediately when keyboard metrics drop to zero',
    (WidgetTester tester) async {
      addTearDown(tester.view.reset);
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1.0;

      final controller = Get.put(
        ChatController(
          keyboardPlatformBehavior: ChatKeyboardPlatformBehavior.resolve(
            isWeb: false,
            targetPlatform: TargetPlatform.android,
          ),
        ),
      );
      controller.sessionId = 'session_test_android_keyboard_drop';
      controller.chatTitle = 'Chat';
      controller.chatType = 'private';
      controller.onReady();

      await tester.pumpWidget(
        MaterialApp(
          home: Material(
            child: TextField(
              controller: controller.inputController,
              focusNode: controller.focusNode,
            ),
          ),
        ),
      );
      await tester.pump();

      await tester.tap(find.byType(TextField));
      await tester.pump();
      expect(controller.focusNode.hasFocus, isTrue);

      tester.view.viewInsets = const FakeViewPadding(bottom: 260);
      await tester.pump();
      expect(controller.inputLayoutKeyboardInsetBottom, 260);

      tester.view.viewInsets = FakeViewPadding.zero;
      await tester.pump();
      expect(controller.inputLayoutKeyboardInsetBottom, 0);

      await tester.pump(const Duration(milliseconds: 100));
    },
  );

  testWidgets('buildForwardTargetOptions uses private peer nickname', (
    WidgetTester tester,
  ) async {
    final friendService = _FakeFriendService();
    friendService.nicknames['1001'] = '备注名';
    Get.put<FriendService>(friendService);

    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'session_private_1',
        title: 'hi',
        type: 'private',
        peerId: '1001',
        peerType: 1,
        updatedAt: 10,
        lastMessage: '最后一条消息',
        lastMessageTime: 20,
      ),
    ]);

    final controller = Get.put(ChatController());
    final options = controller.buildForwardTargetOptions();

    expect(options, hasLength(1));
    expect(options.first.title, '备注名');
    expect(options.first.subtitle, '最后一条消息');
  });

  testWidgets(
    'buildForwardTargetOptions prefers session peer nickname over session title',
    (WidgetTester tester) async {
      final friendService = _FakeFriendService();
      friendService.nicknames['1001'] = '好友缓存昵称';
      Get.put<FriendService>(friendService);

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'session_private_2',
          title: 'hello',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          peerNickname: '会话昵称',
          updatedAt: 10,
          lastMessage: 'preview',
          lastMessageTime: 20,
        ),
      ]);

      final controller = Get.put(ChatController());
      final options = controller.buildForwardTargetOptions();

      expect(options, hasLength(1));
      expect(options.first.title, '会话昵称');
    },
  );

  testWidgets('buildForwardTargetOptions keeps empty summary empty', (
    WidgetTester tester,
  ) async {
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'session_private_3',
        title: 'raw title',
        type: 'private',
        peerId: '1002',
        peerType: 1,
        peerNickname: 'Alice',
        updatedAt: 10,
        lastMessage: '',
        lastMessageTime: 20,
      ),
    ]);

    final controller = Get.put(ChatController());
    final options = controller.buildForwardTargetOptions();

    expect(options, hasLength(1));
    expect(options.first.subtitle, isEmpty);
  });

  testWidgets('forward selection only notifies the toggled message flag', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    final first = MessageModel(
      msgId: 'forward-flag-1',
      sessionId: 'source-session',
      senderId: '1001',
      content: 'first',
      createdAt: 1,
    );
    final second = MessageModel(
      msgId: 'forward-flag-2',
      sessionId: 'source-session',
      senderId: '1002',
      content: 'second',
      createdAt: 2,
    );
    final firstKey = ChatMessageIdentity.selectionKey(first);
    final secondKey = ChatMessageIdentity.selectionKey(second);
    final firstFlag = controller.forwardSelectionFlagByKey(firstKey);
    final secondFlag = controller.forwardSelectionFlagByKey(secondKey);
    var firstNotifications = 0;
    var secondNotifications = 0;
    final firstWorker = ever<bool>(firstFlag, (_) {
      firstNotifications++;
    });
    final secondWorker = ever<bool>(secondFlag, (_) {
      secondNotifications++;
    });
    addTearDown(() {
      firstWorker.dispose();
      secondWorker.dispose();
    });

    controller.beginForwardSelection(first);
    await tester.pump();

    expect(controller.isForwardSelectionMode, isTrue);
    expect(controller.isForwardMessageSelectedByKey(firstKey), isTrue);
    expect(controller.isForwardMessageSelectedByKey(secondKey), isFalse);
    expect(firstNotifications, 1);
    expect(secondNotifications, 0);

    controller.toggleForwardMessageSelection(second);
    await tester.pump();

    expect(controller.selectedForwardMessageCount, 2);
    expect(controller.isForwardMessageSelectedByKey(firstKey), isTrue);
    expect(controller.isForwardMessageSelectedByKey(secondKey), isTrue);
    expect(firstNotifications, 1);
    expect(secondNotifications, 1);
  });

  testWidgets('visible-to selection only notifies the toggled member flag', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    final firstFlag = controller.visibleToSelectionFlagByMemberId('2001');
    final secondFlag = controller.visibleToSelectionFlagByMemberId('2002');
    var firstNotifications = 0;
    var secondNotifications = 0;
    final firstWorker = ever<bool>(firstFlag, (_) {
      firstNotifications++;
    });
    final secondWorker = ever<bool>(secondFlag, (_) {
      secondNotifications++;
    });
    addTearDown(() {
      firstWorker.dispose();
      secondWorker.dispose();
    });

    controller.toggleVisibleToMember('2001');
    await tester.pump();

    expect(controller.isMemberSelectedForVisibleTo('2001'), isTrue);
    expect(controller.isMemberSelectedForVisibleTo('2002'), isFalse);
    expect(firstNotifications, 1);
    expect(secondNotifications, 0);

    controller.toggleVisibleToMember('2002');
    await tester.pump();

    expect(controller.isMemberSelectedForVisibleTo('2001'), isTrue);
    expect(controller.isMemberSelectedForVisibleTo('2002'), isTrue);
    expect(firstNotifications, 1);
    expect(secondNotifications, 1);

    controller.toggleVisibleToMember('2001');
    await tester.pump();

    expect(controller.isMemberSelectedForVisibleTo('2001'), isFalse);
    expect(controller.isMemberSelectedForVisibleTo('2002'), isTrue);
    expect(firstNotifications, 2);
    expect(secondNotifications, 1);
  });

  testWidgets('messageListSnapshot rebuilds once per window version', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'snapshot-1',
        sessionId: 'snapshot-session',
        senderId: '1001',
        content: 'first',
        createdAt: 1,
      ),
      MessageModel(
        msgId: 'snapshot-2',
        sessionId: 'snapshot-session',
        senderId: '1002',
        content: 'second',
        createdAt: 2,
      ),
    ]);
    final firstBuildCount = controller.debugMessageListSnapshotBuildCount;
    controller.onMessageListWindowChanged();
    expect(controller.debugMessageListSnapshotBuildCount, firstBuildCount + 1);

    final firstSnapshot = controller.messageListSnapshot;
    final secondSnapshot = controller.messageListSnapshot;

    expect(identical(firstSnapshot, secondSnapshot), isTrue);
    expect(controller.debugMessageListSnapshotBuildCount, firstBuildCount + 1);

    imService.currentMessages.add(
      MessageModel(
        msgId: 'snapshot-3',
        sessionId: 'snapshot-session',
        senderId: '1003',
        content: 'third',
        createdAt: 3,
      ),
    );
    final secondBuildCount = controller.debugMessageListSnapshotBuildCount;
    controller.onMessageListWindowChanged();
    expect(controller.debugMessageListSnapshotBuildCount, secondBuildCount + 1);
    final thirdSnapshot = controller.messageListSnapshot;

    expect(identical(firstSnapshot, thirdSnapshot), isFalse);
    expect(controller.debugMessageListSnapshotBuildCount, secondBuildCount + 1);
  });

  testWidgets('messageListSnapshot reuses card decode for unchanged messages', (
    WidgetTester tester,
  ) async {
    ChatMessageCardCodec.debugResetDecodeFromMessageCount();
    final controller = Get.put(ChatController());
    final execApprovalEnvelope = ChatMessageCardCodec.buildExecApprovalCard(
      approvalId: 'approval-snapshot-cache-1',
      approvalSlug: 'req-snapshot-cache-1',
      command: 'pwd',
      host: 'gateway',
    );
    final openSessionEnvelope = ChatMessageCardCodec.buildAgentOpenSessionCard(
      summaryText: 'open missing cwd',
      detailText: 'send cwd',
    );
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'snapshot-cache-1',
        sessionId: 'snapshot-cache-session',
        senderId: '1001',
        senderType: 2,
        content: execApprovalEnvelope.content,
        extra: execApprovalEnvelope.extra,
        createdAt: 1,
      ),
      MessageModel(
        msgId: 'snapshot-cache-2',
        sessionId: 'snapshot-cache-session',
        senderId: '1002',
        senderType: 2,
        content: openSessionEnvelope.content,
        extra: openSessionEnvelope.extra,
        createdAt: 2,
      ),
    ]);

    controller.onMessageListWindowChanged();
    final firstDecodeCount = ChatMessageCardCodec.debugDecodeFromMessageCount;
    expect(firstDecodeCount, 2);

    controller.onMessageListWindowChanged();
    expect(ChatMessageCardCodec.debugDecodeFromMessageCount, firstDecodeCount);

    imService.currentMessages[1] = imService.currentMessages[1].copyWith(
      content: 'plain text, not a card',
    );
    controller.onMessageListWindowChanged();

    expect(
      ChatMessageCardCodec.debugDecodeFromMessageCount,
      firstDecodeCount + 1,
    );
  });

  testWidgets(
    'friend list updates only bump changed active sender profile versions',
    (WidgetTester tester) async {
      final friendService = _FakeFriendService();
      Get.put<FriendService>(friendService);
      final controller = Get.put(ChatController());
      addTearDown(() {
        if (!controller.isClosed) {
          controller.onClose();
        }
      });
      controller.sessionId = 'session_friend_sender_scope';
      controller.chatTitle = 'session_friend_sender_scope';
      controller.chatType = 'group';
      imService.currentMessages.assignAll([
        MessageModel(
          msgId: 'friend-scope-1',
          sessionId: controller.sessionId,
          senderId: '2001',
          senderType: 1,
          content: 'first',
          createdAt: 1,
        ),
        MessageModel(
          msgId: 'friend-scope-2',
          sessionId: controller.sessionId,
          senderId: '2002',
          senderType: 1,
          content: 'second',
          createdAt: 2,
        ),
      ]);

      controller.onReady();
      await tester.pump();

      final before2001 = controller.senderProfileVersionFor(
        senderId: '2001',
        senderType: 1,
        isMine: false,
      );
      final before2002 = controller.senderProfileVersionFor(
        senderId: '2002',
        senderType: 1,
        isMine: false,
      );

      friendService.friendList.assignAll([
        FriendItem(
          id: 'f-2001',
          userId: '2001',
          username: 'user_2001',
          nickname: 'Alice',
          remarkName: '',
          avatarUrl: '',
        ),
      ]);
      await tester.pump(const Duration(milliseconds: 20));

      final afterInsert2001 = controller.senderProfileVersionFor(
        senderId: '2001',
        senderType: 1,
        isMine: false,
      );
      final afterInsert2002 = controller.senderProfileVersionFor(
        senderId: '2002',
        senderType: 1,
        isMine: false,
      );
      expect(afterInsert2001, greaterThan(before2001));
      expect(afterInsert2002, before2002);

      friendService.friendList[0] = friendService.friendList[0].copyWith(
        remarkName: 'Alice Remark',
      );
      await tester.pump(const Duration(milliseconds: 20));

      final afterUpdate2001 = controller.senderProfileVersionFor(
        senderId: '2001',
        senderType: 1,
        isMine: false,
      );
      final afterUpdate2002 = controller.senderProfileVersionFor(
        senderId: '2002',
        senderType: 1,
        isMine: false,
      );
      expect(afterUpdate2001, greaterThan(afterInsert2001));
      expect(afterUpdate2002, afterInsert2002);

      friendService.friendList.assignAll([
        friendService.friendList.first,
        FriendItem(
          id: 'f-3003',
          userId: '3003',
          username: 'user_3003',
          nickname: 'Unrelated',
          remarkName: '',
          avatarUrl: '',
        ),
      ]);
      await tester.pump(const Duration(milliseconds: 20));

      expect(
        controller.senderProfileVersionFor(
          senderId: '2001',
          senderType: 1,
          isMine: false,
        ),
        afterUpdate2001,
      );
      expect(
        controller.senderProfileVersionFor(
          senderId: '2002',
          senderType: 1,
          isMine: false,
        ),
        afterUpdate2002,
      );
    },
  );

  testWidgets(
    'profile cache updates only bump changed active sender profile versions',
    (WidgetTester tester) async {
      final friendService = _FakeFriendService();
      Get.put<FriendService>(friendService);

      imService.currentMessages.assignAll([
        MessageModel(
          msgId: 'active-2001',
          sessionId: 'session_test_1',
          senderId: '2001',
          senderType: 1,
          content: 'from 2001',
          createdAt: 1,
        ),
        MessageModel(
          msgId: 'active-2002',
          sessionId: 'session_test_1',
          senderId: '2002',
          senderType: 1,
          content: 'from 2002',
          createdAt: 2,
        ),
      ]);

      final controller = Get.put(ChatController());
      addTearDown(() {
        if (!controller.isClosed) {
          controller.onClose();
        }
      });
      controller.sessionId = 'session_test_1';
      controller.chatTitle = 'session';
      controller.chatType = 'group';
      controller.onReady();
      await tester.pump();

      final before2001 = controller.senderProfileVersionFor(
        senderId: '2001',
        senderType: 1,
        isMine: false,
      );
      final before2002 = controller.senderProfileVersionFor(
        senderId: '2002',
        senderType: 1,
        isMine: false,
      );

      friendService.nicknames['2001'] = 'Alice';
      friendService.profileCacheVersion.value++;
      await tester.pump();

      final afterUpdate2001 = controller.senderProfileVersionFor(
        senderId: '2001',
        senderType: 1,
        isMine: false,
      );
      final afterUpdate2002 = controller.senderProfileVersionFor(
        senderId: '2002',
        senderType: 1,
        isMine: false,
      );
      expect(afterUpdate2001, greaterThan(before2001));
      expect(afterUpdate2002, before2002);

      friendService.nicknames['3003'] = 'Unrelated';
      friendService.profileCacheVersion.value++;
      await tester.pump();

      expect(
        controller.senderProfileVersionFor(
          senderId: '2001',
          senderType: 1,
          isMine: false,
        ),
        afterUpdate2001,
      );
      expect(
        controller.senderProfileVersionFor(
          senderId: '2002',
          senderType: 1,
          isMine: false,
        ),
        afterUpdate2002,
      );
    },
  );

  testWidgets('forwardMessages separate keeps card extra', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    final cardEnvelope = ChatMessageCardCodec.buildUserProfileCard(
      userId: '1001',
      nickname: 'Alice',
      avatarUrl: '',
    );

    final sentCount = await controller.forwardMessages(
      messages: [
        MessageModel(
          msgId: 'msg-card-1',
          sessionId: 'source-session',
          senderId: '42',
          content: cardEnvelope.content,
          extra: cardEnvelope.extra,
          createdAt: 1,
        ),
      ],
      targetSessionId: 'target-session',
      mode: ChatForwardDispatchMode.separate,
    );

    expect(sentCount, 1);
    expect(imService.sendCalls, 1);
    expect(imService.sentSessionId, 'target-session');
    expect(imService.sentContent, isNotNull);
    final content = imService.sentContent ?? '';
    final uriMatch = RegExp(r'\((grix://card/[^)]+)\)').firstMatch(content);
    expect(uriMatch, isNotNull);
    expect(
      ChatMessageCardCodec.decodeGrixUriCard(uriMatch!.group(1)!),
      isNotNull,
    );
  });

  testWidgets('forwardConversationCard sends private conversation card extra', (
    WidgetTester tester,
  ) async {
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'private-session-source',
        title: 'Alice',
        type: 'private',
        peerId: '1001',
        peerType: 1,
        peerNickname: 'Alice',
        updatedAt: 10,
        lastMessage: '',
        lastMessageTime: 20,
      ),
    ]);

    final controller = Get.put(ChatController());
    controller.sessionId = 'private-session-source';
    controller.chatTitle = 'Alice';
    controller.chatType = 'private';

    final sentCount = await controller.forwardConversationCard(
      targetSessionId: 'target-session',
    );

    expect(sentCount, 1);
    expect(imService.sendCalls, 1);
    expect(imService.sentSessionId, 'target-session');
    expect(imService.sentContent, isNotNull);

    final content = imService.sentContent ?? '';
    final uriMatch = RegExp(r'\((grix://card/[^)]+)\)').firstMatch(content);
    expect(uriMatch, isNotNull);
    final decoded = ChatMessageCardCodec.decodeGrixUriCard(uriMatch!.group(1)!);
    expect(decoded, isA<ChatConversationCardData>());

    final card = decoded as ChatConversationCardData;
    expect(card.sessionId, 'private-session-source');
    expect(card.sessionType, 'private');
    expect(card.title, 'Alice');
    expect(card.peerId, '1001');
  });

  testWidgets(
    'forwardConversationCard sends accompanying text and card as one message',
    (WidgetTester tester) async {
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'private-session-source-with-message',
          title: 'Alice',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          peerNickname: 'Alice',
          updatedAt: 10,
          lastMessage: '',
          lastMessageTime: 20,
        ),
      ]);

      final controller = Get.put(ChatController());
      controller.sessionId = 'private-session-source-with-message';
      controller.chatTitle = 'Alice';
      controller.chatType = 'private';

      final sentCount = await controller.forwardConversationCard(
        targetSessionId: 'target-session',
        accompanyingMessage: '  请查看这个会话。\n有问题随时联系我。  ',
      );

      expect(sentCount, 1);
      expect(imService.sendCalls, 1);
      expect(imService.sentSessionId, 'target-session');
      final content = imService.sentContent ?? '';
      expect(content, startsWith('请查看这个会话。\n有问题随时联系我。\n\n'));
      final uriMatch = RegExp(r'\((grix://card/[^)]+)\)').firstMatch(content);
      expect(uriMatch, isNotNull);
      final decoded = ChatMessageCardCodec.decodeGrixUriCard(
        uriMatch!.group(1)!,
      );
      expect(decoded, isA<ChatConversationCardData>());
      expect(
        (decoded as ChatConversationCardData).sessionId,
        'private-session-source-with-message',
      );
    },
  );

  testWidgets('forwardConversationCard sends group conversation card extra', (
    WidgetTester tester,
  ) async {
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'group-session-source',
        title: '研发群',
        type: 'group',
        updatedAt: 10,
        lastMessage: '',
        lastMessageTime: 20,
      ),
    ]);

    final controller = Get.put(ChatController());
    controller.sessionId = 'group-session-source';
    controller.chatTitle = '研发群';
    controller.chatType = 'group';

    final sentCount = await controller.forwardConversationCard(
      targetSessionId: 'target-session',
    );

    expect(sentCount, 1);
    expect(imService.sendCalls, 1);
    expect(imService.sentSessionId, 'target-session');
    expect(imService.sentContent, isNotNull);

    final content = imService.sentContent ?? '';
    final uriMatch = RegExp(r'\((grix://card/[^)]+)\)').firstMatch(content);
    expect(uriMatch, isNotNull);
    final decoded = ChatMessageCardCodec.decodeGrixUriCard(uriMatch!.group(1)!);
    expect(decoded, isA<ChatConversationCardData>());

    final card = decoded as ChatConversationCardData;
    expect(card.sessionId, 'group-session-source');
    expect(card.sessionType, 'group');
    expect(card.title, '研发群');
    expect(card.peerId, isEmpty);
  });

  testWidgets(
    'group send carries explicit mention ids for local display names',
    (WidgetTester tester) async {
      final friendService = _FakeFriendService();
      friendService.nicknames['1002'] = '老板';
      Get.put<FriendService>(friendService);

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_mention_1';
      controller.chatTitle = 'group';
      controller.chatType = 'group';
      sessionService.detailResp = {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 3},
          {'member_id': '1002', 'member_type': 1, 'role': 1},
        ],
      };

      await controller.refreshSessionDetail();
      await tester.pump();

      controller.inputController.text = ' @老板 ';

      controller.sendMessage();
      await tester.pump(const Duration(milliseconds: 120));

      expect(imService.sendCalls, 1);
      expect(imService.sentContent, '@老板');
      expect(imService.sentExtra, isNotNull);
      expect(imService.sentExtra!['mention_user_ids'], ['1002']);
    },
  );

  testWidgets(
    'group mention parsing ignores transient out-of-bounds selection',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_mention_out_of_bounds';
      controller.chatTitle = 'group';
      controller.chatType = 'group';
      sessionService.detailResp = {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 3},
          {'member_id': '1002', 'member_type': 1, 'role': 1, 'nickname': '老板'},
        ],
      };

      await controller.refreshSessionDetail();
      await tester.pump();

      expect(
        () => controller.inputController.value = const TextEditingValue(
          text: '@老',
          selection: TextSelection.collapsed(offset: 3),
        ),
        returnsNormally,
      );

      expect(controller.showMentionList.value, isFalse);
      expect(controller.mentionSelectedIndex.value, 0);
    },
  );

  testWidgets(
    'group mention works without leading space and preserves send flow',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_mention_adjacent_text';
      controller.chatTitle = 'group';
      controller.chatType = 'group';
      sessionService.detailResp = {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 3},
          {'member_id': '1002', 'member_type': 1, 'role': 1, 'nickname': '老板'},
        ],
      };

      controller.onReady();
      await tester.pump();

      controller.inputController.value = const TextEditingValue(
        text: '你好@老',
        selection: TextSelection.collapsed(offset: 4),
      );
      await tester.pump();

      expect(controller.showMentionList.value, isTrue);
      expect(controller.mentionSearchQuery.value, '老');
      expect(controller.filteredMentionList, hasLength(1));

      expect(controller.mentionSelectCurrent(), isTrue);
      await tester.pump();

      expect(controller.inputController.text, '你好@老板 ');
      expect(controller.showMentionList.value, isFalse);

      controller.sendMessage();
      await tester.pump(const Duration(milliseconds: 120));

      expect(imService.sendCalls, 1);
      expect(imService.sentContent, '你好@1002');
      expect(imService.sentExtra, isNotNull);
      expect(imService.sentExtra!['mention_user_ids'], ['1002']);
    },
  );

  testWidgets(
    'group mention selection defers text rewrite until composition ends',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_mention_deferred';
      controller.chatTitle = 'group';
      controller.chatType = 'group';
      sessionService.detailResp = {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 3},
          {'member_id': '1002', 'member_type': 1, 'role': 1, 'nickname': '老板'},
        ],
      };

      controller.onReady();
      await tester.pump();

      controller.inputController.value = const TextEditingValue(
        text: '你好@老',
        selection: TextSelection.collapsed(offset: 4),
        composing: TextRange(start: 3, end: 4),
      );
      await tester.pump();

      expect(controller.showMentionList.value, isTrue);
      expect(controller.mentionSelectCurrent(), isTrue);
      await tester.pump();

      expect(controller.inputController.text, '你好@老');
      expect(controller.showMentionList.value, isFalse);

      controller.inputController.value = const TextEditingValue(
        text: '你好@老',
        selection: TextSelection.collapsed(offset: 4),
      );
      await tester.pump();

      expect(controller.inputController.text, '你好@老板 ');

      controller.sendMessage();
      await tester.pump(const Duration(milliseconds: 120));

      expect(imService.sendCalls, 1);
      expect(imService.sentContent, '你好@老板');
      expect(imService.sentExtra, isNotNull);
      expect(imService.sentExtra!['mention_user_ids'], ['1002']);
    },
  );

  testWidgets(
    'mentionSenderFromMessage inserts mention token and carries mention_user_ids',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_mention_long_press_1';
      controller.chatTitle = 'group';
      controller.chatType = 'group';
      sessionService.detailResp = {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 3},
          {'member_id': '1002', 'member_type': 1, 'role': 1, 'nickname': '老板'},
        ],
      };

      await controller.refreshSessionDetail();
      await tester.pump();

      controller.inputController.value = const TextEditingValue(
        text: '你好',
        selection: TextSelection.collapsed(offset: 2),
      );

      controller.mentionSenderFromMessage(
        senderId: '1002',
        senderType: 1,
        isMine: false,
        senderName: '老板',
      );
      await tester.pump();

      expect(controller.inputController.text, '你好 @老板 ');

      controller.sendMessage();
      await tester.pump(const Duration(milliseconds: 120));

      expect(imService.sendCalls, 1);
      expect(imService.sentContent, '你好 @1002');
      expect(imService.sentExtra, isNotNull);
      expect(imService.sentExtra!['mention_user_ids'], ['1002']);
    },
  );

  testWidgets(
    'pinned mention prefixes send content and mention_user_ids without clearing pin',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_pinned_mention_1';
      controller.chatTitle = 'group';
      controller.chatType = 'group';
      sessionService.detailResp = {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 3},
          {'member_id': '1002', 'member_type': 1, 'role': 1, 'nickname': '老板'},
        ],
      };

      await controller.refreshSessionDetail();
      await tester.pump();

      controller.togglePinnedMention({
        'member_id': '1002',
        'member_type': 1,
        'nickname': '老板',
      });
      expect(controller.isPinnedMention('1002'), isTrue);
      expect(controller.pinnedMentions.length, 1);

      controller.inputController.text = '你好';
      controller.sendMessage();
      await tester.pump(const Duration(milliseconds: 120));

      expect(imService.sendCalls, 1);
      expect(imService.sentContent, '@1002 你好');
      expect(imService.sentExtra, isNotNull);
      expect(imService.sentExtra!['mention_user_ids'], ['1002']);
      // 固定艾特发送后仍保留。
      expect(controller.isPinnedMention('1002'), isTrue);
      expect(controller.inputController.text, isEmpty);
    },
  );

  testWidgets('pinned-only send works without typed body text', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_pinned_only_1';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 2,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3},
        {'member_id': '1002', 'member_type': 1, 'role': 1, 'nickname': '老板'},
      ],
    };

    await controller.refreshSessionDetail();
    await tester.pump();

    controller.togglePinnedMention({
      'member_id': '1002',
      'member_type': 1,
      'nickname': '老板',
    });
    controller.inputController.text = '';
    controller.sendMessage();
    await tester.pump(const Duration(milliseconds: 120));

    expect(imService.sendCalls, 1);
    expect(imService.sentContent, '@1002');
    expect(imService.sentExtra!['mention_user_ids'], ['1002']);
    expect(controller.isPinnedMention('1002'), isTrue);
  });

  testWidgets(
    'mentionSenderFromMessage probes session type when chat type is stale private',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_mention_probe_1';
      controller.chatTitle = 'group';
      controller.chatType = 'private';
      sessionService.detailResp = {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 3},
          {'member_id': '1002', 'member_type': 1, 'role': 1, 'nickname': '老板'},
        ],
      };

      controller.inputController.value = const TextEditingValue(
        text: 'Hi',
        selection: TextSelection.collapsed(offset: 2),
      );

      controller.mentionSenderFromMessage(
        senderId: '1002',
        senderType: 1,
        isMine: false,
        senderName: '老板',
      );
      await tester.pump();

      expect(controller.chatType, 'group');
      expect(controller.inputController.text, 'Hi @老板 ');
    },
  );

  testWidgets('group mention trigger ignores email-like ascii prefix', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_mention_email_like';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 2,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3},
        {'member_id': '1002', 'member_type': 1, 'role': 1, 'nickname': 'boss'},
      ],
    };

    controller.onReady();
    await tester.pump();

    controller.inputController.value = const TextEditingValue(
      text: 'mail@bo',
      selection: TextSelection.collapsed(offset: 7),
    );
    await tester.pump();

    expect(controller.showMentionList.value, isFalse);
    expect(controller.filteredMentionList, isEmpty);
    controller.inputController.clear();
    await tester.pump();
  });

  testWidgets('bare at mention lists all group members including agents', (
    WidgetTester tester,
  ) async {
    agentService.agents.assignAll([
      AgentModel(id: '9001', agentName: 'Agent Alpha'),
      AgentModel(id: '9002', agentName: 'Agent Beta'),
    ]);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_mention_all_members';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 14,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3, 'nickname': '我'},
        for (var i = 0; i < 11; i++)
          {
            'member_id': '${1001 + i}',
            'member_type': 1,
            'role': 1,
            'nickname': '成员${i + 1}',
          },
        {'member_id': '9001', 'member_type': 2, 'role': 1},
        {'member_id': '9002', 'member_type': 2, 'role': 1},
      ],
    };

    controller.onReady();
    await tester.pump();

    controller.inputController.value = const TextEditingValue(
      text: '@',
      selection: TextSelection.collapsed(offset: 1),
    );
    await tester.pump();

    expect(controller.showMentionList.value, isTrue);
    expect(controller.filteredMentionList, hasLength(14));
    expect(
      controller.filteredMentionList.first['member_id'],
      '__mention_all__',
    );
    expect(
      controller.filteredMentionList.any(
        (member) => member['member_id'] == '1011',
      ),
      isTrue,
    );
    expect(
      controller.filteredMentionList.any(
        (member) => member['member_id'] == '9002',
      ),
      isTrue,
    );
    controller.inputController.clear();
    await tester.pump();
  });

  testWidgets('mention-all appears in groups larger than two members', (
    WidgetTester tester,
  ) async {
    agentService.agents.assignAll([
      AgentModel(id: '9001', agentName: 'Agent Alpha'),
      AgentModel(id: '9002', agentName: 'Agent Beta'),
    ]);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_mention_all_threshold';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 3,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3, 'nickname': '我'},
        {'member_id': '9001', 'member_type': 2, 'role': 1},
        {'member_id': '9002', 'member_type': 2, 'role': 1},
      ],
    };

    controller.onReady();
    await tester.pump();

    controller.inputController.value = const TextEditingValue(
      text: '@',
      selection: TextSelection.collapsed(offset: 1),
    );
    await tester.pump();

    expect(controller.showMentionList.value, isTrue);
    expect(
      controller.filteredMentionList.first['member_id'],
      '__mention_all__',
    );
    expect(controller.filteredMentionList, hasLength(3));

    controller.inputController.clear();
    await tester.pump(const Duration(milliseconds: 220));
  });

  testWidgets('mention-all stays hidden in two-member group chats', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_mention_all_two_members';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 2,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3, 'nickname': '我'},
        {'member_id': '1002', 'member_type': 1, 'role': 1, 'nickname': '成员甲'},
      ],
    };

    controller.onReady();
    await tester.pump();

    controller.inputController.value = const TextEditingValue(
      text: '@',
      selection: TextSelection.collapsed(offset: 1),
    );
    await tester.pump();

    expect(
      controller.filteredMentionList.any(
        (member) => member['member_id'] == '__mention_all__',
      ),
      isFalse,
    );

    controller.inputController.clear();
    await tester.pump(const Duration(milliseconds: 220));
  });

  testWidgets('group send carries mention_all when selecting everyone', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_mention_everyone_send';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 4,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3, 'nickname': '我'},
        {'member_id': '1002', 'member_type': 1, 'role': 1, 'nickname': '成员甲'},
        {'member_id': '1003', 'member_type': 1, 'role': 1, 'nickname': '成员乙'},
        {'member_id': '9001', 'member_type': 2, 'role': 1},
      ],
    };

    controller.onReady();
    await tester.pump();

    controller.inputController.value = const TextEditingValue(
      text: '@',
      selection: TextSelection.collapsed(offset: 1),
    );
    await tester.pump();

    expect(controller.mentionSelectCurrent(), isTrue);
    await tester.pump();

    expect(controller.inputController.text, '@所有人 ');

    controller.sendMessage();
    await tester.pump(const Duration(milliseconds: 120));

    expect(imService.sendCalls, 1);
    expect(imService.sentContent, '@所有人');
    expect(imService.sentExtra, isNotNull);
    expect(imService.sentExtra!['mention_all'], isTrue);
    expect(imService.sentExtra!.containsKey('mention_user_ids'), isFalse);
  });

  testWidgets('mention popup stays hidden when there are no matches', (
    WidgetTester tester,
  ) async {
    agentService.agents.assignAll([
      AgentModel(id: '9001', agentName: 'Agent Alpha'),
    ]);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_mention_no_match';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 3,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3, 'nickname': '我'},
        {'member_id': '1002', 'member_type': 1, 'role': 1, 'nickname': '成员甲'},
        {'member_id': '9001', 'member_type': 2, 'role': 1},
      ],
    };

    controller.onReady();
    await tester.pump();

    controller.inputController.value = const TextEditingValue(
      text: '@zzzzz',
      selection: TextSelection.collapsed(offset: 6),
    );
    await tester.pump();

    expect(controller.showMentionList.value, isFalse);
    expect(controller.filteredMentionList, isEmpty);
    controller.inputController.clear();
    await tester.pump();
  });

  testWidgets('mention popup refreshes when member nickname arrives later', (
    WidgetTester tester,
  ) async {
    final friendService = _FakeFriendService();
    Get.put<FriendService>(friendService);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_mention_late_profile';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 2,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3},
        {'member_id': '1002', 'member_type': 1, 'role': 1},
      ],
    };

    controller.onReady();
    await tester.pump();

    controller.inputController.value = const TextEditingValue(
      text: '@老板',
      selection: TextSelection.collapsed(offset: 3),
    );
    await tester.pump();

    expect(controller.showMentionList.value, isFalse);
    expect(controller.filteredMentionList, isEmpty);

    friendService.completeProfile('1002', nickname: '老板');
    await tester.pump();

    expect(controller.showMentionList.value, isTrue);
    expect(controller.filteredMentionList, hasLength(1));
    expect(controller.filteredMentionList.first['member_id'], '1002');

    controller.inputController.clear();
    await tester.pump();
  });

  testWidgets(
    'resolveGroupMemberDisplayName prefers session member nickname over cached friend nickname',
    (WidgetTester tester) async {
      final friendService = _FakeFriendService();
      friendService.nicknames['1002'] = '本地昵称';
      Get.put<FriendService>(friendService);

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_display_name_1';
      controller.chatTitle = 'group';
      controller.chatType = 'group';
      sessionService.detailResp = {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 3},
          {
            'member_id': '1002',
            'member_type': 1,
            'role': 1,
            'nickname': '当前用户备注',
          },
        ],
      };

      await controller.refreshSessionDetail();
      await tester.pump();

      final member = controller.groupMembers[1];
      expect(controller.resolveGroupMemberDisplayName(member), '当前用户备注');
    },
  );

  testWidgets(
    'resolveGroupMemberDisplayName uses my group nickname when available',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_display_name_2';
      controller.chatTitle = 'group';
      controller.chatType = 'group';
      sessionService.detailResp = {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {
            'member_id': '42',
            'member_type': 1,
            'role': 3,
            'nickname': '群里我的昵称',
            'group_nickname': '群里我的昵称',
          },
          {
            'member_id': '1002',
            'member_type': 1,
            'role': 1,
            'nickname': '成员昵称',
            'group_nickname': '成员昵称',
          },
        ],
      };

      await controller.refreshSessionDetail();
      await tester.pump();

      final me = controller.groupMembers.first;
      expect(controller.resolveGroupMemberDisplayName(me), '群里我的昵称');
      expect(controller.myGroupNickname, '群里我的昵称');
    },
  );

  testWidgets(
    'formatMessageContentForDisplay prefers remark over nickname for @numeric id',
    (WidgetTester tester) async {
      final friendService = _FakeFriendService();
      friendService.remarks['2031343392729862144'] = '备注张三';
      Get.put<FriendService>(friendService);

      final controller = Get.put(ChatController());
      final formatted = controller.formatMessageContentForDisplay(
        '你好 @2031343392729862144',
      );

      expect(formatted, '你好 @备注张三');
    },
  );

  testWidgets(
    'formatMessageContentForDisplay prefers remark over group nickname and account name',
    (WidgetTester tester) async {
      final friendService = _FakeFriendService();
      friendService.remarks['202698989848778'] = '备注张三';
      friendService.usernames['202698989848778'] = 'zhangsan';
      Get.put<FriendService>(friendService);

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_mention_priority_1';
      controller.chatTitle = 'group';
      controller.chatType = 'group';
      sessionService.detailResp = {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 3},
          {
            'member_id': '202698989848778',
            'member_type': 1,
            'role': 1,
            'nickname': '全局昵称',
            'group_nickname': '群里小妍',
          },
        ],
      };

      await controller.refreshSessionDetail();
      await tester.pump();

      final formatted = controller.formatMessageContentForDisplay(
        '你好 @202698989848778',
      );

      expect(formatted, '你好 @备注张三');
    },
  );

  testWidgets(
    'formatMessageContentForDisplay falls back to group nickname when remark is missing',
    (WidgetTester tester) async {
      final friendService = _FakeFriendService();
      friendService.usernames['202698989848779'] = 'zhangsan';
      Get.put<FriendService>(friendService);

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_mention_priority_2';
      controller.chatTitle = 'group';
      controller.chatType = 'group';
      sessionService.detailResp = {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 3},
          {
            'member_id': '202698989848779',
            'member_type': 1,
            'role': 1,
            'nickname': '全局昵称',
            'group_nickname': '群里小妍',
          },
        ],
      };

      await controller.refreshSessionDetail();
      await tester.pump();

      final formatted = controller.formatMessageContentForDisplay(
        '你好 @202698989848779',
      );

      expect(formatted, '你好 @群里小妍');
    },
  );

  testWidgets(
    'formatMessageContentForDisplay falls back to nickname when remark and group nickname are missing',
    (WidgetTester tester) async {
      final friendService = _FakeFriendService();
      friendService.usernames['202698989848780'] = 'zhangsan';
      Get.put<FriendService>(friendService);

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_mention_priority_3';
      controller.chatTitle = 'group';
      controller.chatType = 'group';
      sessionService.detailResp = {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 3},
          {
            'member_id': '202698989848780',
            'member_type': 1,
            'role': 1,
            'nickname': '账号名称',
            'group_nickname': '',
          },
        ],
      };

      await controller.refreshSessionDetail();
      await tester.pump();

      final formatted = controller.formatMessageContentForDisplay(
        '你好 @202698989848780',
      );

      expect(formatted, '你好 @账号名称');
    },
  );

  testWidgets(
    'formatMessageContentForDisplay falls back to cached nickname when remark group nickname and member nickname are missing',
    (WidgetTester tester) async {
      final friendService = _FakeFriendService();
      friendService.nicknames['202698989848783'] = '缓存昵称';
      friendService.usernames['202698989848783'] = 'zhangsan';
      Get.put<FriendService>(friendService);

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_mention_priority_3c';
      controller.chatTitle = 'group';
      controller.chatType = 'group';
      sessionService.detailResp = {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 3},
          {
            'member_id': '202698989848783',
            'member_type': 1,
            'role': 1,
            'nickname': '',
            'group_nickname': '',
          },
        ],
      };

      await controller.refreshSessionDetail();
      await tester.pump();

      final formatted = controller.formatMessageContentForDisplay(
        '你好 @202698989848783',
      );

      expect(formatted, '你好 @缓存昵称');
    },
  );

  testWidgets(
    'formatMessageContentForDisplay skips duplicate nickname and falls back to username',
    (WidgetTester tester) async {
      final friendService = _FakeFriendService();
      friendService.nicknames['202698989848784'] = 'zhangsan';
      friendService.usernames['202698989848784'] = 'zhangsan';
      Get.put<FriendService>(friendService);

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_mention_priority_3d';
      controller.chatTitle = 'group';
      controller.chatType = 'group';
      sessionService.detailResp = {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 3},
          {
            'member_id': '202698989848784',
            'member_type': 1,
            'role': 1,
            'nickname': '',
            'group_nickname': '',
          },
        ],
      };

      await controller.refreshSessionDetail();
      await tester.pump();

      final formatted = controller.formatMessageContentForDisplay(
        '你好 @202698989848784',
      );

      expect(formatted, '你好 @zhangsan');
    },
  );

  testWidgets(
    'formatMessageContentForDisplay falls back to username when remark group nickname and nickname are missing',
    (WidgetTester tester) async {
      final friendService = _FakeFriendService();
      friendService.usernames['202698989848782'] = 'zhangsan';
      Get.put<FriendService>(friendService);

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_mention_priority_3b';
      controller.chatTitle = 'group';
      controller.chatType = 'group';
      sessionService.detailResp = {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 3},
          {
            'member_id': '202698989848782',
            'member_type': 1,
            'role': 1,
            'nickname': '',
            'group_nickname': '',
          },
        ],
      };

      await controller.refreshSessionDetail();
      await tester.pump();

      final formatted = controller.formatMessageContentForDisplay(
        '你好 @202698989848782',
      );

      expect(formatted, '你好 @zhangsan');
    },
  );

  testWidgets(
    'formatMessageContentForDisplay falls back to numeric id when no remark group nickname nickname or account exists',
    (WidgetTester tester) async {
      Get.put<FriendService>(_FakeFriendService());

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_mention_priority_4';
      controller.chatTitle = 'group';
      controller.chatType = 'group';
      sessionService.detailResp = {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 3},
          {
            'member_id': '202698989848781',
            'member_type': 1,
            'role': 1,
            'nickname': '',
            'group_nickname': '',
          },
        ],
      };

      await controller.refreshSessionDetail();
      await tester.pump();

      final formatted = controller.formatMessageContentForDisplay(
        '你好 @202698989848781',
      );

      expect(formatted, '你好 @202698989848781');
    },
  );

  testWidgets(
    'formatMessageContentForDisplay falls back to nickname before username for @numeric id',
    (WidgetTester tester) async {
      final friendService = _FakeFriendService();
      friendService.nicknames['202698989848778'] = '张三';
      friendService.usernames['202698989848778'] = 'zhangsan';
      Get.put<FriendService>(friendService);

      final controller = Get.put(ChatController());
      final formatted = controller.formatMessageContentForDisplay(
        '你好 @202698989848778',
      );

      expect(formatted, '你好 @张三');
    },
  );

  testWidgets('formatMessageContentForDisplay falls back to username', (
    WidgetTester tester,
  ) async {
    final friendService = _FakeFriendService();
    friendService.usernames['202698989848778'] = 'zhangsan';
    Get.put<FriendService>(friendService);

    final controller = Get.put(ChatController());
    final formatted = controller.formatMessageContentForDisplay(
      '你好 @202698989848778',
    );

    expect(formatted, '你好 @zhangsan');
  });

  testWidgets('resolveGroupMemberAccount uses cached username', (
    WidgetTester tester,
  ) async {
    final friendService = _FakeFriendService();
    friendService.usernames['1002'] = 'alice_account';
    friendService.nicknames['1002'] = 'Alice';
    Get.put<FriendService>(friendService);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_account_1';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 2,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3},
        {'member_id': '1002', 'member_type': 1, 'role': 1},
      ],
    };

    await controller.refreshSessionDetail();
    await tester.pump();

    final member = controller.groupMembers[1];
    expect(controller.resolveGroupMemberAccount(member), 'alice_account');
  });

  testWidgets('resolveGroupMemberAccount ignores numeric member id alias', (
    WidgetTester tester,
  ) async {
    final friendService = _FakeFriendService();
    friendService.usernames['1002'] = '1002';
    friendService.nicknames['1002'] = 'Alice';
    Get.put<FriendService>(friendService);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_account_2';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 2,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3},
        {'member_id': '1002', 'member_type': 1, 'role': 1},
      ],
    };

    await controller.refreshSessionDetail();
    await tester.pump();

    final member = controller.groupMembers[1];
    expect(controller.resolveGroupMemberAccount(member), isEmpty);
  });

  testWidgets('resolveSenderName 共享 agent 在私聊里返回 sharedAgents 名字而非 Agent<id>', (
    WidgetTester tester,
  ) async {
    // 共享给我的 agent 不在 owner 列表 agents 里，只在 sharedAgents。
    // 修复前 _resolveKnownAgentName 只查 agents 会返回 'Agent <id>'，
    // 修复后会 fallback 到 sharedAgents 拿到真正的 agentName。
    agentService.agents.clear();
    agentService.sharedAgents.assignAll([
      AgentModel.fromJson({
        'id': '8888',
        'agent_name': '客服助手',
        'owner_id': '7777',
        'status': 1,
        'provider_type': 3,
      }),
    ]);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_private_shared_agent_1';
    controller.chatTitle = 'private';
    controller.chatType = 'private';

    expect(
      controller.resolveSenderName(
        senderId: '8888',
        isMine: false,
        isGroup: false,
        senderType: 2,
      ),
      '客服助手',
      reason: 'sharedAgents 命中应回真实 agentName',
    );

    // 私聊里 peerDisplayName 兜底走 agent fallback 之前 —— 私聊场景
    // resolveSenderName 在 senderType=2 时会优先走 _resolveKnownAgentName，
    // 所以 sharedAgents 命中后不会再回退到 peerDisplayName。
  });

  testWidgets(
    'resolveSenderName 共享 agent 不在 sharedAgents 也不在 agents 时回退 Agent<id>',
    (WidgetTester tester) async {
      agentService.agents.clear();
      agentService.sharedAgents.clear();

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_private_unknown_agent_1';
      controller.chatTitle = 'private';
      controller.chatType = 'private';

      expect(
        controller.resolveSenderName(
          senderId: '9999',
          isMine: false,
          isGroup: false,
          senderType: 2,
        ),
        'Agent 9999',
      );
    },
  );

  testWidgets('群聊唯一 @ 共享 agent 时设置 groupToolbarTargetAgentId', (
    WidgetTester tester,
  ) async {
    agentService.agents.clear();
    agentService.sharedAgents.assignAll([
      AgentModel.fromJson({
        'id': '8888',
        'agent_name': '共享助手',
        'owner_id': '7777',
        'status': 1,
        'provider_type': 3,
      }),
    ]);
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'session_group_toolbar_shared',
        title: 'group',
        type: 'group',
        updatedAt: 1,
        lastMessageTime: 1,
      ),
    ]);

    final controller = ChatController();
    addTearDown(() {
      if (!controller.isClosed) {
        controller.onClose();
      }
    });
    Get.put(controller);
    controller.sessionId = 'session_group_toolbar_shared';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    controller.onReady();
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 3,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3},
        {'member_id': '1002', 'member_type': 1, 'role': 1},
        {'member_id': '8888', 'member_type': 2, 'role': 1, 'nickname': '共享助手'},
      ],
    };
    await controller.refreshSessionDetail();
    await tester.pump();

    controller.inputController.value = const TextEditingValue(
      text: '@共享助手 ',
      selection: TextSelection.collapsed(offset: 5),
    );
    await tester.pump(const Duration(milliseconds: 220));

    expect(
      controller.groupToolbarTargetAgentId,
      '8888',
      reason: '共享给我的 agent 应能出群工具栏目标',
    );
  });

  testWidgets('群聊固定唯一可访问 agent 时设置 groupToolbarTargetAgentId', (
    WidgetTester tester,
  ) async {
    agentService.agents.clear();
    agentService.sharedAgents.assignAll([
      AgentModel.fromJson({
        'id': '8888',
        'agent_name': '共享助手',
        'owner_id': '7777',
        'status': 1,
        'provider_type': 3,
      }),
    ]);
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'session_group_toolbar_pinned',
        title: 'group',
        type: 'group',
        updatedAt: 1,
        lastMessageTime: 1,
      ),
    ]);

    final controller = ChatController();
    addTearDown(() {
      if (!controller.isClosed) {
        controller.onClose();
      }
    });
    Get.put(controller);
    controller.sessionId = 'session_group_toolbar_pinned';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    controller.onReady();
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 3,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3},
        {'member_id': '1002', 'member_type': 1, 'role': 1},
        {'member_id': '8888', 'member_type': 2, 'role': 1, 'nickname': '共享助手'},
      ],
    };
    await controller.refreshSessionDetail();
    await tester.pump();

    controller.togglePinnedMention({
      'member_id': '8888',
      'member_type': 2,
      'nickname': '共享助手',
    });
    await tester.pump();

    expect(
      controller.groupToolbarTargetAgentId,
      '8888',
      reason: '固定唯一可访问 agent 应出群工具栏',
    );

    controller.removePinnedMention('8888');
    await tester.pump();
    expect(
      controller.groupToolbarTargetAgentId,
      isEmpty,
      reason: '取消固定后应收起群工具栏目标',
    );
  });

  testWidgets('群聊固定 agent 再 @ 另一人时不设置 groupToolbarTargetAgentId', (
    WidgetTester tester,
  ) async {
    agentService.agents.clear();
    agentService.sharedAgents.assignAll([
      AgentModel.fromJson({
        'id': '8888',
        'agent_name': '共享助手',
        'owner_id': '7777',
        'status': 1,
        'provider_type': 3,
      }),
    ]);
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'session_group_toolbar_pin_plus_at',
        title: 'group',
        type: 'group',
        updatedAt: 1,
        lastMessageTime: 1,
      ),
    ]);

    final controller = ChatController();
    addTearDown(() {
      if (!controller.isClosed) {
        controller.onClose();
      }
    });
    Get.put(controller);
    controller.sessionId = 'session_group_toolbar_pin_plus_at';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    controller.onReady();
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 3,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3},
        {'member_id': '1002', 'member_type': 1, 'role': 1, 'nickname': '老板'},
        {'member_id': '8888', 'member_type': 2, 'role': 1, 'nickname': '共享助手'},
      ],
    };
    await controller.refreshSessionDetail();
    await tester.pump();

    controller.togglePinnedMention({
      'member_id': '8888',
      'member_type': 2,
      'nickname': '共享助手',
    });
    expect(controller.groupToolbarTargetAgentId, '8888');

    controller.inputController.value = const TextEditingValue(
      text: '@老板 ',
      selection: TextSelection.collapsed(offset: 4),
    );
    await tester.pump(const Duration(milliseconds: 220));

    expect(
      controller.groupToolbarTargetAgentId,
      isEmpty,
      reason: '固定 agent + 输入框另一 @ 属于多目标，不应出工具栏',
    );
  });

  testWidgets('群聊固定 @所有人 或无权 agent 时不设置 groupToolbarTargetAgentId', (
    WidgetTester tester,
  ) async {
    agentService.agents.clear();
    agentService.sharedAgents.clear();
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'session_group_toolbar_pin_edge',
        title: 'group',
        type: 'group',
        updatedAt: 1,
        lastMessageTime: 1,
      ),
    ]);

    final controller = ChatController();
    addTearDown(() {
      if (!controller.isClosed) {
        controller.onClose();
      }
    });
    Get.put(controller);
    controller.sessionId = 'session_group_toolbar_pin_edge';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    controller.onReady();
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 3,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3},
        {'member_id': '1002', 'member_type': 1, 'role': 1},
        {'member_id': '9999', 'member_type': 2, 'role': 1, 'nickname': '别人助手'},
      ],
    };
    await controller.refreshSessionDetail();
    await tester.pump();

    controller.togglePinnedMention({
      'member_id': '__mention_all__',
      'member_type': 0,
      'nickname': '所有人',
    });
    await tester.pump();
    expect(
      controller.groupToolbarTargetAgentId,
      isEmpty,
      reason: '固定 @所有人 不应出工具栏',
    );

    controller.removePinnedMention('__mention_all__');
    controller.togglePinnedMention({
      'member_id': '9999',
      'member_type': 2,
      'nickname': '别人助手',
    });
    await tester.pump();
    expect(
      controller.groupToolbarTargetAgentId,
      isEmpty,
      reason: '固定无权 agent 不应出工具栏',
    );
  });

  testWidgets('群聊 @ 无权使用的 agent 时不设置 groupToolbarTargetAgentId', (
    WidgetTester tester,
  ) async {
    agentService.agents.clear();
    agentService.sharedAgents.clear();
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'session_group_toolbar_forbidden',
        title: 'group',
        type: 'group',
        updatedAt: 1,
        lastMessageTime: 1,
      ),
    ]);

    final controller = ChatController();
    addTearDown(() {
      if (!controller.isClosed) {
        controller.onClose();
      }
    });
    Get.put(controller);
    controller.sessionId = 'session_group_toolbar_forbidden';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    controller.onReady();
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 3,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3},
        {'member_id': '1002', 'member_type': 1, 'role': 1},
        {'member_id': '9999', 'member_type': 2, 'role': 1, 'nickname': '别人助手'},
      ],
    };
    await controller.refreshSessionDetail();
    await tester.pump();

    controller.inputController.value = const TextEditingValue(
      text: '@别人助手 ',
      selection: TextSelection.collapsed(offset: 5),
    );
    await tester.pump(const Duration(milliseconds: 220));

    expect(
      controller.groupToolbarTargetAgentId,
      isEmpty,
      reason: '无权使用的 agent 不应出群工具栏目标',
    );
  });

  testWidgets(
    'resolveSenderName uses group agent nickname for non-owner viewer',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_agent_name_1';
      controller.chatTitle = 'group';
      controller.chatType = 'group';
      sessionService.detailResp = {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 3},
          {
            'member_id': '9001',
            'member_type': 2,
            'role': 1,
            'nickname': 'OpenClaw',
          },
        ],
      };

      await controller.refreshSessionDetail();
      await tester.pump();

      expect(
        controller.resolveSenderName(
          senderId: '9001',
          isMine: false,
          isGroup: true,
          senderType: 2,
        ),
        'OpenClaw',
      );
    },
  );

  testWidgets(
    'formatMessageContentForDisplay does not replace email local part',
    (WidgetTester tester) async {
      final friendService = _FakeFriendService();
      friendService.usernames['202698989848778'] = 'zhangsan';
      Get.put<FriendService>(friendService);

      final controller = Get.put(ChatController());
      final formatted = controller.formatMessageContentForDisplay(
        'a@202698989848778.com @202698989848778',
      );

      expect(formatted, 'a@202698989848778.com @zhangsan');
    },
  );

  testWidgets(
    'formatMessageContentForDisplay collapses numeric mention with trailing alias',
    (WidgetTester tester) async {
      final friendService = _FakeFriendService();
      friendService.remarks['2039911742531702784'] = '老板';
      friendService.usernames['2039911742531702784'] = 'xiaoyan';
      Get.put<FriendService>(friendService);

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_numeric_alias_1';
      controller.chatTitle = 'group';
      controller.chatType = 'group';
      sessionService.detailResp = {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 3},
          {
            'member_id': '2039911742531702784',
            'member_type': 1,
            'role': 1,
            'nickname': '小妍',
            'group_nickname': '小妍',
          },
        ],
      };

      await controller.refreshSessionDetail();
      await tester.pump();

      final formatted = controller.formatMessageContentForDisplay(
        '@2039911742531702784 小妍，请采集本周概况',
      );

      expect(formatted, '@老板，请采集本周概况');
    },
  );

  testWidgets('onReady prefetches profiles for numeric mention targets', (
    WidgetTester tester,
  ) async {
    final friendService = _FakeFriendService();
    Get.put<FriendService>(friendService);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_prefetch_mention_1';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 1,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3},
      ],
    };
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'mention-prefetch-1',
        sessionId: controller.sessionId,
        senderId: '9001',
        senderType: 2,
        content: '@2039911742531702784 小妍，请采集本周概况',
        createdAt: 1735689600000,
      ),
    ]);

    controller.onReady();
    await tester.pump();

    expect(
      friendService._pendingProfiles.containsKey('2039911742531702784'),
      isTrue,
    );
  });

  testWidgets('sessionActivityLabel uses user nickname in group chat', (
    WidgetTester tester,
  ) async {
    final friendService = _FakeFriendService();
    friendService.nicknames['1002'] = '老板';
    Get.put<FriendService>(friendService);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_activity_1';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    imService.sessionActivities[controller.sessionId] = [
      SessionActivityModel(
        sessionId: controller.sessionId,
        kind: 'composing',
        active: true,
        actorId: '1002',
        actorType: 'human',
        executorId: '',
        executorType: '',
        source: '',
        refMsgId: '',
        refEventId: '',
        statusText: '',
        updatedAt: 1,
        expiresAt: DateTime.now().millisecondsSinceEpoch + 60 * 1000,
      ),
    ];

    expect(
      controller.sessionActivityLabel,
      'chat_composing_named'.trParams({'name': '老板'}),
    );
  });

  testWidgets('sessionActivityLabel uses agent name for agent actor', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_activity_2';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    agentService.agents.assignAll([
      AgentModel(id: '9001', agentName: 'OpenClaw'),
    ]);
    imService.sessionActivities[controller.sessionId] = [
      SessionActivityModel(
        sessionId: controller.sessionId,
        kind: 'composing',
        active: true,
        actorId: '9001',
        actorType: 'agent',
        executorId: '',
        executorType: '',
        source: '',
        refMsgId: '',
        refEventId: '',
        statusText: '',
        updatedAt: 1,
        expiresAt: DateTime.now().millisecondsSinceEpoch + 60 * 1000,
      ),
    ];

    expect(
      controller.sessionActivityLabel,
      'chat_composing_named'.trParams({'name': 'OpenClaw'}),
    );
  });

  testWidgets(
    'sessionActivityLabel does not show numeric id when unknown user',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_activity_3';
      controller.chatTitle = 'group';
      controller.chatType = 'group';
      imService.sessionActivities[controller.sessionId] = [
        SessionActivityModel(
          sessionId: controller.sessionId,
          kind: 'composing',
          active: true,
          actorId: '202698989848778',
          actorType: 'human',
          executorId: '',
          executorType: '',
          source: '',
          refMsgId: '',
          refEventId: '',
          statusText: '',
          updatedAt: 1,
          expiresAt: DateTime.now().millisecondsSinceEpoch + 60 * 1000,
        ),
      ];

      final fallbackName = 'profile_default_nickname'.tr;
      expect(
        controller.sessionActivityLabel,
        'chat_composing_named'.trParams({'name': fallbackName}),
      );
      expect(
        controller.sessionActivityLabel.contains('202698989848778'),
        isFalse,
      );
    },
  );

  testWidgets('onReady enters session and connects when disconnected', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_1';
    controller.chatTitle = 'session_test_1';
    controller.chatType = 'private';

    controller.onReady();
    await tester.pump();

    expect(imService.enterSessionCalls, 1);
    expect(imService.enteredSessionId, 'session_test_1');
    expect(imService.connectCalls, 1);
    expect(imService.connectUrl, ImService.defaultWsUrl);
    expect(agentService.loadCalls, 1);
    expect(imService.refreshDelegateStatesCalls, 0);
  });

  testWidgets('onReady refreshes delegate states when already connected', (
    WidgetTester tester,
  ) async {
    imService.connected = true;
    imService.authenticated = true;
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_1';
    controller.chatTitle = 'session_test_1';
    controller.chatType = 'private';

    controller.onReady();
    await tester.pump();

    expect(imService.enterSessionCalls, 1);
    expect(imService.connectCalls, 0);
    expect(imService.refreshDelegateStatesCalls, 1);
    expect(agentService.loadCalls, 1);
  });

  testWidgets('onReady ignores callbacks after controller is closed', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_closed_ready';
    controller.chatTitle = 'session_test_closed_ready';
    controller.chatType = 'private';

    expect(await Get.delete<ChatController>(), isTrue);
    expect(controller.isClosed, isTrue);
    expect(() => controller.onReady(), returnsNormally);
    await tester.pump();

    expect(imService.enterSessionCalls, 0);
    expect(imService.connectCalls, 0);
    expect(agentService.loadCalls, 0);
    expect(tester.takeException(), isNull);
  });

  testWidgets('onReady microtask stops when controller closes', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_closed_microtask';
    controller.chatTitle = 'session_test_closed_microtask';
    controller.chatType = 'private';

    controller.onReady();
    expect(await Get.delete<ChatController>(), isTrue);
    await tester.pump();

    expect(imService.enterSessionCalls, 0);
    expect(imService.connectCalls, 0);
    expect(agentService.loadCalls, 0);
    expect(tester.takeException(), isNull);
  });

  testWidgets(
    'onReady does not overwrite existing session title with route title',
    (WidgetTester tester) async {
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'session_title_keep_1',
          title: 'Topic Alpha',
          type: 'private',
          updatedAt: 1,
          lastMessageTime: 1,
        ),
      ]);

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_title_keep_1';
      controller.chatTitle = 'Alice';
      controller.chatType = 'private';

      controller.onReady();
      await tester.pump();

      final idx = imService.sessions.indexWhere(
        (s) => s.sessionId == 'session_title_keep_1',
      );
      expect(idx, isNonNegative);
      expect(imService.sessions[idx].title, 'Topic Alpha');
      expect(controller.displayChatTitle, 'Topic Alpha');
    },
  );

  testWidgets(
    'private chat title shows renamed session name and keeps header avatar visible',
    (WidgetTester tester) async {
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'session_private_title_profile_1',
          title: '记住你叫刘德华',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          peerNickname: 'Liu',
          updatedAt: 1,
          lastMessageTime: 1,
        ),
      ]);

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_private_title_profile_1';
      controller.chatTitle = '记住你叫刘德华';
      controller.chatType = 'private';

      controller.onReady();
      await tester.pump();

      expect(controller.displayChatTitle, '记住你叫刘德华');
      expect(controller.chatSubtitle, '');
      expect(controller.shouldShowHeaderAvatar, isTrue);
    },
  );

  testWidgets(
    'private agent chat title keeps fresher session snapshot over stale agent cache',
    (WidgetTester tester) async {
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'session_private_agent_title_live_1',
          title: 'New Agent Name',
          type: 'private',
          peerId: 'agent-title-live-1',
          peerType: 2,
          peerNickname: 'New Agent Name',
          updatedAt: 1,
          lastMessageTime: 1,
        ),
      ]);
      agentService.agents.assignAll([
        AgentModel(
          id: 'agent-title-live-1',
          agentName: 'Old Agent Name',
          providerType: 3,
          sessionId: 'session_private_agent_title_live_1',
        ),
      ]);

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_private_agent_title_live_1';
      controller.chatTitle = 'New Agent Name';
      controller.chatType = 'private';

      controller.onReady();
      await tester.pump();

      expect(controller.displayChatTitle, 'New Agent Name');
      expect(controller.privatePeerNickname, 'New Agent Name');
    },
  );

  testWidgets('onHeaderAvatarTap routes group chat to group info page', (
    WidgetTester tester,
  ) async {
    Get.testMode = false;

    await tester.pumpWidget(
      GetMaterialApp(
        initialRoute: AppRoutes.chat,
        getPages: [
          GetPage(name: AppRoutes.chat, page: () => const SizedBox.shrink()),
          GetPage(
            name: AppRoutes.groupInfo,
            page: () => const SizedBox.shrink(),
          ),
          GetPage(
            name: AppRoutes.accountInfo,
            page: () => const SizedBox.shrink(),
          ),
        ],
      ),
    );
    await tester.pumpAndSettle();

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_avatar_1';
    controller.chatTitle = 'Dev Group';
    controller.chatType = 'group';

    controller.onHeaderAvatarTap();
    await tester.pumpAndSettle();

    expect(Uri.parse(Get.currentRoute).path, AppRoutes.groupInfo);
    expect(Get.parameters['session_id'], 'session_group_avatar_1');
  });

  testWidgets('onHeaderAvatarTap routes private chat to account info page', (
    WidgetTester tester,
  ) async {
    Get.testMode = false;

    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'session_private_avatar_1',
        title: 'session_private_avatar_1',
        type: 'private',
        peerId: '1001',
        peerType: 1,
        peerNickname: 'Liu',
        peerUsername: 'liu',
        updatedAt: 1,
        lastMessageTime: 1,
      ),
    ]);

    await tester.pumpWidget(
      GetMaterialApp(
        initialRoute: AppRoutes.chat,
        getPages: [
          GetPage(name: AppRoutes.chat, page: () => const SizedBox.shrink()),
          GetPage(
            name: AppRoutes.groupInfo,
            page: () => const SizedBox.shrink(),
          ),
          GetPage(
            name: AppRoutes.accountInfo,
            page: () => const SizedBox.shrink(),
          ),
        ],
      ),
    );
    await tester.pumpAndSettle();

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_private_avatar_1';
    controller.chatTitle = 'Liu';
    controller.chatType = 'private';

    controller.onHeaderAvatarTap();
    await tester.pumpAndSettle();

    expect(Uri.parse(Get.currentRoute).path, AppRoutes.accountInfo);
    expect(Get.parameters['session_id'], 'session_private_avatar_1');
    expect(Get.parameters['peer_id'], '1001');
    expect(Get.parameters['peer_type'], '1');

    final args = Get.arguments as Map<String, dynamic>;
    expect(args['group_key'], 'private:1:1001');
    expect(args['nickname'], 'Liu');
    expect(args['username'], 'liu');
  });

  testWidgets(
    'onHeaderAvatarTap routes private agent chat to account info page',
    (WidgetTester tester) async {
      Get.testMode = false;

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'session_private_agent_avatar_1',
          title: 'Ops Agent',
          type: 'private',
          peerId: 'agent-1',
          peerType: 2,
          peerNickname: 'Ops Agent',
          updatedAt: 1,
          lastMessageTime: 1,
        ),
      ]);
      agentService.agents.assignAll([
        AgentModel(
          id: 'agent-1',
          agentName: 'Ops Agent',
          providerType: 3,
          sessionId: 'session_private_agent_avatar_1',
          avatarUrl: 'https://example.com/avatar/agent-1.png',
        ),
      ]);

      await tester.pumpWidget(
        GetMaterialApp(
          initialRoute: AppRoutes.chat,
          getPages: [
            GetPage(name: AppRoutes.chat, page: () => const SizedBox.shrink()),
            GetPage(
              name: AppRoutes.groupInfo,
              page: () => const SizedBox.shrink(),
            ),
            GetPage(
              name: AppRoutes.accountInfo,
              page: () => const SizedBox.shrink(),
            ),
          ],
        ),
      );
      await tester.pumpAndSettle();

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_private_agent_avatar_1';
      controller.chatTitle = 'Ops Agent';
      controller.chatType = 'private';

      controller.onHeaderAvatarTap();
      await tester.pumpAndSettle();

      expect(Uri.parse(Get.currentRoute).path, AppRoutes.accountInfo);
      expect(Get.parameters['session_id'], 'session_private_agent_avatar_1');
      expect(Get.parameters['peer_id'], 'agent-1');
      expect(Get.parameters['peer_type'], '2');
      expect(Get.parameters['group_key'], 'private:2:agent-1');

      final args = Get.arguments as Map<String, dynamic>;
      expect(args['group_key'], 'private:2:agent-1');
      expect(args['nickname'], 'Ops Agent');
      expect(args['avatar_url'], 'https://example.com/avatar/agent-1.png');
    },
  );

  testWidgets(
    'onMessageAvatarTap routes private self message to account info',
    (WidgetTester tester) async {
      Get.testMode = false;

      await tester.pumpWidget(
        GetMaterialApp(
          initialRoute: AppRoutes.chat,
          getPages: [
            GetPage(name: AppRoutes.chat, page: () => const SizedBox.shrink()),
            GetPage(
              name: AppRoutes.groupInfo,
              page: () => const SizedBox.shrink(),
            ),
            GetPage(
              name: AppRoutes.accountInfo,
              page: () => const SizedBox.shrink(),
            ),
          ],
        ),
      );
      await tester.pumpAndSettle();

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_private_message_avatar_self_1';
      controller.chatTitle = 'Liu';
      controller.chatType = 'private';

      controller.onMessageAvatarTap(
        senderId: 'me',
        senderType: 1,
        isMine: true,
        senderName: 'MeUser',
        senderAvatarUrl: 'https://example.com/avatar/me.png',
      );
      await tester.pumpAndSettle();

      expect(Uri.parse(Get.currentRoute).path, AppRoutes.accountInfo);
      expect(
        Get.parameters['session_id'],
        'session_private_message_avatar_self_1',
      );
      expect(Get.parameters['peer_id'], '42');
      expect(Get.parameters['peer_type'], '1');
      expect(Get.parameters['group_key'], 'private:1:42');

      final args = Get.arguments as Map<String, dynamic>;
      expect(args['nickname'], 'MeUser');
      expect(args['avatar_url'], 'https://example.com/avatar/me.png');
      expect(args['title'], 'MeUser');
    },
  );

  testWidgets(
    'onMessageAvatarTap routes private peer message via account info page',
    (WidgetTester tester) async {
      Get.testMode = false;

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'session_private_message_avatar_peer_1',
          title: 'session_private_message_avatar_peer_1',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          peerNickname: 'Liu',
          peerUsername: 'liu',
          updatedAt: 1,
          lastMessageTime: 1,
        ),
      ]);

      await tester.pumpWidget(
        GetMaterialApp(
          initialRoute: AppRoutes.chat,
          getPages: [
            GetPage(name: AppRoutes.chat, page: () => const SizedBox.shrink()),
            GetPage(
              name: AppRoutes.groupInfo,
              page: () => const SizedBox.shrink(),
            ),
            GetPage(
              name: AppRoutes.accountInfo,
              page: () => const SizedBox.shrink(),
            ),
          ],
        ),
      );
      await tester.pumpAndSettle();

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_private_message_avatar_peer_1';
      controller.chatTitle = 'Liu';
      controller.chatType = 'private';

      controller.onMessageAvatarTap(
        senderId: '1001',
        senderType: 1,
        isMine: false,
        senderName: 'Liu',
        senderAvatarUrl: '',
      );
      await tester.pumpAndSettle();

      expect(Uri.parse(Get.currentRoute).path, AppRoutes.accountInfo);
      expect(
        Get.parameters['session_id'],
        'session_private_message_avatar_peer_1',
      );
      expect(Get.parameters['peer_id'], '1001');
      expect(Get.parameters['peer_type'], '1');

      final args = Get.arguments as Map<String, dynamic>;
      expect(args['group_key'], 'private:1:1001');
      expect(args['nickname'], 'Liu');
      expect(args['username'], 'liu');
    },
  );

  testWidgets(
    'onMessageAvatarTap routes private agent message to account info page',
    (WidgetTester tester) async {
      Get.testMode = false;

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'session_private_agent_message_avatar_1',
          title: 'Ops Agent',
          type: 'private',
          peerId: 'agent-1',
          peerType: 2,
          peerNickname: 'Ops Agent',
          updatedAt: 1,
          lastMessageTime: 1,
        ),
      ]);
      agentService.agents.assignAll([
        AgentModel(
          id: 'agent-1',
          agentName: 'Ops Agent',
          providerType: 3,
          sessionId: 'session_private_agent_message_avatar_1',
          avatarUrl: 'https://example.com/avatar/agent-1.png',
        ),
      ]);

      await tester.pumpWidget(
        GetMaterialApp(
          initialRoute: AppRoutes.chat,
          getPages: [
            GetPage(name: AppRoutes.chat, page: () => const SizedBox.shrink()),
            GetPage(
              name: AppRoutes.groupInfo,
              page: () => const SizedBox.shrink(),
            ),
            GetPage(
              name: AppRoutes.accountInfo,
              page: () => const SizedBox.shrink(),
            ),
          ],
        ),
      );
      await tester.pumpAndSettle();

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_private_agent_message_avatar_1';
      controller.chatTitle = 'Ops Agent';
      controller.chatType = 'private';

      controller.onMessageAvatarTap(
        senderId: 'agent-1',
        senderType: 2,
        isMine: false,
        senderName: 'Ops Agent',
        senderAvatarUrl: '',
      );
      await tester.pumpAndSettle();

      expect(Uri.parse(Get.currentRoute).path, AppRoutes.accountInfo);
      expect(
        Get.parameters['session_id'],
        'session_private_agent_message_avatar_1',
      );
      expect(Get.parameters['peer_id'], 'agent-1');
      expect(Get.parameters['peer_type'], '2');
      expect(Get.parameters['group_key'], 'private:2:agent-1');

      final args = Get.arguments as Map<String, dynamic>;
      expect(args['group_key'], 'private:2:agent-1');
      expect(args['nickname'], 'Ops Agent');
      expect(args['avatar_url'], 'https://example.com/avatar/agent-1.png');
    },
  );

  testWidgets(
    'onMessageCardTap routes user profile card to account info page',
    (WidgetTester tester) async {
      Get.testMode = false;

      await tester.pumpWidget(
        GetMaterialApp(
          initialRoute: AppRoutes.chat,
          getPages: [
            GetPage(name: AppRoutes.chat, page: () => const SizedBox.shrink()),
            GetPage(
              name: AppRoutes.groupInfo,
              page: () => const SizedBox.shrink(),
            ),
            GetPage(
              name: AppRoutes.accountInfo,
              page: () => const SizedBox.shrink(),
            ),
          ],
        ),
      );
      await tester.pumpAndSettle();

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_profile_card_tap_1';
      controller.chatTitle = 'Dev Group';
      controller.chatType = 'group';

      final envelope = ChatMessageCardCodec.buildUserProfileCard(
        userId: '1001',
        nickname: 'Liu',
        avatarUrl: 'https://example.com/avatar/liu.png',
      );

      controller.onMessageCardTap(envelope.card);
      await tester.pumpAndSettle();

      expect(Uri.parse(Get.currentRoute).path, AppRoutes.accountInfo);
      expect(Get.parameters['session_id'], 'session_profile_card_tap_1');
      expect(Get.parameters['peer_id'], '1001');
      expect(Get.parameters['peer_type'], '1');
      expect(Get.parameters['group_key'], 'private:1:1001');

      final args = Get.arguments as Map<String, dynamic>;
      expect(args['nickname'], 'Liu');
      expect(args['title'], 'Liu');
      expect(args['avatar_url'], 'https://example.com/avatar/liu.png');
    },
  );

  testWidgets(
    'onMessageCardTap routes agent profile card to account info page',
    (WidgetTester tester) async {
      Get.testMode = false;

      agentService.agents.assignAll([
        AgentModel(
          id: 'agent-9',
          agentName: 'Ops Agent',
          providerType: 3,
          sessionId: 'session-agent-profile-card-1',
          avatarUrl: 'https://example.com/avatar/agent-9.png',
        ),
      ]);

      await tester.pumpWidget(
        GetMaterialApp(
          initialRoute: AppRoutes.chat,
          getPages: [
            GetPage(name: AppRoutes.chat, page: () => const SizedBox.shrink()),
            GetPage(
              name: AppRoutes.groupInfo,
              page: () => const SizedBox.shrink(),
            ),
            GetPage(
              name: AppRoutes.accountInfo,
              page: () => const SizedBox.shrink(),
            ),
          ],
        ),
      );
      await tester.pumpAndSettle();

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_profile_card_tap_agent_1';
      controller.chatTitle = 'Dev Group';
      controller.chatType = 'group';

      controller.onMessageCardTap(
        const ChatUserProfileCardData(
          userId: 'agent-9',
          peerType: 2,
          nickname: 'Ops Agent',
          avatarUrl: '',
        ),
      );
      await tester.pumpAndSettle();

      expect(Uri.parse(Get.currentRoute).path, AppRoutes.accountInfo);
      expect(Get.parameters['session_id'], 'session_profile_card_tap_agent_1');
      expect(Get.parameters['peer_id'], 'agent-9');
      expect(Get.parameters['peer_type'], '2');
      expect(Get.parameters['group_key'], 'private:2:agent-9');

      final args = Get.arguments as Map<String, dynamic>;
      expect(args['nickname'], 'Ops Agent');
      expect(args['title'], 'Ops Agent');
      expect(args['avatar_url'], 'https://example.com/avatar/agent-9.png');
    },
  );

  testWidgets('onMessageCardTap routes conversation card to chat page', (
    WidgetTester tester,
  ) async {
    Get.testMode = false;

    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'session-conversation-card-1',
        title: '产品群',
        type: 'group',
        updatedAt: 1,
        lastMessageTime: 1,
      ),
    ]);

    await tester.pumpWidget(
      GetMaterialApp(
        initialRoute: '/home',
        getPages: [
          GetPage(name: '/home', page: () => const SizedBox.shrink()),
          GetPage(name: AppRoutes.chat, page: () => const SizedBox.shrink()),
          GetPage(
            name: AppRoutes.accountInfo,
            page: () => const SizedBox.shrink(),
          ),
        ],
      ),
    );
    await tester.pumpAndSettle();

    final controller = ChatController();
    addTearDown(controller.dispose);
    controller.sessionId = 'session_profile_card_tap_1';
    controller.chatTitle = 'Dev Group';
    controller.chatType = 'group';

    controller.onMessageCardTap(
      const ChatConversationCardData(
        sessionId: 'session-conversation-card-1',
        sessionType: 'group',
        title: '产品群',
      ),
    );
    await tester.pumpAndSettle();

    expect(Uri.parse(Get.currentRoute).path, AppRoutes.chat);
    expect(Get.parameters['session_id'], 'session-conversation-card-1');
    expect(Get.parameters['title'], '产品群');
    expect(Get.parameters['type'], 'group');

    final args = Get.arguments as Map<String, dynamic>;
    expect(args['session_id'], 'session-conversation-card-1');
    expect(args['title'], '产品群');
    expect(args['type'], 'group');

    Get.back<void>();
    await tester.pumpAndSettle();
  });

  testWidgets('onMessageCardTap ignores unknown conversation session', (
    WidgetTester tester,
  ) async {
    Get.testMode = false;

    await tester.pumpWidget(
      GetMaterialApp(
        initialRoute: '/home',
        getPages: [
          GetPage(name: '/home', page: () => const SizedBox.shrink()),
          GetPage(name: AppRoutes.chat, page: () => const SizedBox.shrink()),
        ],
      ),
    );
    await tester.pumpAndSettle();

    final controller = ChatController();
    addTearDown(controller.dispose);
    controller.sessionId = 'source-session';
    controller.chatTitle = 'Source';
    controller.chatType = 'group';

    controller.onMessageCardTap(
      const ChatConversationCardData(
        sessionId: 'unknown-session',
        sessionType: 'group',
        title: '不存在的群',
      ),
    );
    await tester.pumpAndSettle();

    expect(Uri.parse(Get.currentRoute).path, '/home');
    expect(imService.findSessionById('unknown-session'), isNull);

    // 清理 Toast timer
    await tester.pump(const Duration(seconds: 4));
  });

  testWidgets('onMessageCardAction sends exec approval command', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(const GetMaterialApp(home: SizedBox.shrink()));
    await tester.pumpAndSettle();

    final controller = ChatController();
    controller.sessionId = 'session-exec-approval-card-1';
    controller.chatTitle = 'Dev Group';
    controller.chatType = 'group';

    await controller.onMessageCardAction(
      const ChatMessageCardAction(
        card: ChatExecApprovalCardData(
          approvalId: 'approval_full_123',
          approvalSlug: 'req_123',
          approvalCommandId: 'approval_full_123',
          command: 'pwd',
          host: 'gateway',
          allowedDecisions: ['allow-once', 'allow-always', 'deny'],
        ),
        actionId: 'allow-once',
      ),
    );
    await tester.pumpAndSettle();

    expect(imService.sendCalls, 1);
    expect(imService.sentSessionId, 'session-exec-approval-card-1');
    expect(imService.sentUpdateCurrentSessionUi, isFalse);
    expect(
      imService.sentContent,
      '[[exec-approval-resolution|approval_id=approval_full_123|approval_command_id=approval_full_123|decision=allow-once]]',
    );
  });

  testWidgets('onMessageCardAction sends Claude approval command', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(const GetMaterialApp(home: SizedBox.shrink()));
    await tester.pumpAndSettle();

    final controller = ChatController();
    controller.sessionId = 'session-claude-approval-card-1';
    controller.chatTitle = 'Claude Debug';
    controller.chatType = 'private';

    await controller.onMessageCardAction(
      const ChatMessageCardAction(
        card: ChatExecApprovalCardData(
          approvalId: 'req-123',
          approvalSlug: 'req-123',
          approvalCommandId: 'req-123',
          command: 'Tool: Bash\nCommand: pwd',
          host: 'Claude Grix',
          allowedDecisions: ['allow-once', 'deny'],
          decisionCommands: {
            'allow-once': '/grix approval req-123 allow',
            'deny': '/grix approval req-123 deny',
          },
        ),
        actionId: 'allow-once',
      ),
    );
    await tester.pumpAndSettle();

    expect(imService.sendCalls, 1);
    expect(imService.sentSessionId, 'session-claude-approval-card-1');
    expect(imService.sentUpdateCurrentSessionUi, isFalse);
    expect(imService.sentContent, '/grix approval req-123 allow');
  });

  testWidgets('onMessageCardAction sends Claude approval rule command', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(const GetMaterialApp(home: SizedBox.shrink()));
    await tester.pumpAndSettle();

    final controller = ChatController();
    controller.sessionId = 'session-claude-approval-card-rule-1';
    controller.chatTitle = 'Claude Debug';
    controller.chatType = 'private';

    await controller.onMessageCardAction(
      const ChatMessageCardAction(
        card: ChatExecApprovalCardData(
          approvalId: 'req-rule-1',
          approvalSlug: 'req-rule-1',
          approvalCommandId: 'req-rule-1',
          command: 'Tool: Bash\nCommand: pwd',
          host: 'Claude Grix',
          allowedDecisions: ['allow-once', 'allow-rule:1', 'deny'],
          decisionCommands: {
            'allow-once': '/grix approval req-rule-1 allow',
            'allow-rule:1': '/grix approval req-rule-1 allow-rule 1',
            'deny': '/grix approval req-rule-1 deny',
          },
        ),
        actionId: 'allow-rule:1',
      ),
    );
    await tester.pumpAndSettle();

    expect(imService.sendCalls, 1);
    expect(imService.sentSessionId, 'session-claude-approval-card-rule-1');
    expect(imService.sentUpdateCurrentSessionUi, isFalse);
    expect(imService.sentContent, '/grix approval req-rule-1 allow-rule 1');
  });

  testWidgets('onMessageCardAction blocks duplicate exec approval submits', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(const GetMaterialApp(home: SizedBox.shrink()));
    await tester.pumpAndSettle();

    final controller = ChatController();
    controller.sessionId = 'session-exec-approval-card-2';
    controller.chatTitle = 'Dev Group';
    controller.chatType = 'group';

    const action = ChatMessageCardAction(
      card: ChatExecApprovalCardData(
        approvalId: 'approval_full_456',
        approvalSlug: 'req_456',
        approvalCommandId: 'approval_full_456',
        command: 'pwd',
        host: 'gateway',
        allowedDecisions: ['allow-once', 'allow-always', 'deny'],
      ),
      actionId: 'allow-once',
    );

    await controller.onMessageCardAction(action);
    await tester.pumpAndSettle();
    await controller.onMessageCardAction(action);
    await tester.pumpAndSettle();

    expect(imService.sendCalls, 1);
    expect(
      imService.sentContent,
      '[[exec-approval-resolution|approval_id=approval_full_456|approval_command_id=approval_full_456|decision=allow-once]]',
    );

    // 清理 Toast timer
    await tester.pump(const Duration(seconds: 4));
  });

  testWidgets(
    'syncExecApprovalActionLocks clears pending on expired approval status',
    (WidgetTester tester) async {
      await tester.pumpWidget(const GetMaterialApp(home: SizedBox.shrink()));
      await tester.pumpAndSettle();

      final controller = Get.put(ChatController());
      controller.sessionId = 'session-exec-approval-card-expired-1';
      controller.chatTitle = 'Dev Group';
      controller.chatType = 'group';

      const approvalAction = ChatMessageCardAction(
        card: ChatExecApprovalCardData(
          approvalId: 'approval_full_expired',
          approvalSlug: 'req_expired',
          approvalCommandId: 'approval_full_expired',
          command: 'pwd',
          host: 'gateway',
          allowedDecisions: ['allow-once', 'deny'],
        ),
        actionId: 'allow-once',
      );

      await controller.onMessageCardAction(approvalAction);
      await tester.pumpAndSettle();
      expect(
        controller.isExecApprovalActionPending('approval_full_expired'),
        isTrue,
      );

      final expiredEnvelope = ChatMessageCardCodec.buildExecStatusCard(
        status: 'approval-expired',
        summary: 'Exec approval expired.',
        approvalId: 'approval_full_expired',
        warningText: 'This approval request is no longer valid.',
      );
      imService.currentMessages.assignAll([
        MessageModel(
          msgId: 'expired-1',
          sessionId: controller.sessionId,
          senderId: 'agent-1',
          createdAt: 1000,
          content: expiredEnvelope.content,
          extra: expiredEnvelope.extra,
        ),
      ]);

      controller.syncExecApprovalActionLocks();

      expect(
        controller.isExecApprovalActionPending('approval_full_expired'),
        isFalse,
      );
    },
  );

  testWidgets(
    'syncExecApprovalActionLocks clears pending on allow-rule resolution',
    (WidgetTester tester) async {
      await tester.pumpWidget(const GetMaterialApp(home: SizedBox.shrink()));
      await tester.pumpAndSettle();

      final controller = Get.put(ChatController());
      controller.sessionId = 'session-exec-approval-card-rule-2';
      controller.chatTitle = 'Dev Group';
      controller.chatType = 'group';

      const approvalAction = ChatMessageCardAction(
        card: ChatExecApprovalCardData(
          approvalId: 'approval_full_rule',
          approvalSlug: 'req_rule',
          approvalCommandId: 'approval_full_rule',
          command: 'pwd',
          host: 'gateway',
          allowedDecisions: ['allow-once', 'deny'],
        ),
        actionId: 'allow-once',
      );

      await controller.onMessageCardAction(approvalAction);
      await tester.pumpAndSettle();
      expect(
        controller.isExecApprovalActionPending('approval_full_rule'),
        isTrue,
      );

      const resolvedStatus = ChatExecStatusCardData(
        status: 'resolved-allow-rule',
        summary: 'Exec approval allowed by rule.',
        approvalId: 'approval_full_rule',
        decision: 'allow-rule',
      );
      final resolvedEnvelope = ChatMessageCardCodec.encode(resolvedStatus);
      imService.currentMessages.assignAll([
        MessageModel(
          msgId: 'rule-1',
          sessionId: controller.sessionId,
          senderId: 'agent-1',
          createdAt: 1000,
          content: resolvedEnvelope.content,
          extra: resolvedEnvelope.extra,
        ),
      ]);

      controller.syncExecApprovalActionLocks();

      expect(
        controller.isExecApprovalActionPending('approval_full_rule'),
        isFalse,
      );
    },
  );

  testWidgets('onMessageCardAction sends Claude question quick reply', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(const GetMaterialApp(home: SizedBox.shrink()));
    await tester.pumpAndSettle();

    final controller = ChatController();
    controller.sessionId = 'session-claude-question-card-1';
    controller.chatTitle = 'Claude Debug';
    controller.chatType = 'private';
    const card = ChatAgentQuestionCardData(
      requestId: 'question-1',
      questions: [
        ChatAgentQuestionPrompt(
          index: 1,
          header: 'Environment',
          prompt: 'Choose the deployment target.',
          options: ['prod', 'staging'],
        ),
      ],
    );
    final actionId = ChatAgentCardActionEncoder.buildQuestionQuickReplyAction(
      card,
      'staging',
    );

    await controller.onMessageCardAction(
      ChatMessageCardAction(card: card, actionId: actionId),
    );
    await tester.pumpAndSettle();

    expect(imService.sendCalls, 1);
    expect(imService.sentSessionId, 'session-claude-question-card-1');
    expect(imService.sentUpdateCurrentSessionUi, isFalse);
    expect(imService.sentContent, actionId);
  });

  testWidgets('onMessageCardAction sends Claude question command directly', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(const GetMaterialApp(home: SizedBox.shrink()));
    await tester.pumpAndSettle();

    final controller = ChatController();
    controller.sessionId = 'session-claude-question-card-2';
    controller.chatTitle = 'Claude Debug';
    controller.chatType = 'private';
    const card = ChatAgentQuestionCardData(
      requestId: 'question-2',
      questions: [
        ChatAgentQuestionPrompt(
          index: 1,
          header: 'Environment',
          prompt: 'Choose the deployment target.',
        ),
        ChatAgentQuestionPrompt(
          index: 2,
          header: 'Region',
          prompt: 'Choose the deployment region.',
        ),
      ],
    );
    final actionId =
        ChatAgentCardActionEncoder.buildQuestionStructuredReplyAction(
          card,
          const {1: 'prod', 2: 'cn-hz'},
        );

    await controller.onMessageCardAction(
      ChatMessageCardAction(card: card, actionId: actionId),
    );
    await tester.pumpAndSettle();

    expect(imService.sendCalls, 1);
    expect(imService.sentSessionId, 'session-claude-question-card-2');
    expect(imService.sentUpdateCurrentSessionUi, isFalse);
    expect(imService.sentContent, actionId);
  });

  testWidgets(
    'onMessageCardAction keeps request context when answer looks like a command',
    (WidgetTester tester) async {
      await tester.pumpWidget(const GetMaterialApp(home: SizedBox.shrink()));
      await tester.pumpAndSettle();

      final controller = ChatController();
      controller.sessionId = 'session-claude-question-card-3';
      controller.chatTitle = 'Claude Debug';
      controller.chatType = 'private';
      const card = ChatAgentQuestionCardData(
        requestId: 'question-3',
        questions: [
          ChatAgentQuestionPrompt(
            index: 1,
            header: 'Command-like answer',
            prompt: 'Choose the literal answer.',
            options: ['/grix question is literal text'],
          ),
        ],
      );
      final actionId = ChatAgentCardActionEncoder.buildQuestionQuickReplyAction(
        card,
        '/grix question is literal text',
      );

      await controller.onMessageCardAction(
        ChatMessageCardAction(card: card, actionId: actionId),
      );
      await tester.pumpAndSettle();

      expect(imService.sendCalls, 1);
      expect(imService.sentSessionId, 'session-claude-question-card-3');
      expect(imService.sentUpdateCurrentSessionUi, isFalse);
      expect(imService.sentContent, actionId);
    },
  );

  testWidgets('onMessageCardAction sends Claude open command', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(const GetMaterialApp(home: SizedBox.shrink()));
    await tester.pumpAndSettle();

    final controller = ChatController();
    controller.sessionId = 'session-claude-open-card-1';
    controller.chatTitle = 'Claude Debug';
    controller.chatType = 'private';
    const card = ChatAgentOpenSessionCardData(
      summaryText: 'open 缺少目录路径。',
      detailText: '请输入工作目录来启动或恢复 Claude 会话。',
    );
    final actionId = ChatAgentCardActionEncoder.buildOpenSessionAction(
      card,
      '/workspace/demo',
    );

    await controller.onMessageCardAction(
      ChatMessageCardAction(card: card, actionId: actionId),
    );
    await tester.pumpAndSettle();

    expect(imService.sendCalls, 1);
    expect(imService.sentSessionId, 'session-claude-open-card-1');
    expect(imService.sentUpdateCurrentSessionUi, isFalse);
    expect(imService.sentContent, actionId);
  });

  testWidgets(
    'group route seeds header avatar members before session detail loads',
    (WidgetTester tester) async {
      Get.testMode = false;

      await tester.pumpWidget(
        GetMaterialApp(
          initialRoute: '/home',
          getPages: [
            GetPage(name: '/home', page: () => const SizedBox.shrink()),
            GetPage(name: AppRoutes.chat, page: () => const SizedBox.shrink()),
          ],
        ),
      );
      await tester.pumpAndSettle();

      unawaited(
        ChatRouteNavigator.toChat(
          sessionId: 'group_avatar_seed_1',
          title: '群聊',
          type: 'group',
          initialGroupAvatarMembers: const <SessionAvatarMember>[
            SessionAvatarMember(
              memberId: '1001',
              memberType: 1,
              displayName: 'Alice',
              avatarUrl: 'https://example.com/a.png',
            ),
            SessionAvatarMember(
              memberId: '1002',
              memberType: 1,
              displayName: 'Bob',
              avatarUrl: 'https://example.com/b.png',
            ),
          ],
        ),
      );
      await tester.pumpAndSettle();

      final args = Get.arguments as Map<String, dynamic>;
      expect(args['initial_group_avatar_members'], isA<List<dynamic>>());

      final controller = ChatController();
      addTearDown(controller.onClose);
      controller.onInit();

      expect(controller.chatType, 'group');
      expect(controller.groupAvatarMembers, hasLength(2));
      expect(controller.groupAvatarMembers.first.memberId, '1001');
      expect(controller.groupAvatarMembers.first.displayName, 'Alice');
      expect(
        controller.groupAvatarMembers.first.avatarUrl,
        'https://example.com/a.png',
      );
    },
  );

  testWidgets('private route seeds text drafted while session was creating', (
    WidgetTester tester,
  ) async {
    Get.testMode = false;

    await tester.pumpWidget(
      GetMaterialApp(
        initialRoute: '/home',
        getPages: [
          GetPage(name: '/home', page: () => const SizedBox.shrink()),
          GetPage(name: AppRoutes.chat, page: () => const SizedBox.shrink()),
        ],
      ),
    );
    await tester.pumpAndSettle();

    unawaited(
      ChatRouteNavigator.toChat(
        sessionId: 'private_creation_draft_1',
        title: 'OpenCode',
        type: 'private',
        initialDraft: '先写好这条消息',
      ),
    );
    await tester.pumpAndSettle();

    final args = Get.arguments as Map<String, dynamic>;
    expect(args['initial_draft'], '先写好这条消息');

    final controller = ChatController();
    addTearDown(controller.onClose);
    controller.onInit();

    expect(controller.sessionId, 'private_creation_draft_1');
    expect(controller.inputController.text, '先写好这条消息');
  });

  testWidgets(
    'toChat dismisses active input interaction before replacing controller',
    (WidgetTester tester) async {
      Get.testMode = false;

      await tester.pumpWidget(
        GetMaterialApp(
          initialRoute:
              '/chat?session_id=session_switch_old&title=old&type=private',
          getPages: [
            GetPage(name: '/home', page: () => const SizedBox.shrink()),
            GetPage(name: AppRoutes.chat, page: () => const SizedBox.shrink()),
          ],
        ),
      );
      await tester.pumpAndSettle();

      final controller =
          Get.put<ChatController>(
                _DismissSpyChatController(),
                tag: ChatBinding.controllerTagForSession('session_switch_old'),
              )
              as _DismissSpyChatController;
      controller.sessionId = 'session_switch_old';
      controller.chatTitle = 'old';
      controller.chatType = 'private';

      unawaited(
        ChatRouteNavigator.toChat(
          sessionId: 'session_switch_new',
          title: 'new',
          type: 'private',
        ),
      );
      await tester.pumpAndSettle();

      expect(controller.dismissInputInteractionCalled, isTrue);
    },
  );

  testWidgets(
    'toChat from non-chat route does not touch stale chat controllers',
    (WidgetTester tester) async {
      Get.testMode = false;

      await tester.pumpWidget(
        GetMaterialApp(
          initialRoute: '/home',
          getPages: [
            GetPage(name: '/home', page: () => const SizedBox.shrink()),
            GetPage(name: AppRoutes.chat, page: () => const SizedBox.shrink()),
          ],
        ),
      );
      await tester.pumpAndSettle();

      final controller =
          Get.put<ChatController>(_DismissSpyChatController())
              as _DismissSpyChatController;
      controller.sessionId = 'session_stale';
      controller.chatTitle = 'stale';
      controller.chatType = 'private';

      unawaited(
        ChatRouteNavigator.toChat(
          sessionId: 'session_switch_new',
          title: 'new',
          type: 'private',
        ),
      );
      await tester.pumpAndSettle();

      expect(controller.dismissInputInteractionCalled, isFalse);
    },
  );

  testWidgets(
    'toChat pushes a new route for chat-to-chat jumps so back returns to '
    'the previous chat',
    (WidgetTester tester) async {
      Get.testMode = false;

      await tester.pumpWidget(
        GetMaterialApp(
          initialRoute: '/home',
          getPages: [
            GetPage(name: '/home', page: () => const SizedBox.shrink()),
            GetPage(name: AppRoutes.chat, page: () => const SizedBox.shrink()),
          ],
        ),
      );
      await tester.pumpAndSettle();

      unawaited(
        ChatRouteNavigator.toChat(
          sessionId: 'session_chat_replace_a',
          title: 'A',
          type: 'group',
        ),
      );
      await tester.pumpAndSettle();
      expect(Get.currentRoute.startsWith(AppRoutes.chat), isTrue);
      expect(Get.currentRoute.contains('session_chat_replace_a'), isTrue);

      unawaited(
        ChatRouteNavigator.toChat(
          sessionId: 'session_chat_replace_b',
          title: 'B',
          type: 'group',
        ),
      );
      await tester.pumpAndSettle();
      expect(Get.currentRoute.startsWith(AppRoutes.chat), isTrue);
      expect(Get.currentRoute.contains('session_chat_replace_b'), isTrue);

      // 跳转到不同会话使用 push：返回时应回到上一个聊天页（A），而非首页。
      Get.back<void>();
      await tester.pumpAndSettle();
      expect(Get.currentRoute.startsWith(AppRoutes.chat), isTrue);
      expect(Get.currentRoute.contains('session_chat_replace_a'), isTrue);

      // 再返回一次才回到首页。
      Get.back<void>();
      await tester.pumpAndSettle();
      expect(Get.currentRoute, '/home');
    },
  );

  testWidgets(
    'toChat stays put with no navigation when re-opening the active chat '
    'session',
    (WidgetTester tester) async {
      Get.testMode = false;

      await tester.pumpWidget(
        GetMaterialApp(
          initialRoute: '/home',
          getPages: [
            GetPage(name: '/home', page: () => const SizedBox.shrink()),
            GetPage(name: AppRoutes.chat, page: () => const SizedBox.shrink()),
          ],
        ),
      );
      await tester.pumpAndSettle();

      unawaited(
        ChatRouteNavigator.toChat(
          sessionId: 'session_chat_same',
          title: 'Same',
          type: 'group',
        ),
      );
      await tester.pumpAndSettle();
      expect(Get.currentRoute.startsWith(AppRoutes.chat), isTrue);
      final routeBefore = Get.currentRoute;

      // 重新打开当前已在浏览的同一会话：保持无反应，不跳转、不改变路由栈。
      unawaited(
        ChatRouteNavigator.toChat(
          sessionId: 'session_chat_same',
          title: 'Same',
          type: 'group',
        ),
      );
      await tester.pumpAndSettle();
      expect(Get.currentRoute, routeBefore);

      // 只压入过一个聊天页：一次返回即回到首页。
      Get.back<void>();
      await tester.pumpAndSettle();
      expect(Get.currentRoute, '/home');
    },
  );

  testWidgets('onClose releases focused input before disposing nodes', (
    WidgetTester tester,
  ) async {
    final controller = ChatController();
    controller.sessionId = 'session_close_focus_cleanup';
    controller.chatTitle = 'session_close_focus_cleanup';
    controller.chatType = 'private';

    await tester.pumpWidget(
      GetMaterialApp(
        home: Scaffold(
          body: TextField(
            focusNode: controller.focusNode,
            controller: controller.inputController,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byType(TextField));
    await tester.pump();
    expect(controller.focusNode.hasFocus, isTrue);
    expect(FocusManager.instance.primaryFocus, same(controller.focusNode));

    controller.onClose();
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump();

    expect(
      FocusManager.instance.primaryFocus,
      isNot(same(controller.focusNode)),
    );
    expect(tester.takeException(), isNull);
  });

  testWidgets('onReady redirects to home when session id is missing', (
    WidgetTester tester,
  ) async {
    Get.testMode = false;

    await tester.pumpWidget(
      GetMaterialApp(
        initialRoute: '/chat',
        getPages: [
          GetPage(name: '/chat', page: () => const SizedBox.shrink()),
          GetPage(name: '/home', page: () => const SizedBox.shrink()),
          GetPage(name: '/login', page: () => const SizedBox.shrink()),
        ],
      ),
    );
    await tester.pumpAndSettle();

    final controller = Get.put(ChatController());
    controller.sessionId = '';
    controller.chatTitle = '';
    controller.chatType = 'private';

    controller.onReady();
    await tester.pumpAndSettle();

    expect(Get.currentRoute, '/home');
    expect(imService.enterSessionCalls, 0);
    expect(imService.connectCalls, 0);
  });

  testWidgets(
    'closeChatRoute pops to previous route when chat has back stack',
    (WidgetTester tester) async {
      Get.testMode = false;

      await tester.pumpWidget(
        GetMaterialApp(
          initialRoute: AppRoutes.home,
          getPages: [
            GetPage(name: AppRoutes.home, page: () => const SizedBox.shrink()),
            GetPage(name: AppRoutes.chat, page: () => const SizedBox.shrink()),
          ],
        ),
      );
      await tester.pumpAndSettle();

      Get.toNamed(AppRoutes.chat);
      await tester.pumpAndSettle();
      expect(Uri.parse(Get.currentRoute).path, AppRoutes.chat);

      final controller = Get.put(ChatController());
      controller.closeChatRoute();
      await tester.pumpAndSettle();

      expect(Uri.parse(Get.currentRoute).path, AppRoutes.home);
    },
  );

  testWidgets('closeChatRoute resets to home when chat is root route', (
    WidgetTester tester,
  ) async {
    Get.testMode = false;

    await tester.pumpWidget(
      GetMaterialApp(
        initialRoute: AppRoutes.chat,
        getPages: [
          GetPage(name: AppRoutes.chat, page: () => const SizedBox.shrink()),
          GetPage(name: AppRoutes.home, page: () => const SizedBox.shrink()),
        ],
      ),
    );
    await tester.pumpAndSettle();
    expect(Uri.parse(Get.currentRoute).path, AppRoutes.chat);

    final controller = Get.put(ChatController());
    controller.closeChatRoute();
    await tester.pumpAndSettle();

    expect(Uri.parse(Get.currentRoute).path, AppRoutes.home);
  });

  testWidgets('scrolling near top loads history with in-flight guard', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_1';
    controller.chatTitle = 'session_test_1';
    controller.chatType = 'private';
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'msg-1',
        sessionId: 'session_test_1',
        senderId: '42',
        content: 'hello',
        createdAt: 1735689600000,
      ),
    ]);
    controller.onReady();

    await tester.pumpWidget(
      GetMaterialApp(
        home: SizedBox(
          height: 300,
          child: ListView.builder(
            controller: controller.scrollController,
            itemCount: 120,
            itemBuilder: (_, __) => const SizedBox(height: 40),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    controller.scrollToBottom();
    await tester.pump();

    controller.scrollController.jumpTo(300);
    await tester.pump();

    imService.loadMoreCompleter = Completer<void>();
    controller.scrollController.jumpTo(0);
    await tester.pump();
    expect(imService.loadMoreCalls, 1);

    controller.scrollController.jumpTo(30);
    await tester.pump();
    controller.scrollController.jumpTo(0);
    await tester.pump();
    expect(imService.loadMoreCalls, 1);

    imService.loadMoreCompleter?.complete();
    await tester.pump();

    imService.loadMoreCompleter = Completer<void>();
    controller.scrollController.jumpTo(80);
    await tester.pump();
    controller.scrollController.jumpTo(0);
    await tester.pump();
    expect(imService.loadMoreCalls, 2);
  });

  testWidgets('loading older at top keeps viewport pinned to top', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_top_pin';
    controller.chatTitle = 'session_test_top_pin';
    controller.chatType = 'private';
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'msg-top-pin',
        sessionId: 'session_test_top_pin',
        senderId: '42',
        content: 'hello',
        createdAt: 1735689600000,
      ),
    ]);
    controller.onReady();

    final itemCount = ValueNotifier<int>(40);
    imService.onLoadOlder = () {
      itemCount.value += 20;
    };

    await tester.pumpWidget(
      GetMaterialApp(
        home: ValueListenableBuilder<int>(
          valueListenable: itemCount,
          builder: (_, count, __) {
            return SizedBox(
              height: 300,
              child: ListView.builder(
                controller: controller.scrollController,
                itemCount: count,
                itemBuilder: (_, __) => const SizedBox(height: 40),
              ),
            );
          },
        ),
      ),
    );
    await tester.pumpAndSettle();

    controller.scrollToBottom();
    await tester.pump();
    expect(controller.scrollController.offset, greaterThan(0));

    controller.scrollController.jumpTo(0);
    await tester.pumpAndSettle();

    expect(imService.loadMoreCalls, 1);
    expect(controller.scrollController.offset, lessThanOrEqualTo(1));
  });

  testWidgets(
    'loading older with resident trim keeps the visible anchor stable',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_history_anchor_trim';
      controller.chatTitle = 'session_test_history_anchor_trim';
      controller.chatType = 'private';

      MessageModel buildMessage(int id) {
        return MessageModel(
          msgId: 'msg-anchor-$id',
          sessionId: 'session_test_history_anchor_trim',
          senderId: '42',
          content: 'anchor_message_$id',
          createdAt: id,
        );
      }

      final messageWindow = ValueNotifier<List<MessageModel>>(
        List.generate(100, (index) => buildMessage(index + 20)),
      );
      imService.currentMessages.assignAll(messageWindow.value);
      imService.onLoadOlder = () {
        final nextWindow = List.generate(100, buildMessage);
        messageWindow.value = nextWindow;
        imService.currentMessages.assignAll(nextWindow);
      };
      controller.onReady();

      await tester.pumpWidget(
        GetMaterialApp(
          home: ValueListenableBuilder<List<MessageModel>>(
            valueListenable: messageWindow,
            builder: (_, messages, __) {
              return SizedBox(
                height: 300,
                child: ListView.builder(
                  controller: controller.scrollController,
                  cacheExtent: 1200,
                  itemCount: messages.length,
                  itemBuilder: (_, index) {
                    final message = messages[index];
                    final itemKey = ChatMessageIdentity.selectionKey(message);
                    return KeyedSubtree(
                      key: ValueKey(itemKey),
                      child: SizedBox(
                        key: controller.messageViewportItemGlobalKey(itemKey),
                        height: 40,
                        child: Text(message.content),
                      ),
                    );
                  },
                ),
              );
            },
          ),
        ),
      );
      await tester.pumpAndSettle();

      controller.onUserScrollStart(controller.scrollController.position);
      controller.scrollController.jumpTo(80);
      await tester.pump();
      await tester.pump();
      controller.onUserScrollActive(controller.scrollController.position);
      controller.onUserScrollEnd(controller.scrollController.position);

      final anchorFinder = find.text('anchor_message_22');
      expect(anchorFinder, findsOneWidget);
      final beforeTop = tester.getTopLeft(anchorFinder).dy;

      final loadFuture = controller.loadOlderHistoryPreservingOffsetForTest();
      await tester.pump();
      await loadFuture;
      await tester.pumpAndSettle();

      expect(find.text('anchor_message_22'), findsOneWidget);
      final afterTop = tester.getTopLeft(find.text('anchor_message_22')).dy;
      expect(afterTop, closeTo(beforeTop, 1.0));
    },
  );

  testWidgets(
    'loading newer keeps bottom progress when the message window height changes',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_newer_bottom';
      controller.chatTitle = 'session_test_newer_bottom';
      controller.chatType = 'private';
      imService.hasNewer = true;

      MessageModel buildMessage(int id) {
        return MessageModel(
          msgId: 'msg-newer-$id',
          sessionId: 'session_test_newer_bottom',
          senderId: '42',
          content: 'newer_message_$id',
          createdAt: id,
        );
      }

      final itemHeight = ValueNotifier<double>(40);
      final messageWindow = ValueNotifier<List<MessageModel>>(
        List.generate(100, (index) => buildMessage(index + 1)),
      );
      imService.currentMessages.assignAll(messageWindow.value);
      imService.onLoadNewer = () {
        itemHeight.value = 56;
        final nextWindow = List.generate(
          100,
          (index) => buildMessage(index + 21),
        );
        messageWindow.value = nextWindow;
        imService.currentMessages.assignAll(nextWindow);
        imService.hasNewer = false;
      };

      await tester.pumpWidget(
        GetMaterialApp(
          home: ValueListenableBuilder<List<MessageModel>>(
            valueListenable: messageWindow,
            builder: (_, messages, __) {
              return ValueListenableBuilder<double>(
                valueListenable: itemHeight,
                builder: (_, height, __) {
                  return SizedBox(
                    height: 300,
                    child: ListView.builder(
                      controller: controller.scrollController,
                      itemCount: messages.length,
                      itemBuilder: (_, index) {
                        final message = messages[index];
                        return SizedBox(
                          height: height,
                          child: Text(message.content),
                        );
                      },
                    ),
                  );
                },
              );
            },
          ),
        ),
      );
      await tester.pumpAndSettle();

      controller.scrollController.jumpTo(
        controller.scrollController.position.maxScrollExtent,
      );
      await tester.pump();
      final beforeMaxExtent =
          controller.scrollController.position.maxScrollExtent;

      final loadFuture = controller.loadNewerHistoryPreservingOffsetForTest();
      await tester.pump();
      await loadFuture;
      await tester.pumpAndSettle();

      expect(imService.loadNewerCalls, 1);
      final position = controller.scrollController.position;
      expect(position.maxScrollExtent, greaterThan(beforeMaxExtent));
      expect(position.maxScrollExtent - position.pixels, lessThanOrEqualTo(1));
      expect(find.text('newer_message_120'), findsOneWidget);
    },
  );

  testWidgets(
    'loading newer near bottom keeps advancing to the latest window bottom',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_newer_near_bottom';
      controller.chatTitle = 'session_test_newer_near_bottom';
      controller.chatType = 'private';
      imService.hasNewer = false;

      MessageModel buildMessage(int id) {
        return MessageModel(
          msgId: 'msg-newer-near-$id',
          sessionId: 'session_test_newer_near_bottom',
          senderId: '42',
          content: 'newer_near_message_$id',
          createdAt: id,
        );
      }

      final messageWindow = ValueNotifier<List<MessageModel>>(
        List.generate(100, (index) => buildMessage(index + 1)),
      );
      imService.currentMessages.assignAll(messageWindow.value);
      imService.onLoadNewer = () {
        final nextWindow = List.generate(
          100,
          (index) => buildMessage(index + 21),
        );
        messageWindow.value = nextWindow;
        imService.currentMessages.assignAll(nextWindow);
        imService.hasNewer = false;
      };

      await tester.pumpWidget(
        GetMaterialApp(
          home: ValueListenableBuilder<List<MessageModel>>(
            valueListenable: messageWindow,
            builder: (_, messages, __) {
              return SizedBox(
                height: 300,
                child: ListView.builder(
                  controller: controller.scrollController,
                  itemCount: messages.length,
                  itemBuilder: (_, index) {
                    final message = messages[index];
                    return SizedBox(height: 40, child: Text(message.content));
                  },
                ),
              );
            },
          ),
        ),
      );
      await tester.pumpAndSettle();

      final beforePosition = controller.scrollController.position;
      final nearBottomTarget = (beforePosition.maxScrollExtent - 30).clamp(
        beforePosition.minScrollExtent,
        beforePosition.maxScrollExtent,
      );
      controller.scrollController.jumpTo(nearBottomTarget);
      await tester.pump();
      final beforeDistanceToBottom =
          controller.scrollController.position.maxScrollExtent -
          controller.scrollController.position.pixels;
      expect(beforeDistanceToBottom, greaterThan(0));
      expect(beforeDistanceToBottom, lessThanOrEqualTo(60));

      imService.hasNewer = true;
      final loadFuture = controller.loadNewerHistoryPreservingOffsetForTest();
      await tester.pump();
      await loadFuture;
      await tester.pumpAndSettle();

      expect(imService.loadNewerCalls, 1);
      final position = controller.scrollController.position;
      expect(position.maxScrollExtent - position.pixels, lessThanOrEqualTo(1));
      expect(find.text('newer_near_message_120'), findsOneWidget);
    },
  );

  testWidgets('scrollToLoadedTop jumps to top of current message window', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_scroll_top';
    controller.chatTitle = 'session_test_scroll_top';
    controller.chatType = 'private';
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'msg-scroll-top',
        sessionId: 'session_test_scroll_top',
        senderId: '42',
        content: 'hello',
        createdAt: 1735689600000,
      ),
    ]);
    controller.onReady();

    await tester.pumpWidget(
      GetMaterialApp(
        home: SizedBox(
          height: 300,
          child: ListView.builder(
            controller: controller.scrollController,
            itemCount: 120,
            itemBuilder: (_, __) => const SizedBox(height: 40),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    controller.scrollToBottom();
    await tester.pump();
    expect(controller.scrollController.offset, greaterThan(0));

    controller.scrollToLoadedTop(animated: false);
    await tester.pump();
    expect(controller.scrollController.offset, lessThanOrEqualTo(1));
  });

  testWidgets(
    'onScrollMetricsChanged keeps bottom anchoring when content grows',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_1';
      controller.chatTitle = 'session_test_1';
      controller.chatType = 'private';
      imService.currentMessages.assignAll([
        MessageModel(
          msgId: 'msg-1',
          sessionId: 'session_test_1',
          senderId: '42',
          content: 'hello',
          createdAt: 1735689600000,
        ),
      ]);

      final itemCount = ValueNotifier<int>(20);
      await tester.pumpWidget(
        GetMaterialApp(
          home: ValueListenableBuilder<int>(
            valueListenable: itemCount,
            builder: (_, count, __) {
              return SizedBox(
                height: 300,
                child: ListView.builder(
                  controller: controller.scrollController,
                  itemCount: count,
                  itemBuilder: (_, __) => const SizedBox(height: 40),
                ),
              );
            },
          ),
        ),
      );
      await tester.pumpAndSettle();

      controller.scrollToBottom();
      await tester.pump();
      await tester.pump();

      controller.onScrollMetricsChanged(controller.scrollController.position);
      final beforeMaxExtent =
          controller.scrollController.position.maxScrollExtent;

      itemCount.value = 40;
      await tester.pumpAndSettle();
      expect(
        controller.scrollController.position.maxScrollExtent,
        greaterThan(beforeMaxExtent),
      );

      controller.onScrollMetricsChanged(controller.scrollController.position);
      await tester.pump();
      await tester.pump();

      final position = controller.scrollController.position;
      expect(
        (position.maxScrollExtent - position.pixels).abs(),
        lessThanOrEqualTo(1),
      );
    },
  );

  testWidgets(
    'onScrollMetricsChanged does not force bottom when user left bottom',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_1';
      controller.chatTitle = 'session_test_1';
      controller.chatType = 'private';
      imService.currentMessages.assignAll([
        MessageModel(
          msgId: 'msg-1',
          sessionId: 'session_test_1',
          senderId: '42',
          content: 'hello',
          createdAt: 1735689600000,
        ),
      ]);

      final itemCount = ValueNotifier<int>(30);
      await tester.pumpWidget(
        GetMaterialApp(
          home: ValueListenableBuilder<int>(
            valueListenable: itemCount,
            builder: (_, count, __) {
              return SizedBox(
                height: 300,
                child: ListView.builder(
                  controller: controller.scrollController,
                  itemCount: count,
                  itemBuilder: (_, __) => const SizedBox(height: 40),
                ),
              );
            },
          ),
        ),
      );
      await tester.pumpAndSettle();

      controller.scrollToBottom();
      await tester.pump();
      await tester.pump();

      controller.onUserScrollStart(controller.scrollController.position);
      controller.scrollController.jumpTo(200);
      await tester.pump();
      controller.onUserScrollActive(controller.scrollController.position);
      controller.onUserScrollEnd(controller.scrollController.position);

      controller.onScrollMetricsChanged(controller.scrollController.position);
      itemCount.value = 50;
      await tester.pumpAndSettle();

      controller.onScrollMetricsChanged(controller.scrollController.position);
      await tester.pump();
      await tester.pump();

      final position = controller.scrollController.position;
      expect(position.maxScrollExtent - position.pixels, greaterThan(120));
    },
  );

  testWidgets(
    'message layout changes do not restore anchors while user is scrolling',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_active_scroll_layout_change';
      controller.chatTitle = 'session_test_active_scroll_layout_change';
      controller.chatType = 'private';

      final messages = List.generate(
        30,
        (index) => MessageModel(
          msgId: 'active-scroll-msg-$index',
          sessionId: 'session_test_active_scroll_layout_change',
          senderId: '42',
          content: 'active scroll message $index',
          createdAt: 1735689600000 + index,
        ),
      );
      imService.currentMessages.assignAll(messages);

      final heights = List<double>.filled(messages.length, 40);
      final revision = ValueNotifier<int>(0);
      await tester.pumpWidget(
        GetMaterialApp(
          home: ValueListenableBuilder<int>(
            valueListenable: revision,
            builder: (_, __, ___) {
              return SizedBox(
                height: 300,
                child: ListView.builder(
                  controller: controller.scrollController,
                  itemCount: messages.length,
                  itemBuilder: (_, index) {
                    final message = messages[index];
                    final itemKey = ChatMessageIdentity.selectionKey(message);
                    return KeyedSubtree(
                      key: controller.messageViewportItemGlobalKey(itemKey),
                      child: SizedBox(
                        height: heights[index],
                        child: Text(message.content),
                      ),
                    );
                  },
                ),
              );
            },
          ),
        ),
      );
      await tester.pumpAndSettle();
      controller.onScrollMetricsChanged(controller.scrollController.position);

      controller.onUserScrollStart(controller.scrollController.position);
      controller.scrollController.jumpTo(400);
      await tester.pump();
      controller.onUserScrollActive(controller.scrollController.position);

      final beforePixels = controller.scrollController.offset;
      expect(controller.debugLastUserViewportAnchor, isNotNull);

      for (var i = 0; i < 5; i++) {
        heights[i] += 20;
      }
      revision.value++;
      await tester.pumpAndSettle();

      controller.onScrollMetricsChanged(controller.scrollController.position);
      controller.onMessageViewportLayoutChanged();
      await tester.pump();
      await tester.pump();

      expect(controller.scrollController.offset, closeTo(beforePixels, 0.5));

      controller.onUserScrollEnd(controller.scrollController.position);
    },
  );

  testWidgets(
    'app resume restores the visible message anchor after content shrinks below',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_resume_anchor';
      controller.chatTitle = 'session_test_resume_anchor';
      controller.chatType = 'private';

      final messages = List.generate(
        30,
        (index) => MessageModel(
          msgId: 'msg-$index',
          sessionId: 'session_test_resume_anchor',
          senderId: '42',
          content: 'message $index',
          createdAt: 1735689600000 + index,
        ),
      );
      imService.currentMessages.assignAll(messages);

      final heights = List<double>.filled(messages.length, 40);
      final revision = ValueNotifier<int>(0);
      await tester.pumpWidget(
        GetMaterialApp(
          home: ValueListenableBuilder<int>(
            valueListenable: revision,
            builder: (_, __, ___) {
              return SizedBox(
                height: 300,
                child: ListView.builder(
                  controller: controller.scrollController,
                  itemCount: messages.length,
                  itemBuilder: (_, index) {
                    final message = messages[index];
                    final itemKey = ChatMessageIdentity.selectionKey(message);
                    return KeyedSubtree(
                      key: controller.messageViewportItemGlobalKey(itemKey),
                      child: SizedBox(
                        height: heights[index],
                        child: Text(message.content),
                      ),
                    );
                  },
                ),
              );
            },
          ),
        ),
      );
      await tester.pumpAndSettle();
      controller.onScrollMetricsChanged(controller.scrollController.position);

      // 用户往上翻了几屏，然后停下来很久（锚点新鲜度与滚动冷却都已过期）。
      controller.onUserScrollStart(controller.scrollController.position);
      controller.scrollController.jumpTo(400);
      await tester.pump();
      controller.onUserScrollActive(controller.scrollController.position);
      controller.onUserScrollEnd(controller.scrollController.position);
      await tester.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 2700)),
      );

      final anchorFinder = find.text('message 10');
      expect(anchorFinder, findsOneWidget);
      final beforeTop = tester.getTopLeft(anchorFinder).dy;

      // 点链接切走：iOS 先 inactive 再 paused。
      controller.didChangeAppLifecycleState(AppLifecycleState.inactive);
      controller.didChangeAppLifecycleState(AppLifecycleState.paused);

      // 回前台后锚点下方的消息卡片重建变矮，maxScrollExtent 收缩。
      controller.didChangeAppLifecycleState(AppLifecycleState.resumed);
      for (var i = 12; i < 20; i++) {
        heights[i] = 20;
      }
      revision.value++;
      await tester.pump();
      await tester.pumpAndSettle();
      controller.onScrollMetricsChanged(controller.scrollController.position);
      await tester.pump();
      await tester.pump();

      expect(find.text('message 10'), findsOneWidget);
      expect(
        tester.getTopLeft(find.text('message 10')).dy,
        closeTo(beforeTop, 0.5),
      );
      expect(controller.scrollController.position.pixels, closeTo(400, 0.5));
      controller.onClose();
    },
  );

  testWidgets(
    'app resume steps back to an unbuilt anchor after transient extent collapse',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_resume_unbuilt_anchor';
      controller.chatTitle = 'session_test_resume_unbuilt_anchor';
      controller.chatType = 'private';

      final messages = List.generate(
        30,
        (index) => MessageModel(
          msgId: 'msg-$index',
          sessionId: 'session_test_resume_unbuilt_anchor',
          senderId: '42',
          content: 'message $index',
          createdAt: 1735689600000 + index,
        ),
      );
      imService.currentMessages.assignAll(messages);

      final heights = List<double>.filled(messages.length, 40);
      final revision = ValueNotifier<int>(0);
      await tester.pumpWidget(
        GetMaterialApp(
          home: ValueListenableBuilder<int>(
            valueListenable: revision,
            builder: (_, __, ___) {
              return SizedBox(
                height: 300,
                child: ListView.builder(
                  controller: controller.scrollController,
                  cacheExtent: 0,
                  itemCount: messages.length,
                  itemBuilder: (_, index) {
                    final message = messages[index];
                    final itemKey = ChatMessageIdentity.selectionKey(message);
                    return KeyedSubtree(
                      key: controller.messageViewportItemGlobalKey(itemKey),
                      child: SizedBox(
                        height: heights[index],
                        child: Text(message.content),
                      ),
                    );
                  },
                ),
              );
            },
          ),
        ),
      );
      await tester.pumpAndSettle();
      controller.onScrollMetricsChanged(controller.scrollController.position);

      // 用户往上翻了几屏，然后停下来很久（锚点新鲜度与滚动冷却都已过期）。
      controller.onUserScrollStart(controller.scrollController.position);
      controller.scrollController.jumpTo(400);
      await tester.pump();
      controller.onUserScrollActive(controller.scrollController.position);
      controller.onUserScrollEnd(controller.scrollController.position);
      await tester.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 2700)),
      );

      expect(find.text('message 10'), findsOneWidget);
      final beforeTop = tester.getTopLeft(find.text('message 10')).dy;

      // 点链接切走：iOS 先 inactive 再 paused。
      controller.didChangeAppLifecycleState(AppLifecycleState.inactive);
      controller.didChangeAppLifecycleState(AppLifecycleState.paused);
      controller.didChangeAppLifecycleState(AppLifecycleState.resumed);

      // 回前台瞬间 lazy list 的 extent 估算塌缩，Flutter 自己把 pixels 钳到
      // 0；随后高度恢复，但锚点消息已滑出构建范围（cacheExtent 为 0）。
      for (var i = 0; i < heights.length; i++) {
        heights[i] = 10;
      }
      revision.value++;
      await tester.pump();
      expect(controller.scrollController.position.pixels, lessThan(10));

      for (var i = 0; i < heights.length; i++) {
        heights[i] = 40;
      }
      revision.value++;
      await tester.pump();
      controller.onScrollMetricsChanged(controller.scrollController.position);
      await tester.pumpAndSettle();

      expect(find.text('message 10'), findsOneWidget);
      expect(
        tester.getTopLeft(find.text('message 10')).dy,
        closeTo(beforeTop, 0.5),
      );
      expect(controller.scrollController.position.pixels, closeTo(400, 0.5));
      controller.onClose();
    },
  );

  testWidgets(
    'idle viewport holds anchored message long after the last user scroll',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_idle_anchor_hold';
      controller.chatTitle = 'session_test_idle_anchor_hold';
      controller.chatType = 'private';

      final messages = List.generate(
        30,
        (index) => MessageModel(
          msgId: 'idle-msg-$index',
          sessionId: 'session_test_idle_anchor_hold',
          senderId: '42',
          content: 'idle message $index',
          createdAt: 1735689600000 + index,
        ),
      );
      imService.currentMessages.assignAll(messages);

      final heights = List<double>.filled(messages.length, 40);
      final revision = ValueNotifier<int>(0);
      await tester.pumpWidget(
        GetMaterialApp(
          home: ValueListenableBuilder<int>(
            valueListenable: revision,
            builder: (_, __, ___) {
              return SizedBox(
                height: 300,
                child: ListView.builder(
                  controller: controller.scrollController,
                  // 与线上聊天列表一致（chat_view._messageListCacheExtent）。
                  cacheExtent: 600,
                  itemCount: messages.length,
                  itemBuilder: (_, index) {
                    final message = messages[index];
                    final itemKey = ChatMessageIdentity.selectionKey(message);
                    return KeyedSubtree(
                      key: controller.messageViewportItemGlobalKey(itemKey),
                      child: SizedBox(
                        height: heights[index],
                        child: Text(message.content),
                      ),
                    );
                  },
                ),
              );
            },
          ),
        ),
      );
      await tester.pumpAndSettle();
      controller.onScrollMetricsChanged(controller.scrollController.position);

      // 用户翻到中部后停下阅读很久（远超旧的 2.5s 锚点新鲜度窗口）。
      controller.onUserScrollStart(controller.scrollController.position);
      controller.scrollController.jumpTo(400);
      await tester.pump();
      controller.onUserScrollActive(controller.scrollController.position);
      controller.onUserScrollEnd(controller.scrollController.position);
      await tester.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 2700)),
      );

      final beforeTop = tester.getTopLeft(find.text('idle message 10')).dy;

      // 视口下方内容变矮（懒加载估算噪声/块重建），上方可见内容没动。滚动
      // 几何的估算会整体平移，绝对 pixels 允许变，但锚定消息必须留在原位，
      // 不能被旧的差值补偿推走。按真实通知流多驱动几轮直到收敛。
      for (var i = 27; i < 30; i++) {
        heights[i] = 10;
      }
      revision.value++;
      await tester.pumpAndSettle();
      for (var round = 0; round < 4; round++) {
        controller.onScrollMetricsChanged(controller.scrollController.position);
        await tester.pump();
        await tester.pump();
      }

      expect(
        tester.getTopLeft(find.text('idle message 10')).dy,
        closeTo(beforeTop, 0.5),
      );
      controller.onClose();
    },
  );

  testWidgets('fling settle position becomes the anchor before layout jitter', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_fling_anchor';
    controller.chatTitle = 'session_test_fling_anchor';
    controller.chatType = 'private';

    final messages = List.generate(
      30,
      (index) => MessageModel(
        msgId: 'fling-msg-$index',
        sessionId: 'session_test_fling_anchor',
        senderId: '42',
        content: 'fling message $index',
        createdAt: 1735689600000 + index,
      ),
    );
    imService.currentMessages.assignAll(messages);

    final heights = List<double>.filled(messages.length, 40);
    final revision = ValueNotifier<int>(0);
    await tester.pumpWidget(
      GetMaterialApp(
        home: ValueListenableBuilder<int>(
          valueListenable: revision,
          builder: (_, __, ___) {
            return SizedBox(
              height: 300,
              child: ListView.builder(
                controller: controller.scrollController,
                // 与线上聊天列表一致（chat_view._messageListCacheExtent）。
                cacheExtent: 600,
                itemCount: messages.length,
                itemBuilder: (_, index) {
                  final message = messages[index];
                  final itemKey = ChatMessageIdentity.selectionKey(message);
                  return KeyedSubtree(
                    key: controller.messageViewportItemGlobalKey(itemKey),
                    child: SizedBox(
                      height: heights[index],
                      child: Text(message.content),
                    ),
                  );
                },
              ),
            );
          },
        ),
      ),
    );
    await tester.pumpAndSettle();
    controller.onScrollMetricsChanged(controller.scrollController.position);

    // 拖拽到 200 后松手进入惯性滑动：fling 的 ScrollEnd 没有 dragDetails，
    // 不会触发 onUserScrollEnd，最终停在 400 时只有 idle 通知。
    controller.onUserScrollStart(controller.scrollController.position);
    controller.scrollController.jumpTo(200);
    await tester.pump();
    controller.onUserScrollActive(controller.scrollController.position);
    controller.scrollController.jumpTo(400);
    await tester.pump();
    controller.onUserScrollInteractionReset();

    final settledTop = tester.getTopLeft(find.text('fling message 10')).dy;

    // 停稳后视口下方布局抖动，锚点必须是停稳位置的可见消息（message 10），
    // 而不是拖拽中途 200 处的消息；收敛后 message 10 应留在原位。
    for (var i = 27; i < 30; i++) {
      heights[i] = 10;
    }
    revision.value++;
    await tester.pumpAndSettle();
    for (var round = 0; round < 4; round++) {
      controller.onScrollMetricsChanged(controller.scrollController.position);
      await tester.pump();
      await tester.pump();
    }

    expect(
      tester.getTopLeft(find.text('fling message 10')).dy,
      closeTo(settledTop, 0.5),
    );
    controller.onClose();
  });

  testWidgets(
    'onScrollMetricsChanged preserves visible message anchor when content grows',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_anchor_growth';
      controller.chatTitle = 'session_test_anchor_growth';
      controller.chatType = 'private';

      final messages = List.generate(
        30,
        (index) => MessageModel(
          msgId: 'msg-$index',
          sessionId: 'session_test_anchor_growth',
          senderId: '42',
          content: 'message $index',
          createdAt: 1735689600000 + index,
        ),
      );
      imService.currentMessages.assignAll(messages);

      final heights = List<double>.filled(messages.length, 40);
      final revision = ValueNotifier<int>(0);
      await tester.pumpWidget(
        GetMaterialApp(
          home: ValueListenableBuilder<int>(
            valueListenable: revision,
            builder: (_, __, ___) {
              return SizedBox(
                height: 300,
                child: ListView.builder(
                  controller: controller.scrollController,
                  itemCount: messages.length,
                  itemBuilder: (_, index) {
                    final message = messages[index];
                    final itemKey = ChatMessageIdentity.selectionKey(message);
                    return KeyedSubtree(
                      key: controller.messageViewportItemGlobalKey(itemKey),
                      child: SizedBox(
                        height: heights[index],
                        child: Text(message.content),
                      ),
                    );
                  },
                ),
              );
            },
          ),
        ),
      );
      await tester.pumpAndSettle();
      controller.onScrollMetricsChanged(controller.scrollController.position);

      controller.onUserScrollStart(controller.scrollController.position);
      controller.scrollController.jumpTo(400);
      await tester.pump();
      controller.onUserScrollActive(controller.scrollController.position);
      controller.onUserScrollEnd(controller.scrollController.position);
      await tester.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 2100)),
      );

      final capturedAnchor = controller.debugLastUserViewportAnchor;
      expect(capturedAnchor, isNotNull);
      final anchorKey = controller.peekMessageViewportItemGlobalKey(
        capturedAnchor!.itemKey,
      );
      expect(anchorKey, isNotNull);
      final anchorFinder = find.byKey(anchorKey!);
      final beforeTop = tester.getTopLeft(anchorFinder).dy;

      for (var i = 0; i < 5; i++) {
        heights[i] += 20;
      }
      revision.value++;
      await tester.pumpAndSettle();

      controller.onScrollMetricsChanged(controller.scrollController.position);
      controller.onMessageViewportLayoutChanged();
      await tester.pump();
      await tester.pump();

      expect(tester.getTopLeft(anchorFinder).dy, closeTo(beforeTop, 0.5));
    },
  );

  testWidgets('bottom obstruction changes keep chat pinned to latest message', (
    WidgetTester tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(800, 600));
    addTearDown(() async {
      await tester.binding.setSurfaceSize(null);
    });

    final obstructionObserver = _FakeChatBottomObstructionObserver();
    final controller = Get.put(
      ChatController(bottomObstructionObserver: obstructionObserver),
    );
    controller.sessionId = 'session_test_obstruction_anchor';
    controller.chatTitle = 'session_test_obstruction_anchor';
    controller.chatType = 'private';
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'msg-obstruction-1',
        sessionId: 'session_test_obstruction_anchor',
        senderId: '42',
        content: 'hello',
        createdAt: 1735689600000,
      ),
    ]);
    controller.onReady();

    await tester.pumpWidget(
      GetMaterialApp(
        home: ListView.builder(
          controller: controller.scrollController,
          itemCount: 60,
          itemBuilder: (_, __) => const SizedBox(height: 40),
        ),
      ),
    );
    await tester.pumpAndSettle();

    controller.scrollToBottom();
    await tester.pump();
    await tester.pump();

    final beforeKeyboardMax =
        controller.scrollController.position.maxScrollExtent;
    expect(
      (beforeKeyboardMax - controller.scrollController.position.pixels).abs(),
      lessThanOrEqualTo(1),
    );

    await tester.binding.setSurfaceSize(const Size(800, 520));
    obstructionObserver.emit(260);
    await tester.pump();

    expect(
      controller.scrollController.position.maxScrollExtent,
      greaterThan(beforeKeyboardMax),
    );

    await tester.pump(const Duration(milliseconds: 120));
    await tester.pump();

    final position = controller.scrollController.position;
    expect((position.maxScrollExtent - position.pixels).abs(), lessThan(1));
  });

  testWidgets(
    'bottom obstruction changes do not force latest message after focused manual scroll away',
    (WidgetTester tester) async {
      await tester.binding.setSurfaceSize(const Size(800, 600));
      addTearDown(() async {
        await tester.binding.setSurfaceSize(null);
      });

      final obstructionObserver = _FakeChatBottomObstructionObserver();
      final controller = Get.put(
        ChatController(bottomObstructionObserver: obstructionObserver),
      );
      controller.sessionId = 'session_test_obstruction_manual_scroll';
      controller.chatTitle = 'session_test_obstruction_manual_scroll';
      controller.chatType = 'private';
      imService.currentMessages.assignAll([
        MessageModel(
          msgId: 'msg-obstruction-manual-1',
          sessionId: 'session_test_obstruction_manual_scroll',
          senderId: '42',
          content: 'hello',
          createdAt: 1735689600000,
        ),
      ]);
      controller.onReady();

      await tester.pumpWidget(
        GetMaterialApp(
          home: Scaffold(
            body: Column(
              children: [
                Expanded(
                  child: ListView.builder(
                    controller: controller.scrollController,
                    itemCount: 60,
                    itemBuilder: (_, __) => const SizedBox(height: 40),
                  ),
                ),
                TextField(focusNode: controller.focusNode),
              ],
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      controller.scrollToBottom();
      await tester.pump();
      await tester.pump();

      await tester.tap(find.byType(TextField));
      await tester.pump();
      expect(controller.focusNode.hasFocus, isTrue);

      final scrolledOffset =
          controller.scrollController.position.maxScrollExtent - 200;
      controller.onUserScrollStart(controller.scrollController.position);
      controller.scrollController.jumpTo(scrolledOffset);
      await tester.pump();
      controller.onUserScrollActive(controller.scrollController.position);
      controller.onUserScrollEnd(controller.scrollController.position);
      final beforeObstructionOffset = controller.scrollController.offset;

      obstructionObserver.emit(260);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 120));
      await tester.pump();

      expect(
        controller.scrollController.offset,
        closeTo(beforeObstructionOffset, 1.0),
      );
      final position = controller.scrollController.position;
      expect(position.maxScrollExtent - position.pixels, greaterThan(1));
    },
  );

  testWidgets(
    'stream updates stay paused after manual scroll until user returns bottom',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_stream_pause';
      controller.chatTitle = 'session_test_stream_pause';
      controller.chatType = 'private';
      imService.currentMessages.assignAll([
        MessageModel(
          msgId: 'msg-stream-1',
          sessionId: 'session_test_stream_pause',
          senderId: '42',
          content: 'hello',
          createdAt: 1735689600000,
        ),
      ]);

      await tester.pumpWidget(
        GetMaterialApp(
          home: SizedBox(
            height: 300,
            child: ListView.builder(
              controller: controller.scrollController,
              itemCount: 50,
              itemBuilder: (_, __) => const SizedBox(height: 40),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      controller.scrollToBottom();
      await tester.pump();
      await tester.pump();

      controller.onUserScrollStart(controller.scrollController.position);
      controller.scrollController.jumpTo(
        controller.scrollController.position.maxScrollExtent - 40,
      );
      await tester.pump();
      controller.onUserScrollActive(controller.scrollController.position);
      controller.onUserScrollEnd(controller.scrollController.position);

      final pausedOffset = controller.scrollController.offset;
      controller.onStreamingMessageUpdated('msg-stream-1');
      await tester.pump();
      await tester.pump();

      expect(controller.scrollController.offset, closeTo(pausedOffset, 0.1));

      controller.onUserScrollStart(controller.scrollController.position);
      controller.scrollController.jumpTo(
        controller.scrollController.position.maxScrollExtent,
      );
      await tester.pump();
      controller.onUserScrollActive(controller.scrollController.position);
      controller.onUserScrollEnd(controller.scrollController.position);

      controller.scrollController.jumpTo(
        controller.scrollController.position.maxScrollExtent - 40,
      );
      await tester.pump();

      controller.onStreamingMessageUpdated('msg-stream-1');
      await tester.pump();
      await tester.pump();

      final position = controller.scrollController.position;
      expect(
        (position.maxScrollExtent - position.pixels).abs(),
        lessThanOrEqualTo(1),
      );
    },
  );

  testWidgets(
    'nested markdown scroll gesture pauses auto-follow for stream growth',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_nested_stream_pause';
      controller.chatTitle = 'session_test_nested_stream_pause';
      controller.chatType = 'private';
      imService.currentMessages.assignAll([
        MessageModel(
          msgId: 'msg-stream-nested-1',
          sessionId: 'session_test_nested_stream_pause',
          senderId: '42',
          content: 'hello',
          createdAt: 1735689600000,
        ),
      ]);

      final itemCount = ValueNotifier<int>(30);
      await tester.pumpWidget(
        GetMaterialApp(
          home: ValueListenableBuilder<int>(
            valueListenable: itemCount,
            builder: (_, count, __) {
              return SizedBox(
                height: 300,
                child: ListView.builder(
                  controller: controller.scrollController,
                  itemCount: count,
                  itemBuilder: (_, __) => const SizedBox(height: 40),
                ),
              );
            },
          ),
        ),
      );
      await tester.pumpAndSettle();

      controller.scrollToBottom();
      await tester.pump();
      await tester.pump();
      controller.onScrollMetricsChanged(controller.scrollController.position);

      controller.onNestedScrollableUserDragStart();
      controller.onNestedScrollableUserDragActive();
      controller.onNestedScrollableUserDragEnd();

      itemCount.value = 50;
      await tester.pumpAndSettle();
      controller.onScrollMetricsChanged(controller.scrollController.position);
      await tester.pump();
      await tester.pump();

      final pausedPosition = controller.scrollController.position;
      expect(
        pausedPosition.maxScrollExtent - pausedPosition.pixels,
        greaterThan(1),
      );
    },
  );

  testWidgets('retryMessage validates client id before dispatch', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());

    controller.retryMessage(null);
    controller.retryMessage('');
    controller.retryMessage('cid-001');
    await tester.pump();

    expect(imService.retryCalls, 1);
    expect(imService.retriedClientMsgId, 'cid-001');
  });

  testWidgets('retryMessage dispatches by msg id when client id is absent', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());

    controller.retryMessage(null, msgId: '');
    controller.retryMessage(null, msgId: 'msg-001');
    await tester.pump();

    expect(imService.retryCalls, 1);
    expect(imService.retriedClientMsgId, isNull);
    expect(imService.retriedMsgId, 'msg-001');
  });

  testWidgets(
    'deleteCurrentConversation trims session id and deletes locally',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = '  session_test_1  ';
      controller.chatTitle = 'session_test_1';
      controller.chatType = 'private';

      await controller.deleteCurrentConversation();
      await tester.pump();

      expect(imService.deleteConversationCalls, 1);
      expect(imService.deletedSessionId, 'session_test_1');
    },
  );

  testWidgets('delegatedAgentName resolves agent name and fallback', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_1';
    controller.chatTitle = 'session_test_1';
    controller.chatType = 'private';

    imService.delegateStates['session_test_1'] = {
      'agent_id': 'agent-1',
      'active': true,
    };
    agentService.agents.assignAll([
      AgentModel(id: 'agent-1', agentName: 'Support Bot'),
    ]);
    await tester.pump();

    expect(controller.delegatedAgentId, 'agent-1');
    expect(controller.delegatedAgentName, 'Support Bot');

    imService.delegateStates['session_test_1'] = {
      'agent_id': 'agent-2',
      'active': true,
    };
    await tester.pump();

    expect(controller.delegatedAgentName, 'Agent agent-2');
  });

  testWidgets('chatStatusLabel prefers active agent output actor name', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_1';
    controller.chatTitle = 'Support Bot';
    controller.chatType = 'private';
    agentService.agents.assignAll([
      AgentModel(id: 'agent-1', agentName: 'Support Bot'),
    ]);

    imService.agentOutputStates['session_test_1'] = {
      'run_id': 'run-1',
      'agent_id': 'agent-1',
      'state': 'streaming',
      'can_stop': true,
      'updated_at': 10,
    };
    await tester.pump();

    expect(controller.hasChatStatusIndicator, isTrue);
    expect(controller.hasActiveAgentOutput, isTrue);
    expect(controller.chatStatusLabel, 'Support Bot');
    expect(controller.canStopCurrentAgentOutput, isTrue);
  });

  testWidgets(
    'chat status indicator stays visible while agent message is still streaming without output state',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_1';
      controller.chatTitle = 'Support Bot';
      controller.chatType = 'private';
      agentService.agents.assignAll([
        AgentModel(id: 'agent-1', agentName: 'Support Bot'),
      ]);

      imService.currentMessages.add(
        MessageModel(
          msgId: 'stream-msg-status-1',
          sessionId: 'session_test_1',
          senderId: 'agent-1',
          senderType: 2,
          msgType: 4,
          createdAt: 1,
        ),
      );
      imService.currentMessages.refresh();
      imService.debugAddStreamingMessageForTest('stream-msg-status-1');
      await tester.pump();

      expect(controller.hasActiveAgentOutput, isFalse);
      expect(controller.hasChatStatusIndicator, isTrue);
      expect(controller.chatStatusLabel, 'Support Bot');

      // 收尾摘掉流式标记，避免看门狗周期计时器在 fake async 里挂成
      // pending timer。
      imService.debugRemoveStreamingMessageForTest('stream-msg-status-1');
    },
  );

  testWidgets('stopAgentOutput forwards current run to im service', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_1';
    controller.chatTitle = 'session_test_1';
    controller.chatType = 'private';

    imService.agentOutputStates['session_test_1'] = {
      'run_id': 'run-stop-1',
      'state': 'streaming',
      'can_stop': true,
      'updated_at': 20,
    };
    await tester.pump();

    controller.stopAgentOutput();

    expect(imService.agentOutputStopCalls, 1);
    expect(imService.stoppedOutputSessionId, 'session_test_1');
    expect(imService.stoppedOutputRunId, 'run-stop-1');
  });

  testWidgets('stopAgentOutput ignores already stopping state', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_1';
    controller.chatTitle = 'session_test_1';
    controller.chatType = 'private';

    imService.agentOutputStates['session_test_1'] = {
      'run_id': 'run-stop-2',
      'state': 'stopping',
      'can_stop': false,
      'updated_at': 30,
    };
    await tester.pump();

    controller.stopAgentOutput();

    expect(imService.agentOutputStopCalls, 0);
  });

  testWidgets(
    'shouldShowAgentOutputStopForMessage requires active stop permission',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_1';
      controller.chatTitle = 'session_test_1';
      controller.chatType = 'private';

      final msg = MessageModel(
        msgId: 'stream-msg-1',
        sessionId: 'session_test_1',
        senderId: 'agent-1',
        senderType: 2,
        msgType: 4,
        createdAt: 1,
      );

      await tester.pump();

      expect(
        controller.shouldShowAgentOutputStopForMessage(msg, isStreaming: true),
        isFalse,
      );
      expect(
        controller.canStopAgentOutputForMessage(msg, isStreaming: true),
        isFalse,
      );
    },
  );

  testWidgets(
    'shouldShowAgentOutputStopForMessage only matches the active stream message',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_1';
      controller.chatTitle = 'session_test_1';
      controller.chatType = 'private';

      imService.agentOutputStates['session_test_1'] = {
        'run_id': 'run-stop-3',
        'stream_msg_id': 'stream-msg-1',
        'state': 'streaming',
        'can_stop': true,
        'updated_at': 40,
      };

      final currentMsg = MessageModel(
        msgId: 'stream-msg-1',
        sessionId: 'session_test_1',
        senderId: 'agent-1',
        senderType: 2,
        msgType: 4,
        createdAt: 1,
      );
      final otherMsg = MessageModel(
        msgId: 'stream-msg-2',
        sessionId: 'session_test_1',
        senderId: 'agent-1',
        senderType: 2,
        msgType: 4,
        createdAt: 2,
      );

      await tester.pump();

      expect(
        controller.shouldShowAgentOutputStopForMessage(
          currentMsg,
          isStreaming: true,
        ),
        isTrue,
      );
      expect(
        controller.shouldShowAgentOutputStopForMessage(
          otherMsg,
          isStreaming: true,
        ),
        isFalse,
      );
    },
  );

  testWidgets(
    'stopAgentOutputForMessage ignores stream without active stop permission',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_test_1';
      controller.chatTitle = 'session_test_1';
      controller.chatType = 'private';

      final msg = MessageModel(
        msgId: 'stream-msg-local-stop',
        sessionId: 'session_test_1',
        senderId: 'agent-1',
        senderType: 2,
        msgType: 4,
        createdAt: 1,
      );

      await tester.pump();

      expect(
        controller.stopAgentOutputForMessage(msg, isStreaming: true),
        isFalse,
      );
      expect(imService.agentOutputStopCalls, 0);
      expect(imService.localStreamingStopCalls, 0);
    },
  );

  testWidgets(
    'private local agent does not expose online or offline subtitle',
    (WidgetTester tester) async {
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'session_private_local_1',
          title: 'Local Bot',
          type: 'private',
          peerId: 'agent-local-1',
          peerType: 2,
          peerNickname: 'Local Bot',
          updatedAt: 1,
          lastMessageTime: 1,
        ),
      ]);
      agentService.agents.assignAll([
        AgentModel(
          id: 'agent-local-1',
          agentName: 'Local Bot',
          providerType: 2,
          sessionId: 'session_private_local_1',
        ),
      ]);

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_private_local_1';
      controller.chatTitle = 'Local Bot';
      controller.chatType = 'private';

      controller.onReady();
      await tester.pump();

      expect(controller.chatSubtitle, '');
      expect(controller.chatSubtitle.contains('chat_online'.tr), isFalse);
      expect(controller.chatSubtitle.contains('chat_offline'.tr), isFalse);
      expect(controller.isChatSubtitleOnline, isFalse);
      expect(controller.isChatSubtitleOffline, isFalse);
    },
  );

  testWidgets(
    'private remote llm agent does not expose online or offline subtitle',
    (WidgetTester tester) async {
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'session_private_remote_1',
          title: 'Remote Bot',
          type: 'private',
          peerId: 'agent-remote-1',
          peerType: 2,
          peerNickname: 'Remote Bot',
          updatedAt: 1,
          lastMessageTime: 1,
        ),
      ]);
      agentService.agents.assignAll([
        AgentModel(
          id: 'agent-remote-1',
          agentName: 'Remote Bot',
          providerType: 1,
          sessionId: 'session_private_remote_1',
        ),
      ]);

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_private_remote_1';
      controller.chatTitle = 'Remote Bot';
      controller.chatType = 'private';

      controller.onReady();
      await tester.pump();

      expect(controller.chatSubtitle, '');
      expect(controller.chatSubtitle.contains('chat_online'.tr), isFalse);
      expect(controller.chatSubtitle.contains('chat_offline'.tr), isFalse);
      expect(controller.isChatSubtitleOnline, isFalse);
      expect(controller.isChatSubtitleOffline, isFalse);
    },
  );

  testWidgets('private agent api does not expose online or offline subtitle', (
    WidgetTester tester,
  ) async {
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'session_private_api_1',
        title: 'API Bot',
        type: 'private',
        peerId: 'agent-api-1',
        peerType: 2,
        peerNickname: 'API Bot',
        updatedAt: 1,
        lastMessageTime: 1,
      ),
    ]);
    agentService.agents.assignAll([
      AgentModel(
        id: 'agent-api-1',
        agentName: 'API Bot',
        providerType: 3,
        sessionId: 'session_private_api_1',
      ),
    ]);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_private_api_1';
    controller.chatTitle = 'API Bot';
    controller.chatType = 'private';

    controller.onReady();
    await tester.pump();

    expect(controller.chatSubtitle, '');
    expect(controller.chatSubtitle.contains('chat_online'.tr), isFalse);
    expect(controller.chatSubtitle.contains('chat_offline'.tr), isFalse);
    expect(controller.isChatSubtitleOnline, isFalse);
    expect(controller.isChatSubtitleOffline, isFalse);
  });

  testWidgets('private human peer does not expose online or offline subtitle', (
    WidgetTester tester,
  ) async {
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'session_private_user_1',
        title: 'Alice',
        type: 'private',
        peerId: '1001',
        peerType: 1,
        peerNickname: 'Alice',
        updatedAt: 1,
        lastMessageTime: 1,
      ),
    ]);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_private_user_1';
    controller.chatTitle = 'Alice';
    controller.chatType = 'private';

    controller.onReady();
    await tester.pump();

    expect(controller.chatSubtitle, '');
    expect(controller.chatSubtitle.contains('chat_online'.tr), isFalse);
    expect(controller.chatSubtitle.contains('chat_offline'.tr), isFalse);
    expect(controller.isChatSubtitleOnline, isFalse);
    expect(controller.isChatSubtitleOffline, isFalse);
  });

  testWidgets('startDelegate sends default max rounds', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_1';
    controller.chatTitle = 'session_test_1';
    controller.chatType = 'private';

    controller.startDelegate('agent-1');
    await tester.pump();

    expect(imService.delegateStartCalls, 1);
    expect(imService.startedSessionId, 'session_test_1');
    expect(imService.startedAgentId, 'agent-1');
    expect(
      imService.startedMaxConsecutiveReplies,
      ChatController.delegateDefaultRounds,
    );
  });

  testWidgets('delegate rounds can change and save', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_test_1';
    controller.chatTitle = 'session_test_1';
    controller.chatType = 'private';
    controller.onReady();
    await tester.pump();

    imService.delegateStates['session_test_1'] = {
      'agent_id': 'agent-1',
      'active': true,
      'max_consecutive_replies': 10,
    };
    await tester.pump();

    expect(controller.delegateRoundsDraft, 10);
    expect(controller.delegateRoundsDirty, isFalse);

    controller.increaseDelegateRounds();
    await tester.pump();
    expect(controller.delegateRoundsDraft, 11);
    expect(controller.delegateRoundsDirty, isTrue);

    controller.saveDelegateRounds();
    await tester.pump();
    expect(imService.delegateStartCalls, 1);
    expect(imService.startedAgentId, 'agent-1');
    expect(imService.startedMaxConsecutiveReplies, 11);
    expect(controller.delegateRoundsDirty, isFalse);
  });

  testWidgets('onReady fetches session detail for group chat', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_1';
    controller.chatTitle = 'group';
    controller.chatType = 'group';

    controller.onReady();
    await tester.pump();

    expect(sessionService.detailCalls, 1);
  });

  testWidgets('session_member_changed event refreshes group detail', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_event_1';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 2,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3},
        {'member_id': '1002', 'member_type': 1, 'role': 1},
      ],
    };

    controller.onReady();
    await tester.pump();
    expect(sessionService.detailCalls, 1);

    imService.sessionMemberEventVersions['session_group_event_1'] = 1;
    await tester.pump();

    expect(sessionService.detailCalls, 2);
  });

  testWidgets('group chat shows agent status only for mentioned agents', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_status_1';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 3,
      'members': [
        {
          'member_id': '42',
          'member_type': 1,
          'role': 3,
          'last_read_msg_id': '0',
        },
        {
          'member_id': '1002',
          'member_type': 1,
          'role': 1,
          'last_read_msg_id': '0',
        },
        {'member_id': '9001', 'member_type': 2, 'role': 0},
      ],
    };

    await controller.refreshSessionDetail();
    await tester.pump();

    final mentionedAgentMessage = MessageModel(
      msgId: '101',
      sessionId: 'session_group_status_1',
      senderId: '42',
      senderType: 1,
      content: '@OpenClaw hello',
      extra: const {
        'mention_user_ids': ['9001'],
      },
      agentDeliveryStatus: 'received',
      createdAt: 1,
    );
    final plainGroupMessage = MessageModel(
      msgId: '102',
      sessionId: 'session_group_status_1',
      senderId: '42',
      senderType: 1,
      content: 'plain group message',
      agentDeliveryStatus: 'received',
      createdAt: 1,
    );
    final queuedAgentMessage = MessageModel(
      msgId: '103',
      sessionId: 'session_group_status_1',
      senderId: '42',
      senderType: 1,
      content: '@OpenClaw pending',
      extra: const {
        'mention_user_ids': ['9001'],
      },
      agentDeliveryStatus: 'queued',
      createdAt: 1,
    );

    expect(
      controller.agentDeliveryLabelForMessage(mentionedAgentMessage),
      'chat_agent_delivery_received'.tr,
    );
    expect(controller.agentDeliveryLabelForMessage(queuedAgentMessage), isNull);
    expect(controller.agentDeliveryLabelForMessage(plainGroupMessage), isNull);
    expect(
      controller.isAgentDeliveryErrorForMessage(plainGroupMessage),
      isFalse,
    );
  });

  testWidgets(
    'private chat suppresses transient agent delivery errors before confirmed failure',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_private_status_1';
      controller.chatTitle = 'private';
      controller.chatType = 'private';

      final now = DateTime.now().millisecondsSinceEpoch;
      final recentFailedMessage = MessageModel(
        msgId: '201',
        sessionId: 'session_private_status_1',
        senderId: '42',
        senderType: 1,
        content: 'hello',
        agentDeliveryStatus: 'failed',
        createdAt: now - 20 * 1000,
      );
      final oldFailedMessage = recentFailedMessage.copyWith(
        createdAt: now - 3 * 60 * 1000,
      );

      expect(
        controller.agentDeliveryLabelForMessage(recentFailedMessage),
        isNull,
      );
      expect(
        controller.isAgentDeliveryErrorForMessage(recentFailedMessage),
        isFalse,
      );

      imService.agentOutputStates['session_private_status_1'] = {
        'state': 'received',
        'trigger_msg_id': '201',
        'updated_at': now,
      };
      expect(controller.agentDeliveryLabelForMessage(oldFailedMessage), isNull);
      expect(
        controller.isAgentDeliveryErrorForMessage(oldFailedMessage),
        isFalse,
      );

      imService.agentOutputStates.clear();
      expect(
        controller.agentDeliveryLabelForMessage(oldFailedMessage),
        'chat_agent_delivery_failed'.tr,
      );
      expect(
        controller.isAgentDeliveryErrorForMessage(oldFailedMessage),
        isTrue,
      );
    },
  );

  testWidgets('group access lost triggers local leave handling', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_lost_1';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResult = const SessionDetailResult(
      code: 4003,
      message: 'permission denied',
    );

    await controller.refreshSessionDetail();
    await tester.pump();

    expect(imService.revokeSessionAccessCalls, 1);
    expect(imService.accessRevokedSessionId, 'session_group_lost_1');
    expect(controller.groupMemberCount, 0);
    expect(controller.groupMembers, isEmpty);
  });

  testWidgets('session access revoked event triggers local leave handling', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_revoke_1';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 2,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3},
        {'member_id': '1002', 'member_type': 1, 'role': 1},
      ],
    };

    controller.onReady();
    await tester.pump();

    imService.sessionAccessRevokedVersions['session_group_revoke_1'] = 1;
    await tester.pump();

    expect(imService.revokeSessionAccessCalls, 1);
    expect(imService.accessRevokedSessionId, 'session_group_revoke_1');
  });

  testWidgets('network detail failure does not trigger local leave handling', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_lost_2';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResult = const SessionDetailResult(
      code: 50001,
      message: 'timeout',
      networkError: true,
    );

    await controller.refreshSessionDetail();
    await tester.pump();

    expect(imService.revokeSessionAccessCalls, 0);
  });

  testWidgets(
    'onReady probes unknown private chat type and corrects to group',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_probe_1';
      controller.chatTitle = 'probe';
      controller.chatType = 'private';
      sessionService.detailResp = {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 3},
          {'member_id': '1002', 'member_type': 1, 'role': 1},
        ],
      };

      controller.onReady();
      await tester.pump();

      expect(sessionService.detailCalls, 1);
      expect(controller.chatType, 'group');
      expect(controller.groupMemberCount, 2);
    },
  );

  testWidgets('setMyGroupNickname calls session API and refreshes detail', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_set_nickname_1';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 2,
      'members': [
        {
          'member_id': '42',
          'member_type': 1,
          'role': 3,
          'nickname': '旧昵称',
          'group_nickname': '旧昵称',
        },
        {'member_id': '1002', 'member_type': 1, 'role': 1, 'nickname': '成员A'},
      ],
    };
    await controller.refreshSessionDetail();
    expect(sessionService.detailCalls, 1);

    sessionService.setGroupNicknameResp = const SessionMemberNicknameResult(
      code: 0,
      groupNickname: '新昵称',
    );
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 2,
      'members': [
        {
          'member_id': '42',
          'member_type': 1,
          'role': 3,
          'nickname': '新昵称',
          'group_nickname': '新昵称',
        },
        {'member_id': '1002', 'member_type': 1, 'role': 1, 'nickname': '成员A'},
      ],
    };

    final ok = await controller.setMyGroupNickname('  新昵称  ');
    await tester.pump();

    expect(ok, isTrue);
    expect(sessionService.setGroupNicknameCalls, 1);
    expect(
      sessionService.setGroupNicknameSessionId,
      'session_group_set_nickname_1',
    );
    expect(sessionService.setGroupNicknameValue, '  新昵称  ');
    expect(sessionService.detailCalls, 2);
    expect(controller.myGroupNickname, '新昵称');
  });

  testWidgets(
    'refreshSessionDetail clears duplicated member nicknames that match conversation lead text',
    (WidgetTester tester) async {
      agentService.agents.assignAll([
        AgentModel(id: '9001', agentName: 'OpenClaw'),
      ]);
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_member_nickname_guard';
      controller.chatTitle = '请帮我写一个发布公告';
      controller.chatType = 'group';
      sessionService.detailResp = {
        'session_type': 2,
        'title': '请帮我写一个发布公告',
        'member_count': 3,
        'members': [
          {
            'member_id': '42',
            'member_type': 1,
            'role': 3,
            'nickname': '请帮我写一个发布公告',
          },
          {
            'member_id': '1002',
            'member_type': 1,
            'role': 1,
            'nickname': '请帮我写一个发布公告',
          },
          {
            'member_id': '9001',
            'member_type': 2,
            'role': 1,
            'nickname': '请帮我写一个发布公告',
          },
        ],
      };

      await controller.refreshSessionDetail();
      await tester.pump();

      expect(controller.groupMembers, hasLength(3));
      for (final member in controller.groupMembers) {
        expect(member['nickname'], '');
      }
      expect(
        controller.resolveGroupMemberDisplayName(controller.groupMembers[1]),
        '1002',
      );
      expect(
        controller.resolveGroupMemberDisplayName(controller.groupMembers[2]),
        'OpenClaw',
      );
      expect(
        controller.resolveSenderName(
          senderId: '9001',
          isMine: false,
          isGroup: true,
          senderType: 2,
        ),
        'OpenClaw',
      );
    },
  );

  testWidgets(
    'inviteFriendsToGroup sends unique members and refreshes detail',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_2';
      controller.chatTitle = 'group';
      controller.chatType = 'group';
      sessionService.detailResp = {
        'session_type': 2,
        'allow_member_invite': true,
        'member_invite_threshold': 20,
        'member_count': 1,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 3},
        ],
      };
      await controller.refreshSessionDetail();

      sessionService.addMembersResp = {
        'session_id': 'session_group_2',
        'added_count': 2,
        'member_count': 3,
      };
      sessionService.detailResp = {
        'session_type': 2,
        'allow_member_invite': true,
        'member_invite_threshold': 20,
        'member_count': 3,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 3},
          {'member_id': '1002', 'member_type': 1, 'role': 1},
          {'member_id': '1003', 'member_type': 1, 'role': 1},
        ],
      };

      final added = await controller.inviteToGroup(
        userIds: ['1002', '1003', '1002', ' 1003 '],
      );
      await tester.pump();

      expect(added, 2);
      expect(sessionService.addMembersCalls, 1);
      expect(sessionService.addMembersSessionId, 'session_group_2');
      expect(sessionService.addMembersIds, ['1002', '1003']);
      expect(sessionService.addMembersTypes, [1, 1]);
      expect(sessionService.detailCalls, 2);
      expect(controller.groupMemberCount, 3);
    },
  );

  testWidgets('inviteFriendsToGroup allows normal member', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_3';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'allow_member_invite': true,
      'member_invite_threshold': 20,
      'member_count': 2,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 1},
        {'member_id': '1002', 'member_type': 1, 'role': 3},
      ],
    };
    await controller.refreshSessionDetail();

    sessionService.addMembersResp = {
      'session_id': 'session_group_3',
      'added_count': 1,
      'member_count': 3,
    };
    sessionService.detailResp = {
      'session_type': 2,
      'allow_member_invite': true,
      'member_invite_threshold': 20,
      'member_count': 3,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 1},
        {'member_id': '1002', 'member_type': 1, 'role': 3},
        {'member_id': '1003', 'member_type': 1, 'role': 1},
      ],
    };

    final added = await controller.inviteToGroup(userIds: ['1003']);
    await tester.pump();

    expect(controller.canInviteGroupMembers, isTrue);
    expect(controller.canManageGroupMembers, isFalse);
    expect(added, 1);
    expect(sessionService.addMembersCalls, 1);
    expect(sessionService.addMembersSessionId, 'session_group_3');
    expect(sessionService.addMembersIds, ['1003']);
    expect(sessionService.addMembersTypes, [1]);
  });

  testWidgets('normal member invite blocked when runtime setting disables it', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_3_blocked';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'allow_member_invite': false,
      'member_invite_threshold': 20,
      'member_count': 2,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 1},
        {'member_id': '1002', 'member_type': 1, 'role': 3},
      ],
    };
    await controller.refreshSessionDetail();

    final added = await controller.inviteToGroup(userIds: ['1003']);
    await tester.pump();

    expect(controller.canInviteGroupMembers, isFalse);
    expect(added, -1);
    expect(
      controller.lastInviteToGroupErrorMessage,
      'chat_member_invite_disabled'.tr,
    );
    expect(sessionService.addMembersCalls, 0);
  });

  testWidgets(
    'invite shows target rejection message when user disallows group invite',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_target_rejected';
      controller.chatTitle = 'group';
      controller.chatType = 'group';
      sessionService.detailResp = {
        'session_type': 2,
        'allow_member_invite': true,
        'member_invite_threshold': 20,
        'member_count': 2,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 3},
          {'member_id': '1002', 'member_type': 1, 'role': 1},
        ],
      };
      await controller.refreshSessionDetail();

      sessionService.addMembersResult = const SessionAddMembersResult(
        code: 40033,
        message: 'target rejected',
      );

      final added = await controller.inviteToGroup(userIds: ['1003']);
      await tester.pump();

      expect(added, -1);
      expect(
        controller.lastInviteToGroupErrorMessage,
        'chat_target_group_invite_rejected'.tr,
      );
    },
  );

  testWidgets('admin can update group invite setting', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_runtime_settings';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'allow_member_invite': true,
      'member_invite_threshold': 20,
      'member_count': 3,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 2},
        {'member_id': '1002', 'member_type': 1, 'role': 3},
        {'member_id': '1003', 'member_type': 1, 'role': 1},
      ],
    };
    await controller.refreshSessionDetail();

    sessionService.updateInviteSettingResult = const SessionInviteSettingResult(
      code: 0,
      allowMemberInvite: false,
    );
    sessionService.detailResp = {
      'session_type': 2,
      'allow_member_invite': false,
      'member_invite_threshold': 20,
      'member_count': 3,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 2},
        {'member_id': '1002', 'member_type': 1, 'role': 3},
        {'member_id': '1003', 'member_type': 1, 'role': 1},
      ],
    };

    final ok = await controller.updateGroupInviteSetting(false);
    await tester.pump();

    expect(ok, isTrue);
    expect(sessionService.updateInviteSettingCalls, 1);
    expect(
      sessionService.updateInviteSettingSessionId,
      'session_group_runtime_settings',
    );
    expect(sessionService.updateInviteSettingAllowMemberInvite, isFalse);
    expect(controller.allowMemberInvite, isFalse);
    expect(sessionService.detailCalls, 2);
  });

  testWidgets('group speaking state is loaded from session detail', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_speaking_detail';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'all_members_muted': true,
      'member_count': 3,
      'members': [
        {
          'member_id': '42',
          'member_type': 1,
          'role': 1,
          'is_speak_muted': false,
          'can_speak_when_all_muted': false,
        },
        {
          'member_id': '1002',
          'member_type': 1,
          'role': 3,
          'is_speak_muted': false,
          'can_speak_when_all_muted': false,
        },
        {
          'member_id': '9001',
          'member_type': 2,
          'role': 1,
          'is_speak_muted': true,
          'can_speak_when_all_muted': false,
        },
      ],
    };

    await controller.refreshSessionDetail();
    await tester.pump();

    expect(controller.allMembersMuted, isTrue);
    expect(controller.canCurrentUserSpeak, isFalse);
    expect(
      controller.currentUserSpeakingBlockedReason,
      'chat_send_blocked_all_members_muted'.tr,
    );
    expect(
      controller.isGroupMemberSpeakMuted(controller.groupMembers[2]),
      isTrue,
    );
    expect(
      controller.canToggleGroupMemberSpeakWhitelist(controller.groupMembers[2]),
      isFalse,
    );
  });

  testWidgets('admin can update group all members muted', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_all_muted';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'all_members_muted': false,
      'member_count': 3,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 2},
        {'member_id': '1002', 'member_type': 1, 'role': 3},
        {'member_id': '1003', 'member_type': 1, 'role': 1},
      ],
    };
    await controller.refreshSessionDetail();

    sessionService.updateAllMembersMutedResult =
        const SessionAllMembersMutedResult(code: 0, allMembersMuted: true);
    sessionService.detailResp = {
      'session_type': 2,
      'all_members_muted': true,
      'member_count': 3,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 2},
        {'member_id': '1002', 'member_type': 1, 'role': 3},
        {'member_id': '1003', 'member_type': 1, 'role': 1},
      ],
    };

    final ok = await controller.updateGroupAllMembersMuted(true);
    await tester.pump();

    expect(ok, isTrue);
    expect(sessionService.updateAllMembersMutedCalls, 1);
    expect(
      sessionService.updateAllMembersMutedSessionId,
      'session_group_all_muted',
    );
    expect(sessionService.updateAllMembersMutedValue, isTrue);
    expect(controller.allMembersMuted, isTrue);
  });

  testWidgets('owner can update group member speaking state', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_member_speaking';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'all_members_muted': true,
      'member_count': 3,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3},
        {'member_id': '1002', 'member_type': 1, 'role': 1},
        {'member_id': '9001', 'member_type': 2, 'role': 1},
      ],
    };
    await controller.refreshSessionDetail();

    sessionService.updateMemberSpeakingResult =
        const SessionMemberSpeakingResult(
          code: 0,
          memberId: '1002',
          memberType: 1,
          isSpeakMuted: true,
          canSpeakWhenAllMuted: false,
        );
    sessionService.detailResp = {
      'session_type': 2,
      'all_members_muted': true,
      'member_count': 3,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3},
        {
          'member_id': '1002',
          'member_type': 1,
          'role': 1,
          'is_speak_muted': true,
          'can_speak_when_all_muted': false,
        },
        {'member_id': '9001', 'member_type': 2, 'role': 1},
      ],
    };

    final ok = await controller.updateGroupMemberSpeaking(
      controller.groupMembers[1],
      isSpeakMuted: true,
    );
    await tester.pump();

    expect(ok, isTrue);
    expect(sessionService.updateMemberSpeakingCalls, 1);
    expect(
      sessionService.updateMemberSpeakingSessionId,
      'session_group_member_speaking',
    );
    expect(sessionService.updateMemberSpeakingMemberId, '1002');
    expect(sessionService.updateMemberSpeakingMemberType, 1);
    expect(sessionService.updateMemberSpeakingIsMuted, isTrue);
  });

  testWidgets('agent receive mode updates current group member list', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_member_agent_receive';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 2,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3},
        {
          'member_id': '9001',
          'member_type': 2,
          'role': 1,
          'nickname': '香香',
          'agent_receive_mode': 1,
          'agent_receive_backlog_count': 8,
          'agent_receive_editable': true,
        },
      ],
    };
    await controller.refreshSessionDetail();

    sessionService.updateMemberAgentReceiveResult =
        const SessionMemberAgentReceiveResult(
          code: 0,
          memberId: '9001',
          memberType: 2,
          agentReceiveMode: 3,
          agentReceiveBacklogCount: 8,
        );

    final ok = await controller.updateGroupMemberAgentReceive(
      controller.groupMembers[1],
      mode: 3,
    );
    await tester.pump();

    expect(ok, isTrue);
    expect(
      sessionService.updateMemberAgentReceiveSessionId,
      'session_group_member_agent_receive',
    );
    expect(sessionService.updateMemberAgentReceiveMemberId, '9001');
    expect(sessionService.updateMemberAgentReceiveMemberType, 2);
    expect(sessionService.updateMemberAgentReceiveMode, 3);
    expect(
      controller.groupMemberAgentReceiveMode(controller.groupMembers[1]),
      3,
    );
  });

  testWidgets('owner can promote and demote group member', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_4';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 2,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3},
        {'member_id': '1002', 'member_type': 1, 'role': 1},
      ],
    };
    await controller.refreshSessionDetail();

    final target = controller.groupMembers[1];
    expect(controller.canPromoteGroupMember(target), isTrue);
    expect(controller.canDemoteGroupMember(target), isFalse);

    sessionService.updateRoleResp = {
      'session_id': 'session_group_4',
      'member_id': '1002',
      'member_type': 1,
      'role': 2,
    };
    final promote = await controller.updateGroupMemberRole(target, role: 2);
    await tester.pump();

    expect(promote, isTrue);
    expect(sessionService.updateRoleCalls, 1);
    expect(sessionService.updateRoleSessionId, 'session_group_4');
    expect(sessionService.updateRoleMemberId, '1002');
    expect(sessionService.updateRoleMemberType, 1);
    expect(sessionService.updateRoleValue, 2);
  });

  testWidgets('admin can remove normal member but not owner', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_5';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 3,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 2},
        {'member_id': '1002', 'member_type': 1, 'role': 3},
        {'member_id': '1003', 'member_type': 1, 'role': 1},
      ],
    };
    await controller.refreshSessionDetail();

    final owner = controller.groupMembers[1];
    final normal = controller.groupMembers[2];
    expect(controller.canRemoveGroupMember(owner), isFalse);
    expect(controller.canRemoveGroupMember(normal), isTrue);

    sessionService.removeMembersResp = {
      'session_id': 'session_group_5',
      'removed_count': 1,
      'member_count': 2,
    };
    final removed = await controller.removeGroupMember(normal);
    await tester.pump();

    expect(removed, 1);
    expect(sessionService.removeMembersCalls, 1);
    expect(sessionService.removeMembersSessionId, 'session_group_5');
    expect(sessionService.removeMembersIds, ['1003']);
    expect(sessionService.removeMembersTypes, [1]);
  });

  testWidgets('owner can transfer owner to another member', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_6';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 3,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3},
        {'member_id': '1002', 'member_type': 1, 'role': 1},
        {'member_id': '1003', 'member_type': 1, 'role': 1},
      ],
    };
    await controller.refreshSessionDetail();

    final target = controller.groupMembers[1];
    expect(controller.canTransferGroupOwner(target), isTrue);
    sessionService.transferOwnerResp = {
      'session_id': 'session_group_6',
      'owner_id': '1002',
    };

    final ok = await controller.transferGroupOwner(target);
    await tester.pump();

    expect(ok, isTrue);
    expect(sessionService.transferOwnerCalls, 1);
    expect(sessionService.transferOwnerSessionId, 'session_group_6');
    expect(sessionService.transferOwnerMemberId, '1002');
  });

  testWidgets('normal member can leave group', (WidgetTester tester) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_leave_1';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 2,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 1},
        {'member_id': '1002', 'member_type': 1, 'role': 3},
      ],
    };
    await controller.refreshSessionDetail();

    final me = controller.groupMembers.first;
    expect(controller.canLeaveGroup, isTrue);
    expect(controller.canLeaveGroupMember(me), isTrue);

    sessionService.leaveGroupResp = const SessionLeaveResult(
      code: 0,
      sessionId: 'session_group_leave_1',
      left: true,
    );

    final ok = await controller.leaveGroup();
    await tester.pump();

    expect(ok, isTrue);
    expect(sessionService.leaveGroupCalls, 1);
    expect(sessionService.leaveGroupSessionId, 'session_group_leave_1');
    expect(imService.deleteConversationCalls, 1);
    expect(imService.deletedSessionId, 'session_group_leave_1');
    expect(controller.groupMembers, isEmpty);
    expect(controller.groupMemberCount, 0);
  });

  testWidgets('owner cannot leave group', (WidgetTester tester) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_leave_owner';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 2,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3},
        {'member_id': '1002', 'member_type': 1, 'role': 1},
      ],
    };
    await controller.refreshSessionDetail();

    final me = controller.groupMembers.first;
    expect(controller.canLeaveGroup, isFalse);
    expect(controller.canLeaveGroupMember(me), isFalse);

    final ok = await controller.leaveGroup();
    await tester.pump();

    expect(ok, isFalse);
    expect(sessionService.leaveGroupCalls, 0);
    expect(imService.deleteConversationCalls, 0);
  });

  testWidgets('owner can dissolve group', (WidgetTester tester) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_7';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 2,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 3},
        {'member_id': '1002', 'member_type': 1, 'role': 1},
      ],
    };
    await controller.refreshSessionDetail();

    sessionService.dissolveGroupResp = {'session_id': 'session_group_7'};

    final ok = await controller.dissolveGroup();
    await tester.pump();

    expect(ok, isTrue);
    expect(sessionService.dissolveGroupCalls, 1);
    expect(sessionService.dissolveGroupSessionId, 'session_group_7');
    expect(imService.deleteConversationCalls, 1);
    expect(imService.deletedSessionId, 'session_group_7');
    expect(controller.groupMembers, isEmpty);
    expect(controller.groupMemberCount, 0);
  });

  testWidgets('non-owner cannot dissolve group', (WidgetTester tester) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_8';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 2,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 1},
        {'member_id': '1002', 'member_type': 1, 'role': 3},
      ],
    };
    await controller.refreshSessionDetail();

    final ok = await controller.dissolveGroup();
    await tester.pump();

    expect(ok, isFalse);
    expect(sessionService.dissolveGroupCalls, 0);
    expect(imService.deleteConversationCalls, 0);
  });

  testWidgets('sendMessage is blocked when current member cannot speak', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_send_blocked';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'all_members_muted': false,
      'member_count': 2,
      'members': [
        {
          'member_id': '42',
          'member_type': 1,
          'role': 1,
          'is_speak_muted': true,
          'can_speak_when_all_muted': false,
        },
        {'member_id': '1002', 'member_type': 1, 'role': 3},
      ],
    };
    await controller.refreshSessionDetail();
    controller.inputController.text = 'blocked';

    controller.sendMessage();
    await tester.pump(const Duration(milliseconds: 120));

    expect(imService.sendCalls, 0);
    expect(controller.inputController.text, 'blocked');
    expect(
      controller.currentUserSpeakingBlockedReason,
      'chat_send_blocked_member_muted'.tr,
    );
  });

  testWidgets('canRevokeMessage allows group admin to revoke others messages', (
    WidgetTester tester,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = 'session_group_revoke_admin';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    sessionService.detailResp = {
      'session_type': 2,
      'member_count': 2,
      'members': [
        {'member_id': '42', 'member_type': 1, 'role': 2},
        {'member_id': '1002', 'member_type': 1, 'role': 1},
      ],
    };

    await controller.refreshSessionDetail();

    expect(
      controller.canRevokeMessage(
        message: MessageModel(
          msgId: 'group-admin-msg',
          sessionId: 'session_group_revoke_admin',
          senderId: '1002',
          senderType: 1,
          createdAt: 1,
        ),
        isMine: false,
        isSending: false,
        isFailed: false,
        isStreaming: false,
      ),
      isTrue,
    );
  });

  testWidgets(
    'canRevokeMessage rejects other user messages for normal group member',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_group_revoke_member';
      controller.chatTitle = 'group';
      controller.chatType = 'group';
      sessionService.detailResp = {
        'session_type': 2,
        'member_count': 2,
        'members': [
          {'member_id': '42', 'member_type': 1, 'role': 1},
          {'member_id': '1002', 'member_type': 1, 'role': 3},
        ],
      };

      await controller.refreshSessionDetail();

      expect(
        controller.canRevokeMessage(
          message: MessageModel(
            msgId: 'group-member-msg',
            sessionId: 'session_group_revoke_member',
            senderId: '1002',
            senderType: 1,
            createdAt: 1,
          ),
          isMine: false,
          isSending: false,
          isFailed: false,
          isStreaming: false,
        ),
        isFalse,
      );
    },
  );

  testWidgets(
    'canRevokeMessage keeps non-owner private messages non-revokable',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_private_revoke_other';
      controller.chatTitle = 'private';
      controller.chatType = 'private';

      expect(
        controller.canRevokeMessage(
          message: MessageModel(
            msgId: 'private-other-msg',
            sessionId: 'session_private_revoke_other',
            senderId: '1002',
            senderType: 1,
            createdAt: 1,
          ),
          isMine: false,
          isSending: false,
          isFailed: false,
          isStreaming: false,
        ),
        isFalse,
      );
    },
  );

  testWidgets('canRevokeMessage allows owned private agent messages', (
    WidgetTester tester,
  ) async {
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'session_private_agent_revoke',
        title: 'agent',
        type: 'private',
        peerId: '9001',
        peerType: 2,
        updatedAt: 0,
        lastMessageTime: 0,
      ),
    ]);

    final controller = Get.put(ChatController());
    controller.sessionId = 'session_private_agent_revoke';
    controller.chatTitle = 'agent';
    controller.chatType = 'private';

    expect(
      controller.canRevokeMessage(
        message: MessageModel(
          msgId: 'private-agent-msg',
          sessionId: 'session_private_agent_revoke',
          senderId: '9001',
          senderType: 2,
          createdAt: 1,
        ),
        isMine: false,
        isSending: false,
        isFailed: false,
        isStreaming: false,
      ),
      isTrue,
    );
  });

  testWidgets(
    'canRevokeMessage rejects non-peer agent messages in private chat',
    (WidgetTester tester) async {
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'session_private_agent_revoke_other',
          title: 'agent',
          type: 'private',
          peerId: '9001',
          peerType: 2,
          updatedAt: 0,
          lastMessageTime: 0,
        ),
      ]);

      final controller = Get.put(ChatController());
      controller.sessionId = 'session_private_agent_revoke_other';
      controller.chatTitle = 'agent';
      controller.chatType = 'private';

      expect(
        controller.canRevokeMessage(
          message: MessageModel(
            msgId: 'private-agent-msg-other',
            sessionId: 'session_private_agent_revoke_other',
            senderId: '9002',
            senderType: 2,
            createdAt: 1,
          ),
          isMine: false,
          isSending: false,
          isFailed: false,
          isStreaming: false,
        ),
        isFalse,
      );
    },
  );

  testWidgets(
    'revokeMessage persists local removal after delete API succeeds',
    (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_revoke_1';
      controller.chatTitle = 'session_revoke_1';
      controller.chatType = 'private';

      await controller.revokeMessage('18889990001');
      await tester.pump();

      expect(sessionService.deleteMessageCalls, 1);
      expect(sessionService.deleteMessageSessionId, 'session_revoke_1');
      expect(sessionService.deleteMessageMsgId, '18889990001');
      expect(imService.applyLocalMessageRevokeCalls, 1);
      expect(imService.revokedSessionId, 'session_revoke_1');
      expect(imService.revokedMessageId, '18889990001');
    },
  );

  testWidgets(
    'revokeMessage does not mutate local cache when delete API fails',
    (WidgetTester tester) async {
      sessionService.deleteMessageResult = false;
      final controller = Get.put(ChatController());
      controller.sessionId = 'session_revoke_2';
      controller.chatTitle = 'session_revoke_2';
      controller.chatType = 'private';

      await controller.revokeMessage('18889990002');
      await tester.pump();

      expect(sessionService.deleteMessageCalls, 1);
      expect(imService.applyLocalMessageRevokeCalls, 0);
    },
  );

  // ── Wheel scroll cooldown regression tests ────────────────────────

  Future<ChatController> setupWheelTest(
    WidgetTester tester,
    String sessionId,
  ) async {
    final controller = Get.put(ChatController());
    controller.sessionId = sessionId;
    controller.chatTitle = sessionId;
    controller.chatType = 'private';
    imService.currentMessages.assignAll([
      MessageModel(
        msgId: 'msg-$sessionId',
        sessionId: sessionId,
        senderId: '42',
        content: 'hello',
        createdAt: 1735689600000,
      ),
    ]);
    controller.onReady();

    await tester.pumpWidget(
      GetMaterialApp(
        home: SizedBox(
          height: 300,
          child: ListView.builder(
            controller: controller.scrollController,
            itemCount: 50,
            itemBuilder: (_, __) => const SizedBox(height: 40),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    controller.scrollToBottom();
    await tester.pumpAndSettle();

    return controller;
  }

  FixedScrollMetrics makeMetrics({
    required double minScrollExtent,
    required double maxScrollExtent,
    required double pixels,
    required double viewportDimension,
  }) {
    return FixedScrollMetrics(
      minScrollExtent: minScrollExtent,
      maxScrollExtent: maxScrollExtent,
      pixels: pixels,
      viewportDimension: viewportDimension,
      axisDirection: AxisDirection.down,
      devicePixelRatio: 1.0,
    );
  }

  testWidgets('wheel scroll cooldown blocks maxScrollExtent grew adjustment', (
    WidgetTester tester,
  ) async {
    final controller = await setupWheelTest(tester, 'session_wheel_grew');
    final position = controller.scrollController.position;

    controller.onPointerSignalScroll();
    await tester.pump();

    final pixelsBefore = position.pixels;
    final maxExtentBefore = position.maxScrollExtent;

    controller.onScrollMetricsChanged(
      makeMetrics(
        minScrollExtent: position.minScrollExtent,
        maxScrollExtent: maxExtentBefore + 500,
        pixels: pixelsBefore,
        viewportDimension: position.viewportDimension,
      ),
    );
    await tester.pump();

    expect(position.pixels, equals(pixelsBefore));

    // 前序测试在无 overlay 环境触发的 CustomToast 重试会在本测试的
    // GetMaterialApp 中落地并启动 3 秒消失计时器，这里排空避免 pending timer。
    await tester.pump(const Duration(seconds: 3));
    await tester.pump();
  });

  testWidgets('wheel scroll cooldown blocks maxScrollExtent shrank jumpTo', (
    WidgetTester tester,
  ) async {
    final controller = await setupWheelTest(tester, 'session_wheel_shrank');
    final position = controller.scrollController.position;

    // Disable auto-follow so we enter the shrank path.
    controller.onUserScrollStart(position);
    controller.scrollController.jumpTo(position.maxScrollExtent * 0.5);
    await tester.pump();
    controller.onUserScrollActive(position);
    controller.onUserScrollEnd(position);
    await tester.pump();

    // Now simulate wheel scroll.
    controller.onPointerSignalScroll();
    await tester.pump();

    final pixelsBefore = position.pixels;
    final maxExtentBefore = position.maxScrollExtent;

    controller.onScrollMetricsChanged(
      makeMetrics(
        minScrollExtent: position.minScrollExtent,
        maxScrollExtent: maxExtentBefore - 200,
        pixels: pixelsBefore,
        viewportDimension: position.viewportDimension,
      ),
    );
    await tester.pump();

    expect(
      (position.pixels - pixelsBefore).abs(),
      lessThan(1.0),
      reason:
          'Scroll position should stay stable during wheel scroll cooldown '
          'when maxScrollExtent shrinks',
    );
  });

  testWidgets('continuous wheel ticks keep cooldown active', (
    WidgetTester tester,
  ) async {
    final controller = await setupWheelTest(tester, 'session_wheel_continuous');
    final position = controller.scrollController.position;

    for (var i = 0; i < 5; i++) {
      controller.onPointerSignalScroll();
      await tester.pump(const Duration(milliseconds: 16));
    }
    await tester.pump();

    final pixelsBefore = position.pixels;
    final maxExtentBefore = position.maxScrollExtent;

    controller.onScrollMetricsChanged(
      makeMetrics(
        minScrollExtent: position.minScrollExtent,
        maxScrollExtent: maxExtentBefore + 800,
        pixels: pixelsBefore,
        viewportDimension: position.viewportDimension,
      ),
    );
    await tester.pump();

    expect(
      (position.pixels - pixelsBefore).abs(),
      lessThan(1.0),
      reason: 'After continuous wheel ticks, cooldown should protect position',
    );
  });

  testWidgets(
    'drag scroll cooldown still works independently of wheel cooldown',
    (WidgetTester tester) async {
      final controller = await setupWheelTest(tester, 'session_drag_cooldown');
      final position = controller.scrollController.position;

      controller.onUserScrollStart(position);
      controller.scrollController.jumpTo(position.maxScrollExtent * 0.5);
      await tester.pump();
      controller.onUserScrollActive(position);
      controller.onUserScrollEnd(position);
      await tester.pump();

      final pixelsBefore = position.pixels;
      final maxExtentBefore = position.maxScrollExtent;

      controller.onScrollMetricsChanged(
        makeMetrics(
          minScrollExtent: position.minScrollExtent,
          maxScrollExtent: maxExtentBefore + 500,
          pixels: pixelsBefore,
          viewportDimension: position.viewportDimension,
        ),
      );
      await tester.pump();

      expect(
        (position.pixels - pixelsBefore).abs(),
        lessThan(1.0),
        reason: 'Drag scroll cooldown should still protect position',
      );
    },
  );

  testWidgets('cooldown expires and scroll still works normally', (
    WidgetTester tester,
  ) async {
    final controller = await setupWheelTest(tester, 'session_cooldown_expire');
    final position = controller.scrollController.position;

    controller.onPointerSignalScroll();
    await tester.pump();

    // Advance past the near-bottom cooldown (400ms).
    await tester.pump(const Duration(milliseconds: 500));

    controller.scrollController.jumpTo(position.maxScrollExtent * 0.5);
    await tester.pump();
    expect(position.pixels, greaterThan(0));
  });

  group('txt 附件上传（Bug #2026-06-24-001 回归）', () {
    testWidgets('stageFileFromBytes 接受 txt 并解析为 text/plain 文件附件', (
      WidgetTester tester,
    ) async {
      final controller = Get.put(ChatController());
      addTearDown(() {
        if (!controller.isClosed) controller.onClose();
      });
      controller.sessionId = 'session_txt_stage';
      controller.chatType = 'private';

      final bytes = Uint8List.fromList('hello txt upload'.codeUnits);
      await controller.stageFileFromBytes(
        bytes: bytes,
        fileName: 'note.txt',
        contentType: 'text/plain',
      );

      expect(controller.stagedAttachments.length, 1);
      final staged = controller.stagedAttachments.first;
      expect(staged.type, ChatAttachmentType.file);
      expect(staged.fileName, 'note.txt');
      expect(staged.contentType, 'text/plain');
    });

    testWidgets('txt 走完整上传管线：presign(text/plain) + PUT 真字节', (
      WidgetTester tester,
    ) async {
      final controller = Get.put(ChatController());
      addTearDown(() {
        if (!controller.isClosed) controller.onClose();
      });
      controller.sessionId = 'session_txt_upload';
      controller.chatType = 'private';

      final bytes = Uint8List.fromList(
        List<int>.generate(1024, (i) => 65 + (i % 26)),
      );
      await controller
          .uploadPreparedAttachmentsForTest(<ChatPreparedAttachmentUpload>[
            ChatPreparedAttachmentUpload(
              type: ChatAttachmentType.file,
              fileName: 'note.txt',
              contentType: 'text/plain',
              bytes: bytes,
              contentLength: bytes.length,
            ),
          ]);

      expect(ossService.presignCalls, 1);
      expect(ossService.requestedFileNames, contains('note.txt'));
      expect(ossService.requestedContentTypes, contains('text/plain'));
      expect(ossService.uploadCalls, 1);
      expect(ossService.uploadedContentTypes, contains('text/plain'));
      expect(ossService.uploadedByteLengths, contains(bytes.length));
    });

    testWidgets('不支持的类型被拒绝、不入待发列表（触发明确提示，不静默）', (WidgetTester tester) async {
      final controller = Get.put(ChatController());
      addTearDown(() {
        if (!controller.isClosed) controller.onClose();
      });
      controller.sessionId = 'session_txt_reject';
      controller.chatType = 'private';

      await controller.stageFileFromBytes(
        bytes: Uint8List.fromList(<int>[1, 2, 3, 4]),
        fileName: 'virus.exe',
        contentType: 'application/octet-stream',
      );

      expect(controller.stagedAttachments, isEmpty);
    });

    testWidgets('空文件（0 字节 txt）被拒绝、不入待发列表（明确提示而非笼统上传异常）', (
      WidgetTester tester,
    ) async {
      final controller = Get.put(ChatController());
      addTearDown(() {
        if (!controller.isClosed) controller.onClose();
      });
      controller.sessionId = 'session_txt_empty';
      controller.chatType = 'private';

      await controller.stageFileFromBytes(
        bytes: Uint8List(0),
        fileName: 'empty.txt',
        contentType: 'text/plain',
      );

      expect(controller.stagedAttachments, isEmpty);
    });
  });

  group('desktop chat pane', () {
    Future<void> pumpPaneApp(WidgetTester tester) async {
      tester.view.physicalSize = const Size(1400, 900);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.reset);
      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          initialRoute: AppRoutes.home,
          getPages: [
            GetPage(
              name: AppRoutes.home,
              page: () => const Scaffold(
                body: Row(
                  children: [
                    SizedBox(width: 300),
                    Expanded(child: ChatPaneNavigator()),
                  ],
                ),
              ),
            ),
            GetPage(
              name: AppRoutes.chat,
              page: () =>
                  ChatView(controllerTag: ChatBinding.currentControllerTag()),
              binding: ChatBinding(),
            ),
          ],
        ),
      );
      await tester.pumpAndSettle();
    }

    String tagOf(String sid) => ChatBinding.controllerTagForSession(sid);

    testWidgets(
      'opens chats in the pane and replaces the previous controller',
      (WidgetTester tester) async {
        Get.testMode = false;
        sessionService.detailResult = const SessionDetailResult(
          data: {'session_type': 1},
        );
        await pumpPaneApp(tester);
        expect(ChatPaneHost.isAvailable, isTrue);
        expect(find.byType(ChatPanePlaceholder), findsOneWidget);

        unawaited(
          ChatRouteNavigator.toChat(
            sessionId: 'pane_a',
            title: 'A',
            type: 'private',
          ),
        );
        await tester.pumpAndSettle();

        expect(Get.currentRoute, AppRoutes.home);
        expect(ChatPaneHost.activeSessionId, 'pane_a');
        expect(find.byType(ChatView), findsOneWidget);
        expect(tester.widget<ChatView>(find.byType(ChatView)).embedded, isTrue);
        expect(find.byIcon(Icons.arrow_back_ios_rounded), findsNothing);
        final controllerA = Get.find<ChatController>(tag: tagOf('pane_a'));
        expect(controllerA.sessionId, 'pane_a');
        expect(controllerA.chatTitle, 'A');

        unawaited(
          ChatRouteNavigator.toChat(
            sessionId: 'pane_b',
            title: 'B',
            type: 'private',
          ),
        );
        await tester.pumpAndSettle();

        expect(ChatPaneHost.activeSessionId, 'pane_b');
        expect(Get.isRegistered<ChatController>(tag: tagOf('pane_a')), isFalse);
        expect(Get.isRegistered<ChatController>(tag: tagOf('pane_b')), isTrue);
        expect(find.byType(ChatView), findsOneWidget);

        Get.find<ChatController>(tag: tagOf('pane_b')).closeChatRoute();
        await tester.pumpAndSettle();

        expect(ChatPaneHost.activeSessionId, isNull);
        expect(Get.isRegistered<ChatController>(tag: tagOf('pane_b')), isFalse);
        expect(find.byType(ChatPanePlaceholder), findsOneWidget);
        expect(Get.currentRoute, AppRoutes.home);
      },
    );

    testWidgets('without a pane toChat keeps the full-screen chat route', (
      WidgetTester tester,
    ) async {
      Get.testMode = false;
      sessionService.detailResult = const SessionDetailResult(
        data: {'session_type': 1},
      );
      tester.view.physicalSize = const Size(390, 844);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.reset);
      await pumpChatRouteApp(tester);
      expect(ChatPaneHost.isAvailable, isFalse);

      unawaited(
        ChatRouteNavigator.toChat(
          sessionId: 'phone_a',
          title: 'A',
          type: 'private',
        ),
      );
      await tester.pumpAndSettle();

      expect(Get.currentRoute, startsWith(AppRoutes.chat));
      expect(ChatPaneHost.activeSessionId, isNull);
      final view = tester.widget<ChatView>(find.byType(ChatView));
      expect(view.embedded, isFalse);
      expect(find.byIcon(Icons.arrow_back_ios_rounded), findsOneWidget);
      final controller = Get.find<ChatController>(tag: tagOf('phone_a'));
      expect(controller.routeArguments, isNull);
      expect(controller.sessionId, 'phone_a');

      controller.closeChatRoute();
      await tester.pumpAndSettle();
      expect(Get.currentRoute, AppRoutes.home);
      expect(Get.isRegistered<ChatController>(tag: tagOf('phone_a')), isFalse);
    });

    testWidgets('pane is ignored while a full-screen chat route is on top', (
      WidgetTester tester,
    ) async {
      Get.testMode = false;
      sessionService.detailResult = const SessionDetailResult(
        data: {'session_type': 1},
      );
      await pumpPaneApp(tester);
      expect(ChatPaneHost.isAvailable, isTrue);

      // A chat already opened as a root route (e.g. opened while narrow).
      unawaited(
        Get.toNamed(
          AppRoutes.chat,
          arguments: {'session_id': 'top_a', 'title': 'A', 'type': 'private'},
          parameters: {'session_id': 'top_a', 'title': 'A', 'type': 'private'},
        ),
      );
      await tester.pumpAndSettle();
      expect(Get.currentRoute, startsWith(AppRoutes.chat));

      unawaited(
        ChatRouteNavigator.toChat(
          sessionId: 'top_b',
          title: 'B',
          type: 'private',
        ),
      );
      await tester.pumpAndSettle();

      // Chat -> chat stays on the root navigator; the pane is untouched.
      expect(Get.currentRoute, startsWith(AppRoutes.chat));
      expect(ChatPaneHost.activeSessionId, isNull);
      expect(Get.isRegistered<ChatController>(tag: tagOf('top_b')), isTrue);
      final views = tester.widgetList<ChatView>(
        find.byType(ChatView, skipOffstage: false),
      );
      expect(views.every((v) => !v.embedded), isTrue);

      await Get.delete<ChatController>(tag: tagOf('top_b'), force: true);
      await Get.delete<ChatController>(tag: tagOf('top_a'), force: true);
      await tester.pumpAndSettle();
    });

    testWidgets('unmounting the pane disposes the active controller', (
      WidgetTester tester,
    ) async {
      Get.testMode = false;
      sessionService.detailResult = const SessionDetailResult(
        data: {'session_type': 1},
      );
      await pumpPaneApp(tester);
      unawaited(
        ChatRouteNavigator.toChat(
          sessionId: 'pane_c',
          title: 'C',
          type: 'private',
        ),
      );
      await tester.pumpAndSettle();
      expect(Get.isRegistered<ChatController>(tag: tagOf('pane_c')), isTrue);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pumpAndSettle();

      expect(ChatPaneHost.isAvailable, isFalse);
      expect(ChatPaneHost.activeSessionId, isNull);
      expect(Get.isRegistered<ChatController>(tag: tagOf('pane_c')), isFalse);
    });
  });
}
