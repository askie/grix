import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:intl/intl.dart';

import '../../app/routes/app_routes.dart';
import '../../app/theme/app_palette.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/filter_bottom_sheet.dart';
import '../../shared/widgets/infinite_list_view.dart';
import '../../shared/widgets/user_ref.dart';
import 'moderation_controller.dart';
import 'moderation_models.dart';

/// 内容审查页：审查事件列表 + 关键词/仅看禁言筛选 + 无限滚动 + 解除禁言；右上角进入审查设置。
class ModerationView extends GetView<ModerationController> {
  const ModerationView({super.key});

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: '内容审查',
      actions: [
        IconButton(
          tooltip: '审查设置',
          onPressed: () => Get.toNamed(AppRoutes.moderationSettings),
          icon: const Icon(Icons.tune),
        ),
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
            child: InfiniteListView<ModerationEvent>(
              controller: controller,
              emptyText: '没有符合条件的审查事件',
              itemBuilder: (_, event, index) =>
                  _EventCard(event: event, controller: controller),
            ),
          ),
        ],
      ),
    );
  }
}

class _Toolbar extends StatelessWidget {
  const _Toolbar({required this.controller});

  final ModerationController controller;

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
                    hintText: '搜索 用户ID / 消息ID / 会话 / 账号',
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
                Obx(() => FilterBadgeIcon(
                      activeCount: controller.mutedOnly.value ? 1 : 0,
                      onTap: () => _showFilterSheet(context),
                    )),
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
      activeCount: controller.mutedOnly.value ? 1 : 0,
      onReset: controller.resetFilters,
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Obx(() => FilterChip(
                label: const Text('仅看禁言中'),
                selected: controller.mutedOnly.value,
                onSelected: controller.toggleMutedOnly,
              )),
        ],
      ),
    );
  }
}

/// 宽屏下的内联筛选控件。
class _InlineFilters extends StatelessWidget {
  const _InlineFilters({required this.controller});

  final ModerationController controller;

  @override
  Widget build(BuildContext context) {
    return Obx(() => FilterChip(
          label: const Text('仅看禁言中'),
          selected: controller.mutedOnly.value,
          onSelected: controller.toggleMutedOnly,
        ));
  }
}

class _EventCard extends StatelessWidget {
  const _EventCard({required this.event, required this.controller});

  final ModerationEvent event;
  final ModerationController controller;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final df = DateFormat('yyyy-MM-dd HH:mm');
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Align(
                    alignment: Alignment.centerLeft,
                    child: UserRef(
                      event.senderId,
                      placeholderName: event.senderPlaceholderName,
                      style: theme.textTheme.titleMedium,
                    ),
                  ),
                ),
                if (event.currentlyMuted)
                  _pill('禁言中', AppPalette.danger, AppPalette.dangerSoft)
                else if (event.muteApplied)
                  _pill('曾禁言', AppPalette.textSecondary, AppPalette.border),
              ],
            ),
            const SizedBox(height: 4),
            Text('会话 ${event.sessionId}', style: theme.textTheme.bodySmall),
            const SizedBox(height: 6),
            if (event.matchedKeywordsText.isNotEmpty)
              Text('命中关键词：${event.matchedKeywordsText}',
                  style: theme.textTheme.bodySmall
                      ?.copyWith(color: AppPalette.danger)),
            const SizedBox(height: 4),
            Row(
              children: [
                Text('累计命中 ${event.hitCount}　撤回：${event.recallStatusText}',
                    style: theme.textTheme.bodySmall),
                const Spacer(),
                if (event.createdAt != null)
                  Text(df.format(event.createdAt!.toLocal()),
                      style: theme.textTheme.bodySmall),
              ],
            ),
            if (event.currentlyMuted) ...[
              const SizedBox(height: 8),
              Align(
                alignment: Alignment.centerRight,
                child: OutlinedButton.icon(
                  onPressed: () => controller.unmute(event),
                  icon: const Icon(Icons.volume_up, size: 18),
                  label: const Text('解除禁言'),
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _pill(String text, Color fg, Color bg) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(color: bg, borderRadius: BorderRadius.circular(6)),
      child: Text(text,
          style: TextStyle(fontSize: 12, color: fg, fontWeight: FontWeight.w600)),
    );
  }
}
