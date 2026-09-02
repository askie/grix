import 'dart:async';

import 'package:get/get.dart';

import '../../../app/routes/app_routes.dart';
import '../../../data/providers/agent_service.dart';
import '../../../data/providers/auth_service.dart';
import '../../../data/providers/friend_service.dart';
import '../../../data/providers/im_service.dart';
import '../../../data/providers/session_service.dart';
import '../../../shared/models/session_avatar_member.dart';

class GroupMemberProfile {
  const GroupMemberProfile({
    required this.memberId,
    required this.memberType,
    required this.role,
    required this.displayName,
    required this.username,
    required this.avatarUrl,
    required this.isFriend,
    required this.isMe,
  });

  final String memberId;
  final int memberType;
  final int role;
  final String displayName;
  final String username;
  final String avatarUrl;
  final bool isFriend;
  final bool isMe;

  bool get isUser => memberType == 1;

  SessionAvatarMember toAvatarMember() {
    return SessionAvatarMember(
      memberId: memberId,
      memberType: memberType,
      displayName: displayName,
      avatarUrl: avatarUrl,
    );
  }
}

class GroupInfoController extends GetxController {
  GroupInfoController({
    Map<String, dynamic>? initialArguments,
    Map<String, String?>? initialParameters,
    ImService? imService,
    SessionService? sessionService,
    FriendService? friendService,
    AuthService? authService,
    AgentService? agentService,
  }) : _initialArguments = initialArguments,
       _initialParameters = initialParameters,
       _imService = imService ?? Get.find<ImService>(),
       _sessionService =
           sessionService ??
           (Get.isRegistered<SessionService>()
               ? Get.find<SessionService>()
               : null),
       _friendService =
           friendService ??
           (Get.isRegistered<FriendService>()
               ? Get.find<FriendService>()
               : null),
       _authService =
           authService ??
           (Get.isRegistered<AuthService>() ? Get.find<AuthService>() : null),
       _agentService =
           agentService ??
           (Get.isRegistered<AgentService>() ? Get.find<AgentService>() : null);

  final Map<String, dynamic>? _initialArguments;
  final Map<String, String?>? _initialParameters;

  final ImService _imService;
  final SessionService? _sessionService;
  final FriendService? _friendService;
  final AuthService? _authService;
  final AgentService? _agentService;

  final RxString sessionId = ''.obs;
  final RxString groupName = ''.obs;
  final RxBool isLoading = false.obs;
  final RxString loadingError = ''.obs;
  final RxList<GroupMemberProfile> members = <GroupMemberProfile>[].obs;

  Worker? _friendListWorker;
  Worker? _agentListWorker;

  @override
  void onInit() {
    super.onInit();

    final args = _initialArguments ?? _readRouteArguments();
    final params = _initialParameters ?? Get.parameters;

    sessionId.value = _readRoutingValue(
      args: args,
      params: params,
      key: 'session_id',
    ).trim();
    groupName.value = _readRoutingValue(
      args: args,
      params: params,
      key: 'title',
    ).trim();

    if (groupName.value.isEmpty) {
      groupName.value = _imService.resolveSessionDisplayTitleById(
        sessionId.value,
        fallbackTitle: '',
        type: 'group',
      );
    }

    final fs = _friendService;
    if (fs != null) {
      _friendListWorker = ever(fs.friendList, (_) {
        _refreshMemberProfilesFromCache();
      });
    }
    final agentService = _agentService;
    if (agentService != null) {
      _agentListWorker = ever(agentService.agents, (_) {
        _refreshMemberProfilesFromCache();
      });
    }

    unawaited(loadGroupDetail());
  }

  @override
  void onClose() {
    _friendListWorker?.dispose();
    _agentListWorker?.dispose();
    super.onClose();
  }

  int get memberCount => members.length;

  List<SessionAvatarMember> get groupAvatarMembers {
    return members
        .map((m) => m.toAvatarMember())
        .take(9)
        .toList(growable: false);
  }

  String get avatarTitle {
    final normalized = groupName.value.trim();
    if (normalized.isNotEmpty) return normalized;
    return 'chat_group'.tr;
  }

