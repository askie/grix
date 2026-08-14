part of 'auth_service.dart';

mixin _AuthServiceApi on _AuthServiceContract {
  @override
  void attachAuthInterceptor(Dio dio) {
    dio.interceptors.add(
      InterceptorsWrapper(
        onRequest: (options, handler) async {
          await _waitForPendingAuthPayloadApplication();
          _applyLocaleHeader(options);
          final skipRefresh = options.extra['skipAuthRefresh'] == true;
          if (!skipRefresh) {
            await ensureTokenFresh();
          }
          final access = token;
          if (access != null && access.isNotEmpty) {
            options.headers['Authorization'] = 'Bearer $access';
          }
          handler.next(options);
        },
        onError: (e, handler) async {
          final statusCode = e.response?.statusCode ?? 0;
          final alreadyRetried = e.requestOptions.extra['authRetry'] == true;
          final skipRefresh = e.requestOptions.extra['skipAuthRefresh'] == true;
          final requestAccessToken = _extractBearerAccessToken(
            e.requestOptions,
          );
          final shouldTryRefresh =
              statusCode == 401 && !skipRefresh && !alreadyRetried;

          if (shouldTryRefresh) {
            final refreshStatus = await ensureTokenFreshStatus(force: true);
            final access = token;
            if (refreshStatus == TokenRefreshStatus.ready &&
                access != null &&
                access.isNotEmpty) {
              try {
                final retryResp = await _retryRequestWithToken(
                  dio,
                  e.requestOptions,
                  access,
                );
                handler.resolve(retryResp);
                return;
              } on DioException catch (retryErr) {
                handler.next(retryErr);
                return;
              } catch (retryErr) {
                handler.next(
                  DioException(
                    requestOptions: e.requestOptions,
                    error: retryErr,
                  ),
                );
                return;
              }
            }
            if (refreshStatus == TokenRefreshStatus.invalidSession) {
              if (requestAccessToken.isNotEmpty) {
                handleUnauthorized(expectedAccessToken: requestAccessToken);
              }
            }
            handler.next(e);
            return;
          }

          if (statusCode == 401) {
            if (requestAccessToken.isNotEmpty) {
              handleUnauthorized(expectedAccessToken: requestAccessToken);
            }
          }
          handler.next(e);
        },
      ),
    );
  }

  @override
  void _attachLocaleInterceptor() {
    if (_localeInterceptorAttached) return;
    _localeInterceptorAttached = true;
    _dio.interceptors.add(
      InterceptorsWrapper(
        onRequest: (options, handler) {
          _applyLocaleHeader(options);
          handler.next(options);
        },
      ),
    );
  }

  void _applyLocaleHeader(RequestOptions options) {
    options.headers['X-App-Locale'] = _currentLocaleTag();
  }

  String _extractBearerAccessToken(RequestOptions requestOptions) {
    final authHeader =
        requestOptions.headers['Authorization'] ??
        requestOptions.headers['authorization'];
    final raw = authHeader?.toString().trim() ?? '';
    if (raw.isEmpty) {
      return '';
    }
    const bearerPrefix = 'bearer ';
    final lowerRaw = raw.toLowerCase();
    if (!lowerRaw.startsWith(bearerPrefix)) {
      return '';
    }
    return raw.substring(bearerPrefix.length).trim();
  }

  String _currentLocaleTag() {
    final locale = Get.locale;
    if (locale == null) return 'zh-CN';
    final languageCode = locale.languageCode.trim();
    if (languageCode.isEmpty) return 'zh-CN';
    final countryCode = (locale.countryCode ?? '').trim();
    if (countryCode.isEmpty) return languageCode;
    return '$languageCode-$countryCode';
  }

  @override
  Future<ServiceResult<CaptchaData>> fetchCaptcha() async {
    try {
      final response = await _authGet('/auth/captcha');
      final body = _asBody(response.data);
      if (response.statusCode != 200 || body == null) {
        return ServiceResult<CaptchaData>.failure(
          message: 'captcha_fetch_failed'.tr,
          httpStatus: response.statusCode ?? 0,
        );
      }
      final code = _toInt(body['code'], fallback: 50001);
      if (code != 0) {
        return ServiceResult<CaptchaData>.failure(
          message: _extractMessage(body, fallback: 'captcha_fetch_failed'.tr),
          code: code,
          httpStatus: response.statusCode ?? 0,
        );
      }

      final data = _asBody(body['data']);
      if (data == null) {
        return ServiceResult<CaptchaData>.failure(
          message: 'captcha_fetch_failed'.tr,
          httpStatus: response.statusCode ?? 0,
        );
      }

      final captchaId = data['captcha_id']?.toString().trim() ?? '';
      final b64s = data['b64s']?.toString() ?? '';
      if (captchaId.isEmpty || b64s.isEmpty) {
        return ServiceResult<CaptchaData>.failure(
          message: 'captcha_fetch_failed'.tr,
          httpStatus: response.statusCode ?? 0,
        );
      }

      return ServiceResult<CaptchaData>.success(
        data: CaptchaData(captchaId: captchaId, b64s: b64s),
        httpStatus: response.statusCode ?? 200,
      );
    } on DioException catch (e) {
      return _dioFailure<CaptchaData>(
        e,
        fallbackMessage: 'captcha_fetch_failed'.tr,
      );
    } catch (_) {
      return ServiceResult<CaptchaData>.failure(
        message: 'captcha_fetch_failed'.tr,
      );
    }
  }

  @override
  Future<ServiceResult<void>> sendEmailCode({
    required String email,
    required String scene,
    String? captchaId,
    String? captchaValue,
  }) async {
    try {
      final payload = <String, dynamic>{'email': email, 'scene': scene};
      final normalizedCaptchaID = captchaId?.trim() ?? '';
      if (normalizedCaptchaID.isNotEmpty) {
        payload['captcha_id'] = normalizedCaptchaID;
      }
      final normalizedCaptchaValue = captchaValue?.trim() ?? '';
      if (normalizedCaptchaValue.isNotEmpty) {
        payload['captcha_value'] = normalizedCaptchaValue;
      }
      final response = await _authPost('/auth/send-code', data: payload);
      return _plainApiResult(
        response,
        fallbackMessage: 'auth_send_code_failed'.tr,
      );
    } on DioException catch (e) {
      return _dioFailure<void>(e, fallbackMessage: 'auth_send_code_failed'.tr);
    } catch (_) {
      return ServiceResult<void>.failure(message: 'auth_send_code_failed'.tr);
    }
  }

  @override
  Future<ServiceResult<void>> register({
    required String email,
    required String password,
    required String emailCode,
    String region = '',
  }) async {
    try {
      final devicePayload = await _buildAuthGrantDevicePayload();
      final payload = <String, dynamic>{
        'email': email.trim(),
        'password': password.trim(),
        'email_code': emailCode,
        ...devicePayload,
      };
      if (region.isNotEmpty) {
        payload['region'] = region;
      }
      final response = await _authPost('/auth/register', data: payload);
      return _handleAuthGrantResponse(
        response,
        fallbackMessage: 'register_error_failed'.tr,
      );
    } on DioException catch (e) {
      return _dioFailure<void>(e, fallbackMessage: 'register_error_failed'.tr);
    } catch (_) {
      return ServiceResult<void>.failure(message: 'register_error_failed'.tr);
    }
  }

  @override
  Future<ServiceResult<void>> login(String account, String password) async {
    final accountLabel = _debugAccountLabel(account);
    if (_canReuseActiveSessionForAccount(account)) {
      debugPrint(
        '🔐 AuthService.login reused active session: '
        'account=$accountLabel current_user=${userId ?? '-'} '
        'route=${_debugRouteLabel()}',
      );
      return ServiceResult<void>.success();
    }
    try {
      debugPrint(
        '🔐 AuthService.login request start: '
        'account=$accountLabel route=${_debugRouteLabel()} '
        'logged_in=$isLoggedIn has_usable_token=${hasUsableAccessToken()} '
        'current_user=${userId ?? '-'}',
      );
      final devicePayload = await _buildAuthGrantDevicePayload();
      final response = await _authPost(
        '/auth/login',
        data: {'account': account, 'password': password, ...devicePayload},
      );
      debugPrint(
        '🔐 AuthService.login response received: '
        'account=$accountLabel status=${response.statusCode}',
      );
      return _handleAuthGrantResponse(
        response,
        fallbackMessage: 'login_error_failed'.tr,
      );
    } on DioException catch (e, st) {
      debugPrint(
        '❌ AuthService.login dio error: '
        'account=$accountLabel status=${e.response?.statusCode ?? 0} '
        'type=${e.type} msg=${e.message}\n$st',
      );
      return _dioFailure<void>(e, fallbackMessage: 'login_error_failed'.tr);
    } catch (e, st) {
      debugPrint(
        '❌ AuthService.login error: account=$accountLabel err=$e\n$st',
      );
      return ServiceResult<void>.failure(message: 'login_error_failed'.tr);
    }
  }

  bool _canReuseActiveSessionForAccount(String account) {
    if (!isLoggedIn || !hasUsableAccessToken()) {
      return false;
    }

    final normalizedAccount = account.trim().toLowerCase();
    if (normalizedAccount.isEmpty) {
      return false;
    }

    final currentUser = _user.value;
    if (currentUser == null) {
      return false;
    }

    final username = currentUser.username.trim().toLowerCase();
    final email = currentUser.email.trim().toLowerCase();
    return normalizedAccount == username ||
        (email.isNotEmpty && normalizedAccount == email);
  }

  String _debugRouteLabel() {
    final route = Get.currentRoute.trim();
    if (route.isEmpty) {
      return '(empty)';
    }
    return route;
  }

  String _debugAccountLabel(String account) {
    final normalized = account.trim();
    if (normalized.isEmpty) {
      return '(empty)';
    }
    final atIndex = normalized.indexOf('@');
    if (atIndex > 0) {
      final local = normalized.substring(0, atIndex);
      final domain = normalized.substring(atIndex + 1);
      final localHead = local.substring(0, 1);
      final localTail = local.length > 1
          ? local.substring(local.length - 1)
          : '';
      return '$localHead***$localTail@$domain';
    }
    if (normalized.length <= 2) {
      return '${normalized.substring(0, 1)}***';
    }
    return '${normalized.substring(0, 1)}***${normalized.substring(normalized.length - 1)}';
  }

  @override
  Future<ServiceResult<void>> loginWithGoogle(String idToken) async {
    try {
      final devicePayload = await _buildAuthGrantDevicePayload();
      final response = await _authPost(
        '/auth/oauth2/google',
        data: {'id_token': idToken, ...devicePayload},
      );
      return _handleAuthGrantResponse(
        response,
        fallbackMessage: 'login_google_error_failed'.tr,
      );
    } on DioException catch (e) {
      return _dioFailure<void>(
        e,
        fallbackMessage: 'login_google_error_failed'.tr,
      );
    } catch (_) {
      return ServiceResult<void>.failure(
        message: 'login_google_error_failed'.tr,
      );
    }
  }

  @override
  Future<ServiceResult<void>> loginWithApple(String idToken) async {
    try {
      final devicePayload = await _buildAuthGrantDevicePayload();
      final response = await _authPost(
        '/auth/oauth2/apple',
        data: {'id_token': idToken, ...devicePayload},
      );
      return _handleAuthGrantResponse(
        response,
        fallbackMessage: 'login_apple_error_failed'.tr,
      );
    } on DioException catch (e) {
      return _dioFailure<void>(
        e,
        fallbackMessage: 'login_apple_error_failed'.tr,
      );
    } catch (_) {
      return ServiceResult<void>.failure(
        message: 'login_apple_error_failed'.tr,
      );
    }
  }

  @override
  Future<ServiceResult<void>> loginWithQrCodeSession({
    required String qrSessionId,
    required String pollToken,
  }) async {
    final sessionId = qrSessionId.trim();
    final token = pollToken.trim();
    if (sessionId.isEmpty || token.isEmpty) {
      return ServiceResult<void>.failure(
        message: 'login_qr_exchange_failed'.tr,
        code: 10003,
        httpStatus: 400,
      );
    }

    try {
      final devicePayload = await _buildAuthGrantDevicePayload();
      final response = await _authPost(
        '/auth/qr/exchange',
        data: {
          'qr_session_id': sessionId,
          'poll_token': token,
          ...devicePayload,
        },
      );
      return _handleAuthGrantResponse(
        response,
        fallbackMessage: 'login_qr_exchange_failed'.tr,
      );
    } on DioException catch (e) {
      return _dioFailure<void>(
        e,
        fallbackMessage: 'login_qr_exchange_failed'.tr,
      );
    } catch (_) {
      return ServiceResult<void>.failure(
        message: 'login_qr_exchange_failed'.tr,
      );
    }
  }

  @override
  Future<ServiceResult<void>> resetPassword({
    required String email,
    required String newPassword,
    required String emailCode,
  }) async {
    try {
      final response = await _authPost(
        '/auth/reset-password',
        data: {
          'email': email,
          'new_password': newPassword,
          'email_code': emailCode,
        },
      );
      return _plainApiResult(
        response,
        fallbackMessage: 'reset_error_failed'.tr,
      );
    } on DioException catch (e) {
      return _dioFailure<void>(e, fallbackMessage: 'reset_error_failed'.tr);
    } catch (_) {
      return ServiceResult<void>.failure(message: 'reset_error_failed'.tr);
    }
  }

  @override
  Future<ServiceResult<void>> sendChangePasswordEmailCode() async {
    final tokenReady = await ensureTokenFresh();
    if (!tokenReady) {
      return ServiceResult<void>.failure(
        message: 'auth_error_unauthorized'.tr,
        code: 401,
        httpStatus: 401,
      );
    }
    final access = token;
    if (access == null || access.isEmpty) {
      return ServiceResult<void>.failure(
        message: 'auth_error_unauthorized'.tr,
        code: 401,
        httpStatus: 401,
      );
    }

    try {
      final response = await _dio.post(
        '/users/password/email-code',
        options: Options(headers: {'Authorization': 'Bearer $access'}),
      );
      return _plainApiResult(
        response,
        fallbackMessage: 'me_change_password_send_code_failed'.tr,
      );
    } on DioException catch (e) {
      if (e.response?.statusCode == 401) {
        handleUnauthorized();
      }
      return _dioFailure<void>(
        e,
        fallbackMessage: 'me_change_password_send_code_failed'.tr,
      );
    } catch (_) {
      return ServiceResult<void>.failure(
        message: 'me_change_password_send_code_failed'.tr,
      );
    }
  }

  @override
  Future<ServiceResult<void>> changeOwnPassword({
    required String newPassword,
    required String emailCode,
  }) async {
    final normalizedPassword = newPassword.trim();
    final normalizedEmailCode = emailCode.trim();
    if (normalizedPassword.isEmpty) {
      return ServiceResult<void>.failure(
        message: 'auth_error_password_required'.tr,
        code: 10003,
        httpStatus: 400,
      );
    }
    if (normalizedEmailCode.isEmpty) {
      return ServiceResult<void>.failure(
        message: 'auth_error_email_code_required'.tr,
        code: 10003,
        httpStatus: 400,
      );
    }

    final tokenReady = await ensureTokenFresh();
    if (!tokenReady) {
      return ServiceResult<void>.failure(
        message: 'auth_error_unauthorized'.tr,
        code: 401,
        httpStatus: 401,
      );
    }
    final access = token;
    if (access == null || access.isEmpty) {
      return ServiceResult<void>.failure(
        message: 'auth_error_unauthorized'.tr,
        code: 401,
        httpStatus: 401,
      );
    }

    try {
      final response = await _dio.post(
        '/users/password',
        data: {
          'new_password': normalizedPassword,
          'email_code': normalizedEmailCode,
        },
        options: Options(headers: {'Authorization': 'Bearer $access'}),
      );
      return _plainApiResult(
        response,
        fallbackMessage: 'me_change_password_failed'.tr,
      );
    } on DioException catch (e) {
      if (e.response?.statusCode == 401) {
        handleUnauthorized();
      }
      return _dioFailure<void>(
        e,
        fallbackMessage: 'me_change_password_failed'.tr,
      );
    } catch (_) {
      return ServiceResult<void>.failure(
        message: 'me_change_password_failed'.tr,
      );
    }
  }

  @override
  Future<ServiceResult<void>> updateProfile({
    required String nickname,
    required String introduction,
  }) async {
    final normalizedNickname = nickname.trim();
    final normalizedIntroduction = introduction
        .replaceAll('\r\n', '\n')
        .replaceAll('\r', '\n')
        .trim();

    if (normalizedNickname.isEmpty) {
      return ServiceResult<void>.failure(
        message: 'profile_edit_nickname_required'.tr,
        code: 10003,
        httpStatus: 400,
      );
    }

    final tokenReady = await ensureTokenFresh();
    if (!tokenReady) {
      return ServiceResult<void>.failure(
        message: 'auth_error_unauthorized'.tr,
        code: 401,
        httpStatus: 401,
      );
    }
    final access = token;
    if (access == null || access.isEmpty) {
      return ServiceResult<void>.failure(
        message: 'auth_error_unauthorized'.tr,
        code: 401,
        httpStatus: 401,
      );
    }

    try {
      final payload = <String, dynamic>{
        'nickname': normalizedNickname,
        'introduction': normalizedIntroduction,
      };

      final response = await _dio.put(
        '/users/profile',
        data: payload,
        options: Options(headers: {'Authorization': 'Bearer $access'}),
      );

      final apiResult = _plainApiResult(
        response,
        fallbackMessage: 'profile_edit_update_failed'.tr,
      );
      if (!apiResult.ok) {
        return apiResult;
      }

      final currentUser = _user.value;
      if (currentUser != null) {
        final nextUser = User(
          id: currentUser.id,
          username: currentUser.username,
          email: currentUser.email,
          nickname: normalizedNickname,
          introduction: normalizedIntroduction,
          avatarUrl: currentUser.avatarUrl,
          usernameModified: currentUser.usernameModified,
        );
        _user.value = nextUser;
        await _persistUser(nextUser);
      }

      return ServiceResult<void>.success(
        httpStatus: response.statusCode ?? 200,
      );
    } on DioException catch (e) {
      if (e.response?.statusCode == 401) {
        handleUnauthorized();
      }
      return _dioFailure<void>(
        e,
        fallbackMessage: 'profile_edit_update_failed'.tr,
      );
    } catch (_) {
      return ServiceResult<void>.failure(
        message: 'profile_edit_update_failed'.tr,
      );
    }
  }

  @override
  Future<ServiceResult<User>> fetchCurrentUserProfile() async {
    final tokenReady = await ensureTokenFresh();
    if (!tokenReady) {
      return ServiceResult<User>.failure(
        message: 'auth_error_unauthorized'.tr,
        code: 401,
        httpStatus: 401,
      );
    }
    final access = token;
    if (access == null || access.isEmpty) {
      return ServiceResult<User>.failure(
        message: 'auth_error_unauthorized'.tr,
        code: 401,
        httpStatus: 401,
      );
    }

    try {
      final response = await _dio.get(
        '/users/profile',
        options: Options(headers: {'Authorization': 'Bearer $access'}),
      );
      final body = _asBody(response.data);
      if (response.statusCode != 200 || body == null) {
        return ServiceResult<User>.failure(
          message: 'common_error'.tr,
          httpStatus: response.statusCode ?? 0,
        );
      }

      final code = _toInt(body['code'], fallback: 50001);
      if (code != 0) {
        return ServiceResult<User>.failure(
          message: _extractMessage(body, fallback: 'common_error'.tr),
          code: code,
          httpStatus: response.statusCode ?? 0,
        );
      }

      final data = _asBody(body['data']);
      if (data == null) {
        return ServiceResult<User>.failure(
          message: 'common_error'.tr,
          httpStatus: response.statusCode ?? 0,
        );
      }

      final nextUser = User.fromJson(data);
      if (nextUser.id.isEmpty) {
        return ServiceResult<User>.failure(
          message: 'common_error'.tr,
          httpStatus: response.statusCode ?? 0,
        );
      }

      _user.value = nextUser;
      await _persistUser(nextUser);
      return ServiceResult<User>.success(
        data: nextUser,
        httpStatus: response.statusCode ?? 200,
      );
    } on DioException catch (e) {
      if (e.response?.statusCode == 401) {
        handleUnauthorized();
      }
      return _dioFailure<User>(e, fallbackMessage: 'common_error'.tr);
    } catch (_) {
      return ServiceResult<User>.failure(message: 'common_error'.tr);
    }
  }

  @override
  Future<ServiceResult<void>> updateUsername({required String username}) async {
    final normalizedUsername = username.trim();
    if (normalizedUsername.isEmpty) {
      return ServiceResult<void>.failure(
        message: 'profile_edit_username_required'.tr,
        code: 10003,
        httpStatus: 400,
      );
    }

    final tokenReady = await ensureTokenFresh();
    if (!tokenReady) {
      return ServiceResult<void>.failure(
        message: 'auth_error_unauthorized'.tr,
        code: 401,
        httpStatus: 401,
      );
    }
    final access = token;
    if (access == null || access.isEmpty) {
      return ServiceResult<void>.failure(
        message: 'auth_error_unauthorized'.tr,
        code: 401,
        httpStatus: 401,
      );
    }

    try {
      final response = await _dio.put(
        '/users/username',
        data: {'username': normalizedUsername},
        options: Options(headers: {'Authorization': 'Bearer $access'}),
      );
      final apiResult = _plainApiResult(
        response,
        fallbackMessage: 'profile_edit_username_update_failed'.tr,
      );
      if (!apiResult.ok) return apiResult;

      final currentUser = _user.value;
      if (currentUser != null) {
        final nextUser = User(
          id: currentUser.id,
          username: normalizedUsername,
          email: currentUser.email,
          nickname: currentUser.nickname,
          introduction: currentUser.introduction,
          avatarUrl: currentUser.avatarUrl,
          usernameModified: true,
        );
        _user.value = nextUser;
        await _persistUser(nextUser);
      }

      return ServiceResult<void>.success(
        httpStatus: response.statusCode ?? 200,
      );
    } on DioException catch (e) {
      if (e.response?.statusCode == 401) {
        handleUnauthorized();
      }
      return _dioFailure<void>(
        e,
        fallbackMessage: 'profile_edit_username_update_failed'.tr,
      );
    } catch (_) {
      return ServiceResult<void>.failure(
        message: 'profile_edit_username_update_failed'.tr,
      );
    }
  }

  @override
  Future<ServiceResult<String>> uploadAvatar({
    required Uint8List bytes,
    required String filename,
  }) async {
    if (bytes.isEmpty) {
      return ServiceResult<String>.failure(
        message: 'profile_avatar_upload_failed'.tr,
        code: 10003,
        httpStatus: 400,
      );
    }

    final tokenReady = await ensureTokenFresh();
    if (!tokenReady) {
      return ServiceResult<String>.failure(
        message: 'auth_error_unauthorized'.tr,
        code: 401,
        httpStatus: 401,
      );
    }
    final access = token;
    if (access == null || access.isEmpty) {
      return ServiceResult<String>.failure(
        message: 'auth_error_unauthorized'.tr,
        code: 401,
        httpStatus: 401,
      );
    }

    final currentUser = _user.value;
    final currentUserId =
        currentUser?.id.trim() ?? (LocalDb.activeUserId?.trim() ?? '');
    final previousAvatarUrl = currentUser?.avatarUrl?.trim() ?? '';

    try {
      final normalizedFilename = filename.trim().isEmpty ? 'avatar' : filename;
      final form = FormData.fromMap({
        'file': MultipartFile.fromBytes(bytes, filename: normalizedFilename),
      });

      final response = await _dio.post(
        '/users/avatar',
        data: form,
        options: Options(headers: {'Authorization': 'Bearer $access'}),
      );

      final body = _asBody(response.data);
      if (response.statusCode != 200 || body == null) {
        return ServiceResult<String>.failure(
          message: 'profile_avatar_upload_failed'.tr,
          httpStatus: response.statusCode ?? 0,
        );
      }
      final code = _toInt(body['code'], fallback: 50001);
      if (code != 0) {
        return ServiceResult<String>.failure(
          message: _extractMessage(
            body,
            fallback: 'profile_avatar_upload_failed'.tr,
          ),
          code: code,
          httpStatus: response.statusCode ?? 0,
        );
      }
      final data = _asBody(body['data']);
      final avatarUrl = data?['avatar_url']?.toString().trim() ?? '';
      if (avatarUrl.isEmpty) {
        return ServiceResult<String>.failure(
          message: 'profile_avatar_upload_failed'.tr,
          httpStatus: response.statusCode ?? 0,
        );
      }

      if (currentUser != null) {
        final nextUser = User(
          id: currentUser.id,
          username: currentUser.username,
          email: currentUser.email,
          nickname: currentUser.nickname,
          introduction: currentUser.introduction,
          avatarUrl: avatarUrl,
          usernameModified: currentUser.usernameModified,
        );
        _user.value = nextUser;
        await _persistUser(nextUser);
      }
      await _evictAvatarImageCacheAfterUpload(
        userId: currentUserId,
        previousAvatarUrl: previousAvatarUrl,
        nextAvatarUrl: avatarUrl,
      );

      return ServiceResult<String>.success(
        data: avatarUrl,
        httpStatus: response.statusCode ?? 200,
      );
    } on DioException catch (e) {
      if (e.response?.statusCode == 401) {
        handleUnauthorized();
      }
      return _dioFailure<String>(
        e,
        fallbackMessage: 'profile_avatar_upload_failed'.tr,
      );
    } catch (_) {
      return ServiceResult<String>.failure(
        message: 'profile_avatar_upload_failed'.tr,
      );
    }
  }

  @override
  Future<ServiceResult<void>> deleteAccount() async {
    final currentUserId = _user.value?.id.trim() ?? '';
    if (currentUserId.isEmpty) {
      return ServiceResult<void>.failure(
        message: 'auth_error_unauthorized'.tr,
        code: 401,
        httpStatus: 401,
      );
    }

    final tokenReady = await ensureTokenFresh();
    if (!tokenReady) {
      return ServiceResult<void>.failure(
        message: 'auth_error_unauthorized'.tr,
        code: 401,
        httpStatus: 401,
      );
    }
    final access = token;
    if (access == null || access.isEmpty) {
      return ServiceResult<void>.failure(
        message: 'auth_error_unauthorized'.tr,
        code: 401,
        httpStatus: 401,
      );
    }

    try {
      final response = await _dio.delete(
        '/users/me',
        options: Options(headers: {'Authorization': 'Bearer $access'}),
      );
      final apiResult = _plainApiResult(
        response,
        fallbackMessage: 'account_delete_failed'.tr,
      );
      if (!apiResult.ok) {
        return apiResult;
      }

      try {
        await AppStorageService.clearActiveUserStorage();
      } catch (error) {
        debugPrint('Delete account local storage cleanup failed: $error');
      }
      await logout(notifyServer: false);

      return ServiceResult<void>.success(
        httpStatus: response.statusCode ?? 200,
      );
    } on DioException catch (e) {
      if (e.response?.statusCode == 401) {
        handleUnauthorized();
      }
      return _dioFailure<void>(e, fallbackMessage: 'account_delete_failed'.tr);
    } catch (_) {
      return ServiceResult<void>.failure(message: 'account_delete_failed'.tr);
    }
  }

  Future<void> _evictAvatarImageCacheAfterUpload({
    required String userId,
    required String previousAvatarUrl,
    required String nextAvatarUrl,
  }) async {
    final normalizedUserId = userId.trim();
    if (normalizedUserId.isEmpty) {
      return;
    }
    try {
      await UserImageCacheManager.evictUserImages(normalizedUserId, <String>[
        previousAvatarUrl,
        nextAvatarUrl,
      ]);
    } catch (error) {
      debugPrint('Avatar cache eviction failed: $error');
    }
  }

  Future<Response<dynamic>> _authGet(String path, {Options? options}) {
    return _requestWithHardTimeout(
      path: path,
      method: 'GET',
      options: options,
      execute: (cancelToken) =>
          _dio.get<dynamic>(path, options: options, cancelToken: cancelToken),
    );
  }

  Future<Response<dynamic>> _authPost(
    String path, {
    Object? data,
    Options? options,
  }) {
    return _requestWithHardTimeout(
      path: path,
      method: 'POST',
      data: data,
      options: options,
      execute: (cancelToken) => _dio.post<dynamic>(
        path,
        data: data,
        options: options,
        cancelToken: cancelToken,
      ),
    );
  }

  Future<Response<dynamic>> _requestWithHardTimeout({
    required String path,
    required String method,
    Object? data,
    Options? options,
    required Future<Response<dynamic>> Function(CancelToken cancelToken)
    execute,
  }) async {
    final cancelToken = CancelToken();

    try {
      return await execute(cancelToken).timeout(
        _authRequestHardTimeout,
        onTimeout: () {
          cancelToken.cancel(_authRequestTimeoutReason);
          throw DioException(
            requestOptions: RequestOptions(
              path: path,
              method: method,
              baseUrl: _dio.options.baseUrl,
              data: data,
              headers: options?.headers,
            ),
            type: DioExceptionType.receiveTimeout,
            message: 'Auth request timed out',
            error: TimeoutException('Auth request timed out'),
          );
        },
      );
    } on DioException catch (e) {
      if (_isAuthTimeoutCancellation(e)) {
        throw DioException(
          requestOptions: e.requestOptions,
          response: e.response,
          type: DioExceptionType.receiveTimeout,
          message: 'Auth request timed out',
          error: TimeoutException('Auth request timed out'),
        );
      }
      rethrow;
    }
  }

  bool _isAuthTimeoutCancellation(DioException e) {
    if (e.type != DioExceptionType.cancel) {
      return false;
    }
    final reason = e.error?.toString() ?? '';
    return reason.contains(_authRequestTimeoutReason);
  }

  Future<Response<dynamic>> _retryRequestWithToken(
    Dio dio,
    RequestOptions request,
    String accessToken,
  ) {
    final headers = Map<String, dynamic>.from(request.headers);
    headers['Authorization'] = 'Bearer $accessToken';

    final options = Options(
      method: request.method,
      headers: headers,
      sendTimeout: request.sendTimeout,
      receiveTimeout: request.receiveTimeout,
      extra: Map<String, dynamic>.from(request.extra)..['authRetry'] = true,
      responseType: request.responseType,
      contentType: request.contentType,
      validateStatus: request.validateStatus,
      receiveDataWhenStatusError: request.receiveDataWhenStatusError,
      followRedirects: request.followRedirects,
      listFormat: request.listFormat,
    );

    return dio.request<dynamic>(
      request.path,
      data: request.data,
      queryParameters: request.queryParameters,
      cancelToken: request.cancelToken,
      onSendProgress: request.onSendProgress,
      onReceiveProgress: request.onReceiveProgress,
      options: options,
    );
  }

  // ===== 手机号无密码短信登录注册 =====
  //
  // 与现有邮箱链路完全并行，老链路零回归。
  // 三个接口都不引入新的契约抽象，避免触发 _AuthServiceContract 全量改动。

  /// 发送短信验证码。scene = register / login / reset / bind。
  /// register/login/reset 走 /v1/auth/sms/send；bind 也走同一路由（后端按 scene 判断）。
  Future<ServiceResult<void>> sendSmsCode({
    required String phoneE164,
    required String scene,
    String? captchaId,
    String? captchaValue,
  }) async {
    try {
      final payload = <String, dynamic>{
        'phone_e164': phoneE164.trim(),
        'scene': scene,
      };
      final id = captchaId?.trim() ?? '';
      if (id.isNotEmpty) {
        payload['captcha_id'] = id;
      }
      final value = captchaValue?.trim() ?? '';
      if (value.isNotEmpty) {
        payload['captcha_value'] = value;
      }
      final response = await _authPost('/auth/sms/send', data: payload);
      return _plainApiResult(
        response,
        fallbackMessage: 'auth_send_code_failed'.tr,
      );
    } on DioException catch (e) {
      return _dioFailure<void>(e, fallbackMessage: 'auth_send_code_failed'.tr);
    } catch (_) {
      return ServiceResult<void>.failure(message: 'auth_send_code_failed'.tr);
    }
  }

  /// 手机号 + 短信码登录或自动注册（接口幂等：账号不存在则注册）。
  /// 走完整 auth grant 流程，与邮箱登录共享 token 持久化。
  Future<ServiceResult<void>> phoneLoginWithCode({
    required String phoneE164,
    required String code,
  }) async {
    try {
      final devicePayload = await _buildAuthGrantDevicePayload();
      final response = await _authPost(
        '/auth/phone/login-code',
        data: {
          'phone_e164': phoneE164.trim(),
          'code': code.trim(),
          ...devicePayload,
        },
      );
      return _handleAuthGrantResponse(
        response,
        fallbackMessage: 'login_error_failed'.tr,
      );
    } on DioException catch (e) {
      return _dioFailure<void>(e, fallbackMessage: 'login_error_failed'.tr);
    } catch (_) {
      return ServiceResult<void>.failure(message: 'login_error_failed'.tr);
    }
  }

  /// 已登录用户给当前账户绑定手机号。
  /// 绑定接口需鉴权：先确保 token 有效，再显式带上 Authorization。
  /// 成功后建议调用方刷一次 user profile。
  Future<ServiceResult<void>> bindPhone({
    required String phoneE164,
    required String code,
  }) async {
    final tokenReady = await ensureTokenFresh();
    if (!tokenReady) {
      return ServiceResult<void>.failure(
        message: 'auth_error_unauthorized'.tr,
        code: 401,
        httpStatus: 401,
      );
    }
    final access = token;
    if (access == null || access.isEmpty) {
      return ServiceResult<void>.failure(
        message: 'auth_error_unauthorized'.tr,
        code: 401,
        httpStatus: 401,
      );
    }

    try {
      final response = await _dio.post(
        '/users/bind-phone',
        data: {
          'phone_e164': phoneE164.trim(),
          'code': code.trim(),
        },
        options: Options(headers: {'Authorization': 'Bearer $access'}),
      );
      return _plainApiResult(
        response,
        fallbackMessage: 'phone_bind_failed'.tr,
      );
    } on DioException catch (e) {
      if (e.response?.statusCode == 401) {
        handleUnauthorized();
      }
      return _dioFailure<void>(e, fallbackMessage: 'phone_bind_failed'.tr);
    } catch (_) {
      return ServiceResult<void>.failure(message: 'phone_bind_failed'.tr);
    }
  }

  /// 拉取当前区域的认证能力开关。匿名接口，每次进登录/注册页或切区域都应刷新。
  ///
  /// region 取值 cn / global；空串后端按 global 处理。
  /// 任何失败默认 phoneLoginEnabled=false / phoneRegisterEnabled=false，
  /// 让调用方按"塘主已关闭"的语义渲染（隐藏入口），不给用户死按钮。
  Future<ServiceResult<AuthMethods>> fetchAuthMethods({
    required String region,
  }) async {
    try {
      final path = region.isEmpty
          ? '/auth/methods'
          : '/auth/methods?region=${Uri.encodeQueryComponent(region)}';
      final response = await _authGet(path);
      final body = _asBody(response.data);
      if (response.statusCode != 200 || body == null) {
        return ServiceResult<AuthMethods>.failure(
          message: 'auth_methods_fetch_failed'.trOrFallback(
            'Failed to load auth methods',
          ),
          httpStatus: response.statusCode ?? 0,
        );
      }
      final code = _toInt(body['code'], fallback: 50001);
      if (code != 0) {
        return ServiceResult<AuthMethods>.failure(
          message: _extractMessage(
            body,
            fallback: 'auth_methods_fetch_failed'.trOrFallback(
              'Failed to load auth methods',
            ),
          ),
          code: code,
          httpStatus: response.statusCode ?? 0,
        );
      }
      final data = _asBody(body['data']);
      if (data == null) {
        return ServiceResult<AuthMethods>.failure(
          message: 'auth_methods_fetch_failed'.trOrFallback(
            'Failed to load auth methods',
          ),
          httpStatus: response.statusCode ?? 0,
        );
      }
      return ServiceResult<AuthMethods>.success(
        data: AuthMethods.fromJson(data),
        httpStatus: response.statusCode ?? 200,
      );
    } on DioException catch (e) {
      return _dioFailure<AuthMethods>(
        e,
        fallbackMessage: 'auth_methods_fetch_failed'.trOrFallback(
          'Failed to load auth methods',
        ),
      );
    } catch (_) {
      return ServiceResult<AuthMethods>.failure(
        message: 'auth_methods_fetch_failed'.trOrFallback(
          'Failed to load auth methods',
        ),
      );
    }
  }
}

extension on String {
  /// i18n key 未配置时使用兜底文案。这里特意不依赖 GetX 的 tr 是否抛错，
  /// 直接读 Get.translations 判断键是否存在。
  String trOrFallback(String fallback) {
    final translated = tr;
    return translated == this ? fallback : translated;
  }
}
