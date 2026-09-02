class AppRelease {
  AppRelease({
    required this.id,
    required this.version,
    required this.buildNumber,
    required this.platform,
    required this.channel,
    required this.changelog,
    required this.statusLabel,
    required this.status,
    required this.publishedAt,
    required this.createdAt,
    required this.updateMethod,
    required this.downloadUrl,
    required this.appStoreUrl,
    required this.fileSize,
  });
  final String id,
      version,
      platform,
      channel,
      changelog,
      statusLabel,
      publishedAt,
      createdAt,
      updateMethod,
      downloadUrl,
      appStoreUrl;
  final int buildNumber, status;
  final int fileSize;

  bool get isDraft => status == 1;
  bool get isPublished => status == 2;
  bool get isPaused => status == 3;
  bool get isRevoked => status == 4;

  factory AppRelease.fromJson(Map<String, dynamic> j) => AppRelease(
    id: (j['id'] ?? '').toString(),
    version: (j['version'] ?? '').toString(),
    buildNumber: (j['build_number'] as num?)?.toInt() ?? 0,
    platform: (j['platform'] ?? '').toString(),
    channel: (j['channel'] ?? '').toString(),
    changelog: (j['changelog'] ?? '').toString(),
    status: (j['status'] as num?)?.toInt() ?? 0,
    statusLabel: (j['status_label'] ?? '').toString(),
    publishedAt: (j['published_at'] ?? '').toString(),
    createdAt: (j['created_at'] ?? '').toString(),
    updateMethod: (j['update_method'] ?? '').toString(),
    downloadUrl: (j['download_url'] ?? '').toString(),
    appStoreUrl: (j['app_store_url'] ?? '').toString(),
    fileSize: (j['file_size'] as num?)?.toInt() ?? 0,
  );
}

class AppRolloutRule {
  AppRolloutRule({
    required this.id,
    required this.releaseId,
    required this.ruleType,
    required this.ruleValue,
    required this.priority,
    required this.status,
    required this.statusLabel,
  });
  final String id, releaseId, ruleType, ruleValue, statusLabel;
  final int priority, status;
  bool get isActive => status == 1;
  factory AppRolloutRule.fromJson(Map<String, dynamic> j) => AppRolloutRule(
    id: (j['id'] ?? '').toString(),
    releaseId: (j['release_id'] ?? '').toString(),
    ruleType: (j['rule_type'] ?? '').toString(),
    ruleValue: (j['rule_value'] ?? '').toString(),
    priority: (j['priority'] as num?)?.toInt() ?? 0,
    status: (j['status'] as num?)?.toInt() ?? 0,
    statusLabel: (j['status_label'] ?? '').toString(),
  );
}

class AppDownloadStats {
  AppDownloadStats({
    required this.version,
    required this.platform,
    required this.total,
    required this.success,
    required this.failed,
    required this.avgDurationMs,
  });
  final String version, platform;
  final int total, success, failed;
  final double avgDurationMs;
  factory AppDownloadStats.fromJson(Map<String, dynamic> j) => AppDownloadStats(
    version: (j['version'] ?? '').toString(),
    platform: (j['platform'] ?? '').toString(),
    total: (j['total'] as num?)?.toInt() ?? 0,
    success: (j['success'] as num?)?.toInt() ?? 0,
    failed: (j['failed'] as num?)?.toInt() ?? 0,
    avgDurationMs: (j['avg_duration_ms'] as num?)?.toDouble() ?? 0,
  );
}
