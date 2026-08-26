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

/// 指定版本上仍未自愈的问题机器所属用户。手机号只有末四位脱敏串。
class ConnectorProblemUser {
  ConnectorProblemUser({required this.userId, required this.nickname, required this.email, required this.phoneMasked, required this.agentIds, required this.failedHosts, required this.errorCodes, required this.lastReportedAt});
  final String userId, nickname, email, phoneMasked, lastReportedAt;
  final List<String> agentIds, errorCodes;
  final int failedHosts;
  bool get hasEmail => email.isNotEmpty;
  bool get hasPhone => phoneMasked.isNotEmpty;
  factory ConnectorProblemUser.fromJson(Map<String, dynamic> j) => ConnectorProblemUser(
    userId: (j['user_id'] ?? '').toString(), nickname: (j['nickname'] ?? '').toString(),
    email: (j['email'] ?? '').toString(), phoneMasked: (j['phone_masked'] ?? '').toString(),
    agentIds: ((j['agent_ids'] as List?) ?? []).map((e) => e.toString()).toList(),
    failedHosts: (j['failed_hosts'] as num?)?.toInt() ?? 0,
    errorCodes: ((j['error_codes'] as List?) ?? []).map((e) => e.toString()).toList(),
    lastReportedAt: (j['last_reported_at'] ?? '').toString(),
  );
}

/// 发送前预览：邮件走阿里云模板渲染，短信是纯文本。
class ConnectorNotifyPreview {
  ConnectorNotifyPreview({required this.emailSubject, required this.emailHtml, required this.emailError, required this.smsText, required this.smsError});
  final String emailSubject, emailHtml, emailError, smsText, smsError;
  factory ConnectorNotifyPreview.fromJson(Map<String, dynamic> j) => ConnectorNotifyPreview(
    emailSubject: (j['email_subject'] ?? '').toString(), emailHtml: (j['email_html'] ?? '').toString(),
    emailError: (j['email_error'] ?? '').toString(), smsText: (j['sms_text'] ?? '').toString(),
    smsError: (j['sms_error'] ?? '').toString(),
  );
}

/// 单个用户的发送结果。
class ConnectorNotifyResult {
  ConnectorNotifyResult({required this.userId, required this.channel, required this.status, required this.error});
  final String userId, channel, status, error;
  bool get isSent => status == 'sent';
  factory ConnectorNotifyResult.fromJson(Map<String, dynamic> j) => ConnectorNotifyResult(
    userId: (j['user_id'] ?? '').toString(), channel: (j['channel'] ?? '').toString(),
    status: (j['status'] ?? '').toString(), error: (j['error'] ?? '').toString(),
  );
  String get statusLabel {
    switch (status) {
      case 'sent': return '已发送';
      case 'duplicate': return '已发过';
      case 'skipped': return '跳过';
      case 'not_configured': return '未配置';
      case 'failed': return '失败';
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

  // 问题用户 + 失败告知
  static Future<({List<ConnectorProblemUser> users, int total})> listProblemUsers({
    required String version, String? clientType, String? statuses, bool includeUnsupported = false,
    int page = 1, int pageSize = 20,
  }) async {
    final data = await ApiClient.instance.get('/connector/reports/problem-users', query: {
      'version': version,
      if (clientType != null && clientType.isNotEmpty) 'client_type': clientType,
      if (statuses != null && statuses.isNotEmpty) 'statuses': statuses,
      if (includeUnsupported) 'include_unsupported': '1',
      'page': page, 'page_size': pageSize,
    });
    final m = (data as Map).cast<String, dynamic>();
    final list = ((m['users'] as List?) ?? []).map((e) => ConnectorProblemUser.fromJson((e as Map).cast<String, dynamic>())).toList();
    return (users: list, total: (m['total'] as num?)?.toInt() ?? 0);
  }

  static Future<ConnectorNotifyPreview> previewNotify({required String title, required String body, String? sampleUserId}) async {
    final data = await ApiClient.instance.post('/connector/reports/notify/preview', data: {
      'title': title, 'body': body,
      if (sampleUserId != null && sampleUserId.isNotEmpty) 'sample_user_id': sampleUserId,
    });
    final m = (data as Map).cast<String, dynamic>();
    return ConnectorNotifyPreview.fromJson((m['preview'] as Map).cast<String, dynamic>());
  }

  static Future<List<ConnectorNotifyResult>> notifyProblemUsers({
    required String version, required List<String> userIds, required String channel,
    required String title, required String body,
  }) async {
    final data = await ApiClient.instance.post('/connector/reports/notify', data: {
      'version': version, 'user_ids': userIds, 'channel': channel, 'title': title, 'body': body,
    });
    final m = (data as Map).cast<String, dynamic>();
    return ((m['results'] as List?) ?? []).map((e) => ConnectorNotifyResult.fromJson((e as Map).cast<String, dynamic>())).toList();
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
