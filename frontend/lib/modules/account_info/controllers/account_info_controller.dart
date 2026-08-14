import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:get/get.dart';

import '../../../app/routes/app_routes.dart';
import '../../../data/models/session_model.dart';
import '../../../data/providers/agent_service.dart';
import '../../../data/providers/auth_service.dart';
import '../../../data/providers/friend_service.dart';
import '../../../data/providers/im_service.dart';
import '../../../data/providers/local_db.dart';
import '../../../data/providers/session_service.dart';
import '../../chat/message_cards/services/chat_message_card_codec.dart';
import '../../chat/models/chat_forward_target_option.dart';
import '../../chat/services/chat_forward_target_option_resolver.dart';
import '../../chat/services/chat_route_navigator.dart';
import '../../../shared/utils/chat_message_preview.dart';
import '../../../shared/utils/time_formatter.dart';
import '../../../shared/utils/toast_util.dart';

part 'account_info_controller_actions.dart';
part 'account_info_controller_session_context.dart';

class AccountInfoController extends GetxController
    with _AccountInfoControllerSessionContext, _AccountInfoControllerActions {
  AccountInfoController({
    Map<String, dynamic>? initialArguments,
    Map<String, String?>? initialParameters,
    ImService? imService,
    FriendService? friendService,
    AgentService? agentService,
    SessionService? sessionService,
    AuthService? authService,
  }) : _initialArguments = initialArguments,
       _initialParameters = initialParameters,
       imService = imService ?? Get.find<ImService>(),
       _friendService =
           friendService ??
           (Get.isRegistered<FriendService>()
               ? Get.find<FriendService>()
               : null),
       _agentService =
           agentService ??
           (Get.isRegistered<AgentService>() ? Get.find<AgentService>() : null),
       _sessionService =
           sessionService ??
           (Get.isRegistered<SessionService>()
               ? Get.find<SessionService>()
               : null),
       _authService =
           authService ??
           (Get.isRegistered<AuthService>() ? Get.find<AuthService>() : null);

  final Map<String, dynamic>? _initialArguments;
  final Map<String, String?>? _initialParameters;

  @override
  final ImService imService;
  @override
  final FriendService? _friendService;
  @override
  final AgentService? _agentService;
  @override
  final SessionService? _sessionService;
  @override
  final AuthService? _authService;
  late final ChatForwardTargetOptionResolver _forwardTargetOptionResolver =
      ChatForwardTargetOptionResolver(
        imService: imService,
        friendService: _friendService,
        agentService: _agentService,
      );

  @override
  final RxString peerId = ''.obs;
  @override
  final RxString nickname = ''.obs;
  @override
  final RxString username = ''.obs;
  @override
  final RxString introduction = ''.obs;
  @override
  final RxString avatarUrl = ''.obs;
  @override
  final RxBool isProfileLoading = false.obs;
  @override
  final RxBool isActionProcessing = false.obs;
  @override
  final RxBool friendRequestSent = false.obs;
  final RxInt _friendListVersion = 0.obs;
  final RxInt _agentListVersion = 0.obs;

  @override
  final RxString searchQuery = ''.obs;

  /// 资料页内容列表的滚动控制器。
  /// 用于在滚动离开顶部资料卡后，将顶栏标题从“用户资料”切换为对方昵称。
  final ScrollController scrollController = ScrollController();

  /// 顶栏是否显示对方昵称（true=显示昵称，false=显示“用户资料”）。
  final RxBool showTitleNickname = false.obs;

  /// 顶部资料卡（含外边距）实测高度，作为标题切换阈值的依据。
  double _profileCardExtent = 0;

  /// 当前资料页生命周期内最近被点击进入聊天页的会话 sessionId。
  /// 仅用于在 _SessionHistoryTile 上渲染淡色高亮，便于从聊天页返回时识别上一次点击的会话；
  /// 控制器销毁后随 RxString 一起释放，不做持久化。
  @override
  final RxString lastTappedSessionId = ''.obs;

  @override
  bool _hasExplicitPeerTarget = false;
  @override
  String conversationGroupKey = '';
  @override
  String seedSessionId = '';
  @override
  String routeFallbackTitle = '';
  @override
  int peerTypeHint = 1;

  Worker? _friendListWorker;
  Worker? _agentListWorker;

  @override
  void onInit() {
    super.onInit();

    final args = _initialArguments ?? _readRouteArguments();
    final params = _initialParameters ?? Get.parameters;

    conversationGroupKey = _readRoutingValue(
      args: args,
      params: params,
      key: 'group_key',
    ).trim();
    seedSessionId = _readRoutingValue(
      args: args,
      params: params,
      key: 'session_id',
    ).trim();
    routeFallbackTitle = _readRoutingValue(
      args: args,
      params: params,
      key: 'title',
    );
    peerTypeHint = _parseInt(
      _readRoutingValue(
        args: args,
        params: params,
        key: 'peer_type',
        fallback: '1',
      ),
      fallback: 1,
    );

    final routedPeerId = _readRoutingValue(
      args: args,
      params: params,
      key: 'peer_id',
    ).trim();
    _hasExplicitPeerTarget = routedPeerId.isNotEmpty;
    peerId.value = routedPeerId.isNotEmpty
        ? routedPeerId
        : _extractPeerIdFromGroupKey(conversationGroupKey);

    nickname.value = _readRoutingValue(
      args: args,
      params: params,
      key: 'nickname',
    ).trim();
    username.value = _readRoutingValue(
      args: args,
      params: params,
      key: 'username',
    ).trim();
    introduction.value = _readRoutingValue(
      args: args,
      params: params,
      key: 'introduction',
    ).trim();
    avatarUrl.value = _readRoutingValue(
      args: args,
      params: params,
      key: 'avatar_url',
    ).trim();

    _applyFromSeedSession();
    _syncProfileFromFriendService();
    _syncProfileFromAgentService();

    final fs = _friendService;
    if (fs != null) {
      _friendListWorker = ever(fs.friendList, (_) {
        _friendListVersion.value++;
        _syncProfileFromFriendService();
      });
    }
    final agentService = _agentService;
    if (agentService != null) {
      _agentListWorker = ever(agentService.agents, (_) {
        _agentListVersion.value++;
        _syncProfileFromAgentService();
      });
    }

    scrollController.addListener(_handleScroll);
    _initDbSearch();

    unawaited(_ensurePeerIdentityAndProfile());
  }

  @override
  void onClose() {
    _disposeDbSearch();
    _friendListWorker?.dispose();
    _agentListWorker?.dispose();
    scrollController.removeListener(_handleScroll);
    scrollController.dispose();
    super.onClose();
  }

  /// 资料卡（含外边距）实测高度变化时回调，更新切换阈值并即时重算标题状态。
  void updateProfileCardExtent(double extent) {
    if ((extent - _profileCardExtent).abs() < 0.5) return;
    _profileCardExtent = extent;
    _handleScroll();
  }

  /// 滚动偏移越过资料卡时显示昵称，回到顶部时显示“用户资料”。
  void _handleScroll() {
    if (!scrollController.hasClients) return;
    final threshold = (_profileCardExtent - 8).clamp(0.0, double.infinity);
    final show = scrollController.offset > threshold;
    if (show != showTitleNickname.value) {
      showTitleNickname.value = show;
    }
  }

  String get avatarSeed {
    final byPeerId = peerId.value.trim();
    if (byPeerId.isNotEmpty) return byPeerId;
    final bySessionId = seedSessionId.trim();
    if (bySessionId.isNotEmpty) return bySessionId;
    return displayNickname;
  }

  String get avatarTitle => displayNickname;

  @override
  String get displayNickname {
    final candidates = <String>[
      nickname.value,
      username.value,
      routeFallbackTitle,
      _resolveSeedSessionDisplayTitle(),
      peerId.value,
    ];
    for (final candidate in candidates) {
      final normalized = candidate.trim();
      if (normalized.isEmpty || normalized == seedSessionId) {
        continue;
      }
      return normalized;
    }
    return 'conversations_thread_untitled'.tr;
  }

  @override
  String get displayAccount {
    final account = username.value.trim();
    if (account.isNotEmpty) {
      return '@$account';
    }
    final pid = peerId.value.trim();
    if (pid.isNotEmpty) {
      return pid;
    }
    return '-';
  }

  String get displayUserId {
    final pid = peerId.value.trim();
    if (pid.isNotEmpty) {
      return pid;
    }
    return '-';
  }

  String get displayIntroduction => introduction.value.trim();

  /// 资料页展示用的简介预览：只保留首个空行之前的内容。
  /// 简介可能承载类似系统提示词的长文本，作者用空行分隔对外展示与内部部分，
  /// 空行之后的内容不对外渲染。
  String get introductionPreview {
    final lines = displayIntroduction.split('\n');
    final blankIndex = lines.indexWhere((line) => line.trim().isEmpty);
    final visible = blankIndex == -1 ? lines : lines.sublist(0, blankIndex);
    return visible.join('\n').trimRight();
  }

  bool get isSelf {
    final myId = _authService?.userId?.trim() ?? '';
    final pid = peerId.value.trim();
    return myId.isNotEmpty && pid.isNotEmpty && myId == pid;
  }

  bool get isFriend {
    _friendListVersion.value;
    final fs = _friendService;
    final pid = peerId.value.trim();
    if (fs == null || pid.isEmpty) {
      return false;
    }
    return fs.isFriend(pid);
  }

  bool get isOwnedAgent {
    _agentListVersion.value;
    final agentService = _agentService;
    final myId = _authService?.userId?.trim() ?? '';
    final pid = peerId.value.trim();
    if (agentService == null || myId.isEmpty || pid.isEmpty) {
      return false;
    }

    final idx = agentService.agents.indexWhere(
      (agent) => agent.id.trim() == pid,
    );
    if (idx == -1) {
      return false;
    }

    return agentService.agents[idx].ownerID.trim() == myId;
  }

  /// 是否是「分享给我的」agent：被共享者也能像自己的 agent 一样发起会话。
  bool get isSharedToMeAgent {
    _agentListVersion.value;
    final agentService = _agentService;
    final pid = peerId.value.trim();
    if (agentService == null || pid.isEmpty) {
      return false;
    }
    return agentService.sharedAgents.any((agent) => agent.id.trim() == pid);
  }

  @override
  bool get canStartChat {
    if (isSelf) return false;
    if (peerTypeHint == 1) {
      return isFriend;
    }
    if (peerTypeHint == 2) {
      return isOwnedAgent || isSharedToMeAgent;
    }
    return false;
  }

  @override
  bool get canAddFriend {
    if (isSelf) return false;
    if (peerTypeHint != 1) return false;
    if (isFriend) return false;
    return !friendRequestSent.value;
  }

  @override
  bool get canEditRemark {
    if (isSelf) return false;
    if (peerTypeHint != 1) return false;
    return isFriend;
  }

  @override
  bool get canDeleteFriend {
    if (isSelf) return false;
    if (peerTypeHint != 1) return false;
    return isFriend;
  }

  @override
  bool get canReportUser {
    if (isSelf) return false;
    if (peerTypeHint != 1) return false;
    return peerId.value.trim().isNotEmpty;
  }

  @override
  bool get canForwardProfileCard {
    if (peerTypeHint != 1 && peerTypeHint != 2) {
      return false;
    }
    return peerId.value.trim().isNotEmpty && displayNickname.trim().isNotEmpty;
  }

  List<ChatForwardTargetOption> buildForwardTargetOptions() {
    return _forwardTargetOptionResolver.resolveAll();
  }
}
