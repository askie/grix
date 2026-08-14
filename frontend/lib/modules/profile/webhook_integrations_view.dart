import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';

import '../../data/providers/auth_service.dart';
import '../../shared/utils/app_runtime_endpoints.dart';
import '../../shared/utils/toast_util.dart';
import '../../shared/utils/webhook_help_text_builder.dart';
import '../../shared/widgets/app_dialog_style.dart';
import '../chat/services/chat_route_navigator.dart';

class _WebhookItem {
  const _WebhookItem({
    required this.id,
    required this.sessionId,
    required this.sessionTitle,
    required this.sessionType,
    required this.url,
    required this.status,
    this.createdAt,
    this.expiresAt,
    this.lastUsedAt,
  });

  final String id;
  final String sessionId;
  final String sessionTitle;
  final int sessionType;
  final String url;
  final String status;
  final DateTime? createdAt;
  final DateTime? expiresAt;
  final DateTime? lastUsedAt;

  factory _WebhookItem.fromJson(Map<String, dynamic> json) {
    DateTime? parse(dynamic v) {
      if (v == null) return null;
      final s = v.toString().trim();
      if (s.isEmpty) return null;
      return DateTime.tryParse(s)?.toLocal();
    }

    return _WebhookItem(
      id: (json['id'] ?? '').toString().trim(),
      sessionId: (json['session_id'] ?? '').toString().trim(),
      sessionTitle: (json['session_title'] ?? '').toString().trim(),
      sessionType: (json['session_type'] ?? 1) as int,
      url: (json['url'] ?? '').toString().trim(),
      status: (json['status'] ?? 'active').toString().trim(),
      createdAt: parse(json['created_at']),
      expiresAt: parse(json['expires_at']),
      lastUsedAt: parse(json['last_used_at']),
    );
  }
}

class WebhookIntegrationsView extends StatefulWidget {
  const WebhookIntegrationsView({super.key});

  @override
  State<WebhookIntegrationsView> createState() =>
      _WebhookIntegrationsViewState();
}

class _WebhookIntegrationsViewState extends State<WebhookIntegrationsView> {
  late final Dio _dio;
  final List<_WebhookItem> _items = <_WebhookItem>[];
  bool _loading = false;

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
      final res = await _dio.get(
        '/api/webhooks',
        queryParameters: {'limit': 200, 'offset': 0},
      );
      final body = res.data as Map<String, dynamic>?;
      final code = (body?['code'] ?? -1) as int;
      if (code != 0) {
        CustomToast.show(
          (body?['msg'] ?? 'settings_webhook_load_failed'.tr).toString(),
        );
        return;
      }
      final rawItems =
          ((body?['data'] ?? const {}) as Map<String, dynamic>)['items'];
      final list = <_WebhookItem>[
        if (rawItems is List)
          for (final item in rawItems)
            if (item is Map)
              _WebhookItem.fromJson(Map<String, dynamic>.from(item)),
      ];
      if (!mounted) return;
      setState(() {
        _items
          ..clear()
          ..addAll(list);
      });
    } catch (_) {
      CustomToast.show('settings_webhook_load_failed'.tr);
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _deleteItem(_WebhookItem item) async {
    final ok = await showAppConfirmDialog(
      context: context,
      title: 'settings_webhook_delete_title'.tr,
      message: 'settings_webhook_delete_message'.tr,
      confirmText: 'settings_webhook_delete'.tr,
      isDestructive: true,
    );
    if (!ok) return;
    try {
      final res = await _dio.delete('/api/webhooks/${item.id}');
      final body = res.data as Map<String, dynamic>?;
      final code = (body?['code'] ?? -1) as int;
      if (code != 0) {
        CustomToast.show(
          (body?['msg'] ?? 'settings_webhook_delete_failed'.tr).toString(),
        );
        return;
      }
      CustomToast.show('settings_webhook_deleted'.tr, isError: false);
      await _load();
    } catch (_) {
      CustomToast.show('settings_webhook_delete_failed'.tr);
    }
  }

  void _copyUrl(_WebhookItem item) {
    final text = buildWebhookHelpText(item.url);
    Clipboard.setData(ClipboardData(text: text));
    CustomToast.show('chat_webhook_help_copied'.tr, isError: false);
  }

  void _openChat(_WebhookItem item) {
    final type = item.sessionType == 2 ? 'group' : 'private';
    ChatRouteNavigator.toChat(
      sessionId: item.sessionId,
      title: item.sessionTitle.isNotEmpty ? item.sessionTitle : item.sessionId,
      type: type,
    );
  }

  String _fmt(DateTime? time) {
    if (time == null) return '--';
    return '${time.year.toString().padLeft(4, '0')}-${time.month.toString().padLeft(2, '0')}-${time.day.toString().padLeft(2, '0')} '
        '${time.hour.toString().padLeft(2, '0')}:${time.minute.toString().padLeft(2, '0')}';
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text('settings_webhook_integrations'.tr)),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : RefreshIndicator(
              onRefresh: _load,
              child: ListView.builder(
                itemCount: _items.length,
                itemBuilder: (context, index) {
                  final item = _items[index];
                  final title = item.sessionTitle.isNotEmpty
                      ? item.sessionTitle
                      : item.sessionId;
                  return ListTile(
                    title: GestureDetector(
                      onTap: () => _openChat(item),
                      child: Text(
                        title,
                        style: const TextStyle(
                          color: Colors.blue,
                          decoration: TextDecoration.underline,
                          decorationColor: Colors.blue,
                        ),
                      ),
                    ),
                    subtitle: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          '${'settings_webhook_created_label'.tr}: ${_fmt(item.createdAt)}',
                        ),
                        Text(
                          item.url,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(
                            fontSize: 12,
                            color: Theme.of(context).hintColor,
                          ),
                        ),
                      ],
                    ),
                    trailing: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        IconButton(
                          onPressed: () => _copyUrl(item),
                          icon: const Icon(Icons.copy_rounded),
                          tooltip: 'settings_webhook_copy_url'.tr,
                        ),
                        TextButton(
                          onPressed: () => _deleteItem(item),
                          child: Text(
                            'settings_webhook_delete'.tr,
                            style: const TextStyle(color: Colors.red),
                          ),
                        ),
                      ],
                    ),
                  );
                },
              ),
            ),
    );
  }
}
