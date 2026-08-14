import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../data/providers/app_update_service.dart';
import '../../shared/utils/app_external_links.dart';
import '../../shared/utils/toast_util.dart';
import '../../shared/widgets/app_version_text.dart';

class AboutView extends StatefulWidget {
  const AboutView({super.key});

  @override
  State<AboutView> createState() => _AboutViewState();
}

class _AboutViewState extends State<AboutView> {
  bool _checkingUpdate = false;

  Future<void> _openUrl(String rawUrl) async {
    final opened = await AppExternalLinks.open(rawUrl);
    if (!opened) {
      CustomToast.show('settings_link_unavailable'.tr);
    }
  }

  /// 点版本号 = 主动检查更新。检查期间禁用重复点击，否则连点会叠出多个弹窗。
  Future<void> _checkUpdate() async {
    if (_checkingUpdate) return;
    if (!Get.isRegistered<AppUpdateService>()) {
      CustomToast.show('update_check_failed'.tr);
      return;
    }
    setState(() => _checkingUpdate = true);
    try {
      await Get.find<AppUpdateService>().checkForUpdateInteractive();
    } finally {
      if (mounted) setState(() => _checkingUpdate = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      appBar: AppBar(
        leading: IconButton(
          icon: const Icon(Icons.arrow_back_ios_rounded, size: 20),
          onPressed: () => Get.back(),
        ),
        title: Text('me_about'.tr),
      ),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          Container(
            padding: const EdgeInsets.all(16),
            decoration: BoxDecoration(
              color: theme.colorScheme.surface,
              borderRadius: BorderRadius.circular(16),
            ),
            child: Column(
              children: [
                ClipRRect(
                  borderRadius: BorderRadius.circular(16),
                  child: Image.asset(
                    'assets/icons/app_logo.png',
                    width: 72,
                    height: 72,
                    fit: BoxFit.cover,
                  ),
                ),
                const SizedBox(height: 12),
                Text(
                  'app_name'.tr,
                  style: const TextStyle(
                    fontSize: 18,
                    fontWeight: FontWeight.w700,
                  ),
                ),
                const SizedBox(height: 6),
                InkWell(
                  onTap: _checkingUpdate ? null : _checkUpdate,
                  borderRadius: BorderRadius.circular(8),
                  child: Padding(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 12,
                      vertical: 6,
                    ),
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        AppVersionText(
                          style: TextStyle(
                            fontSize: 13,
                            color: theme.colorScheme.secondary,
                          ),
                        ),
                        const SizedBox(width: 6),
                        if (_checkingUpdate)
                          SizedBox(
                            width: 12,
                            height: 12,
                            child: CircularProgressIndicator(
                              strokeWidth: 1.6,
                              color: theme.colorScheme.secondary,
                            ),
                          )
                        else
                          Icon(
                            Icons.refresh_rounded,
                            size: 14,
                            color: theme.colorScheme.secondary,
                          ),
                      ],
                    ),
                  ),
                ),
                const SizedBox(height: 2),
                Text(
                  'update_check_hint'.tr,
                  style: TextStyle(
                    fontSize: 11,
                    color: theme.colorScheme.secondary.withValues(alpha: 0.7),
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(height: 12),
          _AboutAction(
            title: 'privacy_policy_title'.tr,
            subtitle: 'privacy_policy_subtitle'.tr,
            onTap: () => _openUrl(AppExternalLinks.privacyPolicyUrl),
          ),
          const SizedBox(height: 12),
          _AboutAction(
            title: 'privacy_terms_title'.tr,
            subtitle: 'privacy_terms_subtitle'.tr,
            onTap: () => _openUrl(AppExternalLinks.termsOfServiceUrl),
          ),
          const SizedBox(height: 12),
          _AboutAction(
            title: 'help_support_title'.tr,
            subtitle: 'help_support_subtitle'.tr,
            onTap: () => _openUrl(AppExternalLinks.supportUrl),
          ),
        ],
      ),
    );
  }
}

class _AboutAction extends StatelessWidget {
  const _AboutAction({
    required this.title,
    required this.subtitle,
    required this.onTap,
  });

  final String title;
  final String subtitle;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Material(
      color: theme.colorScheme.surface,
      borderRadius: BorderRadius.circular(16),
      child: InkWell(
        borderRadius: BorderRadius.circular(16),
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      title,
                      style: const TextStyle(
                        fontWeight: FontWeight.w700,
                        fontSize: 15,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      subtitle,
                      style: TextStyle(
                        fontSize: 13,
                        color: theme.colorScheme.secondary,
                      ),
                    ),
                  ],
                ),
              ),
              Icon(
                Icons.open_in_new_rounded,
                color: theme.colorScheme.secondary,
              ),
            ],
          ),
        ),
      ),
    );
  }
}
