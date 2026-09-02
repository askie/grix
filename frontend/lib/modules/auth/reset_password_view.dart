import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../shared/widgets/feature_gate.dart';
import 'controllers/reset_password_controller.dart';
import 'widgets/auth_language_switcher.dart';
import 'widgets/captcha_image.dart';
import 'widgets/region_switcher.dart';

class ResetPasswordView extends StatefulWidget {
  const ResetPasswordView({super.key});

  @override
  State<ResetPasswordView> createState() => _ResetPasswordViewState();
}

class _ResetPasswordViewState extends State<ResetPasswordView> {
  final ResetPasswordController controller =
      Get.find<ResetPasswordController>();
  final TextEditingController _emailController = TextEditingController();
  final TextEditingController _passwordController = TextEditingController();
  final TextEditingController _emailCodeController = TextEditingController();
  final TextEditingController _captchaController = TextEditingController();
  bool _isPasswordVisible = false;

  @override
  void dispose() {
    _emailController.dispose();
    _passwordController.dispose();
    _emailCodeController.dispose();
    _captchaController.dispose();
    super.dispose();
  }

  void _submitReset() {
    controller.resetPassword(
      email: _emailController.text,
      newPassword: _passwordController.text,
      emailCode: _emailCodeController.text,
    );
  }

  void _sendEmailCode() {
    controller.sendEmailCode(
      email: _emailController.text,
      captchaValue: _captchaController.text,
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      appBar: AppBar(
        title: Text('reset_title'.tr),
        centerTitle: false,
        actions: [
          FeatureGate(
            feature: 'region_select',
            child: RegionSwitcher(
              selectedRegion: controller.selectedRegion,
              onChanged: controller.switchRegion,
              compact: true,
            ),
          ),
          const Padding(
            padding: EdgeInsets.only(right: 12),
            child: Center(child: AuthLanguageSwitcher(compact: true)),
          ),
        ],
      ),
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(16),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 420),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Text(
                    'reset_subtitle'.tr,
                    textAlign: TextAlign.center,
                    style: theme.textTheme.bodyMedium?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                  const SizedBox(height: 20),
                  TextField(
                    controller: _emailController,
                    decoration: InputDecoration(
                      labelText: 'auth_email'.tr,
                      prefixIcon: const Icon(Icons.alternate_email_rounded),
                      border: OutlineInputBorder(
                        borderRadius: BorderRadius.circular(12),
                      ),
                    ),
                    keyboardType: TextInputType.emailAddress,
                    textInputAction: TextInputAction.next,
                  ),
                  const SizedBox(height: 12),
                  TextField(
                    controller: _passwordController,
                    decoration: InputDecoration(
                      labelText: 'reset_new_password'.tr,
                      prefixIcon: const Icon(Icons.lock_reset_outlined),
                      suffixIcon: IconButton(
                        onPressed: () {
                          setState(() {
                            _isPasswordVisible = !_isPasswordVisible;
                          });
                        },
                        icon: Icon(
                          _isPasswordVisible
                              ? Icons.visibility_off_outlined
                              : Icons.visibility_outlined,
                        ),
                      ),
                      border: OutlineInputBorder(
                        borderRadius: BorderRadius.circular(12),
                      ),
                    ),
                    obscureText: !_isPasswordVisible,
                    textInputAction: TextInputAction.next,
                  ),
                  Obx(() {
                    if (!controller.canRequestEmailCode) {
                      return const SizedBox(height: 12);
                    }
                    return Column(
                      children: [
                        const SizedBox(height: 12),
                        Row(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Expanded(
                              child: TextField(
                                controller: _captchaController,
                                decoration: InputDecoration(
                                  labelText: 'auth_captcha'.tr,
                                  border: OutlineInputBorder(
                                    borderRadius: BorderRadius.circular(12),
                                  ),
                                ),
                                textInputAction: TextInputAction.next,
                              ),
                            ),
                            const SizedBox(width: 12),
                            Stack(
                              alignment: Alignment.center,
                              children: [
                                CaptchaImage(b64s: controller.captchaB64.value),
                                if (controller.isLoadingCaptcha.value)
                                  const SizedBox(
                                    width: 24,
                                    height: 24,
                                    child: CircularProgressIndicator(
                                      strokeWidth: 2,
                                    ),
                                  ),
                              ],
                            ),
                            IconButton(
                              onPressed: controller.refreshCaptcha,
                              icon: const Icon(Icons.refresh_rounded),
                              tooltip: 'auth_refresh_captcha'.tr,
                            ),
                          ],
                        ),
                        const SizedBox(height: 12),
                      ],
                    );
                  }),
                  Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Expanded(
                        child: TextField(
                          controller: _emailCodeController,
                          decoration: InputDecoration(
                            labelText: 'auth_email_code'.tr,
                            prefixIcon: const Icon(
                              Icons.mark_email_read_outlined,
                            ),
                            border: OutlineInputBorder(
                              borderRadius: BorderRadius.circular(12),
                            ),
                          ),
                          textInputAction: TextInputAction.done,
                          onSubmitted: (_) => _submitReset(),
                        ),
                      ),
                      const SizedBox(width: 12),
                      IntrinsicWidth(
                        child: Obx(() {
                          final disabled = !controller.canRequestEmailCode;
                          final label = controller.sendCodeCountdown.value > 0
                              ? '${controller.sendCodeCountdown.value}s'
                              : 'auth_send_code_btn'.tr;
                          return SizedBox(
                            height: 44,
                            child: OutlinedButton(
                              onPressed: disabled ? null : _sendEmailCode,
                              child: controller.isSendingCode.value
                                  ? const SizedBox(
                                      width: 16,
                                      height: 16,
                                      child: CircularProgressIndicator(
                                        strokeWidth: 2,
                                      ),
                                    )
                                  : Text(label),
                            ),
                          );
                        }),
                      ),
                    ],
                  ),
                  const SizedBox(height: 16),
                  Obx(() {
                    if (controller.errorMessage.value == null) {
                      return const SizedBox.shrink();
                    }
                    return Container(
                      padding: const EdgeInsets.all(12),
                      margin: const EdgeInsets.only(bottom: 16),
                      decoration: BoxDecoration(
                        color: theme.colorScheme.errorContainer,
                        borderRadius: BorderRadius.circular(8),
                      ),
                      child: Text(
                        controller.errorMessage.value!,
                        style: TextStyle(
                          color: theme.colorScheme.onErrorContainer,
                        ),
                      ),
                    );
                  }),
                  Obx(
                    () => SizedBox(
                      height: 44,
                      child: ElevatedButton(
                        onPressed: controller.isLoading.value
                            ? null
                            : _submitReset,
                        style: ElevatedButton.styleFrom(
                          shape: RoundedRectangleBorder(
                            borderRadius: BorderRadius.circular(12),
                          ),
                        ),
                        child: controller.isLoading.value
                            ? const SizedBox(
                                width: 24,
                                height: 24,
                                child: CircularProgressIndicator(
                                  strokeWidth: 2,
                                ),
                              )
                            : Text('reset_btn'.tr),
                      ),
                    ),
                  ),
                  const SizedBox(height: 8),
                  Row(
                    children: [
                      TextButton(
                        onPressed: controller.goToLogin,
                        child: Text('reset_to_login'.tr),
                      ),
                      const Spacer(),
                      TextButton(
                        onPressed: controller.goToRegister,
                        child: Text('reset_to_register'.tr),
                      ),
                    ],
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
