/// 离线推送通道开关。
///
/// 后端字段定义在 `internal/systemsetting/push_channel.go`，默认全部开启。
/// 国内连不上 Google 时可单独关掉「安卓 FCM」「网页 WebPush」两个走谷歌的通道。
class PushSettings {
  PushSettings({
    required this.iosApnEnabled,
    required this.androidFcmEnabled,
    required this.webPushEnabled,
    required this.jpushEnabled,
  });

  bool iosApnEnabled;
  bool androidFcmEnabled;
  bool webPushEnabled;
  bool jpushEnabled;

  /// 缺字段一律按「开启」解析，与后端默认全开保持一致。
  factory PushSettings.fromJson(Map<String, dynamic> json) => PushSettings(
    iosApnEnabled: json['ios_apn_enabled'] != false,
    androidFcmEnabled: json['android_fcm_enabled'] != false,
    webPushEnabled: json['web_push_enabled'] != false,
    jpushEnabled: json['jpush_enabled'] != false,
  );

  Map<String, dynamic> toJson() => {
    'ios_apn_enabled': iosApnEnabled,
    'android_fcm_enabled': androidFcmEnabled,
    'web_push_enabled': webPushEnabled,
    'jpush_enabled': jpushEnabled,
  };
}
