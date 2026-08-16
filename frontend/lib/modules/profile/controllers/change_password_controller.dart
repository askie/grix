import 'dart:async';

import 'package:get/get.dart';

import '../../../app/routes/app_routes.dart';
import '../../../app/routes/root_route_navigator.dart';
import '../../../data/providers/auth_service.dart';
import '../../../shared/utils/toast_util.dart';

class ChangePasswordController extends GetxController {
  static const int _sendCodeCooldownSec = 60;

  ChangePasswordController({AuthService? authService})
    : authService = authService ?? Get.find<AuthService>();

  final AuthService authService;

  final RxBool isLoading = false.obs;
  final RxBool isSendingCode = false.obs;
  final RxnString errorMessage = RxnString();
  final RxInt sendCodeCountdown = 0.obs;

  Timer? _countdownTimer;

  @override
  void onClose() {
    _countdownTimer?.cancel();
    super.onClose();
  }

  Future<void> sendEmailCode() async {
    if (isSendingCode.value || sendCodeCountdown.value > 0) return;

    isSendingCode.value = true;
    errorMessage.value = null;

    final result = await authService.sendChangePasswordEmailCode();
    if (isClosed) return;

    isSendingCode.value = false;
    if (!result.ok) {
      errorMessage.value = result.message.isEmpty
          ? 'me_change_password_send_code_failed'.tr
          : result.message;
      return;
    }

    _startSendCodeCountdown();
    CustomToast.show('auth_send_code_success'.tr, isError: false);
  }

  Future<void> changePassword({
    required String newPassword,
    required String confirmPassword,
    required String emailCode,
  }) async {
    if (isLoading.value) return;

    final normalizedPassword = newPassword.trim();
    final normalizedConfirmPassword = confirmPassword.trim();
    final normalizedEmailCode = emailCode.trim();
    if (normalizedPassword.isEmpty) {
      errorMessage.value = 'auth_error_password_required'.tr;
      return;
    }
    if (normalizedConfirmPassword.isEmpty) {
      errorMessage.value = 'auth_error_password_required'.tr;
      return;
    }
    if (normalizedPassword != normalizedConfirmPassword) {
      errorMessage.value = 'auth_error_password_mismatch'.tr;
      return;
    }
    if (normalizedEmailCode.isEmpty) {
      errorMessage.value = 'auth_error_email_code_required'.tr;
      return;
    }

    isLoading.value = true;
    errorMessage.value = null;

    final result = await authService.changeOwnPassword(
      newPassword: normalizedPassword,
      emailCode: normalizedEmailCode,
    );
    if (isClosed) return;

    isLoading.value = false;
    if (!result.ok) {
      errorMessage.value = result.message.isEmpty
          ? 'me_change_password_failed'.tr
          : result.message;
      return;
    }

    CustomToast.show('me_change_password_success'.tr, isError: false);
    await authService.logout(notifyServer: false);
    if (Get.currentRoute != AppRoutes.login) {
      RootRouteNavigator.toLogin();
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
