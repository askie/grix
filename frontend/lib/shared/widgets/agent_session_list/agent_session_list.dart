import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../data/providers/im_service.dart';
import '../../../modules/chat/services/chat_route_navigator.dart';
import '../../utils/time_formatter.dart';
import '../../utils/toast_util.dart';
import '../remote_file_picker/remote_file_picker_model.dart'
    show isDesktopPlatform;

class AgentSessionBindingEntry {
  AgentSessionBindingEntry({
    required this.aibotSessionId,
    required this.agentSessionId,
    required this.cwd,
    required this.workerStatus,
    required this.createdAt,
    required this.updatedAt,
    required this.archived,
    this.title,
  });

  final String aibotSessionId;
  final String agentSessionId;
  final String cwd;
  final String workerStatus;
  final int createdAt;
  final int updatedAt;
  final bool archived;
  final String? title;

  String get displayTitle {
    final t = title?.trim();
    if (t != null && t.isNotEmpty) return t;
    if (agentSessionId.isNotEmpty) return agentSessionId;
    return aibotSessionId;
  }

  bool get hasAibotSession => aibotSessionId.isNotEmpty;

  // 仅用于覆盖标题等局部字段，其余字段保持不变。
  AgentSessionBindingEntry copyWith({String? title}) {
    return AgentSessionBindingEntry(
      aibotSessionId: aibotSessionId,
      agentSessionId: agentSessionId,
      cwd: cwd,
      workerStatus: workerStatus,
      createdAt: createdAt,
      updatedAt: updatedAt,
      archived: archived,
      title: title ?? this.title,
    );
  }

  // Agent 侧会话是否已被删除：删除后该条目不可再进入会话。
  bool get isDeleted => workerStatus == 'deleted';

  // App 侧会话已删除时降级为未绑定条目：保留 provider 会话身份和 cwd，
  // 用户点击可重新导入，而不是让这条电脑端会话从列表里永久消失。
  AgentSessionBindingEntry asUnbound() {
    return AgentSessionBindingEntry(
      aibotSessionId: '',
      agentSessionId: agentSessionId,
      cwd: cwd,
      workerStatus: 'inactive',
      createdAt: createdAt,
      updatedAt: updatedAt,
      archived: archived,
      title: title,
    );
  }

  factory AgentSessionBindingEntry.fromMap(Map<String, dynamic> m) {
    String stringValue(Object? value) {
      final text = value?.toString().trim() ?? '';
      return text == 'null' ? '' : text;
    }

    int intValue(Object? value) {
      if (value is int) return value;
      // 旧版 connector 的时间戳是带小数的毫秒数（文件 mtime），按 num 解析后取整
      if (value is num) return value.toInt();
      return num.tryParse(value?.toString() ?? '')?.toInt() ?? 0;
    }

    final rawTitle = stringValue(m['title']);
    return AgentSessionBindingEntry(
      aibotSessionId: stringValue(m['aibotSessionId']),
      agentSessionId: stringValue(m['agentSessionId']),
      cwd: stringValue(m['cwd']),
      workerStatus: stringValue(m['workerStatus']).isEmpty
          ? 'inactive'
          : stringValue(m['workerStatus']),
      createdAt: intValue(m['createdAt']),
      updatedAt: intValue(m['updatedAt']),
      archived: m['archived'] == true,
      title: rawTitle.isEmpty ? null : rawTitle,
    );
  }
}

class AgentSessionBindResult {
  const AgentSessionBindResult({
    required this.sessionId,
    this.status = '',
    this.binding = const <String, dynamic>{},
  });

  final String sessionId;
  final String status;
  final Map<String, dynamic> binding;

  factory AgentSessionBindResult.fromMap(Map<String, dynamic> m) {
    final binding = m['binding'] is Map
        ? Map<String, dynamic>.from(m['binding'] as Map)
        : <String, dynamic>{};
    return AgentSessionBindResult(
      sessionId: m['session_id']?.toString().trim() ?? '',
      status: m['status']?.toString().trim() ?? '',
      binding: binding,
    );
  }
}

class AgentSessionCwdGroup {
  AgentSessionCwdGroup({required this.cwd, required this.entries});

  final String cwd;
  final List<AgentSessionBindingEntry> entries;

  int get latestUpdatedAt {
    var latest = 0;
    for (final entry in entries) {
      if (entry.updatedAt > latest) latest = entry.updatedAt;
    }
    return latest;
  }

