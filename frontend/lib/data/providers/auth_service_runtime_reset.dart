part of 'auth_service.dart';

mixin _AuthServiceRuntimeReset on _AuthServiceContract {
  @override
  Future<void> _notifyServerLogout() async {
    final access = token;
    if (access == null || access.isEmpty) return;
    try {
      final deviceId = await DeviceIdentity.resolveDeviceId();
      await _dio.post(
        '/auth/logout',
        data: {'device_id': deviceId},
        options: Options(headers: {'Authorization': 'Bearer $access'}),
      );
    } catch (e) {
      debugPrint('Logout notify ignored: $e');
    }
  }

  @override
  Future<void> _clearLocalAuthData() async {
    _token.value = null;
    _refreshToken.value = null;
    _accessExpiresAtMs.value = null;
    _user.value = null;
    _isLoggedIn.value = false;

    await _authSessionStore.removeAll(_authSessionKeys);
    // 手表存的是一枚仍然有效的 access token，退出登录必须一起清掉。
    await WatchCredentialSync.clear();
  }

  @override
  Future<void> _resetRuntimeServices() async {
    if (Get.isRegistered<ImService>()) {
      await Get.find<ImService>().resetForAccountSwitch();
    }
    if (Get.isRegistered<FriendService>()) {
      Get.find<FriendService>().resetForAccountSwitch();
    }
    if (Get.isRegistered<AgentService>()) {
      Get.find<AgentService>().resetForAccountSwitch();
    }
    if (Get.isRegistered<AgentCategoryService>()) {
      Get.find<AgentCategoryService>().resetForAccountSwitch();
    }
    if (Get.isRegistered<UserSettingsService>()) {
      Get.find<UserSettingsService>().resetForAccountSwitch();
    }
    if (Get.isRegistered<ConversationAuditPreferenceService>()) {
      Get.find<ConversationAuditPreferenceService>().resetForAccountSwitch();
    }
  }
}
