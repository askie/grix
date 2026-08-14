// 手机号无密码短信登录注册控制器。
//
// 与 LoginController 完全平行：发码 / 验码登录 / 倒计时；
// login-code 接口幂等（账号不存在则自动注册），所以 UI 不区分"登录"和"注册"。
import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';

import '../../../app/routes/app_routes.dart';
import '../../../data/providers/auth_service.dart';
import '../../../shared/utils/toast_util.dart';

/// 控制器支持两种模式：login（默认，幂等登录或注册）和 bind（已登录用户绑定手机号）。
/// 通过路由 arguments 传入 `{'mode': 'bind'}` 触发 bind 模式。
enum PhoneFlowMode { login, bind }

class PhoneLoginController extends GetxController {
  PhoneLoginController({AuthService? authService, PhoneFlowMode? mode})
    : authService = authService ?? Get.find<AuthService>(),
      _initialMode = mode;

  final AuthService authService;
  final PhoneFlowMode? _initialMode;

  PhoneFlowMode get mode {
    if (_initialMode != null) return _initialMode;
    final args = Get.arguments;
    if (args is Map && args['mode'] == 'bind') {
      return PhoneFlowMode.bind;
    }
    return PhoneFlowMode.login;
  }

  bool get isBindMode => mode == PhoneFlowMode.bind;

  /// 国家区号选项；CN 默认 +86，全球区默认 +1。
  /// 维护一个简短的常用 country 列表；不引入第三方 country_code_picker package
  /// 避免增加依赖体积（区号库通常 100KB+）。
  static const List<({String code, String name})> commonCountries = [
    (code: '+86', name: '中国大陆'),
    (code: '+852', name: '中国香港'),
    (code: '+853', name: '中国澳门'),
    (code: '+886', name: '中国台湾'),
    (code: '+65', name: '新加坡'),
    (code: '+60', name: '马来西亚'),
    (code: '+81', name: '日本'),
    (code: '+82', name: '韩国'),
    (code: '+1', name: 'USA / Canada'),
    (code: '+44', name: 'United Kingdom'),
    (code: '+49', name: 'Germany'),
    (code: '+33', name: 'France'),
    (code: '+61', name: 'Australia'),
    (code: '+64', name: 'New Zealand'),
    (code: '+91', name: 'India'),
    (code: '+971', name: 'UAE'),
    (code: '+966', name: 'Saudi Arabia'),
    (code: '+34', name: 'Spain'),
    (code: '+39', name: 'Italy'),
    (code: '+55', name: 'Brazil'),
    (code: '+52', name: 'Mexico'),
  ];

  final countryCode = '+86'.obs;
  final phone = ''.obs;
  final code = ''.obs;
  final sending = false.obs;
  final loggingIn = false.obs;
  final cooldownRemaining = 0.obs;

  /// 图形验证码：60 秒内对同一手机号第 2 次起，后端会要求带 captcha。
  /// captchaRequired=true 时 UI 展开图片+输入框；首次发送 captchaRequired=false。
  final captchaRequired = false.obs;
  final captchaLoading = false.obs;
  final captchaId = ''.obs;
  final captchaB64 = ''.obs;
  final captchaValue = ''.obs;

  Timer? _cooldownTimer;

  /// 当前区域的认证能力开关；deep link 直进时按这个兜底，
  /// 塘主关掉对应区域的手机登录后，发码按钮 disable + 显示提示。
  final authMethods = const AuthMethods.allDisabled().obs;
  final authMethodsLoaded = false.obs;

  @override
  void onInit() {
    super.onInit();
    _refreshAuthMethods();
  }

  Future<void> _refreshAuthMethods() async {
    // bind 模式不受 login/register 开关控制（已登录用户的主动绑定）；
    // 但还是同步拉一次以便未来扩展，UI 只在 login 模式下使用该字段。
    final region = countryCode.value == '+86' ? 'cn' : 'global';
    final result = await authService.fetchAuthMethods(region: region);
    if (result.ok && result.data != null) {
      authMethods.value = result.data!;
    } else {
      authMethods.value = AuthMethods.allDisabled(region: region);
    }
    authMethodsLoaded.value = true;
  }

  /// 标准化 E.164：拼接 countryCode + 用户输入的纯数字号码。
  String get phoneE164 {
    final digits = phone.value.replaceAll(RegExp(r'[^0-9]'), '');
    if (digits.isEmpty) return '';
    return '${countryCode.value}$digits';
  }

  /// 当前区域是否允许手机号登录；bind 模式恒为 true（已登录用户绑定不受该开关控制）。
  bool get phoneLoginAllowed {
    if (isBindMode) return true;
    return authMethods.value.phoneLoginEnabled;
  }

  bool get canSendCode =>
      !sending.value &&
      cooldownRemaining.value <= 0 &&
      phoneE164.length >= 8 &&
      phoneLoginAllowed;

  bool get canSubmit =>
      !loggingIn.value &&
      code.value.trim().length == 6 &&
      phoneE164.length >= 8;

  @override
  void onClose() {
    _cooldownTimer?.cancel();
    super.onClose();
  }

