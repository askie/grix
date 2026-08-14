import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../shared/utils/app_external_links.dart';
import '../../shared/utils/toast_util.dart';
import 'widgets/delete_account_dialog.dart';

class PrivacyView extends StatelessWidget {
  const PrivacyView({super.key});

  Future<void> _openUrl(String rawUrl) async {
    final opened = await AppExternalLinks.open(rawUrl);
    if (!opened) {
      CustomToast.show('settings_link_unavailable'.tr);
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
        title: Text('me_privacy'.tr),
      ),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          _SectionCard(
            title: 'privacy_data_summary_title'.tr,
            children: const [
              _BulletLine(translationKey: 'privacy_data_summary_account'),
              _BulletLine(translationKey: 'privacy_data_summary_messages'),
              _BulletLine(translationKey: 'privacy_data_summary_ai_agents'),
              _BulletLine(translationKey: 'privacy_data_summary_avatar'),
              _BulletLine(translationKey: 'privacy_data_summary_push'),
            ],
          ),
          const SizedBox(height: 12),
          _SectionCard(
            title: 'privacy_controls_title'.tr,
            children: [
              _ActionLine(
                title: 'privacy_policy_title'.tr,
                subtitle: 'privacy_policy_subtitle'.tr,
                onTap: () => _openUrl(AppExternalLinks.privacyPolicyUrl),
              ),
              _ActionLine(
                title: 'privacy_terms_title'.tr,
                subtitle: 'privacy_terms_subtitle'.tr,
                onTap: () => _openUrl(AppExternalLinks.termsOfServiceUrl),
              ),
              _ActionLine(
                title: 'privacy_delete_web_title'.tr,
                subtitle: 'privacy_delete_web_subtitle'.tr,
                onTap: () => _openUrl(AppExternalLinks.accountDeletionUrl),
              ),
            ],
          ),
          const SizedBox(height: 12),
          _SectionCard(
            title: 'account_delete_title'.tr,
            children: [
              Text(
                'privacy_delete_in_app_hint'.tr,
                style: TextStyle(
                  fontSize: 13,
                  color: theme.colorScheme.secondary,
                ),
              ),
              const SizedBox(height: 12),
              SizedBox(
                width: double.infinity,
                child: ElevatedButton(
                  style: ElevatedButton.styleFrom(
                    backgroundColor: const Color(0xFFB3261E),
                    foregroundColor: Colors.white,
                    padding: const EdgeInsets.symmetric(vertical: 14),
                  ),
                  onPressed: () => showDeleteAccountDialog(context),
                  child: Text('account_delete_action'.tr),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _SectionCard extends StatelessWidget {
  const _SectionCard({
    required this.title,
    required this.children,
  });

  final String title;
  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            title,
            style: const TextStyle(
              fontSize: 15,
              fontWeight: FontWeight.w700,
            ),
          ),
          const SizedBox(height: 12),
          ...children,
        ],
      ),
    );
  }
}

class _BulletLine extends StatelessWidget {
  const _BulletLine({required this.translationKey});

  final String translationKey;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: const EdgeInsets.only(top: 6),
            child: Container(
              width: 6,
              height: 6,
              decoration: BoxDecoration(
                color: theme.colorScheme.primary,
                shape: BoxShape.circle,
              ),
            ),
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Text(
              translationKey.tr,
              style: TextStyle(
                fontSize: 13,
                color: theme.colorScheme.secondary,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _ActionLine extends StatelessWidget {
  const _ActionLine({
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
    return ListTile(
      contentPadding: EdgeInsets.zero,
      title: Text(
        title,
        style: const TextStyle(fontWeight: FontWeight.w600),
      ),
      subtitle: Text(subtitle),
      trailing: Icon(
        Icons.open_in_new_rounded,
        color: theme.colorScheme.secondary,
      ),
      onTap: onTap,
    );
  }
}
