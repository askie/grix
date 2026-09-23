import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../shared/widgets/app_dialog_style.dart';

/// Shows the user agreement / privacy policy consent dialog used on
/// registration entry points. Resolves to `true` only when the user taps
/// "Agree"; declining resolves to `false`.
Future<bool> showAppAgreementConsentDialog(
  BuildContext context, {
  required VoidCallback onOpenUserAgreement,
  required VoidCallback onOpenPrivacyPolicy,
}) async {
  final theme = Theme.of(context);
  final linkStyle = theme.textTheme.bodyMedium?.copyWith(
    color: theme.colorScheme.primary,
    fontWeight: FontWeight.w600,
    height: 1.5,
  );

  WidgetSpan link(Key key, String label, VoidCallback onTap) {
    return WidgetSpan(
      alignment: PlaceholderAlignment.baseline,
      baseline: TextBaseline.alphabetic,
      child: GestureDetector(
        key: key,
        onTap: onTap,
        child: Text(label, style: linkStyle),
      ),
    );
  }

  final accepted = await showAppContentDialog<bool>(
    context: context,
    barrierDismissible: false,
    title: 'privacy_consent_title'.tr,
    content: Text.rich(
      key: const Key('auth_app_agreement_dialog'),
      TextSpan(
        style: theme.textTheme.bodyMedium?.copyWith(height: 1.5),
        children: [
          TextSpan(text: 'privacy_consent_body_prefix'.tr),
          link(
            const Key('auth_app_agreement_dialog_user_agreement_link'),
            'privacy_consent_user_agreement'.tr,
            onOpenUserAgreement,
          ),
          TextSpan(text: 'privacy_consent_body_mid'.tr),
          link(
            const Key('auth_app_agreement_dialog_privacy_policy_link'),
            'privacy_consent_privacy_policy'.tr,
            onOpenPrivacyPolicy,
          ),
          TextSpan(text: 'auth_register_consent_body_suffix'.tr),
        ],
      ),
    ),
    actions: [
      Builder(
        builder: (ctx) => TextButton(
          key: const Key('auth_app_agreement_dialog_disagree_button'),
          onPressed: () => Navigator.of(ctx).pop(false),
          child: Text('privacy_consent_disagree'.tr),
        ),
      ),
      Builder(
        builder: (ctx) => FilledButton(
          key: const Key('auth_app_agreement_dialog_agree_button'),
          onPressed: () => Navigator.of(ctx).pop(true),
          child: Text('privacy_consent_agree'.tr),
        ),
      ),
    ],
  );
  return accepted ?? false;
}
