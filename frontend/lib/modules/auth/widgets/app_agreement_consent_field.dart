import 'package:flutter/material.dart';
import 'package:get/get.dart';

class AppAgreementConsentField extends StatelessWidget {
  const AppAgreementConsentField({
    super.key,
    required this.value,
    required this.onChanged,
    required this.onOpenAgreement,
    this.errorText,
    this.enabled = true,
  });

  final bool value;
  final ValueChanged<bool> onChanged;
  final VoidCallback onOpenAgreement;
  final String? errorText;
  final bool enabled;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final checkboxBorderSide = WidgetStateBorderSide.resolveWith((states) {
      final borderColor = states.contains(WidgetState.selected)
          ? theme.colorScheme.primary
          : theme.colorScheme.outline;
      return BorderSide(color: borderColor, width: 1.6);
    });
    final errorStyle = theme.textTheme.bodySmall?.copyWith(
      color: theme.colorScheme.error,
      height: 1.4,
    );
    final errorMessage = errorText?.trim();

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Checkbox(
              key: const Key('auth_app_agreement_checkbox'),
              value: value,
              activeColor: theme.colorScheme.primary,
              checkColor: theme.colorScheme.onPrimary,
              side: checkboxBorderSide,
              shape: RoundedRectangleBorder(
                borderRadius: BorderRadius.circular(4),
              ),
              onChanged: enabled
                  ? (next) {
                      onChanged(next ?? false);
                    }
                  : null,
            ),
            Expanded(
              child: Padding(
                padding: const EdgeInsets.only(top: 11),
                child: Wrap(
                  crossAxisAlignment: WrapCrossAlignment.center,
                  spacing: 4,
                  runSpacing: 2,
                  children: [
                    Text(
                      'auth_app_agreement_prefix'.tr,
                      style: theme.textTheme.bodyMedium,
                    ),
                    TextButton(
                      key: const Key('auth_app_agreement_link_button'),
                      onPressed: onOpenAgreement,
                      style: TextButton.styleFrom(
                        padding: EdgeInsets.zero,
                        minimumSize: Size.zero,
                        tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                        visualDensity: VisualDensity.compact,
                      ),
                      child: Text('auth_app_agreement_link'.tr),
                    ),
                  ],
                ),
              ),
            ),
          ],
        ),
        if (errorMessage != null && errorMessage.isNotEmpty)
          Padding(
            padding: const EdgeInsets.only(left: 48, top: 6),
            child: Text(errorMessage, style: errorStyle),
          ),
      ],
    );
  }
}
