// 手机号无密码短信登录注册页。
//
// 单页主链路：区号选择 → 输入手机号 → 发码 → 输入 6 位码 → 一键登录/注册。
// login-code 接口幂等：账号不存在自动注册，已存在直接登录。

import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';

import '../../app/routes/app_routes.dart';
import 'controllers/phone_login_controller.dart';

class PhoneLoginView extends StatefulWidget {
  const PhoneLoginView({super.key});

  @override
  State<PhoneLoginView> createState() => _PhoneLoginViewState();
}

class _PhoneLoginViewState extends State<PhoneLoginView> {
  late final PhoneLoginController controller;
  final TextEditingController _phoneController = TextEditingController();
  final TextEditingController _codeController = TextEditingController();
  final TextEditingController _captchaController = TextEditingController();

  @override
  void initState() {
    super.initState();
    controller = Get.find<PhoneLoginController>();
  }

  @override
  void dispose() {
    _phoneController.dispose();
    _codeController.dispose();
    _captchaController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final isBind = controller.isBindMode;
    return Scaffold(
      appBar: AppBar(
        title: Text(isBind ? 'phone_bind_title'.tr : 'phone_login_title'.tr),
        elevation: 0,
      ),
      body: SafeArea(
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 16),
          child: SingleChildScrollView(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                const SizedBox(height: 24),
                Text(
                  isBind
                      ? 'phone_bind_subtitle'.tr
                      : 'phone_login_subtitle'.tr,
                  style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                ),
                const SizedBox(height: 16),
                _buildDisabledBanner(context, isBind),
                const SizedBox(height: 8),
                _buildPhoneRow(context),
                const SizedBox(height: 16),
                _buildCaptchaRow(context),
                _buildCodeRow(context),
                const SizedBox(height: 24),
                _buildSubmitButton(context, isBind),
                const SizedBox(height: 16),
                if (!isBind)
                  TextButton(
                    onPressed: () => Get.offNamed(AppRoutes.login),
                    child: Text('phone_login_switch_to_email'.tr),
                  ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildPhoneRow(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SizedBox(
          width: 130,
          child: Obx(() {
            return DropdownButtonFormField<String>(
              initialValue: controller.countryCode.value,
              decoration: InputDecoration(
                labelText: 'phone_login_country_code'.tr,
                isDense: true,
              ),
              items: PhoneLoginController.commonCountries
                  .map(
                    (c) => DropdownMenuItem(
                      value: c.code,
                      child: Text('${c.code}  ${c.nameKey.tr}'),
                    ),
                  )
                  .toList(),
              selectedItemBuilder: (_) => PhoneLoginController.commonCountries
                  .map((c) => Align(
                        alignment: Alignment.centerLeft,
                        child: Text(c.code),
                      ))
                  .toList(),
              onChanged: (v) {
                if (v != null) controller.countryCode.value = v;
              },
            );
          }),
        ),
        const SizedBox(width: 12),
        Expanded(
          child: TextField(
            controller: _phoneController,
            keyboardType: TextInputType.phone,
            inputFormatters: [DigitsOnlyFormatter()],
            decoration: InputDecoration(
              labelText: 'phone_login_phone_label'.tr,
              hintText: 'phone_login_phone_hint'.tr,
              isDense: true,
            ),
            onChanged: (v) => controller.phone.value = v,
          ),
        ),
      ],
    );
  }

  Widget _buildDisabledBanner(BuildContext context, bool isBind) {
    if (isBind) return const SizedBox.shrink();
    return Obx(() {
      // 还没拉到结果时不显示 banner，避免闪烁；拉到且不允许时才提示。
      if (!controller.authMethodsLoaded.value) return const SizedBox.shrink();
      if (controller.phoneLoginAllowed) return const SizedBox.shrink();
      final theme = Theme.of(context);
      return Container(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
        decoration: BoxDecoration(
          color: theme.colorScheme.errorContainer.withValues(alpha: 0.3),
          borderRadius: BorderRadius.circular(8),
          border: Border.all(
            color: theme.colorScheme.error.withValues(alpha: 0.3),
          ),
        ),
        child: Row(
          children: [
            Icon(Icons.info_outline,
                size: 18, color: theme.colorScheme.error),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                'phone_login_disabled_region'.tr,
                style: TextStyle(
                  fontSize: 13,
                  color: theme.colorScheme.onErrorContainer,
                ),
              ),
            ),
          ],
        ),
      );
    });
  }

  Widget _buildCaptchaRow(BuildContext context) {
    return Obx(() {
      if (!controller.captchaRequired.value) return const SizedBox.shrink();
      final b64 = controller.captchaB64.value;
      Widget image;
      if (b64.isEmpty) {
        image = SizedBox(
          width: 110,
          height: 44,
          child: Center(
            child: controller.captchaLoading.value
                ? const SizedBox(
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : Text(
                    'captcha_fetch_failed'.tr,
                    style: const TextStyle(fontSize: 11),
                  ),
          ),
        );
      } else {
        try {
          final stripped = b64.contains(',') ? b64.split(',').last : b64;
          image = Image.memory(
            base64Decode(stripped),
            width: 110,
            height: 44,
            fit: BoxFit.fill,
            gaplessPlayback: true,
          );
        } catch (_) {
          image = const SizedBox(width: 110, height: 44);
        }
      }
      return Padding(
        padding: const EdgeInsets.only(bottom: 16),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.center,
          children: [
            Expanded(
              child: TextField(
                controller: _captchaController,
                decoration: InputDecoration(
                  labelText: 'auth_captcha'.tr,
                  hintText: 'auth_error_captcha_required'.tr,
                  isDense: true,
                ),
                onChanged: (v) => controller.captchaValue.value = v,
              ),
            ),
            const SizedBox(width: 12),
            GestureDetector(
              onTap: controller.captchaLoading.value
                  ? null
                  : () {
                      _captchaController.clear();
                      controller.refreshCaptcha();
                    },
              child: ClipRRect(
                borderRadius: BorderRadius.circular(6),
                child: image,
              ),
            ),
          ],
        ),
      );
    });
  }

  Widget _buildCodeRow(BuildContext context) {
    return Row(
      children: [
        Expanded(
          child: TextField(
            controller: _codeController,
            keyboardType: TextInputType.number,
            inputFormatters: [
              FilteringTextInputFormatter.digitsOnly,
              LengthLimitingTextInputFormatter(6),
            ],
            decoration: InputDecoration(
              labelText: 'phone_login_code_label'.tr,
              hintText: '6',
              isDense: true,
            ),
            onChanged: (v) => controller.code.value = v,
          ),
        ),
        const SizedBox(width: 12),
        SizedBox(
          width: 130,
          child: Obx(() {
            final cooldown = controller.cooldownRemaining.value;
            final enabled = controller.canSendCode;
            return OutlinedButton(
              onPressed: enabled ? controller.sendCode : null,
              child: controller.sending.value
                  ? const SizedBox(
                      width: 16,
                      height: 16,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : Text(
                      cooldown > 0
                          ? '${cooldown}s'
                          : 'phone_login_send_code'.tr,
                    ),
            );
          }),
        ),
      ],
    );
  }

  Widget _buildSubmitButton(BuildContext context, bool isBind) {
    return Obx(() {
      return FilledButton(
        style: FilledButton.styleFrom(
          minimumSize: const Size.fromHeight(48),
        ),
        onPressed: controller.canSubmit ? controller.submit : null,
        child: controller.loggingIn.value
            ? const SizedBox(
                width: 18,
                height: 18,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            : Text(isBind ? 'phone_bind_submit'.tr : 'phone_login_submit'.tr),
      );
    });
  }
}
