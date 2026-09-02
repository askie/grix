import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/theme/app_palette.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/confirm_dialog.dart';
import '../../shared/widgets/filter_bottom_sheet.dart';
import '../../shared/widgets/infinite_list_view.dart';
import 'controller.dart';
import 'models.dart';
import 'service.dart';

class AppReleasesView extends StatelessWidget {
  const AppReleasesView({super.key});

  @override
  Widget build(BuildContext context) {
    return DefaultTabController(
      length: 2,
      child: AdminScaffold(
        title: 'App 版本',
        actions: [
          IconButton(
            tooltip: '新建',
            onPressed: () => _create(context),
            icon: const Icon(Icons.add),
          ),
          IconButton(
            tooltip: '刷新',
            onPressed: Get.find<AppReleasesController>().reload,
            icon: const Icon(Icons.refresh),
          ),
        ],
        bottom: const TabBar(
          tabs: [
            Tab(text: '版本'),
            Tab(text: '统计'),
          ],
        ),
        body: const TabBarView(children: [_ReleasesTab(), _StatsTab()]),
      ),
    );
  }

  Future<void> _create(BuildContext context) async {
    final version = TextEditingController(), build = TextEditingController();
    final changelog = TextEditingController(), url = TextEditingController();
    final appStoreUrl = TextEditingController(),
        sha256 = TextEditingController();
    final fileSize = TextEditingController(),
        minBuild = TextEditingController();
    String platform = 'android', channel = 'stable', updateMethod = 'download';

    final confirmed = await Get.dialog<bool>(
      StatefulBuilder(
        builder: (ctx, setState) => AlertDialog(
          title: const Text('创建版本'),
          scrollable: true,
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: version,
                decoration: const InputDecoration(labelText: '版本号 (如 1.2.3)'),
              ),
              const SizedBox(height: 8),
              TextField(
                controller: build,
                decoration: const InputDecoration(labelText: '构建号'),
                keyboardType: TextInputType.number,
              ),
              const SizedBox(height: 8),
              DropdownButtonFormField<String>(
                value: platform,
                decoration: const InputDecoration(labelText: '平台'),
                items: ['android', 'ios', 'macos', 'windows']
                    .map((v) => DropdownMenuItem(value: v, child: Text(v)))
                    .toList(),
                onChanged: (v) => setState(() => platform = v!),
              ),
              const SizedBox(height: 8),
              DropdownButtonFormField<String>(
                value: channel,
                decoration: const InputDecoration(labelText: '通道'),
                items: ['stable', 'beta']
                    .map((v) => DropdownMenuItem(value: v, child: Text(v)))
                    .toList(),
                onChanged: (v) => setState(() => channel = v!),
              ),
              const SizedBox(height: 8),
              DropdownButtonFormField<String>(
                value: updateMethod,
                decoration: const InputDecoration(labelText: '更新方式'),
                items: [
                  const DropdownMenuItem(
                    value: 'download',
                    child: Text('直接下载'),
                  ),
                  const DropdownMenuItem(
                    value: 'app_store',
                    child: Text('App Store'),
                  ),
                  const DropdownMenuItem(
                    value: 'google_play',
                    child: Text('Google Play'),
                  ),
                ],
                onChanged: (v) => setState(() => updateMethod = v!),
              ),
              const SizedBox(height: 8),
              TextField(
                controller: url,
                decoration: const InputDecoration(labelText: '下载链接'),
              ),
              const SizedBox(height: 8),
              TextField(
                controller: appStoreUrl,
                decoration: const InputDecoration(labelText: '应用商店链接'),
              ),
              const SizedBox(height: 8),
              TextField(
                controller: fileSize,
                decoration: const InputDecoration(labelText: '文件大小 (bytes)'),
                keyboardType: TextInputType.number,
              ),
              const SizedBox(height: 8),
              TextField(
                controller: sha256,
                decoration: const InputDecoration(labelText: 'SHA256（可选）'),
              ),
              const SizedBox(height: 8),
              TextField(
                controller: minBuild,
                decoration: const InputDecoration(
                  labelText: '强制更新阈值 Min Build（留空不强制）',
                ),
                keyboardType: TextInputType.number,
              ),
              const SizedBox(height: 8),
              TextField(
                controller: changelog,
                maxLines: 3,
                decoration: const InputDecoration(labelText: '更新日志'),
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
      final versionText = version.text.trim();
      final semverRe = RegExp(r'^\d+\.\d+(\.\d+)?$');
      if (!semverRe.hasMatch(versionText)) {
        Toast.error('版本号格式错误，应为 x.y 或 x.y.z（纯数字）');
        for (final c in [
          version,
          build,
          changelog,
          url,
          appStoreUrl,
          sha256,
          fileSize,
          minBuild,
        ]) {
          c.dispose();
        }
        return;
      }
      final buildNum = int.tryParse(build.text) ?? 0;
      if (buildNum <= 0) {
        Toast.error('构建号必须为正整数');
        for (final c in [
          version,
          build,
          changelog,
          url,
          appStoreUrl,
          sha256,
          fileSize,
          minBuild,
        ]) {
          c.dispose();
        }
        return;
      }
      try {
        await Get.find<AppReleasesController>().create({
          'version': versionText,
          'build_number': buildNum,
          'platform': platform,
          'channel': channel,
          'update_method': updateMethod,
          'changelog': changelog.text.trim(),
          'download_url': url.text.trim(),
          if (appStoreUrl.text.trim().isNotEmpty)
            'app_store_url': appStoreUrl.text.trim(),
          if (sha256.text.trim().isNotEmpty) 'sha256': sha256.text.trim(),
          if (fileSize.text.trim().isNotEmpty)
            'file_size': int.tryParse(fileSize.text) ?? 0,
          if (minBuild.text.trim().isNotEmpty)
            'min_build': int.tryParse(minBuild.text),
        });
      } catch (e) {
        Toast.error(e.toString());
      }
    }
    for (final c in [
      version,
      build,
      changelog,
      url,
      appStoreUrl,
      sha256,
      fileSize,
      minBuild,
    ]) {
      c.dispose();
    }
  }
}

// ============ 版本 Tab ============

class _ReleasesTab extends GetView<AppReleasesController> {
  const _ReleasesTab();

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        _Toolbar(c: controller),
        const Divider(height: 1),
        Expanded(
          child: InfiniteListView<AppRelease>(
            controller: controller,
            emptyText: '暂无版本',
            itemBuilder: (_, r, __) => _ReleaseCard(r: r, c: controller),
          ),
        ),
      ],
    );
  }
}

