import 'dart:convert';

import 'package:shared_preferences/shared_preferences.dart';

/// 不需要绑定工作目录的 agent 接入类型（排除制）。
/// 空白聊天页的快捷绑定目录组件对这两类之外的所有 agent 展示；
/// 接入类型未知（空串）时不展示。
/// ⚠️ 新增"非 CLI、不该绑目录"的接入类型时，须同步加进本排除集，
/// 否则会默认展示快捷绑定组件。
const Set<String> kDirectoryBindExemptAgentClientTypes = {'hermes', 'openclaw'};

bool isDirectoryBoundAgentClientType(String clientType) {
  final normalized = clientType.trim().toLowerCase();
  if (normalized.isEmpty) return false;
  return !kDirectoryBindExemptAgentClientTypes.contains(normalized);
}

/// 一条最近绑定过的目录记录。
class RecentBindDirectoryEntry {
  const RecentBindDirectoryEntry({
    required this.path,
    required this.agentId,
    required this.hostname,
    required this.updatedAtMs,
  });

  /// 绝对目录路径。
  final String path;

  /// 绑定时所属的 agent。
  final String agentId;

  /// 绑定时 agent 所在的宿主机名（connector 上报）。
  /// 目录路径只在同一台机器上有效，跨 agent 补位仅限同机器。
  final String hostname;

  /// 最近一次绑定时间（毫秒时间戳），用于 MRU 排序。
  final int updatedAtMs;

  /// 展示用目录名：取路径最后一段（兼容 / 与 \ 分隔符）。
  String get displayName {
    final normalized = path.replaceAll('\\', '/');
    final segments = normalized
        .split('/')
        .where((s) => s.trim().isNotEmpty)
        .toList(growable: false);
    if (segments.isEmpty) return path;
    return segments.last;
  }

  Map<String, dynamic> toJson() => <String, dynamic>{
    'path': path,
    'agent_id': agentId,
    'hostname': hostname,
    'updated_at_ms': updatedAtMs,
  };

  static RecentBindDirectoryEntry? fromJson(dynamic raw) {
    if (raw is! Map) return null;
    final path = (raw['path'] as String? ?? '').trim();
    if (path.isEmpty) return null;
    final agentId = (raw['agent_id'] as String? ?? '').trim();
    final hostname = (raw['hostname'] as String? ?? '').trim();
    final updatedAtMs = raw['updated_at_ms'];
    return RecentBindDirectoryEntry(
      path: path,
      agentId: agentId,
      hostname: hostname,
      updatedAtMs: updatedAtMs is int ? updatedAtMs : 0,
    );
  }
}

/// 最近绑定目录的本地缓存（SharedPreferences，纯前端）。
///
/// 每次绑定成功后调用 [record]，新记录排到最前（MRU）；
/// 空白聊天页读取 [listForAgent] 渲染快捷绑定列表。
class ChatRecentBindDirectoryStore {
  ChatRecentBindDirectoryStore({Future<SharedPreferences> Function()? prefs})
    : _prefs = prefs ?? SharedPreferences.getInstance;

  static const String storageKey = 'chat_recent_bind_directories';

  /// 本地最多保留的记录条数。
  static const int maxStoredEntries = 30;

  /// 单次展示的最大条数。
  static const int defaultDisplayLimit = 10;

  final Future<SharedPreferences> Function() _prefs;

  /// 记录一次成功的目录绑定：同 agent 同 path 去重后插到最前。
  Future<void> record({
    required String path,
    required String agentId,
    String hostname = '',
    int? nowMs,
  }) async {
    final normalizedPath = _normalizePath(path);
    if (normalizedPath.isEmpty) return;
    final normalizedAgentId = agentId.trim();
    final entries = await _loadAll();
    entries.removeWhere(
      (e) => e.path == normalizedPath && e.agentId == normalizedAgentId,
    );
    entries.insert(
      0,
      RecentBindDirectoryEntry(
        path: normalizedPath,
        agentId: normalizedAgentId,
        hostname: hostname.trim(),
        updatedAtMs: nowMs ?? DateTime.now().millisecondsSinceEpoch,
      ),
    );
    if (entries.length > maxStoredEntries) {
      entries.removeRange(maxStoredEntries, entries.length);
    }
    await _saveAll(entries);
  }

  /// 给某个 agent 的展示列表：该 agent 自己的记录优先（MRU 序），
  /// 再补同一台机器上其他 agent 的记录（同 path 只出现一次），
  /// 总数不超过 [limit]。
  ///
  /// 目录路径只在同一台机器上有效：[hostname] 为空（agent 未上报机器名）
  /// 时不做跨 agent 补位，只展示该 agent 自己的记录。
  Future<List<RecentBindDirectoryEntry>> listForAgent(
    String agentId, {
    String hostname = '',
    int limit = defaultDisplayLimit,
  }) async {
    final normalizedAgentId = agentId.trim();
    final normalizedHostname = hostname.trim();
    final entries = await _loadAll();
    final result = <RecentBindDirectoryEntry>[];
    final seenPaths = <String>{};
    for (final entry in entries) {
      if (entry.agentId != normalizedAgentId) continue;
      if (!seenPaths.add(entry.path)) continue;
      result.add(entry);
      if (result.length >= limit) return result;
    }
    if (normalizedHostname.isEmpty) return result;
    for (final entry in entries) {
      if (entry.agentId == normalizedAgentId) continue;
      if (entry.hostname != normalizedHostname) continue;
      if (!seenPaths.add(entry.path)) continue;
      result.add(entry);
      if (result.length >= limit) return result;
    }
    return result;
  }

  Future<List<RecentBindDirectoryEntry>> _loadAll() async {
    try {
      final prefs = await _prefs();
      final raw = prefs.getString(storageKey);
      if (raw == null || raw.isEmpty) return <RecentBindDirectoryEntry>[];
      final decoded = jsonDecode(raw);
      if (decoded is! List) return <RecentBindDirectoryEntry>[];
      return decoded
          .map(RecentBindDirectoryEntry.fromJson)
          .whereType<RecentBindDirectoryEntry>()
          .toList();
    } catch (_) {
      // 本地缓存损坏时按空列表处理，不影响聊天主流程。
      return <RecentBindDirectoryEntry>[];
    }
  }

  Future<void> _saveAll(List<RecentBindDirectoryEntry> entries) async {
    try {
      final prefs = await _prefs();
      await prefs.setString(
        storageKey,
        jsonEncode(entries.map((e) => e.toJson()).toList(growable: false)),
      );
    } catch (_) {
      // 写缓存失败只影响下次快捷列表，静默忽略。
    }
  }

  static String _normalizePath(String path) {
    var normalized = path.trim();
    if (normalized.isEmpty) return '';
    // 去掉尾部分隔符（保留根目录 "/" 本身）。
    while (normalized.length > 1 &&
        (normalized.endsWith('/') || normalized.endsWith('\\'))) {
      normalized = normalized.substring(0, normalized.length - 1);
    }
    return normalized;
  }
}