  String get tabTitle {
    if (cwd.isEmpty) return 'session_list_unbound_dir'.tr;
    final parts = cwd.split(RegExp(r'[/\\]+')).where((e) => e.isNotEmpty);
    return parts.isEmpty ? cwd : parts.last;
  }
}

/// 把插件返回的原始条目整理成可展示列表：
/// - 已绑定且 App 会话仍存在：优先用本地会话标题覆盖；
/// - 已绑定但 App 会话已删除（本地查不到）：降级为未绑定条目，保留
///   agentSessionId 与 cwd 供重新导入；
/// - 孤儿绑定（无 provider 会话身份）且会话已删：无法重建，丢弃。
List<AgentSessionBindingEntry> resolveAgentSessionEntries(
  List<AgentSessionBindingEntry> raw, {
  required bool Function(String aibotSessionId) sessionExists,
  required String? Function(String aibotSessionId) localTitleFor,
}) {
  final entries = <AgentSessionBindingEntry>[];
  for (final entry in raw) {
    if (!entry.hasAibotSession) {
      entries.add(entry);
      continue;
    }
    if (sessionExists(entry.aibotSessionId)) {
      final title = localTitleFor(entry.aibotSessionId)?.trim();
      entries.add(
        (title == null || title.isEmpty) ? entry : entry.copyWith(title: title),
      );
      continue;
    }
    if (entry.agentSessionId.isNotEmpty && entry.cwd.isNotEmpty) {
      entries.add(entry.asUnbound());
    }
  }
  return entries;
}

typedef SessionBindingsProvider =
    Future<List<AgentSessionBindingEntry>> Function(String agentId);

typedef SessionBindProvider =
    Future<AgentSessionBindResult> Function({
      required String cwd,
      String agentSessionId,
      String title,
    });

class AgentSessionList extends StatefulWidget {
  const AgentSessionList({
    super.key,
    required this.agentId,
    required this.currentSessionId,
    required this.bindingsProvider,
    required this.bindProvider,
    this.agentClientType = '',
    this.scrollController,
  });

  final String agentId;
  final String currentSessionId;
  final String agentClientType;
  final SessionBindingsProvider bindingsProvider;
  final SessionBindProvider bindProvider;
  final ScrollController? scrollController;

  static final Map<String, String> _lastCwdByAgentKey = {};

  /// 清除缓存。新建会话、绑定变更等场景应调用此方法。
  /// [key] 为空时清除全部缓存。
  static void invalidateCache([String? key]) {
    if (key != null) {
      _AgentSessionListState._cache.remove(key);
    } else {
      _AgentSessionListState._cache.clear();
    }
  }

  static void show(
    BuildContext context, {
    required String agentId,
    required String currentSessionId,
    required SessionBindingsProvider bindingsProvider,
    required SessionBindProvider bindProvider,
    String agentClientType = '',
  }) {
    if (isDesktopPlatform) {
      _showAsDialog(
        context,
        agentId: agentId,
        currentSessionId: currentSessionId,
        agentClientType: agentClientType,
        bindingsProvider: bindingsProvider,
        bindProvider: bindProvider,
      );
    } else {
      _showAsBottomSheet(
        context,
        agentId: agentId,
        currentSessionId: currentSessionId,
        agentClientType: agentClientType,
        bindingsProvider: bindingsProvider,
        bindProvider: bindProvider,
      );
    }
  }

  static void _showAsBottomSheet(
    BuildContext context, {
    required String agentId,
    required String currentSessionId,
    required String agentClientType,
    required SessionBindingsProvider bindingsProvider,
    required SessionBindProvider bindProvider,
  }) {
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      constraints: const BoxConstraints(maxWidth: 520),
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(12)),
      ),
      builder: (_) => DraggableScrollableSheet(
        initialChildSize: 0.7,
        minChildSize: 0.35,
        maxChildSize: 0.9,
        expand: false,
        builder: (context, scrollController) => AgentSessionList(
          agentId: agentId,
          currentSessionId: currentSessionId,
          agentClientType: agentClientType,
          bindingsProvider: bindingsProvider,
          bindProvider: bindProvider,
          scrollController: scrollController,
        ),
      ),
    );
  }

  static void _showAsDialog(
    BuildContext context, {
    required String agentId,
    required String currentSessionId,
    required String agentClientType,
    required SessionBindingsProvider bindingsProvider,
    required SessionBindProvider bindProvider,
  }) {
    showDialog<void>( // dialog-guard-allow: 自定义尺寸 agent 会话列表容器
      context: context,
      builder: (context) => Dialog(
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
        child: SizedBox(
          width: MediaQuery.of(context).size.width.clamp(0, 560).toDouble(),
          height: MediaQuery.of(context).size.height.clamp(0, 560).toDouble(),
          child: AgentSessionList(
            agentId: agentId,
            currentSessionId: currentSessionId,
            agentClientType: agentClientType,
            bindingsProvider: bindingsProvider,
            bindProvider: bindProvider,
          ),
        ),
      ),
    );
  }

  @override
  State<AgentSessionList> createState() => _AgentSessionListState();
}

