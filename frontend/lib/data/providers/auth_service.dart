import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:get/get.dart' hide FormData, MultipartFile, Response;

import '../../app/routes/app_routes.dart';
import '../../app/routes/root_route_navigator.dart';
import '../../modules/chat/services/conversation_audit_preference_service.dart';
import '../../shared/utils/app_region_config.dart';
import '../../shared/utils/app_runtime_endpoints.dart';
import '../../shared/utils/app_storage_service.dart';
import '../../shared/utils/device_identity.dart';
import '../../shared/utils/user_image_cache_manager.dart';
import 'agent_category_service.dart';
import 'agent_service.dart';
import 'auth_session_store.dart';
import 'friend_service.dart';
import 'im_service.dart';
import 'local_db.dart';
import 'push_registration_service.dart';
import 'saved_account_store.dart';
import 'user_settings_service.dart';

part 'auth_service_api.dart';
part 'auth_service_payload.dart';
part 'auth_service_lifecycle.dart';
part 'auth_service_runtime_reset.dart';
part 'auth_service_accounts.dart';

const String _authKeyAccessToken = 'access_token';
const String _authKeyRefreshToken = 'refresh_token';
const String _authKeyAccessExpiresAtMs = 'access_expires_at_ms';
const String _authKeyUserId = 'user_id';
const String _authKeyUsername = 'username';
const String _authKeyEmail = 'email';
const String _authKeyNickname = 'nickname';
const String _authKeyIntroduction = 'introduction';
const String _authKeyAvatarUrl = 'avatar_url';
const String _authKeyUsernameModified = 'username_modified';
const String _authKeyPhoneE164 = 'phone_e164';
const String _authKeyPhoneCountry = 'phone_country';
const List<String> _authSessionKeys = <String>[
  _authKeyAccessToken,
  _authKeyRefreshToken,
  _authKeyAccessExpiresAtMs,
  _authKeyUserId,
  _authKeyUsername,
  _authKeyEmail,
  _authKeyNickname,
  _authKeyIntroduction,
  _authKeyAvatarUrl,
  _authKeyUsernameModified,
  _authKeyPhoneE164,
  _authKeyPhoneCountry,
];

const Duration _authRefreshAhead = Duration(minutes: 5);
const Duration _authRefreshRetryDelay = Duration(seconds: 30);
const Duration _authRequestHardTimeout = Duration(seconds: 15);
const String _authRequestTimeoutReason = 'auth_request_timeout';
const String _keyAccessToken = _authKeyAccessToken;
const String _keyRefreshToken = _authKeyRefreshToken;
const String _keyAccessExpiresAtMs = _authKeyAccessExpiresAtMs;
const String _keyUserId = _authKeyUserId;
const String _keyUsername = _authKeyUsername;
const String _keyEmail = _authKeyEmail;
const String _keyNickname = _authKeyNickname;
const String _keyIntroduction = _authKeyIntroduction;
const String _keyAvatarUrl = _authKeyAvatarUrl;
const String _keyUsernameModified = _authKeyUsernameModified;
const String _keyPhoneE164 = _authKeyPhoneE164;
const String _keyPhoneCountry = _authKeyPhoneCountry;
const Duration _refreshAhead = _authRefreshAhead;
const Duration _refreshRetryDelay = _authRefreshRetryDelay;

class ServiceResult<T> {
  final bool ok;
  final T? data;
  final int code;
  final int httpStatus;
  final String message;

  const ServiceResult({
    required this.ok,
    this.data,
    this.code = 0,
    this.httpStatus = 200,
    this.message = '',
  });

  factory ServiceResult.success({T? data, int httpStatus = 200}) {
    return ServiceResult<T>(
      ok: true,
      data: data,
      code: 0,
      httpStatus: httpStatus,
      message: '',
    );
  }

  factory ServiceResult.failure({
    required String message,
    int code = 50001,
    int httpStatus = 0,
  }) {
    return ServiceResult<T>(
      ok: false,
      code: code,
      httpStatus: httpStatus,
      message: message,
    );
  }
}

class CaptchaData {
  final String captchaId;
  final String b64s;

  const CaptchaData({required this.captchaId, required this.b64s});
}

/// 后端 `/v1/auth/methods` 返回的能力开关，按 region 由塘主 SmsSettings 决定。
/// 默认（拉取失败 / 未拉取）按"全部不开"处理：UI 不展示手机号入口，
/// 与"塘主已关闭"的视觉一致，避免给用户一个会失败的按钮。
class AuthMethods {
  final String region;
  final bool phoneLoginEnabled;
  final bool phoneRegisterEnabled;

  const AuthMethods({
    required this.region,
    required this.phoneLoginEnabled,
    required this.phoneRegisterEnabled,
  });

  const AuthMethods.allDisabled({this.region = ''})
    : phoneLoginEnabled = false,
      phoneRegisterEnabled = false;

  factory AuthMethods.fromJson(Map<String, dynamic> data) => AuthMethods(
    region: (data['region'] as String?) ?? '',
    phoneLoginEnabled: data['phone_login_enabled'] == true,
    phoneRegisterEnabled: data['phone_register_enabled'] == true,
  );
}

