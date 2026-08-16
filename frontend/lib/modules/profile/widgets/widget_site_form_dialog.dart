import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../data/providers/session_service.dart';
import '../../../shared/utils/toast_util.dart';
import '../../../shared/widgets/multi_locale_text_field.dart';

/// Result returned by [WidgetSiteFormDialog] when the user confirms.
class WidgetSiteFormResult {
  const WidgetSiteFormResult({
    required this.siteName,
    required this.allowedOrigins,
    required this.displayConfig,
  });

  final String siteName;
  final List<String> allowedOrigins;
  final WidgetDisplayConfig displayConfig;
}

/// Shared create/edit form for a widget site, including display config.
///
/// Returns a [WidgetSiteFormResult] on confirm, or null on cancel.
class WidgetSiteFormDialog extends StatefulWidget {
  const WidgetSiteFormDialog({
    super.key,
    this.initial,
    required this.confirmLabel,
  });

  /// When non-null, the dialog is in edit mode and fields are pre-filled.
  final WidgetSiteModel? initial;
  final String confirmLabel;

  @override
  State<WidgetSiteFormDialog> createState() => _WidgetSiteFormDialogState();
}

class _WidgetSiteFormDialogState extends State<WidgetSiteFormDialog> {
  /// 预设主题色板，首个为 Widget 默认主题色。
  static const List<String> _presetThemeColors = [
    '#0f766e',
    '#2563eb',
    '#4f46e5',
    '#7c3aed',
    '#e11d48',
    '#ea580c',
    '#16a34a',
    '#475569',
  ];

  late final TextEditingController _nameCtrl;
  late final TextEditingController _originsCtrl;
  late final TextEditingController _titleCtrl;
  late final TextEditingController _buttonLabelCtrl;
  late String _themeColor;
  late String _position;
  late bool _autoExpand;
  late Map<String, String> _welcome;

  @override
  void initState() {
    super.initState();
    final site = widget.initial;
    final cfg = site?.displayConfig ?? const WidgetDisplayConfig();
    _nameCtrl = TextEditingController(text: site?.siteName ?? '');
    _originsCtrl =
        TextEditingController(text: site?.allowedOrigins.join('\n') ?? '');
    _themeColor =
        _normalizeHexColor(cfg.themeColor) ?? _presetThemeColors.first;
    _titleCtrl = TextEditingController(text: cfg.title);
    _buttonLabelCtrl = TextEditingController(text: cfg.buttonLabel);
    _welcome = Map.of(cfg.welcome);
    _position = cfg.position.isNotEmpty ? cfg.position : 'right';
    _autoExpand = cfg.autoExpand;
  }

  @override
  void dispose() {
    _nameCtrl.dispose();
    _originsCtrl.dispose();
    _titleCtrl.dispose();
    _buttonLabelCtrl.dispose();
    super.dispose();
  }

  /// 归一化为 6 位小写 hex（含 #）；非法或空值返回 null。
  static String? _normalizeHexColor(String value) {
    final m = RegExp(r'^#?([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$')
        .firstMatch(value.trim());
    if (m == null) return null;
    var h = m.group(1)!.toLowerCase();
    if (h.length == 3) {
      h = h.split('').map((c) => '$c$c').join();
    }
    return '#$h';
  }

  static Color _hexToColor(String hex) =>
      Color(0xFF000000 | int.parse(hex.substring(1), radix: 16));

  /// 可选色板：历史数据里的自定义色不在预设中时，追加为首个可选项，避免编辑丢色。
  List<String> get _themeColorOptions {
    if (_presetThemeColors.contains(_themeColor)) return _presetThemeColors;
    return [_themeColor, ..._presetThemeColors];
  }

  void _onConfirm() {
    final name = _nameCtrl.text.trim();
    final origins = _originsCtrl.text
        .split('\n')
        .map((e) => e.trim())
        .where((e) => e.isNotEmpty)
        .toList(growable: false);
    if (name.isEmpty || origins.isEmpty) {
      CustomToast.show('settings_widget_sites_validate'.tr);
      return;
    }
    final cfg = WidgetDisplayConfig(
      themeColor: _themeColor,
      buttonLabel: _buttonLabelCtrl.text.trim(),
      welcome: _welcome,
      position: _position,
      autoExpand: _autoExpand,
      title: _titleCtrl.text.trim(),
    );
    Navigator.of(context).pop(WidgetSiteFormResult(
      siteName: name,
      allowedOrigins: origins,
      displayConfig: cfg,
    ));
  }

