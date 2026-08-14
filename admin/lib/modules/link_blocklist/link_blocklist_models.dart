/// 链接黑名单规则（后台数据模型）。
class LinkBlocklistRule {
  LinkBlocklistRule({
    required this.id,
    required this.kind,
    required this.value,
    required this.severity,
    required this.source,
    required this.enabled,
    this.note = '',
    this.hitCount = 0,
    this.lastHitAt,
    this.createdAt,
    this.updatedAt,
  });

  final int id;
  final String kind;
  final String value;
  final String severity;
  final String source;
  final bool enabled;
  final String note;
  final int hitCount;
  final DateTime? lastHitAt;
  final DateTime? createdAt;
  final DateTime? updatedAt;

  factory LinkBlocklistRule.fromJson(Map<String, dynamic> json) {
    return LinkBlocklistRule(
      id: _parseInt(json['id']),
      kind: (json['kind'] as String?) ?? 'domain',
      value: (json['value'] as String?) ?? '',
      severity: (json['severity'] as String?) ?? 'malicious',
      source: (json['source'] as String?) ?? 'manual',
      enabled: (json['enabled'] as bool?) ?? true,
      note: (json['note'] as String?) ?? '',
      hitCount: (json['hit_count'] as num?)?.toInt() ?? 0,
      lastHitAt: _parseTime(json['last_hit_at']),
      createdAt: _parseTime(json['created_at']),
      updatedAt: _parseTime(json['updated_at']),
    );
  }

  static int _parseInt(dynamic v) {
    if (v is int) return v;
    if (v is num) return v.toInt();
    if (v is String) return int.tryParse(v) ?? 0;
    return 0;
  }

  static DateTime? _parseTime(dynamic v) {
    if (v == null) return null;
    if (v is String && v.isNotEmpty) return DateTime.tryParse(v);
    return null;
  }
}

/// 设置项（与后端 `LinkSafetySettings` 对齐）。
class LinkSafetySettings {
  LinkSafetySettings({
    required this.enabled,
    required this.ownDomainWhitelist,
    required this.maliciousCacheTtlMs,
    required this.cleanCacheTtlMs,
    required this.externalIntelEnable,
  });

  final bool enabled;
  final List<String> ownDomainWhitelist;
  final int maliciousCacheTtlMs;
  final int cleanCacheTtlMs;
  final bool externalIntelEnable;

  factory LinkSafetySettings.fromJson(Map<String, dynamic> json) {
    return LinkSafetySettings(
      enabled: (json['enabled'] as bool?) ?? true,
      ownDomainWhitelist:
          ((json['own_domain_whitelist'] as List?) ?? const [])
              .map((e) => e.toString())
              .toList(),
      maliciousCacheTtlMs:
          (json['malicious_cache_ttl_ms'] as num?)?.toInt() ?? 24 * 3600 * 1000,
      cleanCacheTtlMs:
          (json['clean_cache_ttl_ms'] as num?)?.toInt() ?? 10 * 60 * 1000,
      externalIntelEnable:
          (json['external_intel_enable'] as bool?) ?? false,
    );
  }

  Map<String, dynamic> toJson() => {
        'enabled': enabled,
        'own_domain_whitelist': ownDomainWhitelist,
        'malicious_cache_ttl_ms': maliciousCacheTtlMs,
        'clean_cache_ttl_ms': cleanCacheTtlMs,
        'external_intel_enable': externalIntelEnable,
      };
}

/// 在线测试结果。
class LinkTestResult {
  LinkTestResult({
    required this.url,
    required this.verdict,
    required this.canonicalHost,
    required this.reason,
    required this.ruleSource,
    required this.ruleId,
  });

  final String url;
  final String verdict;
  final String canonicalHost;
  final String reason;
  final String ruleSource;
  final int ruleId;

