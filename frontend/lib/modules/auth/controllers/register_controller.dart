import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:get/get.dart';

import '../../../app/routes/app_routes.dart';
import '../../../app/routes/root_route_navigator.dart';
import '../../../data/providers/auth_service.dart';
import '../../../data/providers/im_service.dart';
import '../../../shared/utils/app_region_config.dart';
import '../../../shared/utils/app_storage_service.dart';
import '../../../shared/utils/toast_util.dart';
import '../../../shared/utils/web_region_redirect.dart';

class RegisterController extends GetxController {
  static const int _sendCodeCooldownSec = 300;

  final AuthService authService = Get.find<AuthService>();
  final ImService imService = Get.find<ImService>();

  final isLoading = false.obs;
  final isSendingCode = false.obs;
  final errorMessage = RxnString();
  final sendCodeCountdown = 0.obs;

  /// 当前选中的区域，初始值由 SplashController 通过 AuthService 的 baseUrl 推断，
  /// 或从本地存储读取。
  final selectedRegion = AppRegion.cn.obs;

  /// 当前区域的认证能力开关；默认全关，初始 fetch 完成或切区后更新。
  final authMethods = const AuthMethods.allDisabled().obs;
  final authMethodsLoading = true.obs;
  final authMethodsFailed = false.obs;
  int _methodsRequest = 0;
  int _regionEpoch = 0;

  Timer? _countdownTimer;

  bool get canRequestEmailCode =>
      !authMethodsLoading.value &&
      authMethods.value.registrationEnabled &&
      !isSendingCode.value &&
      sendCodeCountdown.value <= 0;

  @override
  void onInit() {
    super.onInit();
    _initRegion();
  }

  @override
  void onClose() {
    _countdownTimer?.cancel();
    super.onClose();
  }

  Future<void> _initRegion() async {
    // 初始区域：沿用用户记住的选择，否则按系统语言推断（海外默认全球区）。
    final initialRequest = _methodsRequest;
    final region = await resolveInitialRegion();
    if (isClosed || initialRequest != _methodsRequest) return;
    selectedRegion.value = region;
    authService.updateBaseUrl(resolveRegionApiBaseUrl(selectedRegion.value));
    // WS 端点预写 ImService，注册成功后 applyAuthPayload 再以服务器返回值覆盖。
    if (!kIsWeb) {
      imService.updateWsEndpoint(resolveRegionWsUrl(selectedRegion.value));
    }
    unawaited(_refreshAuthMethods());
  }

  /// 切换区域：记住选择，取消进行中的请求，重置验证码状态，切换端点。
  void switchRegion(AppRegion region) {
    if (isLoading.value || selectedRegion.value == region) return;
    // Web 端：重定向到目标分区域名，浏览器整页跳转，不继续操作本地状态。
    if (redirectToRegionIfNeeded(region)) return;
    _regionEpoch++;
    selectedRegion.value = region;
    authService.updateBaseUrl(resolveRegionApiBaseUrl(region));
    AppStorageService.saveRegion(region.name);

    // 重置验证码倒计时，因为验证码是在旧区域服务器上发的，切换后无效
    _countdownTimer?.cancel();
    sendCodeCountdown.value = 0;
    errorMessage.value = 'region_switched_resend_code'.tr;
    // 跟登录页一致：先回落全关再拉新结果。
    authMethods.value = const AuthMethods.allDisabled();
    unawaited(_refreshAuthMethods());
  }

  Future<void> refreshAuthMethods() => _refreshAuthMethods();

  Future<void> _refreshAuthMethods() async {
    final request = ++_methodsRequest;
    final region = selectedRegion.value.name;
    authMethodsLoading.value = true;
    authMethodsFailed.value = false;
    try {
      final result = await authService.fetchAuthMethods(region: region);
      if (isClosed ||
          request != _methodsRequest ||
          region != selectedRegion.value.name) {
        return;
      }
      authMethods.value = result.ok && result.data != null
          ? result.data!
          : AuthMethods.allDisabled(region: region);
      authMethodsFailed.value = !result.ok || result.data == null;
    } catch (_) {
      if (isClosed ||
          request != _methodsRequest ||
          region != selectedRegion.value.name) {
        return;
      }
      authMethods.value = AuthMethods.allDisabled(region: region);
      authMethodsFailed.value = true;
    } finally {
      if (!isClosed &&
          request == _methodsRequest &&
          region == selectedRegion.value.name) {
        authMethodsLoading.value = false;
      }
    }
  }

