import '../../core/network/api_client.dart';
import '../../core/network/page_result.dart';
import 'link_blocklist_models.dart';

/// 链接黑名单后台 API。
class LinkBlocklistService {
  static Future<PageResult<LinkBlocklistRule>> listRules({
    String? query,
    String? kind,
    String? severity,
    String? source,
    bool? enabled,
    int page = 1,
    int pageSize = 20,
  }) async {
    final data =
        await ApiClient.instance.get('/link-blocklist/rules', query: {
      if (query != null && query.isNotEmpty) 'q': query,
      if (kind != null && kind.isNotEmpty) 'kind': kind,
      if (severity != null && severity.isNotEmpty) 'severity': severity,
      if (source != null && source.isNotEmpty) 'source': source,
      if (enabled != null) 'enabled': enabled ? '1' : '0',
      'page': page,
      'page_size': pageSize,
    });
    return PageResult.fromData(data, LinkBlocklistRule.fromJson);
  }

  static Future<LinkBlocklistRule> create({
    required String kind,
    required String value,
    required String severity,
    String source = 'manual',
    bool enabled = true,
    String note = '',
  }) async {
    final data = await ApiClient.instance.post(
      '/link-blocklist/rules',
      data: {
        'kind': kind,
        'value': value,
        'severity': severity,
        'source': source,
        'enabled': enabled,
        'note': note,
      },
    );
    return LinkBlocklistRule.fromJson(
      ((data as Map)['item'] as Map).cast<String, dynamic>(),
    );
  }

  static Future<LinkBlocklistRule> update(int id, {
    required String kind,
    required String value,
    required String severity,
    required String source,
    required bool enabled,
    String note = '',
  }) async {
    final data = await ApiClient.instance.put(
      '/link-blocklist/rules/$id',
      data: {
        'kind': kind,
        'value': value,
        'severity': severity,
        'source': source,
        'enabled': enabled,
        'note': note,
      },
    );
    return LinkBlocklistRule.fromJson(
      ((data as Map)['item'] as Map).cast<String, dynamic>(),
    );
  }

  static Future<void> remove(int id) async {
    await ApiClient.instance.delete('/link-blocklist/rules/$id');
  }

  static Future<int> batch(List<int> ids, String action) async {
    final data = await ApiClient.instance.post(
      '/link-blocklist/rules/batch',
      data: {
        'ids': ids.map((e) => e.toString()).toList(),
        'action': action,
      },
    );
    final m = (data as Map).cast<String, dynamic>();
    return (m['affected'] as num?)?.toInt() ?? 0;
  }

  static Future<LinkTestResult> test(String url) async {
    final data = await ApiClient.instance.post(
      '/link-blocklist/test',
      data: {'url': url},
    );
    return LinkTestResult.fromJson(
      ((data as Map)['result'] as Map).cast<String, dynamic>(),
    );
  }

  static Future<LinkSafetySettings> getSettings() async {
    final data = await ApiClient.instance.get('/link-blocklist/settings');
    return LinkSafetySettings.fromJson(
      ((data as Map)['settings'] as Map).cast<String, dynamic>(),
    );
  }

  static Future<void> updateSettings(LinkSafetySettings s) {
    return ApiClient.instance.put(
      '/link-blocklist/settings',
      data: s.toJson(),
    );
  }
}


/// 批量导入 / 统计 / 最近事件
class LinkBlocklistAnalytics {
  static Future<LinkBlocklistImportResult> importCSV(String csv) async {
    final data = await ApiClient.instance.post(
      '/link-blocklist/import',
      data: {'csv': csv},
    );
    return LinkBlocklistImportResult.fromJson(
      ((data as Map)['result'] as Map).cast<String, dynamic>(),
    );
  }

  static Future<LinkBlocklistStats> stats() async {
    final data = await ApiClient.instance.get('/link-blocklist/stats');
    return LinkBlocklistStats.fromJson(
      ((data as Map)['stats'] as Map).cast<String, dynamic>(),
    );
  }
}
