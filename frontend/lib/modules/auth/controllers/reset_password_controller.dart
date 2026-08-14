import 'dart:async';

import 'package:get/get.dart';

import '../../../app/routes/app_routes.dart';
import '../../../app/routes/root_route_navigator.dart';
import '../../../data/providers/auth_service.dart';
import '../../../shared/utils/app_region_config.dart';
import '../../../shared/utils/app_storage_service.dart';
import '../../../shared/utils/toast_util.dart';
import '../../../shared/utils/web_region_redirect.dart';

class ResetPasswordController extends GetxController {
  static const int _sendCodeCooldownSec = 300;

  final AuthService authService = Get.find<AuthService>();

  final isLoading = false.obs;
  final isSendingCode = false.obs;
  final isLoadingCaptcha = false.obs;
  final errorMessage = RxnString();
  final captchaId = ''.obs;
  final captchaB64 = ''.obs;
  final sendCodeCountdown = 0.obs;
  final selectedRegion = AppRegion.cn.obs;

  Timer? _countdownTimer;

  bool get canRequestEmailCode =>
      !isSendingCode.value && sendCodeCountdown.value <= 0;

  @override
  void onInit() {
    super.onInit();
    _initRegion();
    refreshCaptcha();
  }

  @override
  void onClose() {
    _countdownTimer?.cancel();
    super.onClose();
  }

  Future<void> _initRegion() async {
    // 初始区域：沿用用户记住的选择，否则按系统语言推断（海外默认全球区）。
    selectedRegion.value = await resolveInitialRegion();
    authService.updateBaseUrl(resolveRegionApiBaseUrl(selectedRegion.value));
  }

  void switchRegion(AppRegion region) {
    if (selectedRegion.value == region) return;
    // Web 端：重定向到目标分区域名，浏览器整页跳转，不继续操作本地状态。
    if (redirectToRegionIfNeeded(region)) return;
    selectedRegion.value = region;
    authService.updateBaseUrl(resolveRegionApiBaseUrl(region));
    AppStorageService.saveRegion(region.name);
    // 重置验证码倒计时
    _countdownTimer?.cancel();
    sendCodeCountdown.value = 0;
    errorMessage.value = 'region_switched_resend_code'.tr;
    refreshCaptcha();
  }

  Future<void> refreshCaptcha() async {
    if (isLoadingCaptcha.value) return;

    isLoadingCaptcha.value = true;
    final result = await authService.fetchCaptcha();
    if (isClosed) return;

    isLoadingCaptcha.value = false;
    if (!result.ok || result.data == null) {
      errorMessage.value = result.message.isEmpty
          ? 'captcha_fetch_failed'.tr
          : result.message;
      return;
    }

    captchaId.value = result.data!.captchaId;
    captchaB64.value = result.data!.b64s;
  }

  Future<void> sendEmailCode({
    required String email,
    required String captchaValue,
  }) async {
    if (!canRequestEmailCode) return;

    final normalizedEmail = email.trim();
    final normalizedCaptcha = captchaValue.trim();

    if (!GetUtils.isEmail(normalizedEmail)) {
      errorMessage.value = 'auth_error_email_invalid'.tr;
      return;
    }
    if (captchaId.value.trim().isEmpty) {
      errorMessage.value = 'captcha_fetch_failed'.tr;
      await refreshCaptcha();
      return;
    }
    if (normalizedCaptcha.isEmpty) {
      errorMessage.value = 'auth_error_captcha_required'.tr;
      return;
    }

    isSendingCode.value = true;
    errorMessage.value = null;

    final result = await authService.sendEmailCode(
      email: normalizedEmail,
      scene: 'reset',
      captchaId: captchaId.value,
      captchaValue: normalizedCaptcha,
    );

    if (isClosed) return;

    isSendingCode.value = false;
    if (!result.ok) {
      errorMessage.value = result.message.isEmpty
          ? 'auth_send_code_failed'.tr
          : result.message;
      await refreshCaptcha();
      return;
    }

    _startSendCodeCountdown();
    await refreshCaptcha();
    CustomToast.show('auth_send_code_success'.tr, isError: false);
  }

  Future<void> resetPassword({
    required String email,
    required String newPassword,
    required String emailCode,
  }) async {
    if (isLoading.value) return;

    final normalizedEmail = email.trim();
    final normalizedPassword = newPassword.trim();
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

    final result = await authService.resetPassword(
      email: normalizedEmail,
      newPassword: normalizedPassword,
      emailCode: normalizedEmailCode,
    );

    if (isClosed) return;

    isLoading.value = false;
    if (!result.ok) {
      errorMessage.value = result.message.isEmpty
          ? 'reset_error_failed'.tr
          : result.message;
      return;
    }

    CustomToast.show('reset_success'.tr, isError: false);
    RootRouteNavigator.toLogin();
  }

  void goToLogin() {
    if (Get.currentRoute != AppRoutes.login) {
      RootRouteNavigator.toLogin();
    }
  }

  void goToRegister() {
    if (Get.currentRoute != AppRoutes.register) {
      Get.toNamed(AppRoutes.register);
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
