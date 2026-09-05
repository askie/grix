part of 'auth_service.dart';

mixin _AuthServiceLifecycle on _AuthServiceContract {
  @override
  Future<AuthService> init() async {
    _authSessionStore = await AuthSessionStore.create();
    _savedAccountStore = SavedAccountStore(_authSessionStore);
    // 多账号快照（saved_accounts_v1）同样含 token，一并纳入 legacy 明文清理/迁移。
    await _authSessionStore.clearLegacyGlobalAuthData(<String>[
      ..._authSessionKeys,
      SavedAccountStore.storageKey,
    ]);
    _attachLocaleInterceptor();
    // 尽早恢复持久化的 API 端点，避免 token 刷新打到错误区域。
    // 端点存储为空（老账号升级 / 后端未返回 region 导致端点未写入）时，
    // 按用户手选区域推导——saveRegion() 在登录页切换时立即写入，不依赖后端，
    // 确保全球区冷启动不会回落到 CN 默认端点触发 401。
    final savedApiEndpoint = await AppStorageService.loadApiEndpoint();
    if (savedApiEndpoint != null && savedApiEndpoint.isNotEmpty) {
      _dio.options.baseUrl = savedApiEndpoint;
    } else {
      final region = await resolveInitialRegion();
      _dio.options.baseUrl = resolveRegionApiBaseUrl(region);
    }
    await _loadStoredAuth();
    return this as AuthService;
  }

  @override
  Future<bool> ensureTokenFresh({
    bool force = false,
    Duration threshold = _authRefreshAhead,
  }) async {
    final status = await ensureTokenFreshStatus(
      force: force,
      threshold: threshold,
    );
    return status == TokenRefreshStatus.ready;
  }

  @override
  Future<TokenRefreshStatus> ensureTokenFreshStatus({
    bool force = false,
    Duration threshold = _authRefreshAhead,
  }) async {
    final access = token;
    if (access == null || access.isEmpty) {
      return TokenRefreshStatus.invalidSession;
    }

    if (!force && !_isAccessTokenExpiringSoon(within: threshold)) {
      return TokenRefreshStatus.ready;
    }
    if ((refreshToken ?? '').isEmpty) {
      return TokenRefreshStatus.invalidSession;
    }
    return _refreshTokensSingleFlight();
  }

  @override
  Future<void> updateAccessExpiryFromServer(int expiresInSec) async {
    if (expiresInSec <= 0) return;
    final expiresAt =
        DateTime.now().millisecondsSinceEpoch + expiresInSec * 1000;
    _accessExpiresAtMs.value = expiresAt;
    await _authSessionStore.setInt(_keyAccessExpiresAtMs, expiresAt);
    _scheduleRefreshTimer();
  }

  @override
  Future<void> logout({bool notifyServer = true}) async {
    try {
      _refreshTimer?.cancel();
      _refreshTimer = null;

      if (notifyServer) {
        await _notifyServerLogout();
      }

      // 凭证已（或即将）失效：清掉账号列表里的凭证，但保留条目，
      // 用户在"切换账号"里仍能看到并重新登录该账号。
      final currentUserId = _user.value?.id.trim() ?? '';
      if (currentUserId.isNotEmpty) {
        await _savedAccountStore.clearCredentials(currentUserId);
      }

      await _clearLocalAuthData();
      await _resetRuntimeServices();
      await AppStorageService.clearRegionEndpoints();
      await LocalDb.setActiveUser(null);

      debugPrint('✅ Logged out');
    } catch (e) {
      debugPrint('Logout error: $e');
    }
  }

  @override
  void handleUnauthorized({String? expectedAccessToken}) {
    if (!isLoggedIn) {
      return;
    }
    final normalizedExpectedToken = expectedAccessToken?.trim() ?? '';
    if (normalizedExpectedToken.isNotEmpty) {
      final currentAccessToken = token?.trim() ?? '';
      if (currentAccessToken.isNotEmpty &&
          currentAccessToken != normalizedExpectedToken) {
        debugPrint('⚠️ Ignore stale 401 response from an old token.');
        return;
      }
    }
    if (_isHandlingUnauthorized) return;
    _isHandlingUnauthorized = true;
    debugPrint(
      '🔒 401 Unauthorized detected. Clearing session: '
      'route=${Get.currentRoute} user_id=${userId ?? '-'}',
    );
    logout(notifyServer: false).whenComplete(() {
      _isHandlingUnauthorized = false;
      if (Get.currentRoute != AppRoutes.login) {
        debugPrint(
          '🧭 Unauthorized flow navigating to login from route=${Get.currentRoute}',
        );
        RootRouteNavigator.toLogin();
      }
    });
  }

  Future<void> _loadStoredAuth() async {
    try {
      final storedAccess = await _authSessionStore.getString(_keyAccessToken);
      final storedRefresh = await _authSessionStore.getString(_keyRefreshToken);
      final userId = await _authSessionStore.getString(_keyUserId);
      final username = await _authSessionStore.getString(_keyUsername);
      final email = await _authSessionStore.getString(_keyEmail);
      final nickname = await _authSessionStore.getString(_keyNickname);
      final introduction = await _authSessionStore.getString(_keyIntroduction);
      final avatarUrl = await _authSessionStore.getString(_keyAvatarUrl);
      final usernameModified =
          (await _authSessionStore.getBool(_keyUsernameModified)) ?? false;
      final phoneE164 = await _authSessionStore.getString(_keyPhoneE164);
      final phoneCountry = await _authSessionStore.getString(_keyPhoneCountry);
      final expiresAt = await _authSessionStore.getInt(_keyAccessExpiresAtMs);

      debugPrint(
        '🔐 Load stored auth summary: '
        'has_access=${storedAccess?.isNotEmpty == true} '
        'has_refresh=${storedRefresh?.isNotEmpty == true} '
        'stored_user_id=${userId?.isNotEmpty == true ? userId : '-'} '
        'route=${Get.currentRoute}',
      );

      if (storedAccess != null &&
          storedAccess.isNotEmpty &&
          userId != null &&
          userId.isNotEmpty) {
        _token.value = storedAccess;
        _refreshToken.value = storedRefresh;
        _accessExpiresAtMs.value = expiresAt;
        _user.value = User(
          id: userId,
          username: username ?? '',
          email: email ?? '',
          nickname: nickname ?? '',
          introduction: introduction ?? '',
          avatarUrl: avatarUrl,
          usernameModified: usernameModified,
          phoneE164: phoneE164 ?? '',
          phoneCountry: phoneCountry ?? '',
        );
        _isLoggedIn.value = true;
        await LocalDb.setActiveUser(userId);
        debugPrint('✅ Loaded stored auth: userId=$userId');

        if (_isAccessTokenExpiredOrUnknown()) {
          final refreshStatus = await ensureTokenFreshStatus(force: true);
          switch (refreshStatus) {
            case TokenRefreshStatus.ready:
              break;
            case TokenRefreshStatus.invalidSession:
              debugPrint('⚠️ Startup refresh found invalid session, clearing');
              await logout(notifyServer: false);
              return;
            case TokenRefreshStatus.temporaryFailure:
              debugPrint(
                '⚠️ Startup refresh failed temporarily, keep local session',
              );
              break;
          }
        }
        if (isLoggedIn) {
          _scheduleRefreshTimer();
        }
      } else {
        await LocalDb.setActiveUser(null);
      }
    } catch (e) {
      debugPrint('Load stored auth error: $e');
    }
  }

  Future<TokenRefreshStatus> _refreshTokensSingleFlight() {
    final inflight = _refreshFuture;
    if (inflight != null) return inflight;

    final future = _refreshTokensInternal();
    _refreshFuture = future;
    future.whenComplete(() {
      if (identical(_refreshFuture, future)) {
        _refreshFuture = null;
      }
    });
    return future;
  }

  Future<TokenRefreshStatus> _refreshTokensInternal() async {
    final currentRefresh = refreshToken;
    if (currentRefresh == null || currentRefresh.isEmpty) {
      return TokenRefreshStatus.invalidSession;
    }

    try {
      final response = await _dio.post(
        '/auth/refresh',
        data: {'refresh_token': currentRefresh},
      );
      final body = _asBody(response.data);
      final code = _toInt(body?['code'], fallback: 50001);
      if (response.statusCode != 200 || body == null || code != 0) {
        // 只记 code/status，不输出整个响应体（可能含敏感字段）。
        debugPrint('❌ Refresh failed: http=${response.statusCode} code=$code');
        if (_isInvalidRefreshFailure(
          httpStatus: response.statusCode ?? 0,
          code: code,
        )) {
          return TokenRefreshStatus.invalidSession;
        }
        return TokenRefreshStatus.temporaryFailure;
      }

      final data = _asBody(body['data']);
      if (data == null) {
        debugPrint('❌ Refresh failed: malformed payload');
        return TokenRefreshStatus.temporaryFailure;
      }
      final nextAccessToken = data['access_token']?.toString() ?? '';
      final nextRefreshToken = data['refresh_token']?.toString() ?? '';
      final expiresIn = _toInt(data['expires_in'], fallback: 0);
      if (nextAccessToken.isEmpty || nextRefreshToken.isEmpty) {
        debugPrint('❌ Refresh failed: empty token payload');
        return TokenRefreshStatus.temporaryFailure;
      }

      final userData = data['user'];
      if (userData is Map && userData['id'] != null) {
        final mapped = Map<String, dynamic>.from(userData);
        final uid = mapped['id']?.toString().trim() ?? '';
        if (uid.isNotEmpty) {
          _user.value = User.fromJson(mapped);
          await _persistUser(_user.value!);
        }
      }

      await _persistTokens(
        accessToken: nextAccessToken,
        refreshToken: nextRefreshToken,
        expiresInSec: expiresIn,
        // 手机自己刷新 access token 不碰手表：手表有独立的 refresh 家族，
        // 在这里重新签发会把它手上可能还很健康的凭证作废掉。
        issueWatchCredentials: false,
      );
      debugPrint('✅ Token refreshed');
      // refresh token 轮转后旧代作废，必须把最新凭证回写账号列表，
      // 否则切走再切回会用旧凭证被拒。
      await _upsertCurrentAccountSnapshot();

      if (Get.isRegistered<ImService>()) {
        Get.find<ImService>().reAuthWithLatestToken();
      }
      return TokenRefreshStatus.ready;
    } on DioException catch (e) {
      debugPrint('❌ Refresh error: ${e.message}');
      if (_isInvalidRefreshFailure(
        httpStatus: e.response?.statusCode ?? 0,
        code: _toInt(_asBody(e.response?.data)?['code'], fallback: 0),
      )) {
        return TokenRefreshStatus.invalidSession;
      }
      return TokenRefreshStatus.temporaryFailure;
    } catch (e) {
      debugPrint('❌ Refresh error: $e');
      return TokenRefreshStatus.temporaryFailure;
    }
  }

  @override
  void _scheduleRefreshTimer() {
    _refreshTimer?.cancel();
    _refreshTimer = null;

    if ((refreshToken ?? '').isEmpty) return;

    final expiresAtMs = _accessExpiresAtMs.value;
    if (expiresAtMs == null || expiresAtMs <= 0) return;

    final now = DateTime.now().millisecondsSinceEpoch;
    final triggerAtMs = expiresAtMs - _refreshAhead.inMilliseconds;
    final delayMs = triggerAtMs - now;
    final delay = Duration(milliseconds: delayMs > 0 ? delayMs : 1000);

    _refreshTimer = Timer(delay, () async {
      await _runScheduledRefreshAttempt();
    });
  }

  Future<void> _runScheduledRefreshAttempt() async {
    final refreshStatus = await ensureTokenFreshStatus(force: true);
    switch (refreshStatus) {
      case TokenRefreshStatus.ready:
        return;
      case TokenRefreshStatus.invalidSession:
        if (isLoggedIn) {
          handleUnauthorized();
        }
        return;
      case TokenRefreshStatus.temporaryFailure:
        if (isLoggedIn) {
          _refreshTimer = Timer(_refreshRetryDelay, () async {
            await _runScheduledRefreshAttempt();
          });
        }
        return;
    }
  }

  @override
  bool hasUsableAccessToken({Duration minRemaining = Duration.zero}) {
    final access = token?.trim() ?? '';
    if (access.isEmpty) {
      return false;
    }
    final expiresAtMs = _accessExpiresAtMs.value;
    if (expiresAtMs == null || expiresAtMs <= 0) {
      return false;
    }
    final now = DateTime.now().millisecondsSinceEpoch;
    return (expiresAtMs - now) > minRemaining.inMilliseconds;
  }

  bool _isAccessTokenExpiringSoon({Duration within = _authRefreshAhead}) {
    final expiresAtMs = _accessExpiresAtMs.value;
    if (expiresAtMs == null || expiresAtMs <= 0) return true;
    final now = DateTime.now().millisecondsSinceEpoch;
    return (expiresAtMs - now) <= within.inMilliseconds;
  }

  bool _isAccessTokenExpiredOrUnknown() {
    final expiresAtMs = _accessExpiresAtMs.value;
    if (expiresAtMs == null || expiresAtMs <= 0) return true;
    return DateTime.now().millisecondsSinceEpoch >= expiresAtMs;
  }

  bool _isInvalidRefreshFailure({required int httpStatus, required int code}) {
    return httpStatus == 401 || code == 10002;
  }

  @visibleForTesting
  @override
  Future<void> runScheduledRefreshAttemptForTest() {
    return _runScheduledRefreshAttempt();
  }
}
