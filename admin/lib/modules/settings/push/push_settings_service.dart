import '../../../core/network/api_client.dart';
import 'push_settings_models.dart';

/// 离线推送通道开关的后台配置 API。
///
/// 后端路由：`/admin/api/settings/push`，
/// ApiClient.baseUrl 已包含 `/admin/api`，这里只写 `/settings/push`。
class PushSettingsService {
  PushSettingsService._();

  static Future<PushSettings> get() async {
    final data = await ApiClient.instance.get('/settings/push');
    return PushSettings.fromJson((data as Map).cast<String, dynamic>());
  }

  static Future<void> update(PushSettings settings) {
    return ApiClient.instance.put('/settings/push', data: settings.toJson());
  }
}
