import '../../core/network/api_client.dart';
import 'models.dart';

class AppReleaseService {
  static Future<({List<AppRelease> releases, int total})> list({
    String? platform,
    String? channel,
    int page = 1,
    int pageSize = 20,
  }) async {
    final data = await ApiClient.instance.get(
      '/app/releases',
      query: {
        if (platform != null && platform.isNotEmpty) 'platform': platform,
        if (channel != null && channel.isNotEmpty) 'channel': channel,
        'page': page,
        'page_size': pageSize,
      },
    );
    final m = (data as Map).cast<String, dynamic>();
    final list = ((m['releases'] as List?) ?? [])
        .map((e) => AppRelease.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
    return (releases: list, total: (m['total'] as num?)?.toInt() ?? 0);
  }

  static Future<void> create(Map<String, dynamic> body) =>
      ApiClient.instance.post('/app/releases', data: body);
  static Future<void> publish(String id) =>
      ApiClient.instance.post('/app/releases/$id/publish');
  static Future<void> pause(String id) =>
      ApiClient.instance.post('/app/releases/$id/pause');
  static Future<void> resume(String id) =>
      ApiClient.instance.post('/app/releases/$id/resume');
  static Future<void> revoke(String id) =>
      ApiClient.instance.post('/app/releases/$id/revoke');
  static Future<void> delete(String id) =>
      ApiClient.instance.delete('/app/releases/$id');

  static Future<List<AppRolloutRule>> listRules(String releaseId) async {
    final data = await ApiClient.instance.get('/app/releases/$releaseId/rules');
    final m = (data as Map).cast<String, dynamic>();
    return ((m['rules'] as List?) ?? [])
        .map((e) => AppRolloutRule.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  static Future<void> createRule(Map<String, dynamic> body) =>
      ApiClient.instance.post('/app/rollout-rules', data: body);
  static Future<void> toggleRule(String id, int status) => ApiClient.instance
      .post('/app/rollout-rules/$id/toggle', data: {'status': status});
  static Future<void> deleteRule(String id) =>
      ApiClient.instance.delete('/app/rollout-rules/$id');

  static Future<AppDownloadStats?> stats(String releaseId) async {
    try {
      final data = await ApiClient.instance.get('/app/stats/$releaseId');
      final m = (data as Map).cast<String, dynamic>();
      if (m['stats'] == null) return null;
      return AppDownloadStats.fromJson(
        (m['stats'] as Map).cast<String, dynamic>(),
      );
    } catch (_) {
      return null;
    }
  }
}
