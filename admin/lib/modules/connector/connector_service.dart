import '../../core/network/api_client.dart';

class ConnectorRelease {
  ConnectorRelease({required this.id, required this.clientType, required this.version, required this.channel, required this.changelog, required this.npmPackage, required this.npmTag, required this.force, required this.status, required this.statusLabel, required this.publishedAt, required this.createdAt});
  final String id, clientType, version, channel, changelog, npmPackage, npmTag, statusLabel, publishedAt, createdAt;
  final int status;
  final bool force;
  bool get isDraft => status == 1;
  bool get isPublished => status == 2;
  bool get isPaused => status == 3;
  bool get isRevoked => status == 4;
  bool get isHermes => clientType == 'grix-hermes';
  factory ConnectorRelease.fromJson(Map<String, dynamic> j) => ConnectorRelease(
    id: (j['id'] ?? '').toString(), clientType: (j['client_type'] ?? 'grix-connector').toString(),
    version: (j['version'] ?? '').toString(),
    channel: (j['channel'] ?? '').toString(), changelog: (j['changelog'] ?? '').toString(),
    npmPackage: (j['npm_package'] ?? '').toString(), npmTag: (j['npm_tag'] ?? '').toString(),
    force: j['force'] == true, status: (j['status'] as num?)?.toInt() ?? 0,
    statusLabel: (j['status_label'] ?? '').toString(),
    publishedAt: (j['published_at'] ?? '').toString(), createdAt: (j['created_at'] ?? '').toString(),
  );
}

class ConnectorRolloutRule {
  ConnectorRolloutRule({required this.id, required this.releaseId, required this.ruleType, required this.ruleValue, required this.priority, required this.status, required this.statusLabel});
  final String id, releaseId, ruleType, ruleValue, statusLabel;
  final int priority, status;
  bool get isActive => status == 1;
  factory ConnectorRolloutRule.fromJson(Map<String, dynamic> j) => ConnectorRolloutRule(
    id: (j['id'] ?? '').toString(), releaseId: (j['release_id'] ?? '').toString(),
    ruleType: (j['rule_type'] ?? '').toString(), ruleValue: (j['rule_value'] ?? '').toString(),
    priority: (j['priority'] as num?)?.toInt() ?? 0, status: (j['status'] as num?)?.toInt() ?? 0,
    statusLabel: (j['status_label'] ?? '').toString(),
  );
}

class ConnectorUpgradeReport {
  ConnectorUpgradeReport({required this.id, required this.agentId, required this.fromVersion, required this.toVersion, required this.status, required this.errorMsg, required this.createdAt});
  final String id, agentId, fromVersion, toVersion, status, errorMsg, createdAt;
  factory ConnectorUpgradeReport.fromJson(Map<String, dynamic> j) => ConnectorUpgradeReport(
    id: (j['id'] ?? '').toString(), agentId: (j['agent_id'] ?? '').toString(),
    fromVersion: (j['from_version'] ?? '').toString(), toVersion: (j['to_version'] ?? '').toString(),
    status: (j['status'] ?? '').toString(),
    errorMsg: (j['error_msg'] ?? '').toString(), createdAt: (j['reported_at'] ?? '').toString(),
  );
  bool get isSuccess => status == 'success' || status == 'installed';
  bool get isFailed => status == 'failed' || status == 'rolled_back';
  String get statusLabel {
    switch (status) {
      case 'installed': return '已安装';
      case 'success': return '成功';
      case 'failed': return '失败';
      case 'rolled_back': return '已回滚';
      default: return status.isEmpty ? '未知' : status;
    }
  }
}

