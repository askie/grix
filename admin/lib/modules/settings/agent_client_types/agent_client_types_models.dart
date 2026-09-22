import '../../../core/network/api_client.dart';

class AgentClientTypeItem {
  AgentClientTypeItem({
    required this.type,
    required this.label,
    required this.enabled,
  });

  final String type;
  final String label;
  bool enabled;

  factory AgentClientTypeItem.fromJson(Map<String, dynamic> json) {
    return AgentClientTypeItem(
      type: (json['type'] ?? '').toString(),
      label: (json['label'] ?? '').toString(),
      enabled: json['enabled'] == true,
    );
  }
}

class AgentClientTypesSettings {
  AgentClientTypesSettings({
    required this.items,
    required this.allEnabled,
  });

  final List<AgentClientTypeItem> items;
  final bool allEnabled;

  factory AgentClientTypesSettings.fromJson(Map<String, dynamic> json) {
    final rawItems = json['items'];
    final items = <AgentClientTypeItem>[];
    if (rawItems is List) {
      for (final item in rawItems) {
        if (item is Map) {
          items.add(
            AgentClientTypeItem.fromJson(item.cast<String, dynamic>()),
          );
        }
      }
    }
    return AgentClientTypesSettings(
      items: items,
      allEnabled: json['all_enabled'] == true,
    );
  }
}

class AgentClientTypesSettingsService {
  AgentClientTypesSettingsService._();

  static Future<AgentClientTypesSettings> get() async {
    final data = await ApiClient.instance.get('/settings/agent-client-types');
    return AgentClientTypesSettings.fromJson(
      (data as Map).cast<String, dynamic>(),
    );
  }

  static Future<void> update(List<String> enabled) {
    return ApiClient.instance.put(
      '/settings/agent-client-types',
      data: {'enabled': enabled},
    );
  }
}
