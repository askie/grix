import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:get/get.dart';

import '../../../app/routes/app_routes.dart';
import '../../../app/routes/root_route_navigator.dart';
import '../../../data/providers/apple_sign_in_service.dart';
import '../../../data/providers/auth_service.dart';
import '../../../data/providers/feature_flag_service.dart';
import '../../../data/providers/google_sign_in_service.dart';
import '../../../data/providers/im_service.dart';
import '../../../shared/utils/app_region_config.dart';
import '../../../shared/utils/app_storage_service.dart';
import '../../../shared/utils/web_region_redirect.dart';
import '../services/login_credential_storage.dart';

class LoginController extends GetxController {
  LoginController({
    AuthService? authService,
    GoogleSignInService? googleSignInService,
    AppleSignInService? appleSignInService,
    ImService? imService,
    LoginCredentialStorage? credentialStorage,
    Duration submitTimeout = const Duration(seconds: 20),
  }) : authService = authService ?? Get.find<AuthService>(),
       googleSignInService =
           googleSignInService ?? Get.find<GoogleSignInService>(),
       appleSignInService =
           appleSignInService ?? Get.find<AppleSignInService>(),
       imService = imService ?? Get.find<ImService>(),
       credentialStorage = credentialStorage ?? LoginCredentialStorage(),
       _submitTimeout = submitTimeout;

  final AuthService authService;
  final GoogleSignInService googleSignInService;
  final AppleSignInService appleSignInService;
  final ImService imService;
  final LoginCredentialStorage credentialStorage;
  final Duration _submitTimeout;

  final isLoading = false.obs;
  final errorMessage = RxnString();
  // 密码登录被拒（401）时置真：提示用户账号可能注册在另一区域，并给出切换入口。
  final showCrossRegionHint = false.obs;
  final selectedRegion = AppRegion.cn.obs;
  // 当前区域的认证能力开关（拉自后端 /v1/auth/methods）；默认全关，
  // 等 fetch 完成再展示"使用手机号登录"入口，避免塘主关闭后用户还能点进去。
  final authMethods = const AuthMethods.allDisabled().obs;
  int _submitEpoch = 0;
  Worker? _loginStateWorker;
  // 区域解析是异步的（要读本地存储）。登录页一进来就读"记住的凭证"，
  // 必须等这个 Future 完成、selectedRegion 落到真正的当前区域后再读，
  // 否则会用初始默认值去读错区域的凭证（表现为：进登录页看不到记住的账号密码）。
  late final Future<void> _regionReady;

  @override
  void onInit() {
    super.onInit();
    _regionReady = _initRegion();
    _loginStateWorker = ever<bool>(authService.isLoggedInRx, (loggedIn) {
      debugPrint(
        '🔐 LoginController observed auth state: '
        'logged_in=$loggedIn route=${_debugRouteLabel()} '
        'loading=${isLoading.value} error=${_debugMessageLabel(errorMessage.value)}',
      );
      if (loggedIn) {
        _onLoginSuccess();
      }
    });
  }

  @override
  void onClose() {
    _loginStateWorker?.dispose();
    super.onClose();
  }

  Future<void> _initRegion() async {
    // 初始区域：沿用用户记住的选择，否则按系统语言推断（海外默认全球区）。
    selectedRegion.value = await resolveInitialRegion();
    authService.updateBaseUrl(resolveRegionApiBaseUrl(selectedRegion.value));
    // WS 端点：预写入 ImService，登录成功后 applyAuthPayload 再以服务器返回值覆盖。
    // Web 端忽略，始终跟随页面来源。
    if (!kIsWeb) {
      final savedWs = await AppStorageService.loadWsEndpoint();
      imService.updateWsEndpoint(
        savedWs != null && savedWs.isNotEmpty
            ? savedWs
            : resolveRegionWsUrl(selectedRegion.value),
      );
    }
    unawaited(_refreshAuthMethods());
  }

  /// 当前选中区域的另一侧（跨区提示的切换目标）。
  AppRegion get otherRegion =>
      selectedRegion.value == AppRegion.cn ? AppRegion.global : AppRegion.cn;

  void switchToOtherRegion() => switchRegion(otherRegion);

