import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/locale/locale_service.dart';

/// locale.Supported 的字符串形式（如 "en_US"），即后端 map[locale]string 的 key。
String localeCodeOf(Locale locale) => locale.countryCode == null
    ? locale.languageCode
    : '${locale.languageCode}_${locale.countryCode}';

const String kDefaultLocaleCode = 'en_US';

/// 按语言分别填写一段文案的通用输入组件：en_US 常驻必填，其余语言可按需增删。
/// 语言集合与 [LocaleService.supportedLocales] 保持一致，供 Widget 欢迎语、
/// 语音开场白等"多语言文案配置"场景复用。
class MultiLocaleTextField extends StatefulWidget {
  const MultiLocaleTextField({
    super.key,
    required this.initial,
    required this.onChanged,
    this.minLines = 1,
    this.maxLines = 3,
    this.hintText,
  });

  final Map<String, String> initial;
  final ValueChanged<Map<String, String>> onChanged;
  final int minLines;
  final int maxLines;
  final String? hintText;

  @override
  State<MultiLocaleTextField> createState() => _MultiLocaleTextFieldState();
}

class _MultiLocaleTextFieldState extends State<MultiLocaleTextField> {
  late List<String> _localeOrder;
  final Map<String, TextEditingController> _controllers = {};
  // 用户一旦手动编辑过，后续 initial 变化（如父组件因其他状态重建传入的旧快照）
  // 不再覆盖用户输入，避免边输入边被重置。
  bool _dirty = false;

  @override
  void initState() {
    super.initState();
    _rebuildFrom(widget.initial);
  }

  @override
  void didUpdateWidget(covariant MultiLocaleTextField oldWidget) {
    super.didUpdateWidget(oldWidget);
    // initial 异步晚到（如编辑页先以空数据渲染，随后请求回包才带出已配置的文案）时，
    // 在用户还没手动改动过的前提下重新用最新数据构建；避免边输入边被外部快照打断。
    if (!_dirty && !mapEquals(oldWidget.initial, widget.initial)) {
      for (final c in _controllers.values) {
        c.dispose();
      }
      _controllers.clear();
      setState(() => _rebuildFrom(widget.initial));
    }
  }

  void _rebuildFrom(Map<String, String> initial) {
    _localeOrder = [
      kDefaultLocaleCode,
      ...initial.keys.where((k) => k != kDefaultLocaleCode),
    ];
    for (final code in _localeOrder) {
      _controllers[code] = TextEditingController(text: initial[code] ?? '');
    }
  }

  @override
  void dispose() {
    for (final c in _controllers.values) {
      c.dispose();
    }
    super.dispose();
  }

  void _emit() {
    _dirty = true;
    final result = <String, String>{};
    for (final code in _localeOrder) {
      final text = _controllers[code]?.text.trim() ?? '';
      if (text.isNotEmpty) result[code] = text;
    }
    widget.onChanged(result);
  }

  String _nativeLabelOf(String code) {
    for (final entry in LocaleService.supportedLocales) {
      if (localeCodeOf(entry.locale) == code) return entry.nativeLabel;
    }
    return code;
  }

  void _addLocale(String code) {
    setState(() {
      _localeOrder.add(code);
      _controllers[code] = TextEditingController();
    });
    _emit();
  }

  void _removeLocale(String code) {
    setState(() {
      _localeOrder.remove(code);
      _controllers.remove(code)?.dispose();
    });
    _emit();
  }

  @override
  Widget build(BuildContext context) {
    final usedCodes = _localeOrder.toSet();
    final remaining = LocaleService.supportedLocales
        .where((e) => !usedCodes.contains(localeCodeOf(e.locale)))
        .toList(growable: false);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (final code in _localeOrder)
          Padding(
            padding: const EdgeInsets.only(bottom: 8),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  child: TextField(
                    controller: _controllers[code],
                    minLines: widget.minLines,
                    maxLines: widget.maxLines,
                    onChanged: (_) => _emit(),
                    decoration: InputDecoration(
                      labelText: code == kDefaultLocaleCode
                          ? '${_nativeLabelOf(code)} (${'multi_locale_default_required'.tr})'
                          : _nativeLabelOf(code),
                      hintText: widget.hintText,
                      isDense: true,
                    ),
                  ),
                ),
                if (code != kDefaultLocaleCode)
                  IconButton(
                    icon: const Icon(Icons.close, size: 18),
                    tooltip: 'multi_locale_remove'.tr,
                    onPressed: () => _removeLocale(code),
                  ),
              ],
            ),
          ),
        if (remaining.isNotEmpty)
          Align(
            alignment: Alignment.centerLeft,
            child: PopupMenuButton<String>(
              tooltip: 'multi_locale_add_language'.tr,
              onSelected: _addLocale,
              itemBuilder: (context) => [
                for (final entry in remaining)
                  PopupMenuItem(
                    value: localeCodeOf(entry.locale),
                    child: Text(entry.nativeLabel),
                  ),
              ],
              child: Chip(
                avatar: const Icon(Icons.add, size: 16),
                label: Text('multi_locale_add_language'.tr),
              ),
            ),
          ),
      ],
    );
  }
}