class ConnectorUpgradeStats {
  ConnectorUpgradeStats({required this.total, required this.success, required this.failed, required this.pending, required this.errorDistribution});
  final int total, success, failed, pending;
  final Map<String, int> errorDistribution;
  factory ConnectorUpgradeStats.fromJson(Map<String, dynamic> j) => ConnectorUpgradeStats(
    total: (j['total'] as num?)?.toInt() ?? 0, success: (j['success'] as num?)?.toInt() ?? 0,
    failed: (j['failed'] as num?)?.toInt() ?? 0, pending: (j['pending'] as num?)?.toInt() ?? 0,
    errorDistribution: ((j['error_distribution'] as Map?) ?? {}).map((k, v) => MapEntry(k.toString(), (v as num?)?.toInt() ?? 0)),
  );
}

class ConnectorService {
  static Future<List<ConnectorRelease>> listReleases({String? clientType}) async {
    final data = await ApiClient.instance.get('/connector/releases', query: {
      if (clientType != null && clientType.isNotEmpty) 'client_type': clientType,
    });
    final m = (data as Map).cast<String, dynamic>();
    return ((m['releases'] as List?) ?? []).map((e) => ConnectorRelease.fromJson((e as Map).cast<String, dynamic>())).toList();
  }
  static Future<void> create(Map<String, dynamic> body) => ApiClient.instance.post('/connector/releases', data: body);
  static Future<void> publish(String id) => ApiClient.instance.post('/connector/releases/$id/publish');
  static Future<void> pause(String id) => ApiClient.instance.post('/connector/releases/$id/pause');
  static Future<void> resume(String id) => ApiClient.instance.post('/connector/releases/$id/resume');
  static Future<void> revoke(String id) => ApiClient.instance.post('/connector/releases/$id/revoke');
  static Future<Map<String, dynamic>> pushUpgrade() async {
    final data = await ApiClient.instance.post('/connector/releases/push-upgrade');
    return (data as Map).cast<String, dynamic>();
  }

  // 灰度规则
  static Future<List<ConnectorRolloutRule>> listRules(String releaseId) async {
    final data = await ApiClient.instance.get('/connector/releases/$releaseId/rules');
    final m = (data as Map).cast<String, dynamic>();
    return ((m['rules'] as List?) ?? []).map((e) => ConnectorRolloutRule.fromJson((e as Map).cast<String, dynamic>())).toList();
  }
  static Future<void> createRule(Map<String, dynamic> body) => ApiClient.instance.post('/connector/rollout-rules', data: body);
  static Future<void> toggleRule(String id, int status) => ApiClient.instance.post('/connector/rollout-rules/$id/toggle', data: {'status': status});
  static Future<void> deleteRule(String id) => ApiClient.instance.delete('/connector/rollout-rules/$id');

  // 升级报告
  static Future<({List<ConnectorUpgradeReport> reports, int total})> listReports({String? clientType, String? toVersion, String? status, int page = 1, int pageSize = 20}) async {
    final data = await ApiClient.instance.get('/connector/reports', query: {
      if (clientType != null && clientType.isNotEmpty) 'client_type': clientType,
      if (toVersion != null && toVersion.isNotEmpty) 'to_version': toVersion,
      if (status != null && status.isNotEmpty) 'status': status,
      'page': page, 'page_size': pageSize,
    });
    final m = (data as Map).cast<String, dynamic>();
    final list = ((m['reports'] as List?) ?? []).map((e) => ConnectorUpgradeReport.fromJson((e as Map).cast<String, dynamic>())).toList();
    return (reports: list, total: (m['total'] as num?)?.toInt() ?? 0);
  }

  // 升级统计
  static Future<ConnectorUpgradeStats?> stats(String version, {String? clientType}) async {
    try {
      final data = await ApiClient.instance.get('/connector/stats', query: {
        'version': version,
        if (clientType != null && clientType.isNotEmpty) 'client_type': clientType,
      });
      final m = (data as Map).cast<String, dynamic>();
      if (m['stats'] == null) return null;
      return ConnectorUpgradeStats.fromJson((m['stats'] as Map).cast<String, dynamic>());
    } catch (_) { return null; }
  }
}