  void switchRegion(AppRegion region) {
    if (selectedRegion.value == region) return;
    showCrossRegionHint.value = false;
    // Web 端：重定向到目标分区域名，浏览器整页跳转，不继续操作本地状态。
    if (redirectToRegionIfNeeded(region)) return;
    selectedRegion.value = region;
    authService.updateBaseUrl(resolveRegionApiBaseUrl(region));
    if (!kIsWeb) {
      imService.updateWsEndpoint(resolveRegionWsUrl(region));
    }
    AppStorageService.saveRegion(region.name);
    // 切区后能力开关也跟着变；先回落到"全关"再异步拉新结果，避免短暂展示老区域的按钮。
    authMethods.value = const AuthMethods.allDisabled();
    unawaited(_refreshAuthMethods());
    // Apple/Google 登录入口由 FeatureFlagService 控制，同样要按新区域重新拉取，
    // 否则切区后仍显示旧区域的登录方式（如切到全球区后苹果登录按钮状态不变）。
    Get.find<FeatureFlagService>().refresh();
  }

  Future<void> _refreshAuthMethods() async {
    final result = await authService.fetchAuthMethods(
      region: selectedRegion.value.name,
    );
    if (result.ok && result.data != null) {
      authMethods.value = result.data!;
    } else {
      // 失败按"塘主已关闭"渲染（隐藏入口），不弹错误，避免污染登录页。
      authMethods.value = AuthMethods.allDisabled(
        region: selectedRegion.value.name,
      );
    }
  }

  Future<void> login({
    required String account,
    required String password,
    bool saveCredentials = false,
  }) async {
    final normalizedAccount = account.trim();
    final normalizedPassword = password.trim();
    debugPrint(
      '🔐 LoginController.login start: '
      'account=${_debugAccountLabel(normalizedAccount)} '
      'route=${_debugRouteLabel()} '
      'loading=${isLoading.value} '
      'save_credentials=$saveCredentials',
    );

    if (normalizedAccount.isEmpty || normalizedPassword.isEmpty) {
      errorMessage.value = 'login_error_empty'.tr;
      debugPrint(
        '⚠️ LoginController.login blocked: empty credential '
        'account_empty=${normalizedAccount.isEmpty} password_empty=${normalizedPassword.isEmpty}',
      );
      return;
    }

    await _runAuthAttempt(
      request: () => authService.login(normalizedAccount, normalizedPassword),
      fallbackMessage: 'login_error_failed'.tr,
      credentialState: saveCredentials
          ? LoginCredentialState(account: normalizedAccount)
          : null,
      enableCrossRegionHint: true,
    );
  }

  Future<void> loginWithGoogle() async {
    if (isLoading.value) return;
    isLoading.value = true;
    errorMessage.value = null;

    final signInResult = await googleSignInService.signIn();
    final idToken = signInResult.data?.trim() ?? '';
    if (!signInResult.ok || idToken.isEmpty) {
      if (!isClosed) {
        isLoading.value = false;
        errorMessage.value = signInResult.message.isEmpty
            ? 'login_google_error_failed'.tr
            : signInResult.message;
      }
      return;
    }

    if (isClosed) return;
    isLoading.value = false;
    await _runAuthAttempt(
      request: () => authService.loginWithGoogle(idToken),
      fallbackMessage: 'login_google_error_failed'.tr,
    );
  }

  Future<void> loginWithApple() async {
    if (isLoading.value) return;
    isLoading.value = true;
    errorMessage.value = null;

    final signInResult = await appleSignInService.signIn();
    final idToken = signInResult.data?.trim() ?? '';
    if (!signInResult.ok || idToken.isEmpty) {
      if (!isClosed) {
        isLoading.value = false;
        errorMessage.value = signInResult.message.isEmpty
            ? 'login_apple_error_failed'.tr
            : signInResult.message;
      }
      return;
    }

    if (isClosed) return;
    isLoading.value = false;
    await _runAuthAttempt(
      request: () => authService.loginWithApple(idToken),
      fallbackMessage: 'login_apple_error_failed'.tr,
    );
  }

  Future<LoginCredentialState> loadSavedCredentials() async {
    // 等区域解析完成再读，确保读到的是真正的当前区域那一套凭证。
    await _regionReady;
    return credentialStorage.load(selectedRegion.value);
  }

  void goToRegister() {
    if (Get.currentRoute != AppRoutes.register) {
      Get.toNamed(AppRoutes.register);
    }
  }

  void goToResetPassword() {
    if (Get.currentRoute != AppRoutes.resetPassword) {
      Get.toNamed(AppRoutes.resetPassword);
    }
  }