  factory LinkTestResult.fromJson(Map<String, dynamic> json) {
    return LinkTestResult(
      url: (json['url'] as String?) ?? '',
      verdict: (json['verdict'] as String?) ?? 'clean',
      canonicalHost: (json['canonical_host'] as String?) ?? '',
      reason: (json['reason'] as String?) ?? '',
      ruleSource: (json['rule_source'] as String?) ?? '',
      ruleId: LinkBlocklistRule._parseInt(json['rule_id']),
    );
  }
}

const List<String> kLinkRuleKinds = ['domain', 'wildcard', 'regex', 'keyword'];
const List<String> kLinkRuleSeverities = ['malicious', 'suspicious'];

/// 导入结果汇总
class LinkBlocklistImportResult {
  LinkBlocklistImportResult({
    required this.created,
    required this.skipped,
    required this.failures,
  });

  final int created;
  final int skipped;
  final List<LinkBlocklistImportFailure> failures;

  factory LinkBlocklistImportResult.fromJson(Map<String, dynamic> json) {
    final raw = (json['failures'] as List?) ?? const [];
    return LinkBlocklistImportResult(
      created: (json['created'] as num?)?.toInt() ?? 0,
      skipped: (json['skipped'] as num?)?.toInt() ?? 0,
      failures: raw
          .map((e) => LinkBlocklistImportFailure.fromJson(
              (e as Map).cast<String, dynamic>()))
          .toList(),
    );
  }
}

class LinkBlocklistImportFailure {
  LinkBlocklistImportFailure({required this.line, required this.reason});
  final int line;
  final String reason;

  factory LinkBlocklistImportFailure.fromJson(Map<String, dynamic> json) {
    return LinkBlocklistImportFailure(
      line: (json['line'] as num?)?.toInt() ?? 0,
      reason: (json['reason'] as String?) ?? '',
    );
  }
}

/// 拦截统计看板数据
class LinkBlocklistStats {
  LinkBlocklistStats({
    required this.blockedToday,
    required this.blocked7d,
    required this.blocked30d,
    required this.warnedToday,
    required this.warned7d,
    required this.topRules,
    required this.topHosts,
    required this.activeRulesCount,
    required this.disabledRulesCount,
  });

  final int blockedToday;
  final int blocked7d;
  final int blocked30d;
  final int warnedToday;
  final int warned7d;
  final List<LinkBlocklistTopItem> topRules;
  final List<LinkBlocklistTopItem> topHosts;
  final int activeRulesCount;
  final int disabledRulesCount;

  factory LinkBlocklistStats.fromJson(Map<String, dynamic> json) {
    List<LinkBlocklistTopItem> parseTop(String key) {
      final raw = (json[key] as List?) ?? const [];
      return raw
          .map((e) => LinkBlocklistTopItem.fromJson(
              (e as Map).cast<String, dynamic>()))
          .toList();
    }

    return LinkBlocklistStats(
      blockedToday: (json['blocked_today'] as num?)?.toInt() ?? 0,
      blocked7d: (json['blocked_7d'] as num?)?.toInt() ?? 0,
      blocked30d: (json['blocked_30d'] as num?)?.toInt() ?? 0,
      warnedToday: (json['warned_today'] as num?)?.toInt() ?? 0,
      warned7d: (json['warned_7d'] as num?)?.toInt() ?? 0,
      topRules: parseTop('top_rules'),
      topHosts: parseTop('top_hosts'),
      activeRulesCount: (json['active_rules_count'] as num?)?.toInt() ?? 0,
      disabledRulesCount: (json['disabled_rules_count'] as num?)?.toInt() ?? 0,
    );
  }
}

class LinkBlocklistTopItem {
  LinkBlocklistTopItem({required this.key, required this.count});
  final String key;
  final int count;

  factory LinkBlocklistTopItem.fromJson(Map<String, dynamic> json) {
    return LinkBlocklistTopItem(
      key: (json['key'] as String?) ?? '',
      count: (json['count'] as num?)?.toInt() ?? 0,
    );
  }
}
