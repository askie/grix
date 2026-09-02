import '../../../core/network/api_client.dart';
import 'sms_settings_models.dart';

/// 手机号短信登录注册的后台配置 API。
///
/// 后端路由：`/admin/api/settings/sms` 与 `/test`，
/// ApiClient.baseUrl 已包含 `/admin/api`，这里只写 `/settings/sms` 后半段。
class SmsSettingsService {
  SmsSettingsService._();

  static Future<SmsSettings> get() async {
    final data = await ApiClient.instance.get('/settings/sms');
    return SmsSettings.fromJson((data as Map).cast<String, dynamic>());
  }

  static Future<void> update(SmsSettingsPatch patch) {
    return ApiClient.instance.put('/settings/sms', data: patch.toJson());
  }

  /// 给指定手机号发一条测试码。region 留空表示按手机号自动判定。
  static Future<void> test({required String phoneE164, String region = ''}) {
    return ApiClient.instance.post(
      '/settings/sms/test',
      data: {'phone_e164': phoneE164, if (region.isNotEmpty) 'region': region},
    );
  }
}
