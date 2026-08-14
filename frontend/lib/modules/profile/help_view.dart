import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../shared/utils/app_external_links.dart';
import '../../shared/utils/toast_util.dart';

class HelpView extends StatelessWidget {
  const HelpView({super.key});

  Future<void> _openUrl(String rawUrl) async {
    final opened = await AppExternalLinks.open(rawUrl);
    if (!opened) {
      CustomToast.show('settings_link_unavailable'.tr);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        leading: IconButton(
          icon: const Icon(Icons.arrow_back_ios_rounded, size: 20),
          onPressed: () => Get.back(),
        ),
        title: Text('me_help'.tr),
      ),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          _HelpAction(
            title: 'help_support_title'.tr,
            subtitle: 'help_support_subtitle'.tr,
            onTap: () => _openUrl(AppExternalLinks.supportUrl),
          ),
          const SizedBox(height: 12),
          _HelpAction(
            title: 'privacy_policy_title'.tr,
            subtitle: 'privacy_policy_subtitle'.tr,
            onTap: () => _openUrl(AppExternalLinks.privacyPolicyUrl),
          ),
          const SizedBox(height: 12),
          _HelpAction(
            title: 'privacy_terms_title'.tr,
            subtitle: 'privacy_terms_subtitle'.tr,
            onTap: () => _openUrl(AppExternalLinks.termsOfServiceUrl),
          ),
        ],
      ),
    );
  }
}

class _HelpAction extends StatelessWidget {
  const _HelpAction({
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
