part of 'auth_service.dart';

mixin _AuthServicePayload on _AuthServiceContract {
  /// 上一次为手表签发凭证的时刻，用于限频（见 [ensureWatchCredentials]）。
  int _lastWatchIssueAtMs = 0;

  @override
  Future<ServiceResult<void>> _handleAuthGrantResponse(
    Response<dynamic> response, {
    required String fallbackMessage,
  }) async {
    final body = _asBody(response.data);
    if (response.statusCode != 200 || body == null) {
      debugPrint(
        '❌ Auth grant invalid HTTP response: '
        'status=${response.statusCode ?? 0} has_body=${body != null} '
        'route=${Get.currentRoute}',
      );
      return ServiceResult<void>.failure(
        message: fallbackMessage,
        httpStatus: response.statusCode ?? 0,
      );
    }

    final code = _toInt(body['code'], fallback: 50001);
    if (code != 0) {
      debugPrint(
        '❌ Auth grant business failure: '
        'status=${response.statusCode ?? 0} code=$code '
        'msg=${_extractMessage(body, fallback: fallbackMessage)} '
        'route=${Get.currentRoute}',
      );
      return ServiceResult<void>.failure(
        message: _extractMessage(body, fallback: fallbackMessage),
        code: code,
        httpStatus: response.statusCode ?? 0,
      );
    }

    final data = _asBody(body['data']);
    if (data == null) {
      debugPrint(
        '❌ Auth grant missing data payload: '
        'status=${response.statusCode ?? 0} code=$code route=${Get.currentRoute}',
      );
      return ServiceResult<void>.failure(
        message: fallbackMessage,
        httpStatus: response.statusCode ?? 0,
      );
    }

    final userData = Map<String, dynamic>.from(data['user'] ?? {});
    final nextUserId = userData['id']?.toString().trim() ?? '';
    debugPrint(
      '🔐 Auth grant payload accepted: '
      'status=${response.statusCode ?? 0} code=$code '
      'next_user_id=${nextUserId.isEmpty ? '-' : nextUserId} '
      'route=${Get.currentRoute}',
    );

    final authApplied = await _applyAuthPayload(data);
    if (!authApplied) {
      debugPrint(
        '❌ Auth grant apply returned false: '
        'next_user_id=${nextUserId.isEmpty ? '-' : nextUserId} '
        'route=${Get.currentRoute}',
      );
      return ServiceResult<void>.failure(
        message: fallbackMessage,
        httpStatus: response.statusCode ?? 0,
      );
    }

    debugPrint(
      '✅ Auth grant applied: '
      'next_user_id=${nextUserId.isEmpty ? '-' : nextUserId} '
      'logged_in=${_isLoggedIn.value} route=${Get.currentRoute}',
    );
    return ServiceResult<void>.success(httpStatus: response.statusCode ?? 200);
  }

  @override
  Future<Map<String, String>> _buildAuthGrantDevicePayload() async {
    return <String, String>{
      'device_id': await DeviceIdentity.resolveDeviceId(),
      'platform': DeviceIdentity.platformLabel(),
    };
  }

  @override
  Future<bool> _applyAuthPayload(Map<String, dynamic> data) async {
    final applyCompleter = Completer<void>();
    _authPayloadApplyCompleter = applyCompleter;
    try {
      final accessToken = data['access_token']?.toString() ?? '';
      final nextRefreshToken = data['refresh_token']?.toString() ?? '';
      final expiresIn = _toInt(data['expires_in'], fallback: 0);
      final userData = Map<String, dynamic>.from(data['user'] ?? {});
      final nextUserId = userData['id']?.toString().trim() ?? '';
      final currentUserId = _user.value?.id.trim() ?? '';
      final hadActiveSession =
          _isLoggedIn.value && (_token.value?.trim().isNotEmpty ?? false);
      final isSameUserReauth =
          hadActiveSession &&
          currentUserId.isNotEmpty &&
          currentUserId == nextUserId;

      debugPrint(
        '🔐 Apply auth payload start: '
        'next_user_id=${nextUserId.isEmpty ? '-' : nextUserId} '
        'current_user_id=${currentUserId.isEmpty ? '-' : currentUserId} '
        'same_user_reauth=$isSameUserReauth had_active_session=$hadActiveSession '
        'route=${Get.currentRoute}',
      );

      if (accessToken.isEmpty ||
          nextRefreshToken.isEmpty ||
          nextUserId.isEmpty) {
        debugPrint(
          '❌ Apply auth payload rejected: '
          'missing_access=${accessToken.isEmpty} '
          'missing_refresh=${nextRefreshToken.isEmpty} '
          'missing_user_id=${nextUserId.isEmpty}',
        );
        return false;
      }

      if (!isSameUserReauth) {
        debugPrint('🔐 Apply auth payload resetting runtime services');
        await _resetRuntimeServices();
      }
      await _persistTokens(
        accessToken: accessToken,
        refreshToken: nextRefreshToken,
        expiresInSec: expiresIn,
        // 只有登录（含注册 / 扫码 / 切号）才给手表重新签发一份独立凭证；
        // 手机自己刷新 access token 时不能动手表的家族。
        issueWatchCredentials: true,
      );
      debugPrint(
        '🔐 Apply auth payload persisted tokens: '
        'next_user_id=$nextUserId expires_in=$expiresIn',
      );

      // 持久化区域端点（仅在响应中包含时更新，避免 token 刷新时覆盖）。
      // 只更新端点缓存，不碰区域标识——区域由用户在登录页手动选择并记住，
      // 后端返回的 region 不得覆盖，否则退出后回登录页会丢失用户的手选区域。
      final wsEndpoint = data['ws_endpoint']?.toString().trim() ?? '';
      final region = data['region']?.toString().trim() ?? '';
      if (region.isNotEmpty) {
        final currentApiUrl = _dio.options.baseUrl;
        final effectiveWsEndpoint = wsEndpoint.isNotEmpty
            ? wsEndpoint
            : (await AppStorageService.loadWsEndpoint() ?? '');
        // ws_endpoint 只在非空时写入，避免覆盖已有的正确值
        await AppStorageService.saveRegionEndpoints(
          apiEndpoint: currentApiUrl,
          wsEndpoint: effectiveWsEndpoint,
        );
        // ImService 同步持有最新端点，让 ensureConnected() 无需再读存储
        if (effectiveWsEndpoint.isNotEmpty && Get.isRegistered<ImService>()) {
          Get.find<ImService>().updateWsEndpoint(effectiveWsEndpoint);
        }
      }

      await LocalDb.setActiveUser(nextUserId);

      final nextUser = User.fromJson(userData);
      _user.value = nextUser;
      await _persistUser(nextUser);
      _isLoggedIn.value = true;
      debugPrint(
        '✅ Apply auth payload marked login: '
        'next_user_id=$nextUserId route=${Get.currentRoute}',
      );
      await _upsertCurrentAccountSnapshot();

      if (Get.isRegistered<ImService>()) {
        try {
          await Get.find<ImService>().loadSessionsForCurrentUser();
        } catch (e) {
          debugPrint('Load sessions after auth failed: $e');
        }
      }

      if (Get.isRegistered<PushRegistrationService>()) {
        unawaited(
          Get.find<PushRegistrationService>().refreshBindingIfNeeded(
            force: true,
          ),
        );
      }

      return true;
    } catch (e, st) {
      debugPrint('❌ Apply auth payload error: $e\n$st');
      rethrow;
    } finally {
      if (!applyCompleter.isCompleted) {
        applyCompleter.complete();
      }
      if (identical(_authPayloadApplyCompleter, applyCompleter)) {
        _authPayloadApplyCompleter = null;
      }
    }
  }

  @visibleForTesting
  @override
  Future<bool> applyAuthPayloadForTest(Map<String, dynamic> data) {
    return _applyAuthPayload(data);
  }

  @override
  ServiceResult<void> _plainApiResult(
    Response<dynamic> response, {
    required String fallbackMessage,
  }) {
    final body = _asBody(response.data);
    if (response.statusCode != 200 || body == null) {
      return ServiceResult<void>.failure(
        message: fallbackMessage,
        httpStatus: response.statusCode ?? 0,
      );
    }

    final code = _toInt(body['code'], fallback: 50001);
    if (code != 0) {
      return ServiceResult<void>.failure(
        message: _extractMessage(body, fallback: fallbackMessage),
        code: code,
        httpStatus: response.statusCode ?? 0,
      );
    }

    return ServiceResult<void>.success(httpStatus: response.statusCode ?? 200);
  }

  @override
  ServiceResult<T> _dioFailure<T>(
    DioException e, {
    required String fallbackMessage,
  }) {
    final body = _asBody(e.response?.data);
    if (body != null) {
      return ServiceResult<T>.failure(
        message: _extractMessage(body, fallback: fallbackMessage),
        code: _toInt(body['code'], fallback: 50001),
        httpStatus: e.response?.statusCode ?? 0,
      );
    }

    final message = _friendlyTransportErrorMessage(
      e,
      fallbackMessage: fallbackMessage,
    );
    return ServiceResult<T>.failure(
      message: message,
      code: 50001,
      httpStatus: e.response?.statusCode ?? 0,
    );
  }

  String _friendlyTransportErrorMessage(
    DioException e, {
    required String fallbackMessage,
  }) {
    switch (e.type) {
      case DioExceptionType.connectionTimeout:
      case DioExceptionType.sendTimeout:
      case DioExceptionType.receiveTimeout:
      case DioExceptionType.transformTimeout:
        return 'auth_error_timeout'.tr;
      case DioExceptionType.connectionError:
      case DioExceptionType.badCertificate:
        return 'auth_error_network'.tr;
      case DioExceptionType.cancel:
        return 'auth_error_canceled'.tr;
      case DioExceptionType.badResponse:
        return _friendlyHttpStatusMessage(
          statusCode: e.response?.statusCode ?? 0,
          fallbackMessage: fallbackMessage,
        );
      case DioExceptionType.unknown:
        final hasResponse = e.response != null;
        if (!hasResponse) {
          return 'auth_error_network'.tr;
        }
        return fallbackMessage;
    }
  }

  String _friendlyHttpStatusMessage({
    required int statusCode,
    required String fallbackMessage,
  }) {
    switch (statusCode) {
      case 400:
        return 'auth_error_bad_request'.tr;
      case 401:
        return 'auth_error_unauthorized'.tr;
      case 403:
        return 'auth_error_forbidden'.tr;
      case 404:
        return 'auth_error_api_not_found'.tr;
      case 429:
        return 'auth_error_rate_limit'.tr;
      default:
        if (statusCode >= 500) {
          return 'auth_error_server'.tr;
        }
        return fallbackMessage;
    }
  }

  @override
  Map<String, dynamic>? _asBody(dynamic source) {
    if (source is Map<String, dynamic>) {
      return source;
    }
    if (source is Map) {
      return Map<String, dynamic>.from(source);
    }
    return null;
  }

  @override
  String _extractMessage(
    Map<String, dynamic> body, {
    required String fallback,
  }) {
    final msg = body['msg']?.toString().trim() ?? '';
    if (msg.isEmpty) {
      return fallback;
    }
    return msg;
  }

  @override
  Future<void> _persistTokens({
    required String accessToken,
    required String refreshToken,
    required int expiresInSec,
    required bool issueWatchCredentials,
  }) async {
    final safeExpiresIn = expiresInSec > 0 ? expiresInSec : 7200;
    final expiresAtMs =
        DateTime.now().millisecondsSinceEpoch + safeExpiresIn * 1000;

    await _authSessionStore.setString(_keyAccessToken, accessToken);
    await _authSessionStore.setString(_keyRefreshToken, refreshToken);
    await _authSessionStore.setInt(_keyAccessExpiresAtMs, expiresAtMs);

    _token.value = accessToken;
    _refreshToken.value = refreshToken;
    _accessExpiresAtMs.value = expiresAtMs;
    _scheduleRefreshTimer();
    if (issueWatchCredentials) {
      // 必须不阻塞：这次请求要过 Dio 的鉴权拦截器，而拦截器第一件事就是等
      // `_applyAuthPayload` 跑完——而它正 await 着这里，await 下去就是自锁。
      unawaited(ensureWatchCredentials(afterLogin: true));
    }
  }

  /// 手表凭证补推的唯一入口。
  ///
  /// 除了登录成功，手机冷启动 / 回到前台、用户刚在手表上装好 App、手表自己发现
  /// 没有可用凭证，都会经原生侧唤起这里：只在登录那一刻签发是不够的——已经处于
  /// 登录态的用户不会再登录第二次，手表就永远拿不到凭证。
  ///
  /// [afterLogin] 表示刚换上一份新的手机凭证，必须无条件重新签发；其余入口受
  /// [_watchIssueMinInterval] 限频——每次 issue 都会撤销上一条手表家族，连发会把
  /// 刚推出去的那份当场作废。
  /// [watchRequested] 表示是手表主动索要：手机已退出登录时回一份空凭证，让手表
  /// 把陈旧的 token 丢掉。
  @override
  Future<void> ensureWatchCredentials({
    bool afterLogin = false,
    bool watchRequested = false,
  }) async {
    if (!WatchCredentialSync.isSupported) return;
    final accessToken = _token.value?.trim() ?? '';
    // 登录路径是在 _applyAuthPayload 中途调进来的：那时 token 已经写好，但
    // `_isLoggedIn` 还要等用户资料落地才置位，所以那条路只看 token。其余入口
    // 必须确实处于登录态，否则拿不到能用的 access token。
    final hasSession = afterLogin
        ? accessToken.isNotEmpty
        : (_isLoggedIn.value && accessToken.isNotEmpty);
    if (!hasSession) {
      if (watchRequested) {
        await WatchCredentialSync.clear();
      }
      return;
    }
    final nowMs = DateTime.now().millisecondsSinceEpoch;
    if (!afterLogin &&
        nowMs - _lastWatchIssueAtMs < _watchIssueMinInterval.inMilliseconds) {
      debugPrint('⌚️ watch issue skipped: throttled');
      return;
    }
    _lastWatchIssueAtMs = nowMs;
    await _issueAndSyncWatchCredentials(accessToken);
  }

  /// 为手表单独签发一对令牌，并推给它。
  ///
  /// 手表拿到的是**它自己的 refresh 家族**，和手机互不影响：refresh token 每次
  /// 使用都会轮转，共用一份会互相踢下线。因此手机后续自己刷新 access token 时
  /// 不再推送——那会把手表正在用的、可能还很健康的凭证作废掉。
  ///
  /// 手机自己的 refresh token 永远不外传。整个过程失败不影响手机端登录流程。
  Future<void> _issueAndSyncWatchCredentials(String grantedAccessToken) async {
    if (!WatchCredentialSync.isSupported) return;
    try {
      // AuthService 自己的 _dio 只挂了 locale 拦截器，鉴权拦截器是给别的 service
      // 用的（见 attachAuthInterceptor）。这个文件里需要鉴权的请求一律显式带头，
      // 漏掉就是必然 401。用传进来的这枚 token：调用方刚拿到它，不必也不该在这里
      // 再走一次 ensureTokenFresh() 去和登录流程互相等。
      final response = await _dio.post(
        '/auth/watch/issue',
        options: Options(
          headers: {'Authorization': 'Bearer $grantedAccessToken'},
          // 401/5xx 不抛异常，好把状态码和 code/msg 一起打进日志——静默失败正是
          // 这个缺口拖到真机才被发现的原因。
          validateStatus: (_) => true,
        ),
      );
      final body = _asBody(response.data);
      final data = _asBody(body?['data']);
      if (response.statusCode != 200 ||
          _toInt(body?['code'], fallback: 50001) != 0 ||
          data == null) {
        debugPrint(
          '⌚️ watch issue failed: http=${response.statusCode ?? 0} '
          'code=${_toInt(body?['code'], fallback: 50001)} '
          'msg=${_extractMessage(body ?? const <String, dynamic>{}, fallback: '-')}',
        );
        return;
      }
      final watchAccessToken = data['access_token']?.toString() ?? '';
      final watchRefreshToken = data['refresh_token']?.toString() ?? '';
      if (watchAccessToken.isEmpty || watchRefreshToken.isEmpty) {
        debugPrint('⌚️ watch issue returned an incomplete token pair');
        return;
      }
      // 这次请求是脱离登录流程跑的：期间用户可能已经退出或换了账号，
      // 那份手表凭证不该再推出去（也不该盖掉 logout 的清空）。
      if (_token.value != grantedAccessToken) {
        debugPrint('⌚️ watch issue dropped: session changed while issuing');
        return;
      }
      final expiresInSec = _toInt(data['expires_in'], fallback: 7200);
      final wsUrl = Get.isRegistered<ImService>()
          ? (Get.find<ImService>().currentWsUrl ?? '')
          : '';
      await WatchCredentialSync.push(
        accessToken: watchAccessToken,
        refreshToken: watchRefreshToken,
        apiBaseUrl: _dio.options.baseUrl,
        wsBaseUrl: watchWsHttpBaseUrl(
          wsUrl.isNotEmpty ? wsUrl : resolveDefaultWsUrl(),
        ),
        accessExpiresAtMs:
            DateTime.now().millisecondsSinceEpoch +
            (expiresInSec > 0 ? expiresInSec : 7200) * 1000,
      );
    } catch (e) {
      debugPrint('⌚️ watch issue error: $e');
    }
  }

  @override
  Future<void> _waitForPendingAuthPayloadApplication() async {
    final pending = _authPayloadApplyCompleter;
    if (pending == null || pending.isCompleted) {
      return;
    }
    await pending.future;
  }

  @override
  Future<void> _persistUser(User user) async {
    await _authSessionStore.setString(_keyUserId, user.id);
    await _authSessionStore.setString(_keyUsername, user.username);
    await _authSessionStore.setString(_keyEmail, user.email);
    await _authSessionStore.setString(_keyNickname, user.nickname);
    await _authSessionStore.setString(_keyIntroduction, user.introduction);
    await _authSessionStore.setString(_keyAvatarUrl, user.avatarUrl ?? '');
    await _authSessionStore.setBool(
      _keyUsernameModified,
      user.usernameModified,
    );
    await _authSessionStore.setString(_keyPhoneE164, user.phoneE164);
    await _authSessionStore.setString(_keyPhoneCountry, user.phoneCountry);
  }

  @override
  int _toInt(dynamic value, {int fallback = 0}) {
    if (value is int) return value;
    if (value is num) return value.toInt();
    return int.tryParse(value?.toString() ?? '') ?? fallback;
  }
}
