import 'package:flutter/material.dart';
import 'package:get/get.dart';

import 'models/privacy_policy_content.dart';

class PrivacyPolicyView extends StatelessWidget {
  const PrivacyPolicyView({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      key: const Key('privacy_policy_page'),
      appBar: AppBar(
        title: Text('privacy_policy_page_title'.tr),
        centerTitle: false,
      ),
      body: SafeArea(
        child: SelectionArea(
          child: ListView(
            padding: const EdgeInsets.all(20),
            children: [
              _PrivacyPolicyHero(theme: theme),
              const SizedBox(height: 16),
              for (
                var index = 0;
                index < PrivacyPolicyContent.sections.length;
                index++
              )
                Padding(
                  padding: EdgeInsets.only(
                    bottom: index == PrivacyPolicyContent.sections.length - 1
                        ? 0
                        : 16,
                  ),
                  child: _PrivacyPolicySectionCard(
                    index: index + 1,
                    section: PrivacyPolicyContent.sections[index],
                    theme: theme,
                  ),
                ),
              const SizedBox(height: 16),
              Text(
                'privacy_policy_page_footer'.tr,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurface.withValues(alpha: 0.64),
                  height: 1.5,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _PrivacyPolicyHero extends StatelessWidget {
  const _PrivacyPolicyHero({required this.theme});

  final ThemeData theme;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        color: theme.colorScheme.primaryContainer,
        borderRadius: BorderRadius.circular(20),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'privacy_policy_page_title'.tr,
            key: const Key('privacy_policy_page_title'),
            style: theme.textTheme.headlineSmall?.copyWith(
              fontWeight: FontWeight.w700,
              color: theme.colorScheme.onPrimaryContainer,
            ),
          ),
          const SizedBox(height: 10),
          Text(
            'privacy_policy_page_subtitle'.tr,
            style: theme.textTheme.bodyMedium?.copyWith(
              color: theme.colorScheme.onPrimaryContainer.withValues(
                alpha: 0.86,
              ),
              height: 1.5,
            ),
          ),
          const SizedBox(height: 8),
          Text(
            'privacy_policy_page_effective_date'.tr,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onPrimaryContainer.withValues(
                alpha: 0.72,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _PrivacyPolicySectionCard extends StatelessWidget {
  const _PrivacyPolicySectionCard({
    required this.index,
    required this.section,
    required this.theme,
  });

  final int index;
  final PrivacyPolicySectionData section;
  final ThemeData theme;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(18),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(
          color: theme.colorScheme.outline.withValues(alpha: 0.16),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                width: 28,
                height: 28,
                alignment: Alignment.center,
                decoration: BoxDecoration(
                  color: theme.colorScheme.secondaryContainer,
                  borderRadius: BorderRadius.circular(999),
                ),
                child: Text(
                  '$index',
                  style: theme.textTheme.labelLarge?.copyWith(
                    fontWeight: FontWeight.w700,
                    color: theme.colorScheme.onSecondaryContainer,
                  ),
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  section.titleKey.tr,
                  style: theme.textTheme.titleMedium?.copyWith(
                    fontWeight: FontWeight.w700,
                  ),
                ),
              ),
            ],
          ),
          for (final key in section.paragraphKeys)
            Padding(
              padding: const EdgeInsets.only(top: 12),
              child: Text(
                key.tr,
                style: theme.textTheme.bodyMedium?.copyWith(height: 1.6),
              ),
            ),
          for (final key in section.bulletKeys)
            Padding(
              padding: const EdgeInsets.only(top: 10),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Padding(
                    padding: const EdgeInsets.only(top: 6),
                    child: Icon(
                      Icons.circle,
                      size: 8,
                      color: theme.colorScheme.primary,
                    ),
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Text(
                      key.tr,
                      style: theme.textTheme.bodyMedium?.copyWith(height: 1.6),
                    ),
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}
