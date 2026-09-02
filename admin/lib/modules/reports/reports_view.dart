import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:intl/intl.dart';

import '../../app/theme/app_palette.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/filter_bottom_sheet.dart';
import '../../shared/widgets/infinite_list_view.dart';
import 'report_models.dart';
import 'reports_controller.dart';

/// 举报管理列表页：关键词 + 状态 + 对象类型 多维筛选，无限滚动，点击进入详情。
class ReportsView extends GetView<ReportsController> {
  const ReportsView({super.key});

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: '举报管理',
      actions: [
        IconButton(
          tooltip: '刷新',
          onPressed: controller.reload,
          icon: const Icon(Icons.refresh),
        ),
      ],
      body: Column(
        children: [
          _Toolbar(controller: controller),
          const Divider(height: 1),
          Expanded(
            child: InfiniteListView<ReportListItem>(
              controller: controller,
              emptyText: '没有符合条件的举报',
              itemBuilder: (_, item, index) => _ReportCard(
                item: item,
                onTap: () async {
                  await Get.toNamed('/reports/${item.id}');
                  controller.reload();
                },
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _Toolbar extends StatelessWidget {
  const _Toolbar({required this.controller});

  final ReportsController controller;

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
                    hintText: '搜索 举报ID / 用户ID / 原因',
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
          Text('举报状态', style: Theme.of(context).textTheme.titleSmall),
          const SizedBox(height: 8),
          Obx(
            () => SizedBox(
              width: double.infinity,
              child: SegmentedButton<String>(
                segments: const [
                  ButtonSegment(value: '', label: Text('全部')),
                  ButtonSegment(value: 'pending', label: Text('待处理')),
                  ButtonSegment(value: 'review', label: Text('审核中')),
                  ButtonSegment(value: 'resolved', label: Text('已处理')),
                ],
                selected: {controller.statusFilter.value},
                onSelectionChanged: (s) => controller.changeStatus(s.first),
              ),
            ),
          ),
          const SizedBox(height: 16),
          Text('对象类型', style: Theme.of(context).textTheme.titleSmall),
          const SizedBox(height: 8),
          Obx(
            () => SizedBox(
              width: double.infinity,
              child: SegmentedButton<String>(
                segments: const [
                  ButtonSegment(value: '', label: Text('全部对象')),
                  ButtonSegment(value: 'user', label: Text('用户')),
                  ButtonSegment(value: 'group', label: Text('群组')),
                ],
                selected: {controller.targetTypeFilter.value},
                onSelectionChanged: (s) => controller.changeTargetType(s.first),
              ),
            ),
          ),
          const SizedBox(height: 16),
          Text('举报原因', style: Theme.of(context).textTheme.titleSmall),
          const SizedBox(height: 8),
          Obx(
            () => SizedBox(
              width: double.infinity,
              child: DropdownButton<String>(
                value: controller.reasonCode.value,
                underline: const SizedBox.shrink(),
                isExpanded: true,
                items: const [
                  DropdownMenuItem(value: '', child: Text('全部原因')),
                  DropdownMenuItem(value: 'harassment', child: Text('骚扰辱骂')),
                  DropdownMenuItem(value: 'pornography', child: Text('色情低俗')),
                  DropdownMenuItem(value: 'violence', child: Text('暴力威胁')),
                  DropdownMenuItem(value: 'fraud', child: Text('诈骗欺诈')),
                  DropdownMenuItem(value: 'spam', child: Text('垃圾信息')),
                  DropdownMenuItem(value: 'impersonation', child: Text('冒充他人')),
                  DropdownMenuItem(value: 'illegal', child: Text('违法内容')),
                  DropdownMenuItem(value: 'other', child: Text('其他')),
                ],
                onChanged: (v) => controller.changeReasonCode(v ?? ''),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// 宽屏下的内联筛选控件。
class _InlineFilters extends StatelessWidget {
  const _InlineFilters({required this.controller});

  final ReportsController controller;

  @override
  Widget build(BuildContext context) {
    return Wrap(
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
                ButtonSegment(value: 'pending', label: Text('待处理')),
                ButtonSegment(value: 'review', label: Text('审核中')),
                ButtonSegment(value: 'resolved', label: Text('已处理')),
              ],
              selected: {controller.statusFilter.value},
              onSelectionChanged: (s) => controller.changeStatus(s.first),
            ),
          ),
        ),
        Obx(
          () => SegmentedButton<String>(
            segments: const [
              ButtonSegment(value: '', label: Text('全部对象')),
              ButtonSegment(value: 'user', label: Text('用户')),
              ButtonSegment(value: 'group', label: Text('群组')),
            ],
            selected: {controller.targetTypeFilter.value},
            onSelectionChanged: (s) => controller.changeTargetType(s.first),
          ),
        ),
        Obx(
          () => DropdownButton<String>(
            value: controller.reasonCode.value,
            underline: const SizedBox.shrink(),
            items: const [
              DropdownMenuItem(value: '', child: Text('全部原因')),
              DropdownMenuItem(value: 'harassment', child: Text('骚扰辱骂')),
              DropdownMenuItem(value: 'pornography', child: Text('色情低俗')),
              DropdownMenuItem(value: 'violence', child: Text('暴力威胁')),
              DropdownMenuItem(value: 'fraud', child: Text('诈骗欺诈')),
              DropdownMenuItem(value: 'spam', child: Text('垃圾信息')),
              DropdownMenuItem(value: 'impersonation', child: Text('冒充他人')),
              DropdownMenuItem(value: 'illegal', child: Text('违法内容')),
              DropdownMenuItem(value: 'other', child: Text('其他')),
            ],
            onChanged: (v) => controller.changeReasonCode(v ?? ''),
          ),
        ),
      ],
    );
  }
}

class _ReportCard extends StatelessWidget {
  const _ReportCard({required this.item, required this.onTap});

  final ReportListItem item;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final df = DateFormat('yyyy-MM-dd HH:mm');
    final resolved = item.isResolved;
    final statusColor = resolved ? AppPalette.success : AppPalette.warning;
    final statusBg = resolved ? AppPalette.successSoft : AppPalette.warningSoft;
    return Card(
      child: InkWell(
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(14),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  _pill(item.statusText, statusColor, statusBg),
                  const SizedBox(width: 6),
                  _pill(
                    item.targetTypeText,
                    AppPalette.info,
                    AppPalette.infoSoft,
                  ),
                  const SizedBox(width: 6),
                  Expanded(
                    child: Text(
                      '原因：${item.reasonText}',
                      style: theme.textTheme.bodyMedium,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                  const Icon(
                    Icons.chevron_right,
                    color: AppPalette.textTertiary,
                  ),
                ],
              ),
              const SizedBox(height: 8),
              Text(
                '举报人：${item.reporterName}　${item.reporterInfo}',
                style: theme.textTheme.bodySmall,
              ),
              const SizedBox(height: 2),
              Text(
                '被举报：${item.targetTitle}　${item.targetInfo}',
                style: theme.textTheme.bodySmall,
              ),
              const SizedBox(height: 6),
              Row(
                children: [
                  Text('#${item.id}', style: theme.textTheme.bodySmall),
                  const Spacer(),
                  if (item.createdAt != null)
                    Text(
                      df.format(item.createdAt!.toLocal()),
                      style: theme.textTheme.bodySmall,
                    ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _pill(String text, Color fg, Color bg) {
    if (text.isEmpty) return const SizedBox.shrink();
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: bg,
        borderRadius: BorderRadius.circular(6),
      ),
      child: Text(
        text,
        style: TextStyle(fontSize: 12, color: fg, fontWeight: FontWeight.w600),
      ),
    );
  }
}