  Future<void> _runAuthAttempt({
    required Future<ServiceResult<void>> Function() request,
    required String fallbackMessage,
    LoginCredentialState? credentialState,
    bool enableCrossRegionHint = false,
  }) async {
    if (isLoading.value) return;

    final submitEpoch = ++_submitEpoch;
    // 请求前快照当前区域：登录成功后控制器可能已被销毁（见下方保存凭证处说明），
    // 销毁后再读 selectedRegion.value 不可靠，必须用此快照。
    final region = selectedRegion.value;
    isLoading.value = true;
    errorMessage.value = null;
    showCrossRegionHint.value = false;
    // 发起请求前再次把接口地址钉到当前区域。
    // AuthService 的 baseUrl 是多入口共享的可变状态（启动页/注册/找回密码都会改），
    // 仅靠 onInit 设置一次不可靠；退出后再登录时可能残留到错误区域导致 404。
    authService.updateBaseUrl(resolveRegionApiBaseUrl(region));
    debugPrint(
      '🔐 LoginController auth attempt started: '
      'epoch=$submitEpoch route=${_debugRouteLabel()} '
      'has_credential_state=${credentialState != null}',
    );

    ServiceResult<void> result;
    try {
      result = await request().timeout(
        _submitTimeout,
        onTimeout: () =>
            ServiceResult<void>.failure(message: 'auth_error_timeout'.tr),
      );
    } catch (e, st) {
      debugPrint('❌ LoginController auth attempt threw: $e\n$st');
      result = ServiceResult<void>.failure(message: fallbackMessage);
    } finally {
      if (!isClosed && _submitEpoch == submitEpoch) {
        isLoading.value = false;
        debugPrint(
          '🔐 LoginController auth attempt finished loading=false: '
          'epoch=$submitEpoch route=${_debugRouteLabel()}',
        );
      }
    }

    // 凭证保存必须先于下面的 isClosed/epoch 闸门：
    // authService.login() 内部会在返回前就把登录态置真，同步触发 onInit 里监听
    // 登录态的 ever worker，抢先 _onLoginSuccess → offAllNamed 清栈导航，进而销毁
    // 本控制器（lazyPut 无 fenix），令 isClosed 变为 true。若把保存放在闸门之后，
    // 凭证会被静默跳过——这正是"记住账号存不上"的根因。保存只依赖 SharedPreferences，
    // 与控制器存活无关，放在这里安全。注意：这里只保存账号，密码绝不落盘。
    if (result.ok && credentialState != null) {
      await credentialStorage.save(credentialState, region);
      debugPrint(
        '🔐 LoginController saved credentials: '
        'account=${_debugAccountLabel(credentialState.account)} '
        'region=${region.name}',
      );
    }

    if (isClosed || _submitEpoch != submitEpoch) return;
    debugPrint(
      '🔐 LoginController auth attempt result: '
      'epoch=$submitEpoch ok=${result.ok} code=${result.code} '
      'http=${result.httpStatus} message=${_debugMessageLabel(result.message)} '
      'logged_in=${authService.isLoggedIn} route=${_debugRouteLabel()}',
    );
    if (result.ok) {
      _onLoginSuccess();
      return;
    }

    errorMessage.value = result.message.isEmpty
        ? fallbackMessage
        : result.message;
    // 凭证被拒（401）：账号很可能注册在另一区域（两区账号库独立），
    // 提示用户切换区域重试；其他失败（网络/超时等）不提示，避免误导。
    showCrossRegionHint.value =
        enableCrossRegionHint && result.httpStatus == 401;
    debugPrint(
      '❌ LoginController auth failed: '
      'epoch=$submitEpoch error=${_debugMessageLabel(errorMessage.value)} '
      'route=${_debugRouteLabel()}',
    );
  }

  void _onLoginSuccess() {
    errorMessage.value = null;
    showCrossRegionHint.value = false;
    debugPrint(
      '✅ LoginController login success: '
      'route=${_debugRouteLabel()} logged_in=${authService.isLoggedIn} '
      'user_id=${authService.userId ?? '-'} im_connected=${imService.isConnected}',
    );
    if (!imService.isConnected) {
      debugPrint('🔌 LoginController triggering IM connect');
      imService.ensureConnected();
    }
    debugPrint(
      '🧭 LoginController navigating to home from route=${_debugRouteLabel()}',
    );
    RootRouteNavigator.toHome();
  }

  String _debugRouteLabel() {
    final route = Get.currentRoute.trim();
    if (route.isEmpty) {
      return '(empty)';
    }
    return route;
  }

  String _debugMessageLabel(String? message) {
    final normalized = message?.trim() ?? '';
    if (normalized.isEmpty) {
      return '-';
    }
    return normalized;
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
}
