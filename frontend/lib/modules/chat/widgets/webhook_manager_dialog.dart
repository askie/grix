import 'package:dio/dio.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';

import '../../../data/providers/auth_service.dart';
import '../../../shared/utils/app_runtime_endpoints.dart';
import '../../../shared/utils/toast_util.dart';
import '../../../shared/utils/webhook_help_text_builder.dart';
import '../../../shared/widgets/app_dialog_style.dart';

class WebhookManagerDialog extends StatefulWidget {
  const WebhookManagerDialog({super.key, required this.sessionId});

  final String sessionId;

  @override
  State<WebhookManagerDialog> createState() => _WebhookManagerDialogState();
}

class _WebhookEndpointItem {
  _WebhookEndpointItem({
    required this.id,
    required this.url,
    required this.createdAt,
    required this.status,
    this.expiresAt,
    this.lastUsedAt,
  });

  final String id;
  final String url;
  final DateTime createdAt;
  final DateTime? expiresAt;
  final DateTime? lastUsedAt;
  final String status;

  factory _WebhookEndpointItem.fromJson(Map<String, dynamic> json) {
    DateTime? parseTime(dynamic value) {
      if (value == null) return null;
      final raw = value.toString().trim();
      if (raw.isEmpty) return null;
      return DateTime.tryParse(raw)?.toLocal();
    }

    return _WebhookEndpointItem(
      id: (json['id'] ?? '').toString(),
      url: (json['url'] ?? '').toString(),
      createdAt: parseTime(json['created_at']) ?? DateTime.now(),
      expiresAt: parseTime(json['expires_at']),
      lastUsedAt: parseTime(json['last_used_at']),
      status: (json['status'] ?? 'active').toString(),
    );
  }
}

class _WebhookManagerDialogState extends State<WebhookManagerDialog> {
  late final Dio _dio;
  bool _loading = true;
  bool _creating = false;
  bool _permanent = true;
  DateTime? _expiresAt;
  final List<_WebhookEndpointItem> _items = <_WebhookEndpointItem>[];

  @override
  void initState() {
    super.initState();
    _dio = Dio(BaseOptions(baseUrl: AppRuntimeEndpoints.apiBaseUrl));
    Get.find<AuthService>().attachAuthInterceptor(_dio);
    _load();
  }

