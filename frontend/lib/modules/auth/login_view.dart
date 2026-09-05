import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:qr_flutter/qr_flutter.dart';

import '../../app/routes/app_routes.dart';
import '../../shared/utils/app_region_config.dart';
import 'controllers/login_controller.dart';
import 'controllers/qr_login_controller.dart';
import 'services/desktop_qr_login_visibility.dart';
import 'services/login_entry_mode.dart';
import 'widgets/app_agreement_consent_field.dart';
import 'widgets/auth_language_switcher.dart';
import '../../../shared/widgets/feature_gate.dart';
import 'widgets/region_switcher.dart';

class LoginView extends StatefulWidget {
  const LoginView({super.key});

  @override
  State<LoginView> createState() => _LoginViewState();
}

class _LoginViewState extends State<LoginView> {
  static const _showSocialLogin = true;

  final LoginController controller = Get.find<LoginController>();
  final QrLoginController qrLoginController = Get.find<QrLoginController>();
  final TextEditingController _accountController = TextEditingController();
  final TextEditingController _passwordController = TextEditingController();
  bool _qrFlowStarted = false;
  bool _isDesktopQrExpanded = false;
  bool _isPasswordObscured = true;
  bool _hasAcceptedAppAgreement = true;
  bool _rememberCredentials = true;
  String? _appAgreementErrorText;

  @override
  void initState() {
    super.initState();
    _restoreSavedCredentials();
  }

  @override
  void dispose() {
    qrLoginController.stopDesktopFlow();
    _accountController.dispose();
    _passwordController.dispose();
    super.dispose();
  }

  Future<void> _restoreSavedCredentials({bool force = false}) async {
    final savedState = await controller.loadSavedCredentials();
    if (!mounted) {
      return;
    }
    if (force || _accountController.text.isEmpty) {
      _accountController.value = TextEditingValue(
        text: savedState.account,
        selection: TextSelection.collapsed(offset: savedState.account.length),
      );
    }
    if (force || _passwordController.text.isEmpty) {
      _passwordController.value = TextEditingValue(
        text: savedState.password,
        selection: TextSelection.collapsed(offset: savedState.password.length),
      );
    }
  }

  void _onRegionChanged(dynamic region) {
    controller.switchRegion(region);
    _restoreSavedCredentials(force: true);
  }

  void _submitLogin() {
    if (!_ensureAppAgreementAccepted()) {
      return;
    }
    controller.login(
      account: _accountController.text,
      password: _passwordController.text,
      saveCredentials: _rememberCredentials,
    );
  }

  void _updateAppAgreementAccepted(bool value) {
    setState(() {
      _hasAcceptedAppAgreement = value;
      if (value) {
        _appAgreementErrorText = null;
        return;
      }
      _isDesktopQrExpanded = false;
    });
  }

  bool _ensureAppAgreementAccepted() {
    if (_hasAcceptedAppAgreement) {
      return true;
    }
    setState(() {
      _appAgreementErrorText = 'auth_app_agreement_required'.tr;
      _isDesktopQrExpanded = false;
    });
    return false;
  }

  void _openAppAgreement() {
    Get.toNamed(AppRoutes.appAgreement);
  }

  bool _isTabletLayout(BuildContext context) {
    return MediaQuery.of(context).size.shortestSide >=
        desktopQrLoginTabletMinShortestSide;
  }

  bool _shouldShowDesktopQrLogin(BuildContext context) {
    final width = MediaQuery.of(context).size.width;
    return shouldShowDesktopQrLogin(
      isWeb: kIsWeb,
      isMobile: GetPlatform.isMobile,
      isDesktop: GetPlatform.isDesktop,
      isTablet: _isTabletLayout(context),
      width: width,
    );
  }

  String _qrLoginDeviceLabel(BuildContext context) {
    if (!kIsWeb && !GetPlatform.isDesktop && _isTabletLayout(context)) {
      return 'tablet';
    }
    return 'web_desktop';
  }

  String _qrStatusText(String status) {
    switch (status.trim()) {
      case 'pending_scan':
        return 'login_qr_status_pending_scan'.tr;
      case 'scanned':
        return 'login_qr_status_scanned'.tr;
      case 'confirmed':
        return 'login_qr_status_confirmed'.tr;
      case 'canceled':
        return 'login_qr_status_canceled'.tr;
      case 'expired':
        return 'login_qr_status_expired'.tr;
      case 'consumed':
        return 'login_qr_status_consumed'.tr;
      default:
        return status;
    }
  }

