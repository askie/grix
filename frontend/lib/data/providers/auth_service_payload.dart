part of 'auth_service.dart';

mixin _AuthServicePayload on _AuthServiceContract {
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
    await _syncWatchCredentials(accessToken, expiresAtMs);
  }

  /// 每次登录/刷新后把新 access token 推给手表。手表不会自己刷新 token，
  /// 这里是它唯一的凭证来源；失败不影响手机端登录流程。
  Future<void> _syncWatchCredentials(String accessToken, int expiresAtMs) async {
    final wsUrl = Get.isRegistered<ImService>()
        ? (Get.find<ImService>().currentWsUrl ?? '')
        : '';
    await WatchCredentialSync.push(
      accessToken: accessToken,
      apiBaseUrl: _dio.options.baseUrl,
      wsBaseUrl: watchWsHttpBaseUrl(
        wsUrl.isNotEmpty ? wsUrl : resolveDefaultWsUrl(),
      ),
      accessExpiresAtMs: expiresAtMs,
    );
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
