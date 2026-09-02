import '../../core/network/api_client.dart';
import 'settings_models.dart';

/// 系统设置 API。
class SettingsService {
  static Future<SettingsBundle> get() async {
    final data = await ApiClient.instance.get('/settings');
    final map = (data as Map).cast<String, dynamic>();
    return SettingsBundle(
      auth: AuthSettings.fromJson((map['auth'] as Map).cast<String, dynamic>()),
      group: GroupSettings.fromJson(
        (map['group'] as Map).cast<String, dynamic>(),
      ),
    );
  }

  static Future<void> updateAuth(AuthSettings auth) {
    return ApiClient.instance.put('/settings/auth', data: auth.toJson());
  }

  static Future<void> updateGroup(int memberInviteThreshold) {
    return ApiClient.instance.put(
      '/settings/group',
      data: {'member_invite_threshold': memberInviteThreshold},
    );
  }

  static Future<void> changePassword(String current, String next) {
    return ApiClient.instance.post(
      '/settings/password',
      data: {'current_password': current, 'new_password': next},
    );
  }

  static Future<VoiceModelsConfig> getVoiceModels() async {
    final data = await ApiClient.instance.get('/settings/voice-models');
    return VoiceModelsConfig.fromJson((data as Map).cast<String, dynamic>());
  }

  static Future<void> updateVoiceModels(List<VoiceModelOption> options) {
    return ApiClient.instance.put(
      '/settings/voice-models',
      data: {'options': options.map((e) => e.toJson()).toList()},
    );
  }
}
