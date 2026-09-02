import '../../core/network/api_client.dart';
import '../../core/network/page_result.dart';
import 'moderation_models.dart';

/// 内容审查 API。
class ModerationService {
  static Future<PageResult<ModerationEvent>> listEvents({
    String? query,
    bool mutedOnly = false,
    int page = 1,
    int pageSize = 20,
  }) async {
    final data = await ApiClient.instance.get(
      '/moderation',
      query: {
        if (query != null && query.isNotEmpty) 'q': query,
        if (mutedOnly) 'muted': '1',
        'page': page,
        'page_size': pageSize,
      },
    );
    return PageResult.fromData(data, ModerationEvent.fromJson);
  }

  static Future<ModerationSettings> getSettings() async {
    final data = await ApiClient.instance.get('/moderation/settings');
    final map = (data as Map).cast<String, dynamic>();
    return ModerationSettings.fromJson(
      (map['settings'] as Map).cast<String, dynamic>(),
    );
  }

  static Future<void> updateSettings(ModerationSettings settings) {
    return ApiClient.instance.put(
      '/moderation/settings',
      data: settings.toJson(),
    );
  }

  static Future<void> unmute(String sessionId, String memberId) {
    return ApiClient.instance.post(
      '/moderation/unmute',
      data: {'session_id': sessionId, 'member_id': memberId},
    );
  }
}
