/// 塘主短信登录注册设置数据模型。
///
/// 后端字段定义在 `internal/admin/service/sms_settings_service.go`，
/// ak/sk 在读取时只回传末四位（access_key_id_hint），写入时空串表示保留原值。
class SmsSettings {
  SmsSettings({
    required this.phoneRegisterEnabledCn,
    required this.phoneRegisterEnabledGlobal,
    required this.phoneLoginEnabledCn,
    required this.phoneLoginEnabledGlobal,
    required this.allowedCountryCodesCn,
    required this.allowedCountryCodesGlobal,
    required this.cnSmsProvider,
    required this.globalSmsProvider,
    required this.aliyun,
    required this.awsSns,
  });

  bool phoneRegisterEnabledCn;
  bool phoneRegisterEnabledGlobal;
  bool phoneLoginEnabledCn;
  bool phoneLoginEnabledGlobal;
  List<String> allowedCountryCodesCn;
  List<String> allowedCountryCodesGlobal;
  String cnSmsProvider;
  String globalSmsProvider;
  SmsAliyun aliyun;
  SmsAwsSns awsSns;

  factory SmsSettings.fromJson(Map<String, dynamic> json) {
    List<String> readList(dynamic v) {
      if (v is List) {
        return v.map((e) => e.toString()).toList();
      }
      return <String>[];
    }

    return SmsSettings(
      phoneRegisterEnabledCn: json['phone_register_enabled_cn'] == true,
      phoneRegisterEnabledGlobal: json['phone_register_enabled_global'] == true,
      phoneLoginEnabledCn: json['phone_login_enabled_cn'] == true,
      phoneLoginEnabledGlobal: json['phone_login_enabled_global'] == true,
      allowedCountryCodesCn: readList(json['allowed_country_codes_cn']),
      allowedCountryCodesGlobal: readList(json['allowed_country_codes_global']),
      cnSmsProvider: (json['cn_sms_provider'] as String?) ?? '',
      globalSmsProvider: (json['global_sms_provider'] as String?) ?? '',
      aliyun: SmsAliyun.fromJson(
        ((json['aliyun'] as Map?) ?? const {}).cast<String, dynamic>(),
      ),
      awsSns: SmsAwsSns.fromJson(
        ((json['aws_sns'] as Map?) ?? const {}).cast<String, dynamic>(),
      ),
    );
  }
}

class SmsAliyun {
  SmsAliyun({
    required this.regionId,
    required this.accessKeyIdHint,
    required this.accessKeySecretHint,
    required this.signName,
    required this.templateCodeRegister,
    required this.templateCodeLogin,
    required this.templateCodeReset,
  });

  String regionId;
  String accessKeyIdHint;
  String accessKeySecretHint;
  String signName;
  String templateCodeRegister;
  String templateCodeLogin;
  String templateCodeReset;

  factory SmsAliyun.fromJson(Map<String, dynamic> json) => SmsAliyun(
        regionId: (json['region_id'] as String?) ?? '',
        accessKeyIdHint: (json['access_key_id_hint'] as String?) ?? '',
        accessKeySecretHint: (json['access_key_secret_hint'] as String?) ?? '',
        signName: (json['sign_name'] as String?) ?? '',
        templateCodeRegister: (json['template_code_register'] as String?) ?? '',
        templateCodeLogin: (json['template_code_login'] as String?) ?? '',
        templateCodeReset: (json['template_code_reset'] as String?) ?? '',
      );
}

class SmsAwsSns {
  SmsAwsSns({
    required this.region,
    required this.accessKeyIdHint,
    required this.accessKeySecretHint,
    required this.senderId,
  });

  String region;
  String accessKeyIdHint;
  String accessKeySecretHint;
  String senderId;

  factory SmsAwsSns.fromJson(Map<String, dynamic> json) => SmsAwsSns(
        region: (json['region'] as String?) ?? '',
        accessKeyIdHint: (json['access_key_id_hint'] as String?) ?? '',
        accessKeySecretHint: (json['access_key_secret_hint'] as String?) ?? '',
        senderId: (json['sender_id'] as String?) ?? '',
      );
}

/// 提交时的请求体。ak/sk 字段留空表示保留原值。
class SmsSettingsPatch {
  SmsSettingsPatch({
    required this.phoneRegisterEnabledCn,
    required this.phoneRegisterEnabledGlobal,
    required this.phoneLoginEnabledCn,
    required this.phoneLoginEnabledGlobal,
    required this.allowedCountryCodesCn,
    required this.allowedCountryCodesGlobal,
    required this.cnSmsProvider,
    required this.globalSmsProvider,
    required this.aliyun,
    required this.awsSns,
  });

  bool phoneRegisterEnabledCn;
  bool phoneRegisterEnabledGlobal;
  bool phoneLoginEnabledCn;
  bool phoneLoginEnabledGlobal;
  List<String> allowedCountryCodesCn;
  List<String> allowedCountryCodesGlobal;
  String cnSmsProvider;
  String globalSmsProvider;
  SmsAliyunPatch aliyun;
  SmsAwsSnsPatch awsSns;

  Map<String, dynamic> toJson() => {
        'phone_register_enabled_cn': phoneRegisterEnabledCn,
        'phone_register_enabled_global': phoneRegisterEnabledGlobal,
        'phone_login_enabled_cn': phoneLoginEnabledCn,
        'phone_login_enabled_global': phoneLoginEnabledGlobal,
        'allowed_country_codes_cn': allowedCountryCodesCn,
        'allowed_country_codes_global': allowedCountryCodesGlobal,
        'cn_sms_provider': cnSmsProvider,
        'global_sms_provider': globalSmsProvider,
        'aliyun': aliyun.toJson(),
        'aws_sns': awsSns.toJson(),
      };
}

class SmsAliyunPatch {
  SmsAliyunPatch({
    required this.regionId,
    required this.accessKeyId,
    required this.accessKeySecret,
    required this.signName,
    required this.templateCodeRegister,
    required this.templateCodeLogin,
    required this.templateCodeReset,
  });

  String regionId;
  String accessKeyId;
  String accessKeySecret;
  String signName;
  String templateCodeRegister;
  String templateCodeLogin;
  String templateCodeReset;

  Map<String, dynamic> toJson() => {
        'region_id': regionId,
        'access_key_id': accessKeyId,
        'access_key_secret': accessKeySecret,
        'sign_name': signName,
        'template_code_register': templateCodeRegister,
        'template_code_login': templateCodeLogin,
        'template_code_reset': templateCodeReset,
      };
}

class SmsAwsSnsPatch {
  SmsAwsSnsPatch({
    required this.region,
    required this.accessKeyId,
    required this.accessKeySecret,
    required this.senderId,
  });

  String region;
  String accessKeyId;
  String accessKeySecret;
  String senderId;

  Map<String, dynamic> toJson() => {
        'region': region,
        'access_key_id': accessKeyId,
        'access_key_secret': accessKeySecret,
        'sender_id': senderId,
      };
}
