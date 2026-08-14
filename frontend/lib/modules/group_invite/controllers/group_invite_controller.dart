import 'dart:async';

import 'package:get/get.dart';

import '../../../data/providers/group_qr_service.dart';
import '../../chat/services/chat_route_navigator.dart';

class GroupInviteController extends GetxController {
  GroupInviteController({
    Map<String, dynamic>? initialArguments,
    Map<String, String?>? initialParameters,
    GroupQrService? groupQrService,
  }) : _initialArguments = initialArguments,
       _initialParameters = initialParameters,
       _groupQrService =
           groupQrService ??
           (Get.isRegistered<GroupQrService>()
               ? Get.find<GroupQrService>()
               : null);

  final Map<String, dynamic>? _initialArguments;
  final Map<String, String?>? _initialParameters;
  final GroupQrService? _groupQrService;

  final RxString code = ''.obs;
  final RxString sessionId = ''.obs;
  final RxString groupName = ''.obs;
  final RxString ownerNickname = ''.obs;
  final RxInt memberCount = 0.obs;
  final RxBool isMember = false.obs;

  final RxBool isLoading = false.obs;
  final RxBool isJoining = false.obs;
  final RxString loadingError = ''.obs;
  final RxString actionError = ''.obs;

  @override
  void onInit() {
    super.onInit();

    final args = _initialArguments ?? _readRouteArguments();
    final params = _initialParameters ?? Get.parameters;

    code.value = _readRoutingValue(
      args: args,
      params: params,
      key: 'code',
    ).trim();
    sessionId.value = _readRoutingValue(
      args: args,
      params: params,
      key: 'session_id',
    ).trim();
    groupName.value = _readRoutingValue(
      args: args,
      params: params,
      key: 'group_name',
    ).trim();
    ownerNickname.value = _readRoutingValue(
      args: args,
      params: params,
      key: 'owner_nickname',
    ).trim();
    memberCount.value = _parseInt(
      _readRoutingValue(args: args, params: params, key: 'member_count'),
    );
    isMember.value = _parseBool(
      _readRoutingValue(args: args, params: params, key: 'is_member'),
    );

    if (code.value.isEmpty) {
      loadingError.value = 'conversations_scan_invalid_qr'.tr;
      return;
    }

    unawaited(loadGroupPreview());
  }

  String get displayGroupName {
    final value = groupName.value.trim();
    if (value.isNotEmpty) {
      return value;
    }
    return 'conversations_group'.tr;
  }

  String get displayOwner {
    final value = ownerNickname.value.trim();
    if (value.isNotEmpty) {
      return value;
    }
    return '-';
  }

  String get actionButtonLabel {
    if (isMember.value) {
      return 'group_invite_enter_group'.tr;
    }
    return 'group_invite_join_group'.tr;
  }

  Future<void> loadGroupPreview() async {
    final qrCode = code.value.trim();
    if (qrCode.isEmpty) {
      loadingError.value = 'conversations_scan_invalid_qr'.tr;
      return;
    }
    final service = _groupQrService;
    if (service == null) {
      loadingError.value = 'common_unknown_error'.tr;
      return;
    }

    isLoading.value = true;
    loadingError.value = '';
    actionError.value = '';

    try {
      final result = await service.resolveCodeDetailed(qrCode);
      if (!result.ok || result.data == null) {
        loadingError.value = result.message.trim().isNotEmpty
            ? result.message
            : 'common_unknown_error'.tr;
        return;
      }

      final data = result.data!;
      code.value = data.code.trim().isEmpty ? qrCode : data.code.trim();
      sessionId.value = data.sessionId.trim();
      groupName.value = data.groupName.trim();
      ownerNickname.value = data.ownerNickname.trim();
      memberCount.value = data.memberCount;
      isMember.value = data.isMember;
    } finally {
      isLoading.value = false;
    }
  }

  Future<void> joinOrEnterGroup() async {
    if (isJoining.value) {
      return;
    }

    final sid = sessionId.value.trim();
    if (isMember.value && sid.isNotEmpty) {
      await _openChat(sid);
      return;
    }

    final qrCode = code.value.trim();
    if (qrCode.isEmpty) {
      actionError.value = 'conversations_scan_invalid_qr'.tr;
      return;
    }

    final service = _groupQrService;
    if (service == null) {
      actionError.value = 'common_unknown_error'.tr;
      return;
    }

    isJoining.value = true;
    actionError.value = '';
    try {
      final result = await service.joinByCodeDetailed(qrCode);
      if (!result.ok || result.data == null) {
        actionError.value = result.message.trim().isNotEmpty
            ? result.message
            : 'common_unknown_error'.tr;
        return;
      }

      final data = result.data!;
      final nextSessionId = data.sessionId.trim();
      if (nextSessionId.isEmpty) {
        actionError.value = 'common_unknown_error'.tr;
        return;
      }

      sessionId.value = nextSessionId;
      if (data.groupName.trim().isNotEmpty) {
        groupName.value = data.groupName.trim();
      }
      isMember.value = true;
      await _openChat(nextSessionId);
    } finally {
      isJoining.value = false;
    }
  }

  Future<void> _openChat(String sid) async {
    await ChatRouteNavigator.toChat(
      sessionId: sid,
      title: groupName.value.trim(),
      type: 'group',
    );
  }

  Map<String, dynamic>? _readRouteArguments() {
    final raw = Get.arguments;
    if (raw is Map<String, dynamic>) {
      return raw;
    }
    if (raw is Map) {
      return raw.map((key, value) => MapEntry(key.toString(), value));
    }
    return null;
  }

  String _readRoutingValue({
    required Map<String, dynamic>? args,
    required Map<String, String?>? params,
    required String key,
    String fallback = '',
  }) {
    final fromArgs = args?[key];
    if (fromArgs != null) {
      final normalized = fromArgs.toString().trim();
      if (normalized.isNotEmpty) {
        return normalized;
      }
    }
    final fromParams = params?[key];
    if (fromParams != null) {
      final normalized = fromParams.trim();
      if (normalized.isNotEmpty) {
        return normalized;
      }
    }
    return fallback;
  }

  int _parseInt(String raw, {int fallback = 0}) {
    return int.tryParse(raw.trim()) ?? fallback;
  }

  bool _parseBool(String raw) {
    final normalized = raw.trim().toLowerCase();
    if (normalized.isEmpty) {
      return false;
    }
    return normalized == '1' || normalized == 'true' || normalized == 'yes';
  }
}
