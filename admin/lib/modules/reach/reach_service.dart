import '../../core/network/api_client.dart';
import '../../core/network/page_result.dart';
import 'reach_models.dart';

class ReachService {
  static Future<PageResult<ReachTask>> listTasks({
    String? status,
    String? kind,
    int page = 1,
    int pageSize = 20,
  }) async {
    final data = await ApiClient.instance.get('/reach/tasks', query: {
      if (status != null && status.isNotEmpty) 'status': status,
      if (kind != null && kind.isNotEmpty) 'kind': kind,
      'page': page,
      'page_size': pageSize,
    });
    final map = (data as Map).cast<String, dynamic>();
    final raw = (map['tasks'] as List?) ?? const [];
    final items = raw
        .map((e) => ReachTask.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
    return PageResult(
      items: items,
      total: (map['total'] as num?)?.toInt() ?? 0,
      page: page,
      pageSize: pageSize,
    );
  }

  static Future<ReachTaskDetail> getTask(String id) async {
    final data = await ApiClient.instance.get('/reach/tasks/$id');
    final map = (data as Map).cast<String, dynamic>();
    return ReachTaskDetail.fromJson(map);
  }

  static Future<void> pauseTask(String id) {
    return ApiClient.instance.post('/reach/tasks/$id/pause');
  }

  static Future<void> cancelTask(String id) {
    return ApiClient.instance.post('/reach/tasks/$id/cancel');
  }

  static Future<void> resumeTask(String id) {
    return ApiClient.instance.post('/reach/tasks/$id/resume');
  }

  static Future<void> updateTaskContent(
    String id,
    ReachAnnouncementContent content,
  ) {
    return ApiClient.instance
        .put('/reach/tasks/$id/content', data: content.toJson());
  }

  static Future<void> sendTask(String id) {
    return ApiClient.instance.post('/reach/tasks/$id/send');
  }

  static Future<ReachTaskStats> getTaskStats(String id) async {
    final data = await ApiClient.instance.get('/reach/tasks/$id/stats');
    final map = (data as Map).cast<String, dynamic>();
    return ReachTaskStats.fromJson(map);
  }

  static Future<PageResult<ReachTemplate>> listTemplates({
    int page = 1,
    int pageSize = 20,
  }) async {
    final data = await ApiClient.instance.get('/reach/templates', query: {
      'page': page,
      'page_size': pageSize,
    });
    final map = (data as Map).cast<String, dynamic>();
    final raw = (map['templates'] as List?) ?? const [];
    final items = raw
        .map(
            (e) => ReachTemplate.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
    return PageResult(
      items: items,
      total: (map['total'] as num?)?.toInt() ?? 0,
      page: page,
      pageSize: pageSize,
    );
  }

  static Future<ReachTemplate> getTemplate(String id) async {
    final data = await ApiClient.instance.get('/reach/templates/$id');
    final map = (data as Map).cast<String, dynamic>();
    return ReachTemplate.fromJson(map);
  }

  static Future<ReachTemplate> createTemplate({
    required String name,
    required String title,
    String inAppBody = '',
    String pushBody = '',
    String emailHtml = '',
    String smsBody = '',
  }) async {
    final data = await ApiClient.instance.post('/reach/templates', data: {
      'name': name,
      'title': title,
      'in_app_body': inAppBody,
      'push_body': pushBody,
      'email_html': emailHtml,
      'sms_body': smsBody,
    });
    final map = (data as Map).cast<String, dynamic>();
    return ReachTemplate.fromJson(map);
  }

  static Future<ReachTemplate> updateTemplate(
    String id, {
    required String name,
    required String title,
    String inAppBody = '',
    String pushBody = '',
    String emailHtml = '',
    String smsBody = '',
  }) async {
    final data = await ApiClient.instance.put('/reach/templates/$id', data: {
      'name': name,
      'title': title,
      'in_app_body': inAppBody,
      'push_body': pushBody,
      'email_html': emailHtml,
      'sms_body': smsBody,
    });
    final map = (data as Map).cast<String, dynamic>();
    return ReachTemplate.fromJson(map);
  }

  static Future<void> deleteTemplate(String id) {
    return ApiClient.instance.delete('/reach/templates/$id');
  }

  static Future<int> previewAudience(Map<String, dynamic> audience) async {
    final data =
        await ApiClient.instance.post('/reach/audience/preview', data: audience);
    final map = (data as Map).cast<String, dynamic>();
    return int.tryParse((map['count'] ?? '0').toString()) ?? 0;
  }

  static Future<ReachTask> createMarketingTask({
    required String templateId,
    required List<String> channels,
    String region = '',
    Map<String, dynamic>? audience,
    DateTime? scheduledAt,
  }) async {
    final data =
        await ApiClient.instance.post('/reach/tasks/marketing', data: {
      'template_id': templateId,
      'channels': channels,
      'region': region,
      if (audience != null) 'audience': audience,
      if (scheduledAt != null) 'scheduled_at': scheduledAt.toUtc().toIso8601String(),
    });
    final map = (data as Map).cast<String, dynamic>();
    return ReachTask.fromJson(map);
  }

  static Future<ABTestResult> createABTest({
    required List<Map<String, dynamic>> variants,
    required List<String> channels,
    String region = '',
    Map<String, dynamic>? audience,
    DateTime? scheduledAt,
  }) async {
    final data =
        await ApiClient.instance.post('/reach/tasks/ab-test', data: {
      'variants': variants,
      'channels': channels,
      'region': region,
      if (audience != null) 'audience': audience,
      if (scheduledAt != null) 'scheduled_at': scheduledAt.toUtc().toIso8601String(),
    });
    final map = (data as Map).cast<String, dynamic>();
    return ABTestResult.fromJson(map);
  }

  static Future<ABTestStats> getABTestStats(String groupId) async {
    final data =
        await ApiClient.instance.get('/reach/ab/$groupId/stats');
    final map = (data as Map).cast<String, dynamic>();
    return ABTestStats.fromJson(map);
  }

  static Future<ReachSubscriptionOverview> getSubscriptionOverview() async {
    final data =
        await ApiClient.instance.get('/reach/subscriptions/overview');
    final map = (data as Map).cast<String, dynamic>();
    return ReachSubscriptionOverview.fromJson(map);
  }
}
