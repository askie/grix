import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../app/locale/locale_change_coordinator.dart';
import '../../../app/locale/locale_service.dart';

class AuthLanguageSwitcher extends StatelessWidget {
  const AuthLanguageSwitcher({super.key, this.compact = false});

  final bool compact;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final horizontalPadding = compact ? 10.0 : 12.0;
    final verticalPadding = compact ? 6.0 : 8.0;
    final label = LocaleService.currentNativeLabel(Get.locale);

    return InkWell(
      borderRadius: BorderRadius.circular(20),
      onTap: () => _showPicker(context),
      child: Container(
        padding: EdgeInsets.symmetric(
          horizontal: horizontalPadding,
          vertical: verticalPadding,
        ),
        decoration: BoxDecoration(
          borderRadius: BorderRadius.circular(20),
          border: Border.all(
            color: theme.colorScheme.outline.withValues(alpha: 0.4),
          ),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.language_rounded, size: 16),
            const SizedBox(width: 6),
            Text(
              label,
              style: theme.textTheme.bodySmall?.copyWith(
                fontWeight: FontWeight.w600,
              ),
            ),
            const SizedBox(width: 2),
            const Icon(Icons.arrow_drop_down_rounded, size: 16),
          ],
        ),
      ),
    );
  }

  void _showPicker(BuildContext context) {
    final current = Get.locale;
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (ctx) => SafeArea(
        child: ConstrainedBox(
          constraints: BoxConstraints(
            maxHeight: MediaQuery.of(ctx).size.height * 0.7,
          ),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const SizedBox(height: 12),
              Container(
                width: 36,
                height: 4,
                decoration: BoxDecoration(
                  color: Theme.of(
                    ctx,
                  ).colorScheme.outline.withValues(alpha: 0.3),
                  borderRadius: BorderRadius.circular(2),
                ),
              ),
              const SizedBox(height: 8),
              Flexible(
                child: ListView(
                  shrinkWrap: true,
                  padding: const EdgeInsets.only(bottom: 8),
                  children: LocaleService.supportedLocales.map((entry) {
                    final isSelected =
                        current?.languageCode == entry.locale.languageCode &&
                        (entry.locale.countryCode == null ||
                            current?.countryCode == entry.locale.countryCode);
                    return ListTile(
                      title: Text(entry.nativeLabel),
                      subtitle: Text(entry.label),
                      trailing: isSelected
                          ? Icon(
                              Icons.check_rounded,
                              color: Theme.of(ctx).primaryColor,
                            )
                          : null,
                      onTap: () async {
                        Navigator.of(ctx).pop();
                        await LocaleChangeCoordinator.changeLocale(
                          entry.locale,
                        );
                      },
                    );
                  }).toList(),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
