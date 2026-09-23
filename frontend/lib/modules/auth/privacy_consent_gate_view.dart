import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';

import '../../app/routes/app_routes.dart';
import '../../shared/services/privacy_consent_store.dart';
import 'privacy_policy_view.dart';
import 'user_agreement_view.dart';

/// Full-screen first-launch privacy gate shown before [GrixApp] on Android.
///
/// Blocks interaction with the rest of the app until the user accepts.
/// Declining exits the process via [SystemNavigator.pop].
class PrivacyConsentGateView extends StatelessWidget {
  const PrivacyConsentGateView({
    super.key,
    required this.onAccepted,
    this.onDeclined,
  });

  final Future<void> Function() onAccepted;
  final VoidCallback? onDeclined;

  Future<void> _handleAgree() async {
    await PrivacyConsentStore.acceptCurrentVersion();
    await onAccepted();
  }

  void _handleDisagree() {
    final custom = onDeclined;
    if (custom != null) {
      custom();
      return;
    }
    SystemNavigator.pop();
  }

  void _openUserAgreement(BuildContext context) {
    // Prefer GetX routes when GrixApp's navigator is active; fall back to a
    // local MaterialPageRoute for the pre-GrixApp MaterialApp gate.
    if (Get.key.currentState != null) {
      Get.toNamed(AppRoutes.userAgreement);
      return;
    }
    Navigator.of(
      context,
    ).push(MaterialPageRoute<void>(builder: (_) => const UserAgreementView()));
  }

  void _openPrivacyPolicy(BuildContext context) {
    if (Get.key.currentState != null) {
      Get.toNamed(AppRoutes.privacyPolicy);
      return;
    }
    Navigator.of(
      context,
    ).push(MaterialPageRoute<void>(builder: (_) => const PrivacyPolicyView()));
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      key: const Key('privacy_consent_gate'),
      backgroundColor: theme.colorScheme.surface,
      body: SafeArea(
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 420),
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 16),
              child: Material(
                elevation: 6,
                borderRadius: BorderRadius.circular(16),
                color: theme.colorScheme.surfaceContainerHighest,
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(20, 20, 20, 16),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      Text(
                        'privacy_consent_title'.tr,
                        key: const Key('privacy_consent_title'),
                        style: theme.textTheme.titleLarge?.copyWith(
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                      const SizedBox(height: 12),
                      ConstrainedBox(
                        constraints: BoxConstraints(
                          maxHeight: MediaQuery.of(context).size.height * 0.5,
                        ),
                        child: SingleChildScrollView(
                          child: Text.rich(
                            TextSpan(
                              style: theme.textTheme.bodyMedium?.copyWith(
                                height: 1.5,
                              ),
                              children: [
                                TextSpan(
                                  text: 'privacy_consent_body_prefix'.tr,
                                ),
                                WidgetSpan(
                                  alignment: PlaceholderAlignment.baseline,
                                  baseline: TextBaseline.alphabetic,
                                  child: _InlineLink(
                                    key: const Key(
                                      'privacy_consent_user_agreement_link',
                                    ),
                                    label: 'privacy_consent_user_agreement'.tr,
                                    onTap: () => _openUserAgreement(context),
                                  ),
                                ),
                                TextSpan(text: 'privacy_consent_body_mid'.tr),
                                WidgetSpan(
                                  alignment: PlaceholderAlignment.baseline,
                                  baseline: TextBaseline.alphabetic,
                                  child: _InlineLink(
                                    key: const Key(
                                      'privacy_consent_privacy_policy_link',
                                    ),
                                    label: 'privacy_consent_privacy_policy'.tr,
                                    onTap: () => _openPrivacyPolicy(context),
                                  ),
                                ),
                                TextSpan(
                                  text: 'privacy_consent_body_suffix'.tr,
                                ),
                              ],
                            ),
                          ),
                        ),
                      ),
                      const SizedBox(height: 20),
                      Row(
                        children: [
                          Expanded(
                            child: OutlinedButton(
                              key: const Key('privacy_consent_disagree_button'),
                              onPressed: _handleDisagree,
                              child: Text('privacy_consent_disagree'.tr),
                            ),
                          ),
                          const SizedBox(width: 12),
                          Expanded(
                            child: FilledButton(
                              key: const Key('privacy_consent_agree_button'),
                              onPressed: () {
                                unawaited(_handleAgree());
                              },
                              child: Text('privacy_consent_agree'.tr),
                            ),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _InlineLink extends StatelessWidget {
  const _InlineLink({super.key, required this.label, required this.onTap});

  final String label;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return GestureDetector(
      onTap: onTap,
      child: Text(
        label,
        style: theme.textTheme.bodyMedium?.copyWith(
          color: theme.colorScheme.primary,
          height: 1.5,
          decoration: TextDecoration.underline,
          decorationColor: theme.colorScheme.primary,
        ),
      ),
    );
  }
}
