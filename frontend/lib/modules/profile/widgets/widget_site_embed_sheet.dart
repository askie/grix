import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';

import '../../../app/locale/locale_service.dart';
import '../../../app/themes/app_theme.dart';
import '../../../data/providers/session_service.dart';
import '../../../shared/utils/app_external_links.dart';
import '../../../shared/utils/toast_util.dart';
import '../../../shared/widgets/multi_locale_text_field.dart';

@immutable
class WidgetSdkDocLink {
  const WidgetSdkDocLink({
    required this.platform,
    required this.label,
    required this.url,
    required this.icon,
  });

  final String platform;
  final String label;
  final String url;
  final IconData icon;
}

const List<WidgetSdkDocLink> widgetSdkDocLinks = [
  WidgetSdkDocLink(
    platform: 'ios',
    label: 'iOS SDK',
    url: 'https://github.com/askie/grix/tree/main/sdk/ios',
    icon: Icons.phone_iphone_rounded,
  ),
  WidgetSdkDocLink(
    platform: 'android',
    label: 'Android SDK',
    url: 'https://github.com/askie/grix/tree/main/sdk/android',
    icon: Icons.android_rounded,
  ),
  WidgetSdkDocLink(
    platform: 'flutter',
    label: 'Flutter SDK',
    url: 'https://github.com/askie/grix/tree/main/sdk/flutter',
    icon: Icons.flutter_dash_rounded,
  ),
];

/// Injects or strips `data-locale` on a backend-generated embed script.
///
/// [localeCode] empty/null means follow browser language (no attribute).
@visibleForTesting
String applyEmbedLocale(String baseEmbedCode, String? localeCode) {
  final code = (localeCode ?? '').trim();
  var embed = baseEmbedCode.replaceFirst(RegExp(r'\s*data-locale="[^"]*"'), '');
  if (code.isEmpty) return embed;
  if (embed.contains(' defer>')) {
    return embed.replaceFirst(' defer>', ' data-locale="$code" defer>');
  }
  if (embed.contains('></script>')) {
    return embed.replaceFirst('></script>', ' data-locale="$code"></script>');
  }
  return '$embed data-locale="$code"';
}

/// Bottom sheet: site key, locale picker, embed script, copy / rotate / edit.
class WidgetSiteEmbedSheet extends StatefulWidget {
  const WidgetSiteEmbedSheet({
    super.key,
    required this.site,
    required this.baseEmbedCode,
    required this.onEdit,
    required this.onDelete,
  });

  final WidgetSiteModel site;
  final String baseEmbedCode;
  final VoidCallback onEdit;
  final VoidCallback onDelete;

  @override
  State<WidgetSiteEmbedSheet> createState() => _WidgetSiteEmbedSheetState();
}

class _WidgetSiteEmbedSheetState extends State<WidgetSiteEmbedSheet> {
  final SessionService _sessionService = Get.find<SessionService>();

  /// null = follow browser (omit data-locale).
  String? _localeCode;

  String get _embedCode => applyEmbedLocale(widget.baseEmbedCode, _localeCode);

  Future<void> _copySiteKey() async {
    await Clipboard.setData(ClipboardData(text: widget.site.siteKey));
    if (mounted == false) return;
    CustomToast.show(
      'settings_widget_sites_copy_site_key_success'.tr,
      isError: false,
    );
  }

  void _openEditForAppAllowlist() {
    Navigator.of(context).pop();
    widget.onEdit();
  }

