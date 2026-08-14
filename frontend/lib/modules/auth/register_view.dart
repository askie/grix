import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/routes/app_routes.dart';
import '../../shared/utils/toast_util.dart';
import '../../../shared/widgets/feature_gate.dart';
import 'controllers/register_controller.dart';
import 'widgets/app_agreement_consent_field.dart';
import 'widgets/auth_language_switcher.dart';
import 'widgets/region_switcher.dart';

class RegisterView extends StatefulWidget {
  const RegisterView({super.key});

  @override
  State<RegisterView> createState() => _RegisterViewState();
}

class _RegisterViewState extends State<RegisterView> {
  final RegisterController controller = Get.find<RegisterController>();
  final TextEditingController _emailController = TextEditingController();
  final TextEditingController _passwordController = TextEditingController();
  final TextEditingController _emailCodeController = TextEditingController();
  Worker? _errorMessageWorker;
  bool _isPasswordVisible = false;
  bool _hasAcceptedAppAgreement = true;
  String? _appAgreementErrorText;

  @override
  void initState() {
    super.initState();
    _errorMessageWorker = ever<String?>(controller.errorMessage, (message) {
      final normalizedMessage = message?.trim() ?? '';
      if (!mounted || normalizedMessage.isEmpty) {
        return;
      }

      CustomToast.show(normalizedMessage, isError: true);
    });
  }

  @override
  void dispose() {
    _errorMessageWorker?.dispose();
    _emailController.dispose();
    _passwordController.dispose();
    _emailCodeController.dispose();
    super.dispose();
  }

  void _submitRegister() {
    if (!_ensureAppAgreementAccepted()) {
      return;
    }
    controller.register(
      email: _emailController.text,
      password: _passwordController.text,
      emailCode: _emailCodeController.text,
    );
  }

  void _sendEmailCode() {
    if (!_ensureAppAgreementAccepted()) {
      return;
    }
    controller.sendEmailCode(email: _emailController.text);
  }

  void _updateAppAgreementAccepted(bool value) {
    setState(() {
      _hasAcceptedAppAgreement = value;
      if (value) {
        _appAgreementErrorText = null;
      }
    });
  }

  bool _ensureAppAgreementAccepted() {
    if (_hasAcceptedAppAgreement) {
      return true;
    }
    setState(() {
      _appAgreementErrorText = 'auth_app_agreement_required'.tr;
    });
    return false;
  }

  void _openAppAgreement() {
    Get.toNamed(AppRoutes.appAgreement);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      appBar: AppBar(
        title: Text('register_title'.tr),
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
                  Center(
                    child: ClipRRect(
                      borderRadius: BorderRadius.circular(16),
                      child: Image.asset(
                        'assets/icons/app_logo.png',
                        width: 84,
                        height: 84,
                        fit: BoxFit.cover,
                      ),
                    ),
                  ),
                  const SizedBox(height: 16),
                  Text(
                    'register_subtitle'.tr,
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
                      labelText: 'auth_password'.tr,
                      prefixIcon: const Icon(Icons.lock_outline),
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
                  const SizedBox(height: 12),
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
                          onSubmitted: (_) => _submitRegister(),
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
                              onPressed: disabled || !_hasAcceptedAppAgreement
                                  ? null
                                  : _sendEmailCode,
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
                  AppAgreementConsentField(
                    value: _hasAcceptedAppAgreement,
                    onChanged: _updateAppAgreementAccepted,
                    onOpenAgreement: _openAppAgreement,
                    errorText: _appAgreementErrorText,
                    enabled: !controller.isLoading.value,
                  ),
                  const SizedBox(height: 8),
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
                        onPressed:
                            controller.isLoading.value ||
                                !_hasAcceptedAppAgreement
                            ? null
                            : _submitRegister,
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
                            : Text('register_btn'.tr),
                      ),
                    ),
                  ),
                  const SizedBox(height: 8),
                  Obx(() {
                    // 跟塘主 SmsSettings.PhoneRegisterEnabled 联动：关闭后入口隐藏。
                    if (!controller.authMethods.value.phoneRegisterEnabled) {
                      return const SizedBox.shrink();
                    }
                    return Align(
                      alignment: Alignment.center,
                      child: TextButton(
                        onPressed: () => Get.offNamed(AppRoutes.phoneLogin),
                        child: Text('register_to_phone'.tr),
                      ),
                    );
                  }),
                  LayoutBuilder(
                    builder: (context, constraints) {
                      final useVerticalLayout = constraints.maxWidth < 320;
                      final loginButton = TextButton(
                        onPressed: controller.goToLogin,
                        child: Text('register_to_login'.tr),
                      );
                      final resetPasswordButton = TextButton(
                        onPressed: controller.goToResetPassword,
                        child: Text('register_to_reset'.tr),
                      );

                      if (useVerticalLayout) {
                        return Column(
                          crossAxisAlignment: CrossAxisAlignment.stretch,
                          children: [
                            Align(
                              alignment: Alignment.centerLeft,
                              child: loginButton,
                            ),
                            Align(
                              alignment: Alignment.centerRight,
                              child: resetPasswordButton,
                            ),
                          ],
                        );
                      }

                      return Row(
                        children: [
                          Expanded(
                            child: Align(
                              alignment: Alignment.centerLeft,
                              child: loginButton,
                            ),
                          ),
                          Expanded(
                            child: Align(
                              alignment: Alignment.centerRight,
                              child: resetPasswordButton,
                            ),
                          ),
                        ],
                      );
                    },
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