class _Toolbar extends StatelessWidget {
  const _Toolbar({required this.c});
  final AppReleasesController c;

  static const _wideThreshold = 600.0;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final wide = constraints.maxWidth >= _wideThreshold;
        if (wide) {
          return Padding(
            padding: const EdgeInsets.all(16),
            child: Wrap(
              spacing: 12,
              runSpacing: 12,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                SingleChildScrollView(
                  scrollDirection: Axis.horizontal,
                  child: Obx(
                    () => SegmentedButton<String>(
                      segments: const [
                        ButtonSegment(value: '', label: Text('全部')),
                        ButtonSegment(value: 'android', label: Text('Android')),
                        ButtonSegment(value: 'ios', label: Text('iOS')),
                        ButtonSegment(value: 'macos', label: Text('macOS')),
                        ButtonSegment(value: 'windows', label: Text('Windows')),
                      ],
                      selected: {c.platform.value},
                      onSelectionChanged: (s) => c.changePlatform(s.first),
                    ),
                  ),
                ),
                Obx(
                  () => SegmentedButton<String>(
                    segments: const [
                      ButtonSegment(value: '', label: Text('全渠道')),
                      ButtonSegment(value: 'stable', label: Text('stable')),
                      ButtonSegment(value: 'beta', label: Text('beta')),
                    ],
                    selected: {c.channel.value},
                    onSelectionChanged: (s) => c.changeChannel(s.first),
                  ),
                ),
              ],
            ),
          );
        }
        return Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
          child: Row(
            mainAxisAlignment: MainAxisAlignment.end,
            children: [
              Obx(
                () => FilterBadgeIcon(
                  activeCount: c.activeFilterCount,
                  onTap: () => _showFilterSheet(context),
                ),
              ),
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
      activeCount: c.activeFilterCount,
      onReset: c.resetFilters,
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('平台', style: Theme.of(context).textTheme.titleSmall),
          const SizedBox(height: 8),
          Obx(
            () => SizedBox(
              width: double.infinity,
              child: SegmentedButton<String>(
                segments: const [
                  ButtonSegment(value: '', label: Text('全部')),
                  ButtonSegment(value: 'android', label: Text('Android')),
                  ButtonSegment(value: 'ios', label: Text('iOS')),
                  ButtonSegment(value: 'macos', label: Text('macOS')),
                  ButtonSegment(value: 'windows', label: Text('Windows')),
                ],
                selected: {c.platform.value},
                onSelectionChanged: (s) => c.changePlatform(s.first),
              ),
            ),
          ),
          const SizedBox(height: 16),
          Text('通道', style: Theme.of(context).textTheme.titleSmall),
          const SizedBox(height: 8),
          Obx(
            () => SizedBox(
              width: double.infinity,
              child: SegmentedButton<String>(
                segments: const [
                  ButtonSegment(value: '', label: Text('全渠道')),
                  ButtonSegment(value: 'stable', label: Text('stable')),
                  ButtonSegment(value: 'beta', label: Text('beta')),
                ],
                selected: {c.channel.value},
                onSelectionChanged: (s) => c.changeChannel(s.first),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _ReleaseCard extends StatelessWidget {
  const _ReleaseCard({required this.r, required this.c});
  final AppRelease r;
  final AppReleasesController c;

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
                Expanded(
                  child: Text(
                    '${r.version} (${r.buildNumber})',
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
              '${r.platform} · ${r.channel} · ${r.updateMethod}${r.fileSize > 0 ? ' · ${(r.fileSize / 1024 / 1024).toStringAsFixed(1)} MB' : ''} · ${r.createdAt}',
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
                if (r.isDraft)
                  OutlinedButton(
                    style: OutlinedButton.styleFrom(
                      foregroundColor: AppPalette.danger,
                    ),
                    onPressed: () => c.delete(r),
                    child: const Text('删除'),
                  ),
                OutlinedButton(
                  onPressed: () => _showStats(context, r),
                  child: const Text('统计'),
                ),
                OutlinedButton(
                  onPressed: () => Get.toNamed(
                    '/app/rollout',
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

  Future<void> _showStats(BuildContext context, AppRelease r) async {
    final stats = await AppReleaseService.stats(r.id);
    if (stats == null) {
      Toast.error('暂无统计数据');
      return;
    }
    Get.dialog(
      AlertDialog(
        title: Text('${r.version} 下载统计'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('总下载: ${stats.total}'),
            Text('成功: ${stats.success}'),
            Text('失败: ${stats.failed}'),
            Text('平均耗时: ${stats.avgDurationMs.toStringAsFixed(0)} ms'),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () {
              Get.back();
              // 切换到统计 Tab
              final tabController = DefaultTabController.of(context);
              tabController.animateTo(1);
            },
            child: const Text('查看详情'),
          ),
          TextButton(onPressed: () => Get.back(), child: const Text('关闭')),
        ],
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
  String? _selectedReleaseId;
  AppDownloadStats? _stats;
  bool _loading = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    final arg =
        Get.arguments?['releaseId']?.toString() ??
        Get.parameters['releaseId'] ??
        '';
    if (arg.isNotEmpty) {
      _selectedReleaseId = arg;
      _load();
    }
  }

  Future<void> _load() async {
    if (_selectedReleaseId == null || _selectedReleaseId!.isEmpty) return;
    setState(() {
      _loading = true;
      _error = null;
      _stats = null;
    });
    try {
      final stats = await AppReleaseService.stats(_selectedReleaseId!);
      setState(() => _stats = stats);
    } catch (e) {
      setState(() => _error = e.toString());
    } finally {
      setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final c = Get.find<AppReleasesController>();
    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.all(16),
          child: Obx(() {
            final releases = c.items.toList();
            if (releases.isEmpty) {
              return const Align(
                alignment: Alignment.centerLeft,
                child: Text('暂无可选版本'),
              );
            }
            return DropdownButtonFormField<String>(
              value: releases.any((r) => r.id == _selectedReleaseId)
                  ? _selectedReleaseId
                  : null,
              isExpanded: true,
              decoration: const InputDecoration(
                hintText: '选择版本',
                prefixIcon: Icon(Icons.search),
                border: OutlineInputBorder(),
              ),
              items: releases
                  .map(
                    (r) => DropdownMenuItem(
                      value: r.id,
                      child: Text('${r.version} (${r.platform})'),
                    ),
                  )
                  .toList(),
              onChanged: (v) {
                if (v == null) return;
                setState(() => _selectedReleaseId = v);
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
              : _selectedReleaseId == null
              ? const Center(child: Text('请选择版本查看统计'))
              : _stats == null
              ? const Center(child: Text('暂无统计数据'))
              : _buildStats(context, _stats!),
        ),
      ],
    );
  }

  Widget _buildStats(BuildContext context, AppDownloadStats s) {
    return SingleChildScrollView(
      padding: const EdgeInsets.all(24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            '${s.version} (${s.platform})',
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
                label: '总下载',
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
                label: '平均耗时',
                value: '${s.avgDurationMs.toStringAsFixed(0)} ms',
                color: AppPalette.warning,
              ),
            ],
          ),
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