  @override
  Widget build(BuildContext context) {
    final site = widget.site;
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    site.siteName,
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                ),
                IconButton(
                  onPressed: () => Navigator.of(context).pop(),
                  icon: const Icon(Icons.close),
                  tooltip: MaterialLocalizations.of(context).closeButtonTooltip,
                ),
              ],
            ),
            const SizedBox(height: 10),
            Text(
              'settings_widget_sites_site_key'.trParams({'key': site.siteKey}),
            ),
            const SizedBox(height: 12),
            Text('settings_widget_sites_locale_label'.tr),
            const SizedBox(height: 6),
            InputDecorator(
              decoration: const InputDecoration(
                border: OutlineInputBorder(),
                isDense: true,
                contentPadding: EdgeInsets.symmetric(
                  horizontal: 12,
                  vertical: 4,
                ),
              ),
              child: DropdownButtonHideUnderline(
                child: DropdownButton<String?>(
                  value: _localeCode,
                  isExpanded: true,
                  items: [
                    DropdownMenuItem<String?>(
                      value: null,
                      child: Text(
                        'settings_widget_sites_locale_follow_browser'.tr,
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                    for (final entry in LocaleService.supportedLocales)
                      DropdownMenuItem<String?>(
                        value: localeCodeOf(entry.locale),
                        child: Text(
                          '${entry.nativeLabel} (${localeCodeOf(entry.locale)})',
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                  ],
                  onChanged: (value) => setState(() => _localeCode = value),
                ),
              ),
            ),
            const SizedBox(height: 12),
            Text('settings_widget_sites_embed_script'.tr),
            const SizedBox(height: 6),
            Container(
              width: double.infinity,
              padding: const EdgeInsets.all(10),
              decoration: BoxDecoration(
                color: Theme.of(context).colorScheme.surfaceContainerHighest,
                borderRadius: BorderRadius.circular(8),
              ),
              child: SelectableText(_embedCode),
            ),
            const SizedBox(height: 16),
            Container(
              key: const ValueKey('widget_app_integration_section'),
              width: double.infinity,
              padding: const EdgeInsets.all(12),
              decoration: BoxDecoration(
                border: Border.all(
                  color: Theme.of(context).colorScheme.outlineVariant,
                ),
                borderRadius: BorderRadius.circular(10),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    'settings_widget_sites_app_integration_title'.tr,
                    style: Theme.of(context).textTheme.titleSmall,
                  ),
                  const SizedBox(height: 6),
                  Text(
                    'settings_widget_sites_app_integration_desc'.tr,
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                  if (site.allowedApps.isEmpty) ...[
                    const SizedBox(height: 8),
                    Container(
                      width: double.infinity,
                      padding: const EdgeInsets.all(8),
                      decoration: BoxDecoration(
                        color: Theme.of(context).colorScheme.errorContainer
                            .withValues(alpha: 0.45),
                        borderRadius: BorderRadius.circular(8),
                      ),
                      child: Row(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Expanded(
                            child: Text(
                              'settings_widget_sites_apps_missing'.tr,
                              style: Theme.of(context).textTheme.bodySmall,
                            ),
                          ),
                          TextButton(
                            key: const ValueKey('widget_apps_configure_button'),
                            onPressed: _openEditForAppAllowlist,
                            child: Text(
                              'settings_widget_sites_apps_configure'.tr,
                            ),
                          ),
                        ],
                      ),
                    ),
                  ],
                  const SizedBox(height: 8),
                  Wrap(
                    spacing: 8,
                    runSpacing: 4,
                    crossAxisAlignment: WrapCrossAlignment.center,
                    children: [
                      OutlinedButton.icon(
                        key: const ValueKey('widget_copy_site_key_button'),
                        onPressed: _copySiteKey,
                        icon: const Icon(Icons.copy_rounded, size: 16),
                        label: Text('settings_widget_sites_copy_site_key'.tr),
                      ),
                      for (final link in widgetSdkDocLinks)
                        TextButton.icon(
                          key: ValueKey('widget_sdk_link_${link.platform}'),
                          onPressed: () async {
                            await AppExternalLinks.open(link.url);
                          },
                          icon: Icon(link.icon, size: 16),
                          label: Text(link.label),
                        ),
                    ],
                  ),
                ],
              ),
            ),
            const SizedBox(height: 12),
            Row(
              children: [
                ElevatedButton(
                  style: ElevatedButton.styleFrom(
                    minimumSize: const Size(0, AppTheme.btnHeightMedium),
                  ),
                  onPressed: () async {
                    await Clipboard.setData(ClipboardData(text: _embedCode));
                    if (!context.mounted) return;
                    CustomToast.show(
                      'settings_widget_sites_copied'.tr,
                      isError: false,
                    );
                  },
                  child: Text('settings_widget_sites_copy_script'.tr),
                ),
                const SizedBox(width: 8),
                TextButton(
                  onPressed: () async {
                    final result = await _sessionService.rotateWidgetSiteSecret(
                      site.id,
                    );
                    if (!result.success) {
                      CustomToast.show(
                        result.message.isNotEmpty
                            ? result.message
                            : 'settings_widget_sites_rotate_failed'.tr,
                      );
                      return;
                    }
                    await Clipboard.setData(
                      ClipboardData(text: result.siteSecret),
                    );
                    CustomToast.show(
                      'settings_widget_sites_secret_copied'.tr,
                      isError: false,
                    );
                  },
                  child: Text('settings_widget_sites_rotate_secret'.tr),
                ),
                const Spacer(),
                TextButton(
                  onPressed: () {
                    Navigator.of(context).pop();
                    widget.onEdit();
                  },
                  child: Text('settings_widget_sites_edit'.tr),
                ),
                TextButton(
                  style: TextButton.styleFrom(
                    foregroundColor: Theme.of(context).colorScheme.error,
                  ),
                  onPressed: () {
                    Navigator.of(context).pop();
                    widget.onDelete();
                  },
                  child: Text('settings_widget_sites_delete'.tr),
                ),
              ],
            ),
            const SizedBox(height: 6),
          ],
        ),
      ),
    );
  }
}