class _AgentSessionListState extends State<AgentSessionList> {
  // 按 cacheKey 缓存已加载的 entries，避免每次打开都请求。
  static final Map<String, List<AgentSessionBindingEntry>> _cache = {};

  List<AgentSessionCwdGroup> _groups = [];
  bool _loading = true;
  String? _error;
  int _selectedIndex = 0;
  String _busyKey = '';

  // 缓存必须按 agentId 隔离:同类型的多个 agent 各自对应一台机器的会话列表,
  // 按 agentClientType 共享会让 B agent 首屏显示 A agent 机器上的缓存列表。
  String get _cacheKey => widget.agentId;

  @override
  void initState() {
    super.initState();
    final cached = _cache[_cacheKey];
    if (cached != null) {
      _applyEntries(cached);
    } else {
      _loadBindings();
    }
  }

  void _applyEntries(List<AgentSessionBindingEntry> entries) {
    final groups = _buildGroups(entries);
    final cachedCwd = AgentSessionList._lastCwdByAgentKey[_cacheKey] ?? '';
    final cachedIndex = groups.indexWhere((g) => g.cwd == cachedCwd);
    setState(() {
      _groups = groups;
      _selectedIndex = cachedIndex >= 0 ? cachedIndex : 0;
      _loading = false;
      _error = null;
    });
  }