  String get avatarSeed {
    final sid = sessionId.value.trim();
    if (sid.isNotEmpty) return sid;
    return avatarTitle;
  }

  Future<void> loadGroupDetail() async {
    final sid = sessionId.value.trim();
    if (sid.isEmpty) {
      loadingError.value = 'session_error_session_id_required'.tr;
      return;
    }

    final sessionService = _sessionService;
    if (sessionService == null) {
      loadingError.value = 'common_unknown_error'.tr;
      return;
    }

    isLoading.value = true;
    loadingError.value = '';

    try {
      final detailResult = await sessionService.fetchSessionDetailResult(sid);
      final detail = detailResult.data;
      if (detail == null) {
        loadingError.value = detailResult.message.trim().isNotEmpty
            ? detailResult.message
            : 'common_unknown_error'.tr;
        return;
      }

      final sessionType = _parseInt(detail['session_type']);
      if (sessionType != 2) {
        loadingError.value = 'common_error'.tr;
        return;
      }

      final groupNameFromDetail =
          (detail['group_name'] ?? detail['title'] ?? '').toString().trim();
      if (groupNameFromDetail.isNotEmpty) {
        groupName.value = groupNameFromDetail;
      } else if (groupName.value.trim().isEmpty) {
        groupName.value = _imService.resolveSessionDisplayTitleById(
          sid,
          fallbackTitle: '',
          type: 'group',
        );
      }

      final parsedMembers = _parseMembers(detail['members']);
      if (parsedMembers.isEmpty) {
        members.assignAll(const <GroupMemberProfile>[]);
        return;
      }

      await _prepareDependencies(parsedMembers);
      members.assignAll(parsedMembers.map(_resolveMemberProfile));
    } finally {
      isLoading.value = false;
    }
  }

  void openMemberProfile(GroupMemberProfile member) {
    if (!member.isUser) {
      return;
    }
    final mid = member.memberId.trim();
    if (mid.isEmpty) {
      return;
    }

    Get.toNamed(
      AppRoutes.accountInfo,
      arguments: {
        'peer_id': mid,
        'peer_type': '1',
        'nickname': member.displayName,
        'username': member.username,
        'avatar_url': member.avatarUrl,
        'title': member.displayName,
      },
      parameters: {'peer_id': mid, 'peer_type': '1'},
    );
  }

  void _refreshMemberProfilesFromCache() {
    if (members.isEmpty) {
      return;
    }
    final updated = members
        .map((member) {
          return _resolveMemberProfile(
            _RawGroupMember(
              memberId: member.memberId,
              memberType: member.memberType,
              role: member.role,
              nickname: member.displayName,
            ),
          );
        })
        .toList(growable: false);

    members.assignAll(updated);
  }

  Future<void> _prepareDependencies(List<_RawGroupMember> rawMembers) async {
    final fs = _friendService;
    final userIds = <String>[];
    var containsAgent = false;

    for (final member in rawMembers) {
      if (member.memberType == 2) {
        containsAgent = true;
        continue;
      }
      if (member.memberType != 1) {
        continue;
      }
      userIds.add(member.memberId);
    }

    if (fs != null && userIds.isNotEmpty) {
      await fs.ensureUserProfiles(userIds);
    }

    final agentService = _agentService;
    if (containsAgent && agentService != null && agentService.agents.isEmpty) {
      await agentService.loadAgents();
    }
  }

  List<_RawGroupMember> _parseMembers(dynamic rawMembers) {
    if (rawMembers is! List) {
      return const <_RawGroupMember>[];
    }

    final parsed = <_RawGroupMember>[];
    for (final raw in rawMembers) {
      if (raw is! Map) {
        continue;
      }
      final memberId = (raw['member_id'] ?? '').toString().trim();
      if (memberId.isEmpty) {
        continue;
      }

      parsed.add(
        _RawGroupMember(
          memberId: memberId,
          memberType: _parseInt(raw['member_type']),
          role: _parseInt(raw['role']),
          nickname: (raw['nickname'] ?? '').toString().trim(),
        ),
      );
    }

    return parsed;
  }

