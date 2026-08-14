import '../../core/network/api_client.dart';
import '../../core/network/page_result.dart';
import 'visitor_ban_item.dart';

/// Widget 访客封禁管理 API。
class VisitorBanService {
  static Future<PageResult<VisitorBanItem>> list({
    String? query,
    String status = 'banned',
    int page = 1,
    int pageSize = 20,
  }) async {
    final data = await ApiClient.instance.get(
      '/visitor-bans',
      query: {
        if (query != null && query.isNotEmpty) 'q': query,
        if (status.isNotEmpty) 'status': status,
        'page': page,
        'page_size': pageSize,
      },
    );
    return PageResult.fromData(data, VisitorBanItem.fromJson);
  }

  static Future<void> unban(String sessionId) {
    return ApiClient.instance.post('/visitor-bans/$sessionId/unban');
  }
}