  /// 默认按区域设置当前 countryCode：cn → +86 / 其他 → +1。
  /// 切区号会跨越能力开关边界，需要重新拉一次 authMethods。
  void setDefaultCountryFromRegion(String region) {
    if (region.toLowerCase() == 'cn') {
      countryCode.value = '+86';
    } else {
      countryCode.value = '+1';
    }
  }

  /// 后端返回 captcha required 的业务码（与 backend/internal/api/handler/auth_phone.go 保持一致）。
  static const int _captchaRequiredCode = 10010;

  Future<void> sendCode() async {
    if (!canSendCode) return;
    // captchaRequired 状态下必须先填图形验证码
    if (captchaRequired.value && captchaValue.value.trim().isEmpty) {
      CustomToast.show('auth_error_captcha_required'.tr, isError: true);
      return;
    }
    sending.value = true;
    try {
      final result = await authService.sendSmsCode(
        phoneE164: phoneE164,
        scene: isBindMode ? 'bind' : 'login',
        captchaId: captchaRequired.value ? captchaId.value : null,
        captchaValue: captchaRequired.value ? captchaValue.value.trim() : null,
      );
      if (result.ok) {
        _startCooldown(60);
        // 发送成功后清空 captcha 输入但保留 captchaRequired（后端的 require 标记会持续 1h）
        captchaValue.value = '';
        await _refreshCaptcha();
        CustomToast.show('phone_login_code_sent_body'.tr, isError: false);
        return;
      }
      // 后端返回 captcha required → 展开 captcha UI 并拉一张图
      if (result.code == _captchaRequiredCode) {
        captchaRequired.value = true;
        await _refreshCaptcha();
        CustomToast.show('auth_error_captcha_required'.tr, isError: true);
        return;
      }
      // 其他失败：若 captcha 已展开，刷新一次图（防止验证码输错被消费）
      if (captchaRequired.value) {
        captchaValue.value = '';
        await _refreshCaptcha();
      }
      CustomToast.show(
        result.message.isNotEmpty ? result.message : 'auth_send_code_failed'.tr,
        isError: true,
      );
    } catch (e) {
      debugPrint('phone send code error: $e');
      CustomToast.show('auth_send_code_failed'.tr, isError: true);
    } finally {
      sending.value = false;
    }
  }

  /// 主动刷新图形验证码（点击图片或后端要求时调用）。
  Future<void> refreshCaptcha() async {
    captchaValue.value = '';
    await _refreshCaptcha();
  }

  Future<void> _refreshCaptcha() async {
    if (captchaLoading.value) return;
    captchaLoading.value = true;
    try {
      final result = await authService.fetchCaptcha();
      if (result.ok && result.data != null) {
        captchaId.value = result.data!.captchaId;
        captchaB64.value = result.data!.b64s;
      } else {
        captchaId.value = '';
        captchaB64.value = '';
      }
    } catch (e) {
      debugPrint('phone captcha fetch error: $e');
      captchaId.value = '';
      captchaB64.value = '';
    } finally {
      captchaLoading.value = false;
    }
  }

  Future<void> submit() async {
    if (!canSubmit) return;
    loggingIn.value = true;
    try {
      if (isBindMode) {
        final result = await authService.bindPhone(
          phoneE164: phoneE164,
          code: code.value.trim(),
        );
        if (result.ok) {
          // 刷新一次 profile，让 user.phoneE164 写到本地缓存，下次启动不再弹引导。
          await authService.fetchCurrentUserProfile();
          Get.back(result: true);
          CustomToast.show('phone_bind_success_body'.tr, isError: false);
        } else {
          CustomToast.show(
            result.message.isNotEmpty ? result.message : 'phone_bind_failed'.tr,
            isError: true,
          );
        }
      } else {
        final result = await authService.phoneLoginWithCode(
          phoneE164: phoneE164,
          code: code.value.trim(),
        );
        if (result.ok) {
          // login-code 已写入 token + user，与邮箱登录一致跳 home。
          Get.offAllNamed(AppRoutes.home);
        } else {
          CustomToast.show(
            result.message.isNotEmpty
                ? result.message
                : 'login_error_failed'.tr,
            isError: true,
          );
        }
      }
    } catch (e) {
      debugPrint('phone submit error: $e');
      CustomToast.show(
        isBindMode ? 'phone_bind_failed'.tr : 'login_error_failed'.tr,
        isError: true,
      );
    } finally {
      loggingIn.value = false;
    }
  }

  void _startCooldown(int seconds) {
    _cooldownTimer?.cancel();
    cooldownRemaining.value = seconds;
    _cooldownTimer = Timer.periodic(const Duration(seconds: 1), (t) {
      final next = cooldownRemaining.value - 1;
      if (next <= 0) {
        cooldownRemaining.value = 0;
        t.cancel();
      } else {
        cooldownRemaining.value = next;
      }
    });
  }
}

/// 输入框限制为纯数字。
class DigitsOnlyFormatter extends TextInputFormatter {
  @override
  TextEditingValue formatEditUpdate(
    TextEditingValue oldValue,
    TextEditingValue newValue,
  ) {
    final cleaned = newValue.text.replaceAll(RegExp(r'[^0-9]'), '');
    return TextEditingValue(
      text: cleaned,
      selection: TextSelection.collapsed(offset: cleaned.length),
    );
  }
}
