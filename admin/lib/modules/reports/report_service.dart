import '../../core/network/api_client.dart';
import '../../core/network/page_result.dart';
import 'report_models.dart';

/// 举报管理 API。
class ReportService {
  static Future<PageResult<ReportListItem>> list({
    String? query,
    String? status,
    String? targetType,
    String? reasonCode,
    int page = 1,
    int pageSize = 20,
  }) async {
    final data = await ApiClient.instance.get('/reports', query: {
      if (query != null && query.isNotEmpty) 'q': query,
      if (status != null && status.isNotEmpty) 'status': status,
      if (targetType != null && targetType.isNotEmpty) 'target_type': targetType,
      if (reasonCode != null && reasonCode.isNotEmpty) 'reason_code': reasonCode,
      'page': page,
      'page_size': pageSize,
    });
    return PageResult.fromData(data, ReportListItem.fromJson);
  }

  static Future<ReportDetail> detail(String id) async {
    final data = await ApiClient.instance.get('/reports/$id');
    final map = (data as Map).cast<String, dynamic>();
    return ReportDetail.fromJson(
        (map['report'] as Map).cast<String, dynamic>());
  }

  /// 处理举报。action: reject/ban_user/ban_group/no_action/duplicate；note 必填。
  static Future<void> resolve(String id, String action, String note) {
    return ApiClient.instance.post('/reports/$id/resolve', data: {
      'action': action,
      'note': note,
    });
  }

  /// 获取附件预签名查看地址。
  static Future<String> attachmentUrl(String reportId, String attachmentId) async {
    final data = await ApiClient.instance
        .get('/reports/$reportId/attachments/$attachmentId');
    return ((data as Map)['url'] ?? '').toString();
  }
}
