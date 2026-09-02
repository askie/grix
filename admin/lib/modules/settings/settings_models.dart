/// 认证设置，对应后端 AuthSettings（auto_add_customer_user_id 以字符串收发）。
class AuthSettings {
  AuthSettings({required this.autoAddCustomerUserId});

  String autoAddCustomerUserId;

  factory AuthSettings.fromJson(Map<String, dynamic> j) => AuthSettings(
    autoAddCustomerUserId: (j['auto_add_customer_user_id'] ?? '0').toString(),
  );

  Map<String, dynamic> toJson() => {
    'auto_add_customer_user_id': autoAddCustomerUserId.trim(),
  };
}

/// 群组设置，对应后端 GroupSettings。
class GroupSettings {
  GroupSettings({required this.memberInviteThreshold});

  int memberInviteThreshold;

  factory GroupSettings.fromJson(Map<String, dynamic> j) => GroupSettings(
    memberInviteThreshold: (j['member_invite_threshold'] as num?)?.toInt() ?? 0,
  );
}

/// 设置聚合。
class SettingsBundle {
  SettingsBundle({required this.auth, required this.group});
  final AuthSettings auth;
  final GroupSettings group;
}

/// 预定义音色，对应后端 systemsetting.VoicePreset。
class VoicePreset {
  VoicePreset({this.id = '', this.label = ''});

  String id;
  String label;

  factory VoicePreset.fromJson(Map<String, dynamic> j) => VoicePreset(
    id: (j['id'] ?? '').toString(),
    label: (j['label'] ?? '').toString(),
  );

  Map<String, dynamic> toJson() => {'id': id.trim(), 'label': label.trim()};
}

/// 语音模型清单中的一条，对应后端 systemsetting.VoiceModelOption。
class VoiceModelOption {
  VoiceModelOption({
    this.id = '',
    this.label = '',
    this.provider = '',
    this.model = '',
    this.endpoint = '',
    this.enabled = true,
    List<VoicePreset>? voices,
  }) : voices = voices ?? [];

  String id;
  String label;
  String provider;
  String model;
  String endpoint;
  bool enabled;
  List<VoicePreset> voices;

  factory VoiceModelOption.fromJson(Map<String, dynamic> j) => VoiceModelOption(
    id: (j['id'] ?? '').toString(),
    label: (j['label'] ?? '').toString(),
    provider: (j['provider'] ?? '').toString(),
    model: (j['model'] ?? '').toString(),
    endpoint: (j['endpoint'] ?? '').toString(),
    enabled: j['enabled'] == true,
    voices: ((j['voices'] as List?) ?? const [])
        .map((e) => VoicePreset.fromJson((e as Map).cast<String, dynamic>()))
        .toList(),
  );

  Map<String, dynamic> toJson() => {
    'id': id.trim(),
    'label': label.trim(),
    'provider': provider.trim(),
    'model': model.trim(),
    'endpoint': endpoint.trim(),
    'enabled': enabled,
    'voices': voices.map((v) => v.toJson()).toList(),
  };
}

/// 语音模型清单配置：可编辑选项 + 后端支持的供应商枚举。
class VoiceModelsConfig {
  VoiceModelsConfig({required this.options, required this.supportedProviders});
  final List<VoiceModelOption> options;
  final List<String> supportedProviders;

  factory VoiceModelsConfig.fromJson(Map<String, dynamic> j) {
    final rawOptions = (j['options'] as List?) ?? const [];
    final rawProviders = (j['supported_providers'] as List?) ?? const [];
    return VoiceModelsConfig(
      options: rawOptions
          .map(
            (e) =>
                VoiceModelOption.fromJson((e as Map).cast<String, dynamic>()),
          )
          .toList(),
      supportedProviders: rawProviders.map((e) => e.toString()).toList(),
    );
  }
}
