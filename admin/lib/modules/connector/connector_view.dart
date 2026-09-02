import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/theme/app_palette.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/async_view.dart';
import '../../shared/widgets/confirm_dialog.dart';
import '../../shared/widgets/filter_bottom_sheet.dart';
import '../../shared/widgets/infinite_list_view.dart';
import 'connector_controller.dart';
import 'connector_problem_users_controller.dart';
import 'connector_problem_users_view.dart';
import 'connector_reports_controller.dart';
import 'connector_service.dart';

class ConnectorView extends StatelessWidget {
  const ConnectorView({super.key});

  @override
  Widget build(BuildContext context) {
    return DefaultTabController(
      length: 4,
      child: AdminScaffold(
        title: '插件',
        actions: [
          IconButton(
            tooltip: '新建',
            onPressed: () => _create(context),
            icon: const Icon(Icons.add),
          ),
          Builder(
            builder: (context) {
              final narrow = MediaQuery.of(context).size.width < 900;
              if (narrow) return const SizedBox.shrink();
              final c = Get.find<ConnectorController>();
              return IconButton(
                tooltip: '刷新',
                onPressed: c.load,
                icon: const Icon(Icons.refresh),
              );
            },
          ),
          Builder(
            builder: (context) => PopupMenuButton<String>(
              tooltip: '更多',
              onSelected: (v) {
                switch (v) {
                  case 'refresh':
                    Get.find<ConnectorController>().load();
                  case 'push':
                    Get.find<ConnectorController>().pushUpgrade();
                  case 'reports':
                    DefaultTabController.of(context).animateTo(1);
                  case 'problem_users':
                    DefaultTabController.of(context).animateTo(2);
                  case 'stats':
                    DefaultTabController.of(context).animateTo(3);
                }
              },
              itemBuilder: (context) {
                final narrow = MediaQuery.of(context).size.width < 900;
                return [
                  if (narrow)
                    const PopupMenuItem(
                      value: 'refresh',
                      child: ListTile(
                        leading: Icon(Icons.refresh),
                        title: Text('刷新'),
                        dense: true,
                        contentPadding: EdgeInsets.zero,
                      ),
                    ),
                  const PopupMenuItem(
                    value: 'push',
                    child: ListTile(
                      leading: Icon(Icons.send),
                      title: Text('推送升级'),
                      dense: true,
                      contentPadding: EdgeInsets.zero,
                    ),
                  ),
                  const PopupMenuItem(
                    value: 'reports',
                    child: ListTile(
                      leading: Icon(Icons.assignment_outlined),
                      title: Text('升级报告'),
                      dense: true,
                      contentPadding: EdgeInsets.zero,
                    ),
                  ),
                  const PopupMenuItem(
                    value: 'problem_users',
                    child: ListTile(
                      leading: Icon(Icons.person_search_outlined),
                      title: Text('问题用户'),
                      dense: true,
                      contentPadding: EdgeInsets.zero,
                    ),
                  ),
                  const PopupMenuItem(
                    value: 'stats',
                    child: ListTile(
                      leading: Icon(Icons.bar_chart_outlined),
                      title: Text('升级统计'),
                      dense: true,
                      contentPadding: EdgeInsets.zero,
                    ),
                  ),
                ];
              },
            ),
          ),
        ],
        bottom: const TabBar(
          tabs: [
            Tab(text: '版本'),
            Tab(text: '报告'),
            Tab(text: '问题用户'),
            Tab(text: '统计'),
          ],
        ),
        body: Column(
          children: [
            _TypeFilterBar(),
            const Expanded(
              child: TabBarView(
                children: [
                  _ReleasesTab(),
                  _ReportsTab(),
                  ConnectorProblemUsersTab(),
                  _StatsTab(),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _create(BuildContext context) async {
    final version = TextEditingController(),
        channel = TextEditingController(text: 'stable');
    final changelog = TextEditingController(),
        pkg = TextEditingController(text: 'grix-connector');
    final tag = TextEditingController(text: 'latest');
    String clientType = 'grix-connector';
    bool force = false;
    final confirmed = await Get.dialog<bool>(
      StatefulBuilder(
        builder: (ctx, setState) => AlertDialog(
          title: const Text('创建版本'),
          scrollable: true,
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              SegmentedButton<String>(
                segments: const [
                  ButtonSegment(value: 'grix-connector', label: Text('连接器')),
                  ButtonSegment(value: 'grix-hermes', label: Text('Hermes')),
                ],
                selected: {clientType},
                onSelectionChanged: (s) => setState(() {
                  clientType = s.first;
                  pkg.text = clientType;
                }),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: version,
                decoration: const InputDecoration(labelText: '版本号'),
              ),
              const SizedBox(height: 8),
              TextField(
                controller: channel,
                decoration: const InputDecoration(labelText: '渠道'),
              ),
              const SizedBox(height: 8),
              TextField(
                controller: pkg,
                decoration: const InputDecoration(labelText: '包名'),
              ),
              const SizedBox(height: 8),
              TextField(
                controller: tag,
                decoration: const InputDecoration(labelText: 'tag'),
              ),
              const SizedBox(height: 8),
              TextField(
                controller: changelog,
                maxLines: 3,
                decoration: const InputDecoration(labelText: '更新日志'),
              ),
              const SizedBox(height: 8),
              CheckboxListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('强制升级（跳过冷却限制）'),
                value: force,
                onChanged: (v) => setState(() => force = v ?? false),
              ),
            ],
          ),
          actions: [
            TextButton(
              onPressed: () => Get.back(result: false),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () => Get.back(result: true),
              child: const Text('创建'),
            ),
          ],
        ),
      ),
    );
    if (confirmed == true) {
      try {
        await Get.find<ConnectorController>().create({
          'client_type': clientType,
          'version': version.text.trim(),
          'channel': channel.text.trim(),
          'changelog': changelog.text.trim(),
          'npm_package': pkg.text.trim(),
          'npm_tag': tag.text.trim(),
          'force': force,
        });
      } catch (e) {
        Toast.error(e.toString());
      }
    }
    for (final c in [version, channel, changelog, pkg, tag]) {
      c.dispose();
    }
  }
}

// ============ 类型筛选栏 ============

class _TypeFilterBar extends StatelessWidget {
  const _TypeFilterBar();

  @override
  Widget build(BuildContext context) {
    final c = Get.find<ConnectorController>();
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      child: Obx(
        () => SegmentedButton<String>(
          segments: const [
            ButtonSegment(value: '', label: Text('全部')),
            ButtonSegment(value: 'grix-connector', label: Text('连接器')),
            ButtonSegment(value: 'grix-hermes', label: Text('Hermes')),
          ],
          selected: {c.typeFilter.value},
          onSelectionChanged: (s) {
            final v = s.first;
            c.changeType(v);
            Get.find<ConnectorReportsController>().changeType(v);
            Get.find<ConnectorProblemUsersController>().changeType(v);
          },
        ),
      ),
    );
  }
}

// ============ 升级 Tab ============

class _ReleasesTab extends GetView<ConnectorController> {
  const _ReleasesTab();

  @override
  Widget build(BuildContext context) {
    return Obx(
      () => AsyncView(
        loading: controller.loading.value,
        error: controller.error.value,
        isEmpty: controller.items.isEmpty,
        onRetry: controller.load,
        emptyText: '暂无版本',
        builder: (_) => ListView.separated(
          padding: const EdgeInsets.all(16),
          itemCount: controller.items.length,
          separatorBuilder: (_, __) => const SizedBox(height: 8),
          itemBuilder: (_, i) =>
              _ReleaseCard(r: controller.items[i], c: controller),
        ),
      ),
    );
  }
}

class _ReleaseCard extends StatelessWidget {
  const _ReleaseCard({required this.r, required this.c});
  final ConnectorRelease r;
  final ConnectorController c;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    Color fg, bg;
    switch (r.status) {
      case 2:
        fg = AppPalette.success;
        bg = AppPalette.successSoft;
      case 3:
        fg = AppPalette.warning;
        bg = AppPalette.warningSoft;
      case 4:
        fg = AppPalette.textSecondary;
        bg = AppPalette.border;
      default:
        fg = AppPalette.info;
        bg = AppPalette.infoSoft;
    }
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 6,
                    vertical: 2,
                  ),
                  margin: const EdgeInsets.only(right: 8),
                  decoration: BoxDecoration(
                    color: r.isHermes
                        ? Colors.deepPurple.shade50
                        : Colors.blue.shade50,
                    borderRadius: BorderRadius.circular(4),
                  ),
                  child: Text(
                    r.isHermes ? 'H' : 'C',
                    style: TextStyle(
                      fontSize: 11,
                      fontWeight: FontWeight.w700,
                      color: r.isHermes ? Colors.deepPurple : Colors.blue,
                    ),
                  ),
                ),
                Expanded(
                  child: Text(
                    '${r.version} (${r.channel})',
                    style: theme.textTheme.titleMedium,
                  ),
                ),
                Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 8,
                    vertical: 3,
                  ),
                  decoration: BoxDecoration(
                    color: bg,
                    borderRadius: BorderRadius.circular(6),
                  ),
                  child: Text(
                    r.statusLabel,
                    style: TextStyle(
                      fontSize: 12,
                      color: fg,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 4),
            Text(
              '${r.npmPackage}@${r.npmTag}${r.force ? ' (强制)' : ''} · ${r.createdAt}',
              style: theme.textTheme.bodySmall,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
            if (r.changelog.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text(
                  r.changelog,
                  style: theme.textTheme.bodySmall,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
            const SizedBox(height: 10),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                if (r.isDraft)
                  OutlinedButton(
                    onPressed: () => c.publish(r),
                    child: const Text('发布'),
                  ),
                if (r.isPublished)
                  FilledButton.icon(
                    onPressed: () => c.pushUpgrade(),
                    icon: const Icon(Icons.send, size: 16),
                    label: const Text('推送升级'),
                  ),
                if (r.isPublished)
                  OutlinedButton(
                    onPressed: () => c.pause(r),
                    child: const Text('暂停'),
                  ),
                if (r.isPaused)
                  OutlinedButton(
                    onPressed: () => c.resume(r),
                    child: const Text('恢复'),
                  ),
                if (r.isPublished || r.isPaused)
                  OutlinedButton(
                    onPressed: () => c.revoke(r),
                    child: const Text('撤回'),
                  ),
                OutlinedButton(
                  onPressed: () => Get.toNamed(
                    '/connector/rollout',
                    arguments: {'releaseId': r.id, 'releaseVersion': r.version},
                  ),
                  child: const Text('灰度规则'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

// ============ 报告 Tab ============

class _ReportsTab extends GetView<ConnectorReportsController> {
  const _ReportsTab();

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        _ReportsToolbar(controller: controller),
        const Divider(height: 1),
        Expanded(
          child: InfiniteListView<ConnectorUpgradeReport>(
            controller: controller,
            emptyText: '暂无报告',
            itemBuilder: (_, r, __) => _ReportCard(r: r),
          ),
        ),
      ],
    );
  }
}

class _ReportsToolbar extends StatelessWidget {
  const _ReportsToolbar({required this.controller});
  final ConnectorReportsController controller;

  static const _wideThreshold = 600.0;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final wide = constraints.maxWidth >= _wideThreshold;
        return Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
          child: Row(
            children: [
              Expanded(
                child: TextField(
                  controller: controller.searchCtrl,
                  decoration: InputDecoration(
                    hintText: '搜索目标版本号',
                    prefixIcon: const Icon(Icons.search),
                    isDense: true,
                    suffixIcon: IconButton(
                      icon: const Icon(Icons.arrow_forward),
                      onPressed: () =>
                          controller.applySearch(controller.searchCtrl.text),
                    ),
                  ),
                  onSubmitted: controller.applySearch,
                ),
              ),
              if (wide) ...[
                const SizedBox(width: 12),
                _InlineFilters(controller: controller),
              ] else ...[
                Obx(
                  () => FilterBadgeIcon(
                    activeCount: controller.activeFilterCount,
                    onTap: () => _showFilterSheet(context),
                  ),
                ),
              ],
            ],
          ),
        );
      },
    );
  }

  void _showFilterSheet(BuildContext context) {
    FilterBottomSheet.show(
      context,
      title: '筛选条件',
      activeCount: controller.activeFilterCount,
      onReset: controller.resetFilters,
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('状态', style: Theme.of(context).textTheme.titleSmall),
          const SizedBox(height: 8),
          Obx(
            () => SizedBox(
              width: double.infinity,
              child: SegmentedButton<String>(
                segments: const [
                  ButtonSegment(value: '', label: Text('全部')),
                  ButtonSegment(value: 'success', label: Text('成功')),
                  ButtonSegment(value: 'failed', label: Text('失败')),
                  ButtonSegment(value: 'pending', label: Text('进行中')),
                ],
                selected: {controller.statusFilter.value},
                onSelectionChanged: (s) => controller.changeStatus(s.first),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _InlineFilters extends StatelessWidget {
  const _InlineFilters({required this.controller});
  final ConnectorReportsController controller;

  @override
  Widget build(BuildContext context) {
    return Obx(
      () => SegmentedButton<String>(
        segments: const [
          ButtonSegment(value: '', label: Text('全部')),
          ButtonSegment(value: 'success', label: Text('成功')),
          ButtonSegment(value: 'failed', label: Text('失败')),
          ButtonSegment(value: 'pending', label: Text('进行中')),
        ],
        selected: {controller.statusFilter.value},
        onSelectionChanged: (s) => controller.changeStatus(s.first),
      ),
    );
  }
}

class _ReportCard extends StatelessWidget {
  const _ReportCard({required this.r});
  final ConnectorUpgradeReport r;

  @override
  Widget build(BuildContext context) {
    final isSuccess = r.isSuccess;
    final isFailed = r.isFailed;
    final color = isSuccess
        ? AppPalette.success
        : isFailed
        ? AppPalette.danger
        : AppPalette.info;
    final bgColor = isSuccess
        ? AppPalette.successSoft
        : isFailed
        ? AppPalette.dangerSoft
        : AppPalette.infoSoft;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Row(
          children: [
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    'Agent: ${r.agentId}',
                    style: Theme.of(context).textTheme.titleSmall,
                  ),
                  const SizedBox(height: 2),
                  Text(
                    '${r.fromVersion} → ${r.toVersion} · ${r.createdAt}',
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                  if (r.errorMsg.isNotEmpty)
                    Text(
                      '错误: ${r.errorMsg}',
                      style: TextStyle(fontSize: 12, color: AppPalette.danger),
                    ),
                ],
              ),
            ),
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
              decoration: BoxDecoration(
                color: bgColor,
                borderRadius: BorderRadius.circular(6),
              ),
              child: Text(
                r.statusLabel,
                style: TextStyle(
                  fontSize: 12,
                  color: color,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

// ============ 统计 Tab ============

class _StatsTab extends StatefulWidget {
  const _StatsTab();

  @override
  State<_StatsTab> createState() => _StatsTabState();
}

class _StatsTabState extends State<_StatsTab> {
  String? _selectedVersion;
  ConnectorUpgradeStats? _stats;
  bool _loading = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    final v =
        Get.arguments?['version']?.toString() ??
        Get.parameters['version'] ??
        '';
    if (v.isNotEmpty) {
      _selectedVersion = v;
      _load();
    }
  }

  Future<void> _load() async {
    final version = _selectedVersion?.trim() ?? '';
    if (version.isEmpty) return;
    setState(() {
      _loading = true;
      _error = null;
      _stats = null;
    });
    try {
      final ct = Get.find<ConnectorController>().typeFilter.value;
      final stats = await ConnectorService.stats(
        version,
        clientType: ct.isEmpty ? null : ct,
      );
      setState(() => _stats = stats);
    } catch (e) {
      setState(() => _error = e.toString());
    } finally {
      setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final controller = Get.find<ConnectorController>();
    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.all(16),
          child: Obx(() {
            // 版本选项来自「升级」页创建的 release 记录（按当前类型过滤后去重）
            final versions = <String>[];
            for (final r in controller.items) {
              if (r.version.isNotEmpty && !versions.contains(r.version)) {
                versions.add(r.version);
              }
            }
            // 从报告页跳转带入的版本号若不在列表中也补进去，保证可显示
            if (_selectedVersion != null &&
                _selectedVersion!.isNotEmpty &&
                !versions.contains(_selectedVersion)) {
              versions.insert(0, _selectedVersion!);
            }
            if (versions.isEmpty) {
              return const Align(
                alignment: Alignment.centerLeft,
                child: Text('暂无可选版本，请先在「版本」页创建版本'),
              );
            }
            return DropdownButtonFormField<String>(
              value: versions.contains(_selectedVersion)
                  ? _selectedVersion
                  : null,
              isExpanded: true,
              decoration: const InputDecoration(
                hintText: '选择目标版本号',
                prefixIcon: Icon(Icons.search),
                border: OutlineInputBorder(),
              ),
              items: versions
                  .map((v) => DropdownMenuItem(value: v, child: Text(v)))
                  .toList(),
              onChanged: (v) {
                if (v == null) return;
                setState(() => _selectedVersion = v);
                _load();
              },
            );
          }),
        ),
        const Divider(height: 1),
        Expanded(
          child: _loading
              ? const Center(child: CircularProgressIndicator())
              : _error != null
              ? Center(
                  child: Text(
                    _error!,
                    style: TextStyle(color: AppPalette.danger),
                  ),
                )
              : (_selectedVersion == null || _selectedVersion!.isEmpty)
              ? const Center(child: Text('请选择版本号查询'))
              : _stats == null
              ? const Center(child: Text('暂无统计数据'))
              : _buildStats(context, _stats!),
        ),
      ],
    );
  }

  Widget _buildStats(BuildContext context, ConnectorUpgradeStats s) {
    final successRate = s.total > 0
        ? (s.success / s.total * 100).toStringAsFixed(1)
        : '0.0';
    return SingleChildScrollView(
      padding: const EdgeInsets.all(24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            '版本 ${_selectedVersion?.trim() ?? ''} 升级统计',
            style: Theme.of(context).textTheme.titleLarge,
          ),
          const SizedBox(height: 16),
          GridView.count(
            crossAxisCount: 2,
            shrinkWrap: true,
            physics: const NeverScrollableScrollPhysics(),
            mainAxisSpacing: 12,
            crossAxisSpacing: 12,
            childAspectRatio: 2.0,
            children: [
              _StatCard(
                label: '总计',
                value: s.total.toString(),
                color: AppPalette.info,
              ),
              _StatCard(
                label: '成功',
                value: s.success.toString(),
                color: AppPalette.success,
              ),
              _StatCard(
                label: '失败',
                value: s.failed.toString(),
                color: AppPalette.danger,
              ),
              _StatCard(
                label: '进行中',
                value: s.pending.toString(),
                color: AppPalette.warning,
              ),
            ],
          ),
          const SizedBox(height: 12),
          Card(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Row(
                children: [
                  const Icon(Icons.percent_outlined),
                  const SizedBox(width: 8),
                  Text(
                    '成功率: $successRate%',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                ],
              ),
            ),
          ),
          if (s.errorDistribution.isNotEmpty) ...[
            const SizedBox(height: 16),
            Text('错误分布', style: Theme.of(context).textTheme.titleSmall),
            const SizedBox(height: 8),
            ...s.errorDistribution.entries.map(
              (e) => Card(
                margin: const EdgeInsets.only(bottom: 4),
                child: Padding(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 16,
                    vertical: 10,
                  ),
                  child: Row(
                    children: [
                      Expanded(
                        child: Text(
                          e.key,
                          style: Theme.of(context).textTheme.bodyMedium,
                        ),
                      ),
                      Text(
                        e.value.toString(),
                        style: TextStyle(
                          color: AppPalette.danger,
                          fontWeight: FontWeight.bold,
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ],
        ],
      ),
    );
  }
}

class _StatCard extends StatelessWidget {
  const _StatCard({
    required this.label,
    required this.value,
    required this.color,
  });
  final String label, value;
  final Color color;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Text(
              value,
              style: Theme.of(context).textTheme.headlineSmall?.copyWith(
                color: color,
                fontWeight: FontWeight.bold,
              ),
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 4),
            Text(label, style: Theme.of(context).textTheme.bodySmall),
          ],
        ),
      ),
    );
  }
}
