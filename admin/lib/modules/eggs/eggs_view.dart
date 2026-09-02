import 'package:flutter/material.dart';
import 'package:get/get.dart';
import '../../app/theme/app_palette.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/async_view.dart';
import '../../shared/widgets/confirm_dialog.dart';
import '../../shared/widgets/filter_bottom_sheet.dart';
import '../../shared/widgets/infinite_list_view.dart';
import 'egg_service.dart';
import 'eggs_controller.dart';

class EggsView extends StatelessWidget {
  const EggsView({super.key});

  @override
  Widget build(BuildContext context) {
    return DefaultTabController(
      length: 2,
      child: AdminScaffold(
        title: '虾蛋',
        actions: [
          IconButton(
            tooltip: '刷新',
            onPressed: () {
              final c = Get.find<EggsController>();
              c.reload();
              c.loadCategories();
            },
            icon: const Icon(Icons.refresh),
          ),
        ],
        bottom: const TabBar(
          tabs: [
            Tab(text: '列表'),
            Tab(text: '分类'),
          ],
        ),
        body: const TabBarView(children: [_EggsListTab(), _CategoriesTab()]),
      ),
    );
  }
}

// ============ 列表 Tab ============

class _EggsListTab extends GetView<EggsController> {
  const _EggsListTab();

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        _Toolbar(c: controller),
        const Divider(height: 1),
        Expanded(
          child: InfiniteListView<EggListItem>(
            controller: controller,
            emptyText: '暂无虾蛋',
            itemBuilder: (_, item, __) => _EggCard(item: item, c: controller),
          ),
        ),
      ],
    );
  }
}