class User {
  final String id;
  final String username;
  final String? _email;
  final String nickname;
  final String introduction;
  final String? avatarUrl;
  final bool usernameModified;
  final String phoneE164;
  final String phoneCountry;

  String get email => _email ?? '';

  /// 是否已绑手机号（用于"绑定引导卡片"判定）。
  bool get hasPhone => phoneE164.trim().isNotEmpty;

  /// 是否已绑邮箱（手机号注册的账号该列为空）。
  bool get hasEmail => email.isNotEmpty;

  /// 邮箱是 Apple「隐藏我的邮箱」的中转地址：只在 Apple 继续转发时可达。
  bool get hasAppleRelayEmail =>
      email.toLowerCase().endsWith('@privaterelay.appleid.com');

  /// 是否还缺一个常用邮箱：没绑，或绑的是 Apple 中转地址。
  bool get needsEmailBinding => !hasEmail || hasAppleRelayEmail;

  User({
    required this.id,
    required this.username,
    String? email,
    required this.nickname,
    this.introduction = '',
    this.avatarUrl,
    this.usernameModified = false,
    this.phoneE164 = '',
    this.phoneCountry = '',
  }) : _email = email;

  factory User.fromJson(Map<String, dynamic> json) {
    final idRaw = json['id']?.toString().trim() ?? '';
    final username = _toNormalizedString(json['username']);
    final nickname = _toNormalizedString(json['nickname']);
    final usernameModified = _toBool(json['username_modified']);
    return User(
      id: idRaw,
      username: username,
      email: _toNormalizedString(json['email']),
      nickname: nickname.isNotEmpty ? nickname : username,
      introduction: _toNormalizedString(json['introduction']),
      avatarUrl: json['avatar_url'],
      usernameModified: usernameModified,
      phoneE164: _toNormalizedString(json['phone_e164']),
      phoneCountry: _toNormalizedString(json['phone_country']),
    );
  }

  static String _toNormalizedString(dynamic source) {
    final value = source?.toString();
    if (value == null) return '';
    return value.trim();
  }

  static bool _toBool(dynamic source) {
    if (source is bool) return source;
    if (source is num) return source != 0;
    final raw = source?.toString().trim().toLowerCase() ?? '';
    return raw == '1' || raw == 'true' || raw == 'yes';
  }
}

enum TokenRefreshStatus { ready, invalidSession, temporaryFailure }

abstract class _AuthServiceContract {
  Dio get _dio;
  RxBool get _isLoggedIn;
  Rxn<User> get _user;
  Rxn<String> get _token;
  Rxn<String> get _refreshToken;
  RxnInt get _accessExpiresAtMs;
  AuthSessionStore get _authSessionStore;
  set _authSessionStore(AuthSessionStore value);
  SavedAccountStore get _savedAccountStore;
  set _savedAccountStore(SavedAccountStore value);
  bool get _isSwitchingAccount;
  set _isSwitchingAccount(bool value);
  Future<TokenRefreshStatus>? get _refreshFuture;
  set _refreshFuture(Future<TokenRefreshStatus>? value);
  Timer? get _refreshTimer;
  set _refreshTimer(Timer? value);
  bool get _isHandlingUnauthorized;
  set _isHandlingUnauthorized(bool value);
  bool get _localeInterceptorAttached;
  set _localeInterceptorAttached(bool value);
  Completer<void>? get _authPayloadApplyCompleter;
  set _authPayloadApplyCompleter(Completer<void>? value);

  bool get isLoggedIn;
  String? get token;
  String? get refreshToken;
  int? get accessExpiresAtMs;
  String? get userId;
  bool hasUsableAccessToken({Duration minRemaining = Duration.zero});

