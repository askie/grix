/// 塘主支付通道（支付宝 / PayPal）设置数据模型。
///
/// 后端字段定义在 `internal/admin/service/pay_channel_settings_service.go`，
/// 私钥/Secret 在读取时只回传末四位（*_hint），写入时空串表示保留原值。
class PayChannelSettings {
  PayChannelSettings({required this.alipay, required this.paypal});

  PayAlipaySettings alipay;
  PayPaypalSettings paypal;

  factory PayChannelSettings.fromJson(Map<String, dynamic> json) {
    return PayChannelSettings(
      alipay: PayAlipaySettings.fromJson(
        ((json['alipay'] as Map?) ?? const {}).cast<String, dynamic>(),
      ),
      paypal: PayPaypalSettings.fromJson(
        ((json['paypal'] as Map?) ?? const {}).cast<String, dynamic>(),
      ),
    );
  }
}

class PayAlipaySettings {
  PayAlipaySettings({
    required this.enabled,
    required this.sandbox,
    required this.appId,
    required this.privateKeyHint,
    required this.alipayPublicKeyHint,
    required this.signType,
  });

  bool enabled;
  bool sandbox;
  String appId;
  String privateKeyHint;
  String alipayPublicKeyHint;
  String signType;

  factory PayAlipaySettings.fromJson(Map<String, dynamic> json) =>
      PayAlipaySettings(
        enabled: json['enabled'] == true,
        sandbox: json['sandbox'] == true,
        appId: (json['app_id'] as String?) ?? '',
        privateKeyHint: (json['private_key_hint'] as String?) ?? '',
        alipayPublicKeyHint: (json['alipay_public_key_hint'] as String?) ?? '',
        signType: (json['sign_type'] as String?) ?? 'RSA2',
      );
}

class PayPaypalSettings {
  PayPaypalSettings({
    required this.enabled,
    required this.sandbox,
    required this.clientId,
    required this.clientSecretHint,
    required this.webhookId,
  });

  bool enabled;
  bool sandbox;
  String clientId;
  String clientSecretHint;
  String webhookId;

  factory PayPaypalSettings.fromJson(Map<String, dynamic> json) =>
      PayPaypalSettings(
        enabled: json['enabled'] == true,
        sandbox: json['sandbox'] == true,
        clientId: (json['client_id'] as String?) ?? '',
        clientSecretHint: (json['client_secret_hint'] as String?) ?? '',
        webhookId: (json['webhook_id'] as String?) ?? '',
      );
}

/// 提交时的请求体。私钥 / Secret 字段留空表示保留原值。
class PayChannelSettingsPatch {
  PayChannelSettingsPatch({required this.alipay, required this.paypal});

  PayAlipayPatch alipay;
  PayPaypalPatch paypal;

  Map<String, dynamic> toJson() => {
        'alipay': alipay.toJson(),
        'paypal': paypal.toJson(),
      };
}

class PayAlipayPatch {
  PayAlipayPatch({
    required this.enabled,
    required this.sandbox,
    required this.appId,
    required this.privateKey,
    required this.alipayPublicKey,
    required this.signType,
  });

  bool enabled;
  bool sandbox;
  String appId;
  String privateKey;
  String alipayPublicKey;
  String signType;

  Map<String, dynamic> toJson() => {
        'enabled': enabled,
        'sandbox': sandbox,
        'app_id': appId,
        'private_key': privateKey,
        'alipay_public_key': alipayPublicKey,
        'sign_type': signType,
      };
}

class PayPaypalPatch {
  PayPaypalPatch({
    required this.enabled,
    required this.sandbox,
    required this.clientId,
    required this.clientSecret,
    required this.webhookId,
  });

  bool enabled;
  bool sandbox;
  String clientId;
  String clientSecret;
  String webhookId;

  Map<String, dynamic> toJson() => {
        'enabled': enabled,
        'sandbox': sandbox,
        'client_id': clientId,
        'client_secret': clientSecret,
        'webhook_id': webhookId,
      };
}