class _Toolbar extends StatelessWidget {
  const _Toolbar({required this.c});
  final EggsController c;

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
                  controller: c.searchCtrl,
                  decoration: InputDecoration(
                    hintText: '搜索 ID / 名称',
                    prefixIcon: const Icon(Icons.search),
                    isDense: true,
                    suffixIcon: IconButton(
                      icon: const Icon(Icons.arrow_forward),
                      onPressed: () => c.applySearch(c.searchCtrl.text),
                    ),
                  ),
                  onSubmitted: c.applySearch,
                ),
              ),
              if (wide) ...[
                const SizedBox(width: 12),
                _InlineFilters(c: c),
              ] else ...[
                Obx(
                  () => FilterBadgeIcon(
                    activeCount: c.activeFilterCount,
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
      activeCount: c.activeFilterCount,
      onReset: c.resetFilters,
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
                  ButtonSegment(value: 'active', label: Text('上架')),
                  ButtonSegment(value: 'inactive', label: Text('下架')),
                ],
                selected: {c.statusFilter.value},
                onSelectionChanged: (s) => c.changeStatus(s.first),
              ),
            ),
          ),
          const SizedBox(height: 16),
          Text('分类', style: Theme.of(context).textTheme.titleSmall),
          const SizedBox(height: 8),
          Obx(
            () => SizedBox(
              width: double.infinity,
              child: DropdownButton<String>(
                value: c.categoryFilter.value.isEmpty
                    ? null
                    : c.categoryFilter.value,
                hint: const Text('全部分类'),
                underline: const SizedBox.shrink(),
                isExpanded: true,
                items: [
                  const DropdownMenuItem(value: '', child: Text('全部分类')),
                  for (final cat in c.categories)
                    DropdownMenuItem(value: cat.id, child: Text(cat.name)),
                ],
                onChanged: (v) => c.changeCategory(v ?? ''),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _InlineFilters extends StatelessWidget {
  const _InlineFilters({required this.c});
  final EggsController c;

  @override
  Widget build(BuildContext context) {
    return Wrap(
      spacing: 12,
      runSpacing: 12,
      crossAxisAlignment: WrapCrossAlignment.center,
      children: [
        Obx(
          () => SegmentedButton<String>(
            segments: const [
              ButtonSegment(value: '', label: Text('全部')),
              ButtonSegment(value: 'active', label: Text('上架')),
              ButtonSegment(value: 'inactive', label: Text('下架')),
            ],
            selected: {c.statusFilter.value},
            onSelectionChanged: (s) => c.changeStatus(s.first),
          ),
        ),
        Obx(
          () => DropdownButton<String>(
            value: c.categoryFilter.value.isEmpty
                ? null
                : c.categoryFilter.value,
            hint: const Text('全部分类'),
            underline: const SizedBox.shrink(),
            items: [
              const DropdownMenuItem(value: '', child: Text('全部分类')),
              for (final cat in c.categories)
                DropdownMenuItem(value: cat.id, child: Text(cat.name)),
            ],
            onChanged: (v) => c.changeCategory(v ?? ''),
          ),
        ),
      ],
    );
  }
}

class _EggCard extends StatelessWidget {
  const _EggCard({required this.item, required this.c});
  final EggListItem item;
  final EggsController c;
  @override
  Widget build(BuildContext context) {
    final active = item.status == 'active';
    return Card(
      child: InkWell(
        onTap: () async {
          await Get.toNamed('/eggs/${item.id}');
          c.reload();
        },
        child: Padding(
          padding: const EdgeInsets.all(14),
          child: Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Flexible(
                          child: Text(
                            item.id,
                            style: Theme.of(context).textTheme.titleMedium,
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                        if (item.pinned) ...[
                          const SizedBox(width: 6),
                          Icon(
                            Icons.push_pin,
                            size: 15,
                            color: AppPalette.warning,
                          ),
                        ],
                      ],
                    ),
                    const SizedBox(height: 2),
                    Text(
                      '分类: ${item.categoryId} · 安装: ${item.installCount}',
                      style: Theme.of(context).textTheme.bodySmall,
                    ),
                  ],
                ),
              ),
              if (item.pinned) ...[
                Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 8,
                    vertical: 3,
                  ),
                  decoration: BoxDecoration(
                    color: AppPalette.warningSoft,
                    borderRadius: BorderRadius.circular(6),
                  ),
                  child: Text(
                    '置顶',
                    style: TextStyle(
                      fontSize: 12,
                      color: AppPalette.warning,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ),
                const SizedBox(width: 8),
              ],
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                decoration: BoxDecoration(
                  color: active ? AppPalette.successSoft : AppPalette.border,
                  borderRadius: BorderRadius.circular(6),
                ),
                child: Text(
                  active ? '上架' : '下架',
                  style: TextStyle(
                    fontSize: 12,
                    color: active
                        ? AppPalette.success
                        : AppPalette.textSecondary,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
              const SizedBox(width: 8),
              PopupMenuButton<String>(
                onSelected: (v) {
                  if (v == 'pin') {
                    c.togglePinned(item.id, true);
                  } else if (v == 'unpin') {
                    c.togglePinned(item.id, false);
                  } else {
                    c.updateStatus(item.id, v);
                  }
                },
                itemBuilder: (_) => [
                  if (!active)
                    const PopupMenuItem(value: 'active', child: Text('上架')),
                  if (active)
                    const PopupMenuItem(value: 'inactive', child: Text('下架')),
                  if (!item.pinned)
                    const PopupMenuItem(value: 'pin', child: Text('置顶')),
                  if (item.pinned)
                    const PopupMenuItem(value: 'unpin', child: Text('取消置顶')),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

// ============ 分类 Tab ============

class _CategoriesTab extends GetView<EggsController> {
  const _CategoriesTab();

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        Row(
          mainAxisAlignment: MainAxisAlignment.end,
          children: [
            Padding(
              padding: const EdgeInsets.all(16),
              child: OutlinedButton.icon(
                onPressed: () => _createCategory(context),
                icon: const Icon(Icons.add),
                label: const Text('新建分类'),
              ),
            ),
          ],
        ),
        const Divider(height: 1),
        Expanded(
          child: Obx(
            () => AsyncView(
              loading: controller.categoriesLoading.value,
              error: controller.categoriesError.value,
              isEmpty: controller.categories.isEmpty,
              onRetry: controller.loadCategories,
              emptyText: '暂无分类',
              builder: (_) => ListView.separated(
                padding: const EdgeInsets.all(16),
                itemCount: controller.categories.length,
                separatorBuilder: (_, __) => const SizedBox(height: 8),
                itemBuilder: (_, i) =>
                    _CategoryCard(cat: controller.categories[i], c: controller),
              ),
            ),
          ),
        ),
      ],
    );
  }

  Future<void> _createCategory(BuildContext context) async {
    final id = TextEditingController();
    final code = TextEditingController();
    final sort = TextEditingController(text: '0');
    final nameZh = TextEditingController();
    final nameEn = TextEditingController();
    final confirmed = await Get.dialog<bool>(
      AlertDialog(
        title: const Text('新建分类'),
        scrollable: true,
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: id,
              decoration: const InputDecoration(labelText: 'ID (唯一标识)'),
            ),
            const SizedBox(height: 8),
            TextField(
              controller: code,
              decoration: const InputDecoration(labelText: 'Code'),
            ),
            const SizedBox(height: 8),
            TextField(
              controller: sort,
              decoration: const InputDecoration(labelText: '排序'),
              keyboardType: TextInputType.number,
            ),
            const SizedBox(height: 8),
            TextField(
              controller: nameZh,
              decoration: const InputDecoration(labelText: '中文名称 (zh-CN)'),
            ),
            const SizedBox(height: 8),
            TextField(
              controller: nameEn,
              decoration: const InputDecoration(labelText: '英文名称 (en-US)'),
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
    );
    if (confirmed == true) {
      final i18n = <Map<String, dynamic>>[];
      if (nameZh.text.trim().isNotEmpty) {
        i18n.add({'locale': 'zh-CN', 'name': nameZh.text.trim()});
      }
      if (nameEn.text.trim().isNotEmpty) {
        i18n.add({'locale': 'en-US', 'name': nameEn.text.trim()});
      }
      try {
        await EggService.createCategory({
          'id': id.text.trim(),
          'code': code.text.trim(),
          'status': 'active',
          'sort_order': int.tryParse(sort.text) ?? 0,
          'i18n': i18n,
        });
        Toast.success('分类已创建');
        await controller.loadCategories();
      } catch (e) {
        Toast.error(e.toString());
      }
    }
    for (final c in [id, code, sort, nameZh, nameEn]) {
      c.dispose();
    }
  }
}

class _CategoryCard extends StatelessWidget {
  const _CategoryCard({required this.cat, required this.c});
  final EggCategory cat;
  final EggsController c;

  @override
  Widget build(BuildContext context) {
    final active = cat.isActive;
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
                    cat.code,
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  const SizedBox(height: 2),
                  Text(
                    'ID: ${cat.id} · 排序: ${cat.sortOrder} · ${cat.name}',
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ],
              ),
            ),
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
              decoration: BoxDecoration(
                color: active ? AppPalette.successSoft : AppPalette.border,
                borderRadius: BorderRadius.circular(6),
              ),
              child: Text(
                active ? '上架' : '下架',
                style: TextStyle(
                  fontSize: 12,
                  color: active ? AppPalette.success : AppPalette.textSecondary,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ),
            const SizedBox(width: 8),
            PopupMenuButton<String>(
              onSelected: (v) => _handleAction(context, v),
              itemBuilder: (_) => [
                const PopupMenuItem(value: 'edit', child: Text('编辑')),
                PopupMenuItem(
                  value: active ? 'inactive' : 'active',
                  child: Text(active ? '下架' : '上架'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _handleAction(BuildContext context, String action) async {
    if (action == 'edit') {
      await _edit(context);
    } else {
      try {
        await EggService.updateCategoryStatus(cat.id, action);
        Toast.success('状态已更新');
        await c.loadCategories();
      } catch (e) {
        Toast.error(e.toString());
      }
    }
  }

  Future<void> _edit(BuildContext context) async {
    final code = TextEditingController(text: cat.code);
    final sort = TextEditingController(text: cat.sortOrder.toString());
    String zhName = '', enName = '';
    for (final row in cat.i18n) {
      final locale = (row['locale'] ?? '').toString();
      final name = (row['name'] ?? '').toString();
      if (locale == 'zh-CN') zhName = name;
      if (locale == 'en-US') enName = name;
    }
    final nameZh = TextEditingController(text: zhName);
    final nameEn = TextEditingController(text: enName);

    final confirmed = await Get.dialog<bool>(
      AlertDialog(
        title: Text('编辑分类: ${cat.id}'),
        scrollable: true,
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: code,
              decoration: const InputDecoration(labelText: 'Code'),
            ),
            const SizedBox(height: 8),
            TextField(
              controller: sort,
              decoration: const InputDecoration(labelText: '排序'),
              keyboardType: TextInputType.number,
            ),
            const SizedBox(height: 8),
            TextField(
              controller: nameZh,
              decoration: const InputDecoration(labelText: '中文名称 (zh-CN)'),
            ),
            const SizedBox(height: 8),
            TextField(
              controller: nameEn,
              decoration: const InputDecoration(labelText: '英文名称 (en-US)'),
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
            child: const Text('保存'),
          ),
        ],
      ),
    );

    if (confirmed == true) {
      final i18n = <Map<String, dynamic>>[];
      if (nameZh.text.trim().isNotEmpty) {
        i18n.add({'locale': 'zh-CN', 'name': nameZh.text.trim()});
      }
      if (nameEn.text.trim().isNotEmpty) {
        i18n.add({'locale': 'en-US', 'name': nameEn.text.trim()});
      }
      try {
        await EggService.updateCategory(cat.id, {
          'code': code.text.trim(),
          'sort_order': int.tryParse(sort.text) ?? 0,
          'i18n': i18n,
        });
        Toast.success('分类已更新');
        await c.loadCategories();
      } catch (e) {
        Toast.error(e.toString());
      }
    }
    for (final ctrl in [code, sort, nameZh, nameEn]) {
      ctrl.dispose();
    }
  }
}