  Future<AuthService> init();
  Future<bool> ensureTokenFresh({
    bool force = false,
    Duration threshold = _authRefreshAhead,
  });
  Future<TokenRefreshStatus> ensureTokenFreshStatus({
    bool force = false,
    Duration threshold = _authRefreshAhead,
  });
  Future<void> updateAccessExpiryFromServer(int expiresInSec);
  Future<void> logout({bool notifyServer = true});
  void handleUnauthorized({String? expectedAccessToken});
  Future<void> runScheduledRefreshAttemptForTest();
  Future<bool> applyAuthPayloadForTest(Map<String, dynamic> data);
  void attachAuthInterceptor(Dio dio);
  Future<void> _waitForPendingAuthPayloadApplication();
  Future<ServiceResult<void>> _handleAuthGrantResponse(
    Response<dynamic> response, {
    required String fallbackMessage,
  });
  Future<Map<String, String>> _buildAuthGrantDevicePayload();
  ServiceResult<void> _plainApiResult(
    Response<dynamic> response, {
    required String fallbackMessage,
  });
  ServiceResult<T> _dioFailure<T>(
    DioException e, {
    required String fallbackMessage,
  });
  Map<String, dynamic>? _asBody(dynamic source);
  String _extractMessage(Map<String, dynamic> body, {required String fallback});
  Future<void> _persistTokens({
    required String accessToken,
    required String refreshToken,
    required int expiresInSec,
  });
  Future<void> _persistUser(User user);
  int _toInt(dynamic value, {int fallback = 0});
  void _scheduleRefreshTimer();
  Future<void> _notifyServerLogout();
  Future<void> _clearLocalAuthData();
  Future<void> _resetRuntimeServices();
  void updateBaseUrl(String baseUrl);
  Future<bool> _applyAuthPayload(Map<String, dynamic> data);
  Future<List<SavedAccount>> listSavedAccounts();
  Future<void> _upsertCurrentAccountSnapshot();
  Future<AccountSwitchOutcome> switchToSavedAccount(String targetUserId);
  Future<void> removeSavedAccount(String targetUserId);
  Future<void> suspendCurrentSessionLocally();
  Future<ServiceResult<CaptchaData>> fetchCaptcha();
  Future<ServiceResult<void>> sendEmailCode({
    required String email,
    required String scene,
    String? captchaId,
    String? captchaValue,
  });
  Future<ServiceResult<void>> register({
    required String email,
    required String password,
    required String emailCode,
    String region = '',
  });
  Future<ServiceResult<void>> login(String account, String password);
  Future<ServiceResult<void>> loginWithGoogle(String idToken);
  Future<ServiceResult<void>> loginWithApple(String idToken);
  Future<ServiceResult<void>> loginWithQrCodeSession({
    required String qrSessionId,
    required String pollToken,
  });
  Future<ServiceResult<void>> resetPassword({
    required String email,
    required String newPassword,
    required String emailCode,
  });
  Future<ServiceResult<void>> sendChangePasswordEmailCode();
  Future<ServiceResult<void>> changeOwnPassword({
    required String newPassword,
    required String emailCode,
  });
  Future<ServiceResult<void>> updateProfile({
    required String nickname,
    required String introduction,
  });
  Future<ServiceResult<User>> fetchCurrentUserProfile();
  Future<ServiceResult<void>> updateUsername({required String username});
  Future<ServiceResult<String>> uploadAvatar({
    required Uint8List bytes,
    required String filename,
  });
  Future<ServiceResult<void>> deleteAccount();
  void _attachLocaleInterceptor();
}

abstract class _AuthServiceBase extends GetxService
    implements _AuthServiceContract {}

class AuthService extends _AuthServiceBase
    with
        _AuthServiceApi,
        _AuthServicePayload,
        _AuthServiceLifecycle,
        _AuthServiceRuntimeReset,
        _AuthServiceAccounts
    implements _AuthServiceContract {
  @override
  final _dio = Dio(
    BaseOptions(
      baseUrl: AppRuntimeEndpoints.apiBaseUrl,
      connectTimeout: const Duration(seconds: 10),
      sendTimeout: const Duration(seconds: 10),
      receiveTimeout: const Duration(seconds: 10),
    ),
  );

  @override
  final _isLoggedIn = false.obs;
  @override
  final _user = Rxn<User>();
  @override
  final _token = Rxn<String>();
  @override
  final _refreshToken = Rxn<String>();
  @override
  final _accessExpiresAtMs = RxnInt();
  @override
  late AuthSessionStore _authSessionStore;
  @override
  late SavedAccountStore _savedAccountStore;
  @override
  bool _isSwitchingAccount = false;

  @override
  Future<TokenRefreshStatus>? _refreshFuture;
  @override
  Timer? _refreshTimer;
  @override
  bool _isHandlingUnauthorized = false;
  @override
  bool _localeInterceptorAttached = false;
  @override
  Completer<void>? _authPayloadApplyCompleter;

  /// Other services' Dio instances registered via [attachAuthInterceptor].
  final _registeredDios = <Dio>{};

  @override
  bool get isLoggedIn => _isLoggedIn.value;
  RxBool get isLoggedInRx => _isLoggedIn;
  User? get user => _user.value;
  Rxn<User> get userRx => _user;
  @override
  String? get token => _token.value;
  @override
  String? get refreshToken => _refreshToken.value;
  @override
  int? get accessExpiresAtMs => _accessExpiresAtMs.value;
  @override
  String? get userId => _user.value?.id;

  @override
  void attachAuthInterceptor(Dio dio) {
    _registeredDios.add(dio);
    // 懒加载 service 在 splash updateBaseUrl 之后才创建时，注册进来的 dio 仍持有
    // 编译期默认 baseUrl（CN）。在此立即同步，确保任何时刻注册的 dio 都使用当前正确端点。
    final currentBaseUrl = _dio.options.baseUrl.trim();
    if (currentBaseUrl.isNotEmpty) {
      dio.options.baseUrl = currentBaseUrl;
    }
    super.attachAuthInterceptor(dio);
  }

  /// 当前实际使用的 API 端点（区域切换后为最新值）。
  String get currentApiBaseUrl => _dio.options.baseUrl;

  /// 运行时切换 API 端点（区域切换时调用）。
  @override
  void updateBaseUrl(String baseUrl) {
    final normalized = baseUrl.trim();
    if (normalized.isEmpty) return;
    _dio.options.baseUrl = normalized;
    for (final dio in _registeredDios) {
      dio.options.baseUrl = normalized;
    }
  }
}