  Widget _buildSocialLoginDivider(ThemeData theme) {
    return Row(
      children: [
        const Expanded(child: Divider()),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 12),
          child: Text(
            'login_third_party_divider'.tr,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
        ),
        const Expanded(child: Divider()),
      ],
    );
  }

  Widget _buildGoogleLoginButton(ThemeData theme) {
    return Obx(() {
      final isLoading = controller.isLoading.value;
      return SizedBox(
        height: 44,
        child: OutlinedButton.icon(
          onPressed: isLoading || !_hasAcceptedAppAgreement
              ? null
              : controller.loginWithGoogle,
          icon: const Icon(Icons.g_mobiledata_rounded, size: 22),
          label: Text('login_google_btn'.tr),
          style: OutlinedButton.styleFrom(
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(12),
            ),
          ),
        ),
      );
    });
  }

  Widget _buildAppleLoginButton(ThemeData theme) {
    return Obx(() {
      final isLoading = controller.isLoading.value;
      return SizedBox(
        height: 44,
        child: OutlinedButton.icon(
          onPressed: isLoading || !_hasAcceptedAppAgreement
              ? null
              : controller.loginWithApple,
          icon: const Icon(Icons.apple_rounded, size: 22),
          label: Text('login_apple_btn'.tr),
          style: OutlinedButton.styleFrom(
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(12),
            ),
          ),
        ),
      );
    });
  }

  Widget _buildDesktopQrLoginCard(ThemeData theme) {
    return Obx(() {
      final isLoading = qrLoginController.isLoading.value;
      final qrText = qrLoginController.qrText.value?.trim() ?? '';
      final statusRaw = qrLoginController.status.value?.trim() ?? '';
      final statusText = statusRaw.isEmpty
          ? ''
          : _qrStatusText(statusRaw).trim();
      final scannerName = qrLoginController.scannerNickname.value?.trim() ?? '';
      final errorText = qrLoginController.errorMessage.value?.trim() ?? '';

      return Container(
        padding: const EdgeInsets.all(14),
        decoration: BoxDecoration(
          color: theme.colorScheme.surface,
          borderRadius: BorderRadius.circular(12),
          border: Border.all(
            color: theme.colorScheme.outline.withValues(alpha: 0.2),
          ),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(
              'login_qr_title'.tr,
              style: theme.textTheme.titleMedium?.copyWith(
                fontWeight: FontWeight.w600,
              ),
            ),
            const SizedBox(height: 8),
            Text(
              'login_qr_hint'.tr,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
            const SizedBox(height: 12),
            Center(
              child: Container(
                width: 220,
                height: 220,
                decoration: BoxDecoration(
                  color: Colors.white,
                  borderRadius: BorderRadius.circular(12),
                ),
                alignment: Alignment.center,
                child: isLoading
                    ? const SizedBox(
                        width: 28,
                        height: 28,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : qrText.isEmpty
                    ? Icon(
                        Icons.qr_code_2_rounded,
                        size: 72,
                        color: theme.colorScheme.onSurfaceVariant,
                      )
                    : QrImageView(
                        data: qrText,
                        size: 190,
                        backgroundColor: Colors.white,
                      ),
              ),
            ),
            const SizedBox(height: 12),
            if (statusText.isNotEmpty)
              Text(
                statusText,
                textAlign: TextAlign.center,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            if (scannerName.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text(
                  'login_qr_scanned_by'.trParams({'name': scannerName}),
                  textAlign: TextAlign.center,
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ),
            if (errorText.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 6),
                child: Text(
                  errorText,
                  textAlign: TextAlign.center,
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.error,
                  ),
                ),
              ),
            const SizedBox(height: 8),
            SizedBox(
              height: 38,
              child: OutlinedButton.icon(
                onPressed: isLoading
                    ? null
                    : () {
                        qrLoginController.refreshDesktopFlow(
                          deviceLabel: _qrLoginDeviceLabel(context),
                        );
                      },
                icon: const Icon(Icons.refresh_rounded, size: 16),
                label: Text('login_qr_refresh'.tr),
              ),
            ),
          ],
        ),
      );
    });
  }

  Widget _buildAuthErrorBanner(ThemeData theme) {
    return Obx(() {
      final message = controller.errorMessage.value;
      if (message == null) {
        return const SizedBox.shrink();
      }

      return Container(
        padding: const EdgeInsets.all(12),
        margin: const EdgeInsets.only(bottom: 16),
        decoration: BoxDecoration(
          color: theme.colorScheme.errorContainer,
          borderRadius: BorderRadius.circular(8),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.error_outline, color: theme.colorScheme.error),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    message,
                    style: TextStyle(color: theme.colorScheme.onErrorContainer),
                  ),
                ),
              ],
            ),
            if (controller.showCrossRegionHint.value)
              _buildCrossRegionHint(theme),
          ],
        ),
      );
    });
  }

  /// 密码被拒时的跨区引导：账号可能注册在另一区域，一键切换重试。
  Widget _buildCrossRegionHint(ThemeData theme) {
    final otherRegionName = controller.otherRegion == AppRegion.cn
        ? 'region_cn'.tr
        : 'region_global'.tr;
    return Padding(
      padding: const EdgeInsets.only(top: 8, left: 32),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'login_cross_region_hint'.tr,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onErrorContainer,
            ),
          ),
          TextButton(
            onPressed: controller.switchToOtherRegion,
            style: TextButton.styleFrom(
              padding: EdgeInsets.zero,
              minimumSize: const Size(0, 32),
              tapTargetSize: MaterialTapTargetSize.shrinkWrap,
            ),
            child: Text(
              'login_cross_region_switch_btn'.trParams({
                'region': otherRegionName,
              }),
            ),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final showDesktopQrLogin = _shouldShowDesktopQrLogin(context);
    final shouldRunDesktopQrFlow = showDesktopQrLogin && _isDesktopQrExpanded;
    final isQrScanEntry =
        resolveLoginEntryMode(
          routeParameters: Get.parameters,
          baseUri: Uri.base,
        ) ==
        LoginEntryMode.qrScan;
    final hideCredentialForm = isQrScanEntry || shouldRunDesktopQrFlow;

    if (shouldRunDesktopQrFlow && !_qrFlowStarted) {
      _qrFlowStarted = true;
      unawaited(
        qrLoginController.startDesktopFlow(
          deviceLabel: _qrLoginDeviceLabel(context),
        ),
      );
    } else if (!shouldRunDesktopQrFlow && _qrFlowStarted) {
      _qrFlowStarted = false;
      qrLoginController.stopDesktopFlow();
    }

    return Scaffold(
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(16),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 420),
              child: Column(
                mainAxisAlignment: MainAxisAlignment.center,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  const Align(
                    alignment: Alignment.centerRight,
                    child: AuthLanguageSwitcher(),
                  ),
                  const SizedBox(height: 8),
                  FeatureGate(
                    feature: 'region_select',
                    child: Align(
                      alignment: Alignment.centerRight,
                      child: RegionSwitcher(
                        selectedRegion: controller.selectedRegion,
                        onChanged: _onRegionChanged,
                        compact: true,
                      ),
                    ),
                  ),
                  const SizedBox(height: 16),
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
                    'app_name'.tr,
                    textAlign: TextAlign.center,
                    style: theme.textTheme.headlineMedium?.copyWith(
                      fontWeight: FontWeight.bold,
                    ),
                  ),
                  const SizedBox(height: 8),
                  Text(
                    'login_subtitle'.tr,
                    textAlign: TextAlign.center,
                    style: theme.textTheme.bodyMedium?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                  const SizedBox(height: 24),
                  if (!hideCredentialForm) ...[
                    TextField(
                      controller: _accountController,
                      decoration: InputDecoration(
                        labelText: 'login_account'.tr,
                        prefixIcon: const Icon(Icons.person_outline),
                        border: OutlineInputBorder(
                          borderRadius: BorderRadius.circular(12),
                        ),
                      ),
                      textInputAction: TextInputAction.next,
                    ),
                    const SizedBox(height: 12),
                    TextField(
                      controller: _passwordController,
                      decoration: InputDecoration(
                        labelText: 'login_password'.tr,
                        prefixIcon: const Icon(Icons.lock_outline),
                        suffixIcon: IconButton(
                          onPressed: () {
                            setState(() {
                              _isPasswordObscured = !_isPasswordObscured;
                            });
                          },
                          icon: Icon(
                            _isPasswordObscured
                                ? Icons.visibility_outlined
                                : Icons.visibility_off_outlined,
                          ),
                        ),
                        border: OutlineInputBorder(
                          borderRadius: BorderRadius.circular(12),
                        ),
                      ),
                      obscureText: _isPasswordObscured,
                      textInputAction: TextInputAction.done,
                      onSubmitted: (_) => _submitLogin(),
                    ),
                    const SizedBox(height: 4),
                    Align(
                      alignment: Alignment.centerLeft,
                      child: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          SizedBox(
                            height: 36,
                            child: Checkbox(
                              value: _rememberCredentials,
                              onChanged: (v) {
                                setState(() {
                                  _rememberCredentials = v ?? false;
                                });
                              },
                            ),
                          ),
                          Flexible(
                            child: GestureDetector(
                              onTap: () {
                                setState(() {
                                  _rememberCredentials = !_rememberCredentials;
                                });
                              },
                              child: Text(
                                'login_remember_credentials'.tr,
                                style: theme.textTheme.bodySmall,
                              ),
                            ),
                          ),
                        ],
                      ),
                    ),
                  ],
                  _buildAuthErrorBanner(theme),
                  AppAgreementConsentField(
                    value: _hasAcceptedAppAgreement,
                    onChanged: _updateAppAgreementAccepted,
                    onOpenAgreement: _openAppAgreement,
                    errorText: _appAgreementErrorText,
                    enabled: !controller.isLoading.value,
                  ),
                  const SizedBox(height: 8),
                  if (!hideCredentialForm) ...[
                    Obx(
                      () => Center(
                        child: SizedBox(
                          width: 200,
                          child: ElevatedButton(
                            onPressed:
                                controller.isLoading.value ||
                                    !_hasAcceptedAppAgreement
                                ? null
                                : _submitLogin,
                            child: controller.isLoading.value
                                ? const SizedBox(
                                    width: 24,
                                    height: 24,
                                    child: CircularProgressIndicator(
                                      strokeWidth: 2,
                                    ),
                                  )
                                : Text(
                                    'login_btn'.tr,
                                    style: const TextStyle(fontSize: 16),
                                  ),
                          ),
                        ),
                      ),
                    ),
                    const SizedBox(height: 12),
                  ],
                  if (_showSocialLogin && !hideCredentialForm) ...[
                    _buildSocialLoginDivider(theme),
                    const SizedBox(height: 12),
                    FeatureGate(
                      feature: 'auth_google_login',
                      child: _buildGoogleLoginButton(theme),
                    ),
                    if (GetPlatform.isIOS || GetPlatform.isMacOS) ...[
                      const SizedBox(height: 8),
                      FeatureGate(
                        feature: 'auth_apple_login',
                        child: _buildAppleLoginButton(theme),
                      ),
                    ],
                  ],
                  if (!hideCredentialForm) ...[
                    const SizedBox(height: 4),
                    Obx(() {
                      final isLoading = controller.isLoading.value;
                      // 跟塘主 SmsSettings.PhoneLoginEnabled 联动：关闭后入口隐藏。
                      final phoneLoginVisible =
                          controller.authMethods.value.phoneLoginEnabled;
                      return LayoutBuilder(
                        builder: (context, constraints) {
                          final useVerticalLayout = constraints.maxWidth < 280;
                          final phoneLoginButton = phoneLoginVisible
                              ? TextButton(
                                  onPressed: isLoading
                                      ? null
                                      : () => Get.toNamed(AppRoutes.phoneLogin),
                                  child: Text('login_to_phone'.tr),
                                )
                              : const SizedBox.shrink();
                          final registerButton = FeatureGate(
                            feature: 'auth_register',
                            child: TextButton(
                              onPressed: isLoading
                                  ? null
                                  : controller.goToRegister,
                              child: Text('login_to_register'.tr),
                            ),
                          );
                          final resetPasswordButton = TextButton(
                            onPressed: isLoading
                                ? null
                                : controller.goToResetPassword,
                            child: Text('login_to_reset'.tr),
                          );

                          if (useVerticalLayout) {
                            return Column(
                              crossAxisAlignment: CrossAxisAlignment.stretch,
                              children: [
                                Align(
                                  alignment: Alignment.center,
                                  child: phoneLoginButton,
                                ),
                                Align(
                                  alignment: Alignment.centerLeft,
                                  child: registerButton,
                                ),
                                Align(
                                  alignment: Alignment.centerRight,
                                  child: resetPasswordButton,
                                ),
                              ],
                            );
                          }

                          return Column(
                            children: [
                              Align(
                                alignment: Alignment.center,
                                child: phoneLoginButton,
                              ),
                              Row(
                                children: [
                                  Expanded(
                                    child: Align(
                                      alignment: Alignment.centerLeft,
                                      child: registerButton,
                                    ),
                                  ),
                                  Expanded(
                                    child: Align(
                                      alignment: Alignment.centerRight,
                                      child: resetPasswordButton,
                                    ),
                                  ),
                                ],
                              ),
                            ],
                          );
                        },
                      );
                    }),
                    const SizedBox(height: 4),
                  ],
                  if (showDesktopQrLogin) ...[
                    const SizedBox(height: 14),
                    SizedBox(
                      height: 40,
                      child: OutlinedButton.icon(
                        onPressed: !_hasAcceptedAppAgreement
                            ? null
                            : () {
                                setState(() {
                                  _isDesktopQrExpanded = !_isDesktopQrExpanded;
                                });
                              },
                        icon: Icon(
                          _isDesktopQrExpanded
                              ? Icons.expand_less_rounded
                              : Icons.qr_code_2_rounded,
                          size: 18,
                        ),
                        label: Text(
                          _isDesktopQrExpanded
                              ? 'login_qr_toggle_hide'.tr
                              : 'login_qr_toggle_show'.tr,
                        ),
                      ),
                    ),
                    if (_isDesktopQrExpanded) ...[
                      const SizedBox(height: 10),
                      _buildDesktopQrLoginCard(theme),
                    ],
                  ],
                  const SizedBox(height: 16),
                  Text(
                    'login_hint'.tr,
                    textAlign: TextAlign.center,
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
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
