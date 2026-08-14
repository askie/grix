import '../../../core/network/api_client.dart';
import 'pay_channel_settings_models.dart';

/// 支付通道（支付宝 / PayPal）商户凭证的后台配置 API。
///
/// 后端路由：`/admin/api/settings/pay_channel` 与 `/test/:code`，
/// ApiClient.baseUrl 已包含 `/admin/api`，这里只写 `/settings/pay_channel` 后半段。
class PayChannelSettingsService {
  PayChannelSettingsService._();

  static Future<PayChannelSettings> get() async {
    final data = await ApiClient.instance.get('/settings/pay_channel');
    return PayChannelSettings.fromJson((data as Map).cast<String, dynamic>());
  }

  static Future<void> update(PayChannelSettingsPatch patch) {
    return ApiClient.instance.put('/settings/pay_channel', data: patch.toJson());
  }

  /// 用已保存（上一次保存）的凭证做一次自检；code 为 alipay 或 paypal。
  static Future<void> test(String code) {
    return ApiClient.instance.post('/settings/pay_channel/test/$code');
  }
}