  Future<void> sendEmailCode({required String email}) async {
    if (!canRequestEmailCode) return;

    final normalizedEmail = email.trim();

    if (!GetUtils.isEmail(normalizedEmail)) {
      errorMessage.value = 'auth_error_email_invalid'.tr;
      return;
    }

    isSendingCode.value = true;
    errorMessage.value = null;

    final requestRegionEpoch = _regionEpoch;
    ServiceResult<void> result;
    try {
      result = await authService.sendEmailCode(
        email: normalizedEmail,
        scene: 'register',
      );
    } catch (_) {
      result = ServiceResult<void>.failure(message: 'auth_send_code_failed'.tr);
    } finally {
      if (!isClosed) {
        isSendingCode.value = false;
      }
    }

    if (isClosed || requestRegionEpoch != _regionEpoch) return;
    if (!result.ok) {
      errorMessage.value = result.message.isEmpty
          ? 'auth_send_code_failed'.tr
          : result.message;
      return;
    }

    _startSendCodeCountdown();
    CustomToast.show('auth_send_code_success'.tr, isError: false);
  }

  Future<void> register({
    required String email,
    required String password,
    required String emailCode,
  }) async {
    if (isLoading.value || authMethodsLoading.value) return;
    if (!authMethods.value.registrationEnabled) {
      errorMessage.value = 'registration_closed'.tr;
      return;
    }

    final normalizedEmail = email.trim();
    final normalizedPassword = password.trim();
    final normalizedEmailCode = emailCode.trim();

    if (!GetUtils.isEmail(normalizedEmail)) {
      errorMessage.value = 'auth_error_email_invalid'.tr;
      return;
    }
    if (normalizedPassword.isEmpty) {
      errorMessage.value = 'auth_error_password_required'.tr;
      return;
    }
    if (normalizedEmailCode.isEmpty) {
      errorMessage.value = 'auth_error_email_code_required'.tr;
      return;
    }

    isLoading.value = true;
    errorMessage.value = null;

    final requestRegionEpoch = _regionEpoch;
    ServiceResult<void> result;
    try {
      result = await authService.register(
        email: normalizedEmail,
        password: normalizedPassword,
        emailCode: normalizedEmailCode,
        region: selectedRegion.value.name,
      );
    } catch (_) {
      result = ServiceResult<void>.failure(message: 'register_error_failed'.tr);
    } finally {
      if (!isClosed) {
        isLoading.value = false;
      }
    }

    if (isClosed || requestRegionEpoch != _regionEpoch) return;
    if (!result.ok) {
      errorMessage.value = result.message.isEmpty
          ? 'register_error_failed'.tr
          : result.message;
      return;
    }

    if (!authService.isLoggedIn) {
      errorMessage.value = 'register_error_failed'.tr;
      return;
    }

    // 注册成功后连接 WS。
    // applyAuthPayload 已将服务器返回的 ws_endpoint 写入 ImService；
    // 直接 ensureConnected()，不再重复读存储或计算 URL。
    if (!imService.isConnected) {
      imService.ensureConnected();
    }
    RootRouteNavigator.toHome();
  }

  void goToLogin() {
    if (Get.currentRoute != AppRoutes.login) {
      RootRouteNavigator.toLogin();
    }
  }

  void goToResetPassword() {
    if (Get.currentRoute != AppRoutes.resetPassword) {
      Get.toNamed(AppRoutes.resetPassword);
    }
  }

  void _startSendCodeCountdown() {
    _countdownTimer?.cancel();
    sendCodeCountdown.value = _sendCodeCooldownSec;
    _countdownTimer = Timer.periodic(const Duration(seconds: 1), (timer) {
      if (sendCodeCountdown.value <= 1) {
        sendCodeCountdown.value = 0;
        timer.cancel();
        return;
      }
      sendCodeCountdown.value -= 1;
    });
  }
}
