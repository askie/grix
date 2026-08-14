part of 'auth_service.dart';

/// 切换到已保存账号的结果。
enum AccountSwitchOutcome {
  /// 切换完成，可直接进入主界面。
  success,

  /// 目标账号凭证缺失或已失效，需要重新登录。
  needLogin,

  /// 切换失败（条目不存在 / 应用凭证失败 / 正在切换中）。
  failed,
}

/// 多账号管理：维护"已登录账号列表"，支持热切换 / 移除 / 为添加账号挂起当前会话。
mixin _AuthServiceAccounts on _AuthServiceContract {
  @override
  Future<List<SavedAccount>> listSavedAccounts() => _savedAccountStore.list();

  /// 把当前登录账号（资料 + 凭证 + 区域端点）快照进账号列表。
  /// 登录成功、token 刷新成功后调用，保证列表里的凭证始终是最新一代
  /// （refresh token 是轮转制，旧代凭证切回时会被服务端拒绝）。
  @override
  Future<void> _upsertCurrentAccountSnapshot() async {
    try {
      final user = _user.value;
      final refresh = _refreshToken.value?.trim() ?? '';
      if (user == null || user.id.trim().isEmpty || refresh.isEmpty) return;
      final region = (await AppStorageService.loadRegion()) ?? '';
      final wsEndpoint = (await AppStorageService.loadWsEndpoint()) ?? '';
      await _savedAccountStore.upsert(
        SavedAccount(
          userId: user.id,
          username: user.username,
          nickname: user.nickname,
          email: user.email,
          avatarUrl: user.avatarUrl ?? '',
          introduction: user.introduction,
          usernameModified: user.usernameModified,
          phoneE164: user.phoneE164,
          phoneCountry: user.phoneCountry,
          accessToken: _token.value ?? '',
          refreshToken: refresh,
          accessExpiresAtMs: _accessExpiresAtMs.value ?? 0,
          region: region,
          apiEndpoint: _dio.options.baseUrl,
          wsEndpoint: wsEndpoint,
          lastActiveAtMs: DateTime.now().millisecondsSinceEpoch,
        ),
      );
    } catch (e) {
      debugPrint('Saved account snapshot error: $e');
    }
  }

  @override
  Future<AccountSwitchOutcome> switchToSavedAccount(String targetUserId) async {
    final normalized = targetUserId.trim();
    if (normalized.isEmpty || _isSwitchingAccount) {
      return AccountSwitchOutcome.failed;
    }
    if (isLoggedIn && userId == normalized) {
      return AccountSwitchOutcome.success;
    }
    final target = await _savedAccountStore.find(normalized);
    if (target == null) return AccountSwitchOutcome.failed;
    if (target.needsRelogin || target.accessToken.trim().isEmpty) {
      return AccountSwitchOutcome.needLogin;
    }

    _isSwitchingAccount = true;
    try {
      if (isLoggedIn) {
        await _upsertCurrentAccountSnapshot();
      }
      _refreshTimer?.cancel();
      _refreshTimer = null;

      // 先恢复目标账号的区域与端点，确保后续所有请求打到正确区域。
      if (target.region.trim().isNotEmpty) {
        await AppStorageService.saveRegion(target.region.trim());
      }
      final apiEndpoint = target.apiEndpoint.trim().isNotEmpty
          ? target.apiEndpoint.trim()
          : _dio.options.baseUrl;
      updateBaseUrl(apiEndpoint);
      await AppStorageService.saveRegionEndpoints(
        apiEndpoint: apiEndpoint,
        wsEndpoint: target.wsEndpoint,
      );
      if (!kIsWeb &&
          target.wsEndpoint.trim().isNotEmpty &&
          Get.isRegistered<ImService>()) {
        Get.find<ImService>().updateWsEndpoint(target.wsEndpoint.trim());
      }

      // 复用登录统一入口：重置运行态服务、写入单槽凭证、切本地库、
      // 拉会话、刷新推送绑定。
      final remainingSec =
          (target.accessExpiresAtMs - DateTime.now().millisecondsSinceEpoch) ~/
          1000;
      final applied = await _applyAuthPayload({
        'access_token': target.accessToken,
        'refresh_token': target.refreshToken,
        'expires_in': remainingSec > 0 ? remainingSec : 1,
        'user': {
          'id': target.userId,
          'username': target.username,
          'email': target.email,
          'nickname': target.nickname,
          'introduction': target.introduction,
          'avatar_url': target.avatarUrl,
          'username_modified': target.usernameModified,
          'phone_e164': target.phoneE164,
          'phone_country': target.phoneCountry,
        },
      });
      if (!applied) return AccountSwitchOutcome.failed;

      // 立即强刷 token：验证凭证仍有效，并把可能已过期的 access token 换新。
      final status = await ensureTokenFreshStatus(force: true);
      if (status == TokenRefreshStatus.invalidSession) {
        await _savedAccountStore.clearCredentials(normalized);
        await logout(notifyServer: false);
        return AccountSwitchOutcome.needLogin;
      }
      // temporaryFailure（网络暂时不可用）与启动恢复会话的策略一致：保留本地会话。
      await _upsertCurrentAccountSnapshot();
      debugPrint('✅ Switched to saved account: $normalized status=$status');
      return AccountSwitchOutcome.success;
    } catch (e) {
      debugPrint('❌ Switch saved account error: $e');
      return AccountSwitchOutcome.failed;
    } finally {
      _isSwitchingAccount = false;
    }
  }

  /// 从账号列表移除一个账号。移除当前登录账号时执行正常退出登录
  /// （通知服务端吊销本设备会话）。
  @override
  Future<void> removeSavedAccount(String targetUserId) async {
    final normalized = targetUserId.trim();
    if (normalized.isEmpty) return;
    final isCurrent = isLoggedIn && userId == normalized;
    await _savedAccountStore.remove(normalized);
    if (isCurrent) {
      await logout();
    }
  }

  @visibleForTesting
  Future<void> clearSavedAccountCredentialsForTest(String userId) =>
      _savedAccountStore.clearCredentials(userId);

  /// "添加账号"前挂起当前会话：快照凭证进列表后仅做本地登出，
  /// 不通知服务端（否则凭证被吊销，切回时就得重新登录）。
  @override
  Future<void> suspendCurrentSessionLocally() async {
    if (!isLoggedIn) return;
    await _upsertCurrentAccountSnapshot();
    _refreshTimer?.cancel();
    _refreshTimer = null;
    await _clearLocalAuthData();
    await _resetRuntimeServices();
    await AppStorageService.clearRegionEndpoints();
    await LocalDb.setActiveUser(null);
    debugPrint('✅ Current session suspended locally for account add');
  }
}