  Future<void> _load() async {
    setState(() => _loading = true);
    try {
      final res = await _dio.get('/api/sessions/${widget.sessionId}/webhooks');
      final data = res.data as Map<String, dynamic>?;
      final code = (data?['code'] ?? -1) as int;
      if (code != 0) {
        CustomToast.show(
          (data?['msg'] ?? 'chat_webhook_load_failed'.tr).toString(),
        );
        return;
      }
      final items =
          ((data?['data'] ?? const {}) as Map<String, dynamic>)['items'];
      final list = (items is List ? items : const <dynamic>[])
          .whereType<Map<String, dynamic>>()
          .map(_WebhookEndpointItem.fromJson)
          .toList();
      setState(() {
        _items
          ..clear()
          ..addAll(list);
      });
    } catch (_) {
      CustomToast.show('chat_webhook_load_failed'.tr);
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _create() async {
    if (_creating) return;
    setState(() => _creating = true);
    try {
      final payload = <String, dynamic>{'session_id': widget.sessionId};
      if (!_permanent && _expiresAt != null) {
        payload['expires_at'] = _expiresAt!.toUtc().toIso8601String();
      }
      final res = await _dio.post('/api/webhooks', data: payload);
      final data = res.data as Map<String, dynamic>?;
      final code = (data?['code'] ?? -1) as int;
      if (code != 0) {
        CustomToast.show(
          (data?['msg'] ?? 'chat_webhook_create_failed'.tr).toString(),
        );
        return;
      }
      CustomToast.show('chat_webhook_created'.tr, isError: false);
      await _load();
    } catch (_) {
      CustomToast.show('chat_webhook_create_failed'.tr);
    } finally {
      if (mounted) setState(() => _creating = false);
    }
  }

  Future<void> _delete(String id) async {
    final confirm = await showAppConfirmDialog(
      context: context,
      title: 'chat_webhook_delete_title'.tr,
      message: 'chat_webhook_delete_confirm'.tr,
      confirmText: 'common_delete'.tr,
      isDestructive: true,
    );
    if (!confirm) return;
    try {
      final res = await _dio.delete('/api/webhooks/$id');
      final data = res.data as Map<String, dynamic>?;
      final code = (data?['code'] ?? -1) as int;
      if (code != 0) {
        CustomToast.show(
          (data?['msg'] ?? 'chat_webhook_delete_failed'.tr).toString(),
        );
        return;
      }
      CustomToast.show('chat_webhook_deleted'.tr, isError: false);
      await _load();
    } catch (_) {
      CustomToast.show('chat_webhook_delete_failed'.tr);
    }
  }

  Future<void> _pickDateTime() async {
    final now = DateTime.now();
    final pickedDate = await showDatePicker(
      context: context,
      firstDate: now,
      lastDate: now.add(const Duration(days: 3650)),
      initialDate: now.add(const Duration(days: 7)),
    );
    if (pickedDate == null) return;
    if (!mounted) return;
    final pickedTime = await showTimePicker(
      context: context,
      initialTime: const TimeOfDay(hour: 23, minute: 59),
    );
    if (pickedTime == null) return;
    setState(() {
      _expiresAt = DateTime(
        pickedDate.year,
        pickedDate.month,
        pickedDate.day,
        pickedTime.hour,
        pickedTime.minute,
      );
    });
  }

  String _fmt(DateTime? time, {String fallback = ''}) {
    if (time == null) return fallback;
    final t = time.toLocal();
    return '${t.year.toString().padLeft(4, '0')}-${t.month.toString().padLeft(2, '0')}-${t.day.toString().padLeft(2, '0')} '
        '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}';
  }

  Widget _metaCell(String label, String value, {Color? valueColor}) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(
          label,
          style: theme.textTheme.labelSmall?.copyWith(
            fontSize: 11,
            fontWeight: FontWeight.w400,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.65),
          ),
        ),
        const SizedBox(height: 2),
        Text(
          value,
          style: theme.textTheme.bodyMedium?.copyWith(
            fontSize: 13,
            fontWeight: FontWeight.w600,
            color: valueColor ?? theme.colorScheme.onSurface,
          ),
        ),
      ],
    );
  }

  Future<void> _showWebhookHelp(String webhookUrl) async {
    final text = buildWebhookHelpText(webhookUrl);
    await showAppContentDialog<void>(
      context: context,
      title: 'chat_webhook_help_title'.tr,
      size: AppDialogSize.wide,
      content: Builder(
        builder: (ctx) => SelectableText(
          text,
          style: Theme.of(ctx).textTheme.bodyMedium?.copyWith(
            fontFamily: 'monospace',
            fontFamilyFallback: AppTheme.textFontFallbackOrNull,
            height: 1.45,
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: () async {
            await Clipboard.setData(ClipboardData(text: text));
            CustomToast.show('chat_webhook_help_copied'.tr, isError: false);
          },
          child: Text('chat_webhook_help_copy_doc'.tr),
        ),
        Builder(
          builder: (ctx) => TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: Text('chat_webhook_close'.tr),
          ),
        ),
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: Text('chat_webhook_manage'.tr),
      content: SizedBox(
        width: resolveDialogConstraints(
          context,
          size: AppDialogSize.wide,
        ).maxWidth,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: SwitchListTile(
                    dense: true,
                    contentPadding: EdgeInsets.zero,
                    title: Text('chat_webhook_expire_permanent'.tr),
                    value: _permanent,
                    onChanged: (v) => setState(() => _permanent = v),
                  ),
                ),
                if (!_permanent)
                  TextButton(
                    onPressed: _pickDateTime,
                    child: Text(
                      _expiresAt == null
                          ? 'chat_webhook_pick_expire'.tr
                          : _fmt(_expiresAt, fallback: ''),
                    ),
                  ),
                const SizedBox(width: 8),
                FilledButton(
                  onPressed: _creating ? null : _create,
                  child: Text(
                    _creating ? 'common_loading'.tr : 'chat_webhook_create'.tr,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 8),
            if (_loading)
              const Padding(
                padding: EdgeInsets.all(24),
                child: Center(child: CircularProgressIndicator()),
              )
            else if (_items.isEmpty)
              Padding(
                padding: const EdgeInsets.all(16),
                child: Text('chat_webhook_empty'.tr),
              )
            else
              SizedBox(
                height: 380,
                child: ListView.separated(
                  itemCount: _items.length,
                  separatorBuilder: (_, __) => const SizedBox(height: 10),
                  itemBuilder: (_, index) {
                    final item = _items[index];
                    final isExpired = item.status == 'expired';
                    return Container(
                      padding: const EdgeInsets.fromLTRB(12, 10, 12, 10),
                      decoration: BoxDecoration(
                        border: Border.all(
                          color: Theme.of(
                            context,
                          ).dividerColor.withValues(alpha: 0.45),
                        ),
                        borderRadius: BorderRadius.circular(10),
                      ),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          SelectableText(
                            item.url,
                            style: Theme.of(context).textTheme.bodyMedium
                                ?.copyWith(
                                  fontFamily: 'monospace',
                                  fontFamilyFallback:
                                      AppTheme.textFontFallbackOrNull,
                                ),
                          ),
                          const SizedBox(height: 10),
                          Row(
                            children: [
                              Expanded(
                                child: _metaCell(
                                  'chat_webhook_created_at'.tr,
                                  _fmt(item.createdAt, fallback: '-'),
                                ),
                              ),
                              Expanded(
                                child: _metaCell(
                                  'chat_webhook_last_used_at'.tr,
                                  _fmt(
                                    item.lastUsedAt,
                                    fallback: 'chat_webhook_never_used'.tr,
                                  ),
                                ),
                              ),
                            ],
                          ),
                          const SizedBox(height: 6),
                          Row(
                            children: [
                              Expanded(
                                child: _metaCell(
                                  'chat_webhook_expires_at'.tr,
                                  _fmt(
                                    item.expiresAt,
                                    fallback: 'chat_webhook_expire_never'.tr,
                                  ),
                                ),
                              ),
                              Expanded(
                                child: _metaCell(
                                  'chat_webhook_status'.tr,
                                  isExpired
                                      ? 'chat_webhook_status_expired'.tr
                                      : 'chat_webhook_status_active'.tr,
                                  valueColor: isExpired
                                      ? Theme.of(context).colorScheme.error
                                      : Theme.of(context).colorScheme.primary,
                                ),
                              ),
                            ],
                          ),
                          const Divider(height: 20),
                          Row(
                            mainAxisAlignment: MainAxisAlignment.end,
                            children: [
                              TextButton(
                                onPressed: () => _showWebhookHelp(item.url),
                                child: Text('chat_webhook_help'.tr),
                              ),
                              TextButton(
                                onPressed: () async {
                                  final text = buildWebhookHelpText(item.url);
                                  await Clipboard.setData(
                                    ClipboardData(text: text),
                                  );
                                  CustomToast.show(
                                    'chat_webhook_help_copied'.tr,
                                    isError: false,
                                  );
                                },
                                child: Text('chat_webhook_copy'.tr),
                              ),
                              TextButton(
                                onPressed: () => _delete(item.id),
                                child: Text('common_delete'.tr),
                              ),
                            ],
                          ),
                        ],
                      ),
                    );
                  },
                ),
              ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: Text('chat_webhook_close'.tr),
        ),
      ],
    );
  }
}