  @override
  Widget build(BuildContext context) {
    final isEdit = widget.initial != null;
    final screen = MediaQuery.of(context).size;
    return AlertDialog(
      insetPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 24),
      title: Text(isEdit
          ? 'settings_widget_sites_edit_title'.tr
          : 'settings_widget_sites_create_title'.tr),
      content: SizedBox(
        width: screen.width,
        height: screen.height * 0.75,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              TextField(
                controller: _nameCtrl,
                decoration: InputDecoration(
                  labelText: 'settings_widget_sites_name_label'.tr,
                ),
              ),
              const SizedBox(height: 10),
              TextField(
                controller: _originsCtrl,
                decoration: InputDecoration(
                  labelText: 'settings_widget_sites_origins_label'.tr,
                  hintText: 'https://example.com',
                ),
                minLines: 2,
                maxLines: 4,
              ),
              const SizedBox(height: 16),
              Text(
                'settings_widget_sites_appearance'.tr,
                style: Theme.of(context).textTheme.titleSmall,
              ),
              const SizedBox(height: 8),
              TextField(
                controller: _titleCtrl,
                decoration: InputDecoration(
                  labelText: 'settings_widget_sites_title_label'.tr,
                  hintText: 'settings_widget_sites_title_hint'.tr,
                ),
              ),
              const SizedBox(height: 14),
              Text(
                'settings_widget_sites_theme_color_label'.tr,
                style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                      color: Theme.of(context).colorScheme.onSurfaceVariant,
                    ),
              ),
              const SizedBox(height: 8),
              Wrap(
                spacing: 10,
                runSpacing: 10,
                children: [
                  for (final hex in _themeColorOptions)
                    _ThemeColorSwatch(
                      key: ValueKey('theme_color_$hex'),
                      color: _hexToColor(hex),
                      selected: _themeColor == hex,
                      onTap: () => setState(() => _themeColor = hex),
                    ),
                ],
              ),
              const SizedBox(height: 14),
              TextField(
                controller: _buttonLabelCtrl,
                decoration: InputDecoration(
                  labelText: 'settings_widget_sites_button_label'.tr,
                  hintText: 'settings_widget_sites_button_hint'.tr,
                ),
              ),
              const SizedBox(height: 10),
              Text(
                'settings_widget_sites_welcome_label'.tr,
                style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                      color: Theme.of(context).colorScheme.onSurfaceVariant,
                    ),
              ),
              const SizedBox(height: 6),
              MultiLocaleTextField(
                initial: _welcome,
                minLines: 1,
                maxLines: 3,
                onChanged: (v) => _welcome = v,
              ),
              const SizedBox(height: 10),
              Row(
                children: [
                  Text('settings_widget_sites_position_label'.tr),
                  const SizedBox(width: 12),
                  DropdownButton<String>(
                    value: _position,
                    items: [
                      DropdownMenuItem(
                        value: 'right',
                        child: Text('settings_widget_sites_position_right'.tr),
                      ),
                      DropdownMenuItem(
                        value: 'left',
                        child: Text('settings_widget_sites_position_left'.tr),
                      ),
                    ],
                    onChanged: (v) =>
                        setState(() => _position = v ?? 'right'),
                  ),
                ],
              ),
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: Text('settings_widget_sites_auto_expand_label'.tr),
                value: _autoExpand,
                onChanged: (v) => setState(() => _autoExpand = v),
              ),
            ],
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: Text('settings_widget_sites_cancel'.tr),
        ),
        ElevatedButton(
          onPressed: _onConfirm,
          child: Text(widget.confirmLabel),
        ),
      ],
    );
  }
}

/// 单个主题色色块：圆形色板，选中时显示对勾与描边。
class _ThemeColorSwatch extends StatelessWidget {
  const _ThemeColorSwatch({
    super.key,
    required this.color,
    required this.selected,
    required this.onTap,
  });

  final Color color;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: onTap,
      behavior: HitTestBehavior.opaque,
      child: Container(
        width: 34,
        height: 34,
        decoration: BoxDecoration(
          color: color,
          shape: BoxShape.circle,
          border: selected
              ? Border.all(
                  color: Theme.of(context).colorScheme.surface,
                  width: 2.5,
                )
              : null,
          boxShadow: selected
              ? [
                  BoxShadow(
                    color: color.withValues(alpha: 0.9),
                    spreadRadius: 2,
                  ),
                ]
              : null,
        ),
        child: selected
            ? const Icon(Icons.check, size: 18, color: Colors.white)
            : null,
      ),
    );
  }
}
