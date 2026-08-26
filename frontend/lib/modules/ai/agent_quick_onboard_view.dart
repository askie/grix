import 'package:flutter/material.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:get/get.dart';

import '../../data/providers/agent_service.dart';
import 'controllers/agent_quick_onboard_controller.dart';

/// One question → paste one task → chat. See [AgentQuickOnboardController].
class AgentQuickOnboardView extends GetView<AgentQuickOnboardController> {
  const AgentQuickOnboardView({super.key});

  static const double _contentMaxWidth = 720;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text('ai_agent_quick_title'.tr)),
      body: Obx(() => _buildBody(context)),
    );
  }

  Widget _buildBody(BuildContext context) {
    final child = switch (controller.step.value) {
      QuickOnboardStep.selectType => _buildSelectType(context),
      QuickOnboardStep.install => _buildInstall(context),
      QuickOnboardStep.online => _buildOnline(context),
    };
    return ListView(
      key: const Key('quick-onboard-scroll'),
      padding: const EdgeInsets.fromLTRB(16, 20, 16, 32),
      children: [
        Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: _contentMaxWidth),
            child: child,
          ),
        ),
      ],
    );
  }

  // ── Step 1: the one question ──────────────────────────────────────────────

  Widget _buildSelectType(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(
          'ai_agent_quick_question'.tr,
          style: theme.textTheme.headlineSmall?.copyWith(
            fontWeight: FontWeight.w800,
          ),
        ),
        const SizedBox(height: 6),
        Text(
          'ai_agent_quick_question_hint'.tr,
          style: theme.textTheme.bodyMedium?.copyWith(
            color: theme.colorScheme.onSurface.withValues(alpha: 0.65),
            height: 1.4,
          ),
        ),
        const SizedBox(height: 20),
        if (controller.isLoadingGuides.value && controller.installGuides.isEmpty)
          const Padding(
            padding: EdgeInsets.all(24),
            child: Center(child: CircularProgressIndicator()),
          )
        else if (controller.installGuides.isEmpty)
          _guidesLoadFailure(context)
        else
          ...controller.installGuides.map((guide) => _typeOption(context, guide)),
      ],
    );
  }

  Widget _guidesLoadFailure(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      children: [
        Icon(
          Icons.cloud_off_outlined,
          size: 42,
          color: theme.colorScheme.outline,
        ),
        const SizedBox(height: 12),
        Text('ai_agent_setup_load_failed'.tr, textAlign: TextAlign.center),
        const SizedBox(height: 12),
        FilledButton.icon(
          key: const Key('quick-onboard-retry-guides'),
          onPressed: controller.loadInstallGuides,
          icon: const Icon(Icons.refresh_rounded),
          label: Text('common_retry'.tr),
        ),
      ],
    );
  }

  Widget _typeOption(BuildContext context, AgentApiInstallGuide guide) {
    final theme = Theme.of(context);
    final type = guide.type.trim().toLowerCase();
    final creating = controller.isCreating.value;
    final isPicked =
        creating && controller.selectedType.value == type;
    final isDefault = type == controller.defaultGuideType;
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: InkWell(
        key: Key('quick-onboard-option-$type'),
        onTap: creating ? null : () => controller.selectTypeAndCreate(type),
        borderRadius: BorderRadius.circular(14),
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(14),
            border: Border.all(
              color: isDefault
                  ? theme.colorScheme.primary.withValues(alpha: 0.55)
                  : theme.colorScheme.outline.withValues(alpha: 0.22),
              width: isDefault ? 1.4 : 1,
            ),
            color: theme.colorScheme.surface,
          ),
          child: Row(
            children: [
              Container(
                width: 40,
                height: 40,
                decoration: BoxDecoration(
                  color: theme.colorScheme.primary.withValues(alpha: 0.09),
                  borderRadius: BorderRadius.circular(11),
                ),
                child: Icon(
                  Icons.terminal_rounded,
                  size: 22,
                  color: theme.colorScheme.primary,
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Flexible(
                          child: Text(
                            guide.label.trim().isEmpty ? guide.type : guide.label,
                            overflow: TextOverflow.ellipsis,
                            style: theme.textTheme.titleSmall?.copyWith(
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                        ),
                        if (isDefault) ...[
                          const SizedBox(width: 8),
                          Container(
                            padding: const EdgeInsets.symmetric(
                              horizontal: 7,
                              vertical: 2,
                            ),
                            decoration: BoxDecoration(
                              color: theme.colorScheme.primary.withValues(
                                alpha: 0.1,
                              ),
                              borderRadius: BorderRadius.circular(999),
                            ),
                            child: Text(
                              'ai_agent_create_recommended'.tr,
                              style: theme.textTheme.labelSmall?.copyWith(
                                color: theme.colorScheme.primary,
                                fontWeight: FontWeight.w700,
                              ),
                            ),
                          ),
                        ],
                      ],
                    ),
                    if (guide.intro.trim().isNotEmpty) ...[
                      const SizedBox(height: 3),
                      Text(
                        guide.intro,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: theme.colorScheme.onSurface.withValues(
                            alpha: 0.6,
                          ),
                        ),
                      ),
                    ],
                  ],
                ),
              ),
              const SizedBox(width: 8),
              if (isPicked)
                const SizedBox(
                  width: 18,
                  height: 18,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              else
                Icon(
                  Icons.chevron_right_rounded,
                  color: theme.colorScheme.onSurface.withValues(alpha: 0.4),
                ),
            ],
          ),
        ),
      ),
    );
  }

  // ── Step 2: paste the task, we watch for the connection ──────────────────

  Widget _buildInstall(BuildContext context) {
    final theme = Theme.of(context);
    final guide = controller.selectedGuide;
    final label = guide == null || guide.label.trim().isEmpty
        ? controller.selectedType.value
        : guide.label;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _statusBanner(context),
        const SizedBox(height: 16),
        _sectionCard(
          context,
          key: const Key('quick-onboard-install-card'),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text(
                      'ai_agent_quick_paste_hint'.trParams({'name': label}),
                      style: theme.textTheme.bodyMedium?.copyWith(height: 1.45),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 8),
              Align(
                alignment: Alignment.centerLeft,
                child: TextButton.icon(
                  key: const Key('quick-onboard-switch-type'),
                  onPressed: () => _openTypeSwitcher(context),
                  icon: const Icon(Icons.swap_horiz_rounded, size: 18),
                  label: Text(
                    'ai_agent_quick_switch_type'.trParams({'name': label}),
                  ),
                ),
              ),
              const SizedBox(height: 4),
              if (controller.hasInstallTask) ...[
                _taskPreview(context),
                const SizedBox(height: 12),
                SizedBox(
                  width: double.infinity,
                  height: 46,
                  child: FilledButton.icon(
                    key: const Key('quick-onboard-copy-task'),
                    onPressed: controller.copyInstallTask,
                    icon: const Icon(Icons.assignment_outlined, size: 18),
                    label: Text('ai_agent_setup_copy_task'.tr),
                  ),
                ),
              ] else
                Text('ai_agent_api_install_type_unavailable'.tr),
            ],
          ),
        ),
      ],
    );
  }

  Widget _statusBanner(BuildContext context) {
    final theme = Theme.of(context);
    final timedOut = controller.pollTimedOut.value;
    final color = timedOut ? Colors.orange : theme.colorScheme.primary;
    return _sectionCard(
      context,
      key: const Key('quick-onboard-status-banner'),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 22,
            height: 22,
            child: timedOut
                ? Icon(Icons.hourglass_disabled_rounded, size: 20, color: color)
                : CircularProgressIndicator(strokeWidth: 2.4, color: color),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Text(
              timedOut
                  ? 'ai_agent_quick_timeout'.tr
                  : 'ai_agent_quick_waiting'.tr,
              style: theme.textTheme.bodyMedium?.copyWith(height: 1.4),
            ),
          ),
          if (timedOut)
            TextButton(
              key: const Key('quick-onboard-resume-poll'),
              onPressed: controller.pollNow,
              child: Text('ai_agent_quick_keep_waiting'.tr),
            ),
        ],
      ),
    );
  }

  Widget _taskPreview(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      key: const Key('quick-onboard-task-preview'),
      width: double.infinity,
      constraints: const BoxConstraints(maxHeight: 240),
      decoration: BoxDecoration(
        color: theme.colorScheme.surfaceContainerHighest.withValues(alpha: 0.5),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Scrollbar(
        child: SingleChildScrollView(
          // Keyed on the type so a switch rebuilds at the top; two tasks look
          // alike mid-body, so a kept offset reads as "nothing changed".
          key: ValueKey(
            'quick-onboard-task-scroll-${controller.selectedType.value}',
          ),
          padding: const EdgeInsets.all(12),
          child: SelectableText(
            controller.installTask,
            style: theme.textTheme.bodySmall?.copyWith(
              fontFamily: 'monospace',
              fontFamilyFallback: AppTheme.textFontFallbackOrNull,
              height: 1.45,
            ),
          ),
        ),
      ),
    );
  }

  Future<void> _openTypeSwitcher(BuildContext context) async {
    final selectedType = controller.selectedType.value;
    final guides = controller.installGuides.toList(growable: false);
    final picked = await showModalBottomSheet<String>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (sheetContext) {
        final theme = Theme.of(sheetContext);
        return SafeArea(
          top: false,
          child: ConstrainedBox(
            constraints: BoxConstraints(
              maxHeight: MediaQuery.of(sheetContext).size.height * 0.72,
            ),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Padding(
                  padding: const EdgeInsets.fromLTRB(20, 0, 20, 8),
                  child: Text(
                    'ai_agent_api_install_type_label'.tr,
                    style: theme.textTheme.titleMedium?.copyWith(
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
                Flexible(
                  child: ListView.builder(
                    key: const Key('quick-onboard-type-sheet'),
                    shrinkWrap: true,
                    itemCount: guides.length,
                    itemBuilder: (itemContext, index) {
                      final guide = guides[index];
                      final type = guide.type.trim().toLowerCase();
                      final isSelected = type == selectedType;
                      return ListTile(
                        key: Key('quick-onboard-type-option-$type'),
                        selected: isSelected,
                        title: Text(guide.label),
                        subtitle: guide.intro.trim().isEmpty
                            ? null
                            : Text(
                                guide.intro,
                                maxLines: 2,
                                overflow: TextOverflow.ellipsis,
                              ),
                        trailing: isSelected
                            ? Icon(
                                Icons.check_rounded,
                                color: theme.colorScheme.primary,
                              )
                            : null,
                        onTap: () => Navigator.of(itemContext).pop(type),
                      );
                    },
                  ),
                ),
              ],
            ),
          ),
        );
      },
    );
    if (picked != null) {
      await controller.switchType(picked);
    }
  }

  // ── Step 3: online, go chat ──────────────────────────────────────────────

  Widget _buildOnline(BuildContext context) {
    final theme = Theme.of(context);
    final name = controller.currentAgent?.agentName ?? '';
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Container(
          key: const Key('quick-onboard-online-card'),
          padding: const EdgeInsets.all(24),
          decoration: BoxDecoration(
            color: Colors.green.withValues(alpha: 0.08),
            borderRadius: BorderRadius.circular(20),
            border: Border.all(color: Colors.green.withValues(alpha: 0.3)),
          ),
          child: Column(
            children: [
              Container(
                width: 56,
                height: 56,
                decoration: const BoxDecoration(
                  color: Colors.green,
                  shape: BoxShape.circle,
                ),
                child: const Icon(
                  Icons.check_rounded,
                  size: 34,
                  color: Colors.white,
                ),
              ),
              const SizedBox(height: 14),
              Text(
                'ai_agent_quick_online_title'.trParams({'name': name}),
                textAlign: TextAlign.center,
                style: theme.textTheme.headlineSmall?.copyWith(
                  fontWeight: FontWeight.w800,
                ),
              ),
              const SizedBox(height: 8),
              Text(
                controller.probeDelivered.value
                    ? 'ai_agent_quick_online_hint'.tr
                    : 'ai_agent_setup_online_hint'.tr,
                textAlign: TextAlign.center,
                style: theme.textTheme.bodyMedium?.copyWith(
                  color: theme.colorScheme.onSurface.withValues(alpha: 0.7),
                  height: 1.4,
                ),
              ),
            ],
          ),
        ),
        const SizedBox(height: 20),
        SizedBox(
          height: 48,
          child: FilledButton.icon(
            key: const Key('quick-onboard-start-chat'),
            onPressed: controller.isNavigating.value ? null : controller.startChat,
            icon: controller.isNavigating.value
                ? const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.chat_bubble_rounded),
            label: Text('ai_agent_setup_start_chat'.tr),
          ),
        ),
      ],
    );
  }

  Widget _sectionCard(
    BuildContext context, {
    required Widget child,
    Key? key,
  }) {
    final theme = Theme.of(context);
    return Container(
      key: key,
      width: double.infinity,
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(
          color: theme.colorScheme.outline.withValues(alpha: 0.18),
        ),
      ),
      child: child,
    );
  }
}
