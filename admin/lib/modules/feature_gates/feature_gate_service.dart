import '../../core/network/api_client.dart';
import '../../core/network/page_result.dart';
import '../users/admin_user_item.dart';
import 'feature_gate_models.dart';

class FeatureGateService {
  static Future<
    ({List<FeatureGateInfo> gates, List<AvailableFeature> available})
  >
  list() async {
    final data = await ApiClient.instance.get('/feature-gates');
    final map = (data as Map).cast<String, dynamic>();
    final gates = ((map['gates'] as List?) ?? [])
        .map(
          (e) => FeatureGateInfo.fromJson((e as Map).cast<String, dynamic>()),
        )
        .toList();
    final avail = ((map['available'] as List?) ?? [])
        .map(
          (e) => AvailableFeature.fromJson((e as Map).cast<String, dynamic>()),
        )
        .toList();
    return (gates: gates, available: avail);
  }

  static Future<void> create(String key) =>
      ApiClient.instance.post('/feature-gates', data: {'key': key});

  static Future<void> updateStatus(String key, String status) => ApiClient
      .instance
      .post('/feature-gates/status', data: {'key': key, 'status': status});

  static Future<void> modifyUsers(String key, String action, String userIds) =>
      ApiClient.instance.post(
        '/feature-gates/users',
        data: {'key': key, 'action': action, 'user_ids': userIds},
      );

  /// 列出指定 feature gate 白名单内的用户（带搜索与分页）。
  static Future<PageResult<AdminUserItem>> listWhitelist({
    required String key,
    String? query,
    int page = 1,
    int pageSize = 20,
  }) async {
    final data = await ApiClient.instance.get(
      '/feature-gates/whitelist',
      query: {
        'key': key,
        if (query != null && query.isNotEmpty) 'q': query,
        'page': page,
        'page_size': pageSize,
      },
    );
    return PageResult.fromData(data, AdminUserItem.fromJson);
  }
}