  GroupMemberProfile _resolveMemberProfile(_RawGroupMember member) {
    final myUserId = _authService?.userId?.trim() ?? '';
    final fs = _friendService;

    if (member.memberType == 2) {
      final displayName = _resolveAgentName(member);
      final avatarUrl = _resolveAgentAvatarUrl(member.memberId);
      return GroupMemberProfile(
        memberId: member.memberId,
        memberType: member.memberType,
        role: member.role,
        displayName: displayName,
        username: '',
        avatarUrl: avatarUrl,
        isFriend: false,
        isMe: false,
      );
    }

    final isMe = myUserId.isNotEmpty && myUserId == member.memberId;
    String displayName = '';
    String username = '';
    String avatarUrl = '';

    if (isMe) {
      if (member.nickname.isNotEmpty) {
        displayName = member.nickname;
      }
      final me = _authService?.user;
      username = me?.username.trim() ?? '';
      avatarUrl = me?.avatarUrl?.trim() ?? '';
      if (displayName.isEmpty) {
        displayName = me?.nickname.trim() ?? '';
      }
      if (displayName.isEmpty) {
        displayName = username;
      }
    } else {
      if (member.nickname.isNotEmpty) {
        displayName = member.nickname;
      }
      if (fs != null) {
        if (displayName.isEmpty) {
          displayName = fs.getUserNickname(member.memberId)?.trim() ?? '';
        }
        username = fs.getUserUsername(member.memberId)?.trim() ?? '';
        avatarUrl = fs.getUserAvatarUrl(member.memberId)?.trim() ?? '';
      }
    }

    if (displayName.isEmpty && member.nickname.isNotEmpty) {
      displayName = member.nickname;
    }
    if (displayName.isEmpty) {
      displayName = member.memberId;
    }

    return GroupMemberProfile(
      memberId: member.memberId,
      memberType: member.memberType,
      role: member.role,
      displayName: displayName,
      username: username,
      avatarUrl: avatarUrl,
      isFriend: fs?.isFriend(member.memberId) ?? false,
      isMe: isMe,
    );
  }

  String _resolveAgentName(_RawGroupMember member) {
    if (member.nickname.isNotEmpty) {
      return member.nickname;
    }

    final agentService = _agentService;
    if (agentService != null) {
      final index = agentService.agents.indexWhere(
        (a) => a.id == member.memberId,
      );
      if (index != -1) {
        final name = agentService.agents[index].agentName.trim();
        if (name.isNotEmpty) {
          return name;
        }
      }
    }

    return 'Agent';
  }

  String _resolveAgentAvatarUrl(String memberId) {
    final agentService = _agentService;
    if (agentService == null) {
      return '';
    }

    final index = agentService.agents.indexWhere((a) => a.id == memberId);
    if (index == -1) {
      return '';
    }

    return agentService.agents[index].avatarUrl.trim();
  }

  int _parseInt(dynamic raw) {
    if (raw is int) return raw;
    if (raw is num) return raw.toInt();
    return int.tryParse(raw?.toString() ?? '') ?? 0;
  }

  Map<String, dynamic> _readRouteArguments() {
    final rawArgs = Get.arguments;
    if (rawArgs is Map<String, dynamic>) {
      return rawArgs;
    }
    if (rawArgs is Map) {
      return rawArgs.map((key, value) => MapEntry(key.toString(), value));
    }
    return const <String, dynamic>{};
  }

  String _readRoutingValue({
    required Map<String, dynamic> args,
    required Map<String, String?> params,
    required String key,
    String fallback = '',
  }) {
    if (args.containsKey(key)) {
      final value = args[key]?.toString();
      if (value != null && value.trim().isNotEmpty) {
        return value;
      }
    }

    final parameterValue = params[key];
    if (parameterValue != null && parameterValue.trim().isNotEmpty) {
      return parameterValue;
    }

    return fallback;
  }
}

class _RawGroupMember {
  const _RawGroupMember({
    required this.memberId,
    required this.memberType,
    required this.role,
    required this.nickname,
  });

  final String memberId;
  final int memberType;
  final int role;
  final String nickname;
}
