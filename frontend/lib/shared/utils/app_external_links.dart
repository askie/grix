import 'app_runtime_endpoints.dart';
import 'package:get/get.dart';
import 'package:url_launcher/url_launcher.dart';

import 'package:grix/data/services/link_safety_service.dart';
import 'package:grix/shared/widgets/link_interstitial.dart';

class AppExternalLinks {
  static const String _configuredPrivacyPolicyUrl = String.fromEnvironment(
    'AIBOT_PRIVACY_POLICY_URL',
    defaultValue: '',
  );
  static const String _configuredTermsOfServiceUrl = String.fromEnvironment(
    'AIBOT_TERMS_URL',
    defaultValue: '',
  );
  static const String _configuredSupportUrl = String.fromEnvironment(
    'AIBOT_SUPPORT_URL',
    defaultValue: '',
  );
  static const String _configuredAccountDeletionUrl = String.fromEnvironment(
    'AIBOT_ACCOUNT_DELETION_URL',
    defaultValue: '',
  );

  static String get privacyPolicyUrl => _resolve(
    configuredUrl: _configuredPrivacyPolicyUrl,
    fallbackPath: '/legal/privacy-policy',
  );

  static String get termsOfServiceUrl => _resolve(
    configuredUrl: _configuredTermsOfServiceUrl,
    fallbackPath: '/legal/terms-of-service',
  );

  static String get supportUrl =>
      _resolve(configuredUrl: _configuredSupportUrl, fallbackPath: '/support');

  static String get accountDeletionUrl => _resolve(
    configuredUrl: _configuredAccountDeletionUrl,
    fallbackPath: '/legal/account-deletion',
  );

  static String _resolve({
    required String configuredUrl,
    required String fallbackPath,
  }) {
    final normalizedConfiguredUrl = configuredUrl.trim();
    if (normalizedConfiguredUrl.isNotEmpty) {
      return normalizedConfiguredUrl;
    }
    return _buildFromApiBase(fallbackPath);
  }

  static String _buildFromApiBase(String fallbackPath) {
    final origin = AppRuntimeEndpoints.publicOrigin.trim();
    if (origin.isEmpty) {
      return '';
    }
    return '$origin$fallbackPath';
  }

  /// 打开外部链接。先经 LinkSafetyService 校验：
  /// - malicious -> 全屏中间页拦死，不放行；
  /// - suspicious -> 中间页提示，用户显式确认才放行；
  /// - clean -> 直接打开。
  /// 校验链路或服务未注册时降级为直接打开，避免误伤正常体验。
  static Future<bool> open(String rawUrl) async {
    final normalized = rawUrl.trim();
    if (normalized.isEmpty) {
      return false;
    }
    final uri = Uri.tryParse(normalized);
    if (uri == null) {
      return false;
    }

    // 仅对 http/https 外链做黑名单校验；mailto/tel/grix 等非 Web 链接直接放行打开，
    // 这些 scheme 无 host、不在黑名单语义范围内，送去校验只会徒增无意义请求。
    final scheme = uri.scheme.toLowerCase();
    final isWebLink = scheme == 'http' || scheme == 'https';

    if (isWebLink && Get.isRegistered<LinkSafetyService>()) {
      final verdict = await Get.find<LinkSafetyService>().check(normalized);
      switch (verdict.level) {
        case LinkVerdictLevel.malicious:
          // 黑名单(恶意)链接：直接静默不响应——不打开、不弹任何中间页（产品决策）。
          return false;
        case LinkVerdictLevel.suspicious:
          final proceed = await LinkInterstitial.showWarning(
            normalized,
            verdict,
          );
          if (!proceed) return false;
          break;
        case LinkVerdictLevel.clean:
          break;
      }
    }

    return launchUrl(uri, mode: LaunchMode.externalApplication);
  }
}