  Future<void> _loadBindings() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final results = await widget.bindingsProvider(widget.agentId);
      if (!mounted) return;
      _cache[_cacheKey] = results;
      _applyEntries(results);
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = userFacingError(
          e,
          fallback: 'im_session_list_send_failed'.tr,
        );
        _loading = false;
      });
    }
  }

  List<AgentSessionCwdGroup> _buildGroups(
    List<AgentSessionBindingEntry> entries,
  ) {
    final buckets = <String, List<AgentSessionBindingEntry>>{};
    for (final entry in entries) {
      buckets
          .putIfAbsent(entry.cwd, () => <AgentSessionBindingEntry>[])
          .add(entry);
    }
    final groups =
        buckets.entries.map((entry) {
          final rows = List<AgentSessionBindingEntry>.from(entry.value)
            ..sort((a, b) {
              final timeCompare = b.updatedAt.compareTo(a.updatedAt);
              if (timeCompare != 0) return timeCompare;
              return a.displayTitle.compareTo(b.displayTitle);
            });
          return AgentSessionCwdGroup(cwd: entry.key, entries: rows);
        }).toList()..sort((a, b) {
          if (a.cwd.isEmpty && b.cwd.isNotEmpty) return 1;
          if (a.cwd.isNotEmpty && b.cwd.isEmpty) return -1;
          final timeCompare = b.latestUpdatedAt.compareTo(a.latestUpdatedAt);
          if (timeCompare != 0) return timeCompare;
          return a.tabTitle.compareTo(b.tabTitle);
        });
    return groups;
  }

  Future<void> _openEntry(AgentSessionBindingEntry entry) async {
    if (entry.isDeleted) return;
    if (entry.hasAibotSession) {
      _goToChat(entry.aibotSessionId, entry.displayTitle);
      return;
    }
    if (entry.cwd.isEmpty || entry.agentSessionId.isEmpty) {
      _showSnack('session_list_no_binding'.tr);
      return;
    }
    await _bindAndOpen(
      busyKey: 'entry:${entry.agentSessionId}',
      cwd: entry.cwd,
      agentSessionId: entry.agentSessionId,
      title: entry.displayTitle,
    );
  }

  Future<void> _createInGroup(AgentSessionCwdGroup group) async {
    if (group.cwd.isEmpty) {
      _showSnack('session_list_no_binding'.tr);
      return;
    }
    await _bindAndOpen(
      busyKey: 'group:${group.cwd}',
      cwd: group.cwd,
      title: group.tabTitle,
    );
  }

  Future<void> _bindAndOpen({
    required String busyKey,
    required String cwd,
    String agentSessionId = '',
    String title = '',
  }) async {
    if (_busyKey.isNotEmpty) return;
    setState(() => _busyKey = busyKey);
    try {
      final result = await widget.bindProvider(
        cwd: cwd,
        agentSessionId: agentSessionId,
        title: title,
      );
      if (!mounted) return;
      if (result.sessionId.isEmpty) {
        _showSnack('session_list_bind_failed'.tr);
        return;
      }
      _cache.remove(_cacheKey);
      _goToChat(result.sessionId, title.isNotEmpty ? title : cwd);
    } catch (e) {
      if (!mounted) return;
      final msg = e.toString();
      if (msg.contains('binding_pending') || msg.contains('timeout')) {
        _showSnack('session_list_binding_pending'.tr);
      } else {
        _showSnack(
          userFacingError(e, fallback: 'session_list_bind_failed'.tr),
        );
      }
    } finally {
      if (mounted) setState(() => _busyKey = '');
    }
  }

  void _goToChat(String sessionId, String title) {
    Navigator.of(context).pop();
    ChatRouteNavigator.toChat(
      sessionId: sessionId,
      title: title,
      type: 'private',
    );
  }

  void _showSnack(String message) {
    CustomToast.show(message, isError: true);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 14, 12, 8),
          child: Row(
            children: [
              Icon(Icons.list_alt, size: 20, color: theme.colorScheme.primary),
              const SizedBox(width: 8),
              Text(
                'session_list_title'.tr,
                style: theme.textTheme.titleMedium?.copyWith(
                  fontWeight: FontWeight.w600,
                ),
              ),
              const Spacer(),
              IconButton(
                icon: const Icon(Icons.refresh, size: 20),
                onPressed: _loading ? null : _loadBindings,
                padding: EdgeInsets.zero,
                constraints: const BoxConstraints(minWidth: 36, minHeight: 36),
              ),
              IconButton(
                icon: const Icon(Icons.close, size: 20),
                onPressed: () => Navigator.of(context).pop(),
                padding: EdgeInsets.zero,
                constraints: const BoxConstraints(minWidth: 36, minHeight: 36),
              ),
            ],
          ),
        ),
        const Divider(height: 1),
        Expanded(child: _buildBody(theme)),
      ],
    );
  }

  Widget _buildBody(ThemeData theme) {
    if (_loading) return const Center(child: CircularProgressIndicator());
    if (_error != null) return _buildError(theme);
    if (_groups.isEmpty) return _buildEmpty(theme);
    final group = _groups[_selectedIndex.clamp(0, _groups.length - 1)];
    return Column(
      children: [
        _buildTabs(theme),
        _buildPathRow(theme, group),
        const Divider(height: 1),
        Expanded(
          child: ListView.builder(
            controller: widget.scrollController,
            itemCount: group.entries.length,
            itemBuilder: (context, index) =>
                _buildItemRow(theme, group.entries[index]),
          ),
        ),
      ],
    );
  }

  Widget _buildError(ThemeData theme) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(Icons.error_outline, size: 42, color: theme.colorScheme.error),
          const SizedBox(height: 12),
          Text(_error!, style: theme.textTheme.bodyMedium),
          const SizedBox(height: 16),
          FilledButton.tonal(
            onPressed: _loadBindings,
            child: Text('common_retry'.tr),
          ),
        ],
      ),
    );
  }

  Widget _buildEmpty(ThemeData theme) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(Icons.inbox_outlined, size: 42, color: theme.disabledColor),
          const SizedBox(height: 12),
          Text('session_list_empty'.tr, style: theme.textTheme.bodyMedium),
        ],
      ),
    );
  }

  Widget _buildTabs(ThemeData theme) {
    return SizedBox(
      height: 52,
      child: ListView.separated(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
        scrollDirection: Axis.horizontal,
        itemCount: _groups.length,
        separatorBuilder: (_, __) => const SizedBox(width: 8),
        itemBuilder: (context, index) {
          final group = _groups[index];
          final selected = index == _selectedIndex;
          return ChoiceChip(
            label: Text(group.tabTitle),
            selected: selected,
            showCheckmark: false,
            backgroundColor: theme.colorScheme.surface,
            selectedColor: theme.colorScheme.primary,
            labelStyle: theme.textTheme.bodyMedium?.copyWith(
              color: selected ? Colors.white : theme.colorScheme.onSurface,
              fontWeight: selected ? FontWeight.w600 : FontWeight.w500,
            ),
            side: BorderSide(
              color: selected
                  ? theme.colorScheme.primary
                  : theme.colorScheme.outline.withValues(alpha: 0.5),
            ),
            onSelected: (_) {
              setState(() => _selectedIndex = index);
              AgentSessionList._lastCwdByAgentKey[_cacheKey] = group.cwd;
            },
          );
        },
      ),
    );
  }

  Widget _buildPathRow(ThemeData theme, AgentSessionCwdGroup group) {
    final busy = _busyKey == 'group:${group.cwd}';
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 4, 10, 10),
      child: Row(
        children: [
          Expanded(
            child: Directionality(
              textDirection: TextDirection.rtl,
              child: Text(
                group.cwd.isEmpty ? 'session_list_unbound_dir'.tr : group.cwd,
                textAlign: TextAlign.left,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.textTheme.bodySmall?.color?.withValues(
                    alpha: 0.65,
                  ),
                  fontFamily: group.cwd.isEmpty ? null : 'monospace',
                ),
              ),
            ),
          ),
          const SizedBox(width: 8),
          // 新建会话按钮
          FilledButton.tonal(
            onPressed: (_busyKey.isNotEmpty || group.cwd.isEmpty)
                ? null
                : () => _createInGroup(group),
            style: FilledButton.styleFrom(
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
              minimumSize: Size.zero,
              tapTargetSize: MaterialTapTargetSize.shrinkWrap,
            ),
            child: busy
                ? const SizedBox(
                    width: 14,
                    height: 14,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      const Icon(Icons.add, size: 14),
                      const SizedBox(width: 3),
                      Text('session_list_create'.tr),
                    ],
                  ),
          ),
        ],
      ),
    );
  }

  Widget _buildItemRow(ThemeData theme, AgentSessionBindingEntry entry) {
    final busy = _busyKey == 'entry:${entry.agentSessionId}';
    final deleted = entry.isDeleted;
    return InkWell(
      onTap: (_busyKey.isNotEmpty || deleted) ? null : () => _openEntry(entry),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
        child: Row(
          children: [
            Icon(
              entry.hasAibotSession ? Icons.chat_bubble_outline : Icons.link,
              size: 20,
              color: deleted
                  ? theme.disabledColor
                  : (entry.hasAibotSession
                        ? theme.colorScheme.primary
                        : theme.colorScheme.secondary),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      if (entry.hasAibotSession)
                        Obx(() {
                          final im = Get.find<ImService>();
                          final active = im.hasSessionLiveActivity(
                            entry.aibotSessionId,
                          );
                          if (!active) return const SizedBox.shrink();
                          return Container(
                            width: 8,
                            height: 8,
                            margin: const EdgeInsets.only(right: 6),
                            decoration: const BoxDecoration(
                              color: Colors.green,
                              shape: BoxShape.circle,
                            ),
                          );
                        }),
                      Expanded(
                        child: Text(
                          entry.displayTitle,
                          style: theme.textTheme.bodyMedium?.copyWith(
                            fontWeight: FontWeight.w600,
                            color: deleted ? theme.disabledColor : null,
                          ),
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 3),
                  Text(
                    _buildMeta(entry),
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.textTheme.bodySmall?.color?.withValues(
                        alpha: 0.6,
                      ),
                    ),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                ],
              ),
            ),
            const SizedBox(width: 8),
            // 删除态不展示进入箭头；处理中展示加载圈；busy 状态箭头为绿色；其余展示 >。
            if (busy)
              const SizedBox(
                width: 18,
                height: 18,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            else if (!deleted)
              Icon(
                Icons.chevron_right,
                size: 18,
                color: entry.workerStatus == 'busy'
                    ? Colors.green
                    : theme.disabledColor,
              )
            else
              const SizedBox(width: 18),
          ],
        ),
      ),
    );
  }

  // 会话条目副标题：只展示更新日期时间，不再显示任何会话 ID。
  String _buildMeta(AgentSessionBindingEntry entry) {
    return _formatUpdatedAt(entry.updatedAt);
  }

  String _formatUpdatedAt(int updatedAt) {
    if (updatedAt <= 0) return '';
    return TimeFormatter.formatChatTime(updatedAt);
  }
}
