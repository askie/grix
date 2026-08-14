import 'package:flutter/material.dart';
import 'package:get/get.dart';

import 'controllers/agent_connection_setup_controller.dart';

class AgentConnectionSetupView extends GetView<AgentConnectionSetupController> {
  const AgentConnectionSetupView({super.key});

  static const double _contentMaxWidth = 720;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text('ai_agent_setup_title'.tr)),
      body: Obx(() => _buildBody(context)),
      bottomNavigationBar: Obx(() => _buildBottomActions(context)),
    );
  }

  Widget _buildBody(BuildContext context) {
    if (controller.isLoadingAgent.value && controller.currentAgent == null) {
      return const Center(child: CircularProgressIndicator());
    }
    if (controller.currentAgent == null) {
      return _buildLoadFailure(context);
    }

    // No LayoutBuilder here: its builder runs during layout, after the
    // surrounding Obx has finished collecting dependencies, so every Rx read
    // below would go unobserved and the card would never rebuild on a change.
    return RefreshIndicator(
      onRefresh: controller.refreshStatus,
      child: ListView(
        key: const Key('agent-setup-scroll'),
        physics: const AlwaysScrollableScrollPhysics(),
        padding: const EdgeInsets.fromLTRB(16, 20, 16, 32),
        children: [
          Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: _contentMaxWidth),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  _buildSuccessCard(context),
                  const SizedBox(height: 16),
                  if (controller.isApiAgent) ...[
                    _buildConnectionStatusCard(context),
                    const SizedBox(height: 16),
                    _buildCredentialCard(context),
                    const SizedBox(height: 16),
                    _buildInstallGuideCard(context),
                  ] else
                    _buildReadyCard(context),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildLoadFailure(BuildContext context) {
    final theme = Theme.of(context);
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 420),
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(
                Icons.cloud_off_outlined,
                size: 48,
                color: theme.colorScheme.outline,
              ),
              const SizedBox(height: 16),
              Text(
                'ai_agent_setup_load_failed'.tr,
                textAlign: TextAlign.center,
                style: theme.textTheme.titleMedium,
              ),
              const SizedBox(height: 16),
              FilledButton.icon(
                key: const Key('agent-setup-retry'),
                onPressed: controller.isLoadingAgent.value
                    ? null
                    : controller.retryInitialLoad,
                icon: const Icon(Icons.refresh_rounded),
                label: Text('common_retry'.tr),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildSuccessCard(BuildContext context) {
    final theme = Theme.of(context);
    final agent = controller.currentAgent!;
    return Container(
      key: const Key('agent-setup-success-card'),
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        color: theme.colorScheme.primaryContainer.withValues(alpha: 0.35),
        borderRadius: BorderRadius.circular(20),
        border: Border.all(
          color: theme.colorScheme.primary.withValues(alpha: 0.2),
        ),
      ),
      child: Column(
        children: [
          Container(
            width: 56,
            height: 56,
            decoration: BoxDecoration(
              color: theme.colorScheme.primary,
              shape: BoxShape.circle,
            ),
            child: Icon(
              Icons.check_rounded,
              size: 34,
              color: theme.colorScheme.onPrimary,
            ),
          ),
          const SizedBox(height: 14),
          Text(
            'ai_agent_setup_created_title'.tr,
            textAlign: TextAlign.center,
            style: theme.textTheme.headlineSmall?.copyWith(
              fontWeight: FontWeight.w800,
            ),
          ),
          const SizedBox(height: 6),
          Text(
            agent.agentName.trim().isEmpty ? '-' : agent.agentName,
            textAlign: TextAlign.center,
            style: theme.textTheme.titleMedium?.copyWith(
              fontWeight: FontWeight.w700,
            ),
          ),
          const SizedBox(height: 6),
          Text(
            controller.isApiAgent
                ? 'ai_agent_setup_created_api_hint'.tr
                : 'ai_agent_setup_created_ready_hint'.tr,
            textAlign: TextAlign.center,
            style: theme.textTheme.bodyMedium?.copyWith(
              color: theme.colorScheme.onSurface.withValues(alpha: 0.7),
              height: 1.35,
            ),
          ),
        ],
      ),
    );
  }

  /// The card itself is the refresh control. It carries no icon of its own — the
  /// one that used to sit here just duplicated the bottom action — but it still
  /// has to be tappable: once the Agent reports online the bottom button turns
  /// into "start chat", and pull-to-refresh is disabled for mouse pointers, so
  /// on Web and desktop there would otherwise be no way to re-check a stale
  /// "online".
  Widget _buildConnectionStatusCard(BuildContext context) {
    final theme = Theme.of(context);
    final online = controller.isOnline;
    final statusColor = online ? Colors.green : Colors.orange;
    return _sectionCard(
      context,
      key: const Key('agent-setup-status-card'),
      padding: EdgeInsets.zero,
      child: InkWell(
        key: const Key('agent-setup-status-refresh'),
        onTap: controller.isRefreshing.value ? null : controller.refreshStatus,
        borderRadius: BorderRadius.circular(16),
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Container(
                width: 42,
                height: 42,
                decoration: BoxDecoration(
                  color: statusColor.withValues(alpha: 0.12),
                  borderRadius: BorderRadius.circular(12),
                ),
                child: controller.isRefreshing.value
                    ? Center(
                        child: SizedBox(
                          width: 18,
                          height: 18,
                          child: CircularProgressIndicator(
                            strokeWidth: 2,
                            color: statusColor,
                          ),
                        ),
                      )
                    : Icon(
                        online ? Icons.link_rounded : Icons.link_off_rounded,
                        color: statusColor,
                      ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      online
                          ? 'ai_agent_setup_online'.tr
                          : 'ai_agent_setup_waiting_connection'.tr,
                      style: theme.textTheme.titleSmall?.copyWith(
                        fontWeight: FontWeight.w700,
                        color: statusColor,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      online
                          ? 'ai_agent_setup_online_hint'.tr
                          : 'ai_agent_setup_waiting_connection_hint'.tr,
                      style: theme.textTheme.bodySmall?.copyWith(height: 1.35),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildCredentialCard(BuildContext context) {
    final theme = Theme.of(context);
    final hasOneTimeSecret = controller.apiKey.value.trim().isNotEmpty;
    return _sectionCard(
      context,
      key: const Key('agent-setup-credentials-card'),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'ai_agent_setup_credentials_title'.tr,
            style: theme.textTheme.titleMedium?.copyWith(
              fontWeight: FontWeight.w700,
            ),
          ),
          const SizedBox(height: 10),
          Container(
            width: double.infinity,
            padding: const EdgeInsets.all(12),
            decoration: BoxDecoration(
              color: hasOneTimeSecret
                  ? Colors.orange.withValues(alpha: 0.08)
                  : theme.colorScheme.surfaceContainerHighest.withValues(
                      alpha: 0.45,
                    ),
              borderRadius: BorderRadius.circular(12),
            ),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Icon(
                  hasOneTimeSecret
                      ? Icons.warning_amber_rounded
                      : Icons.info_outline_rounded,
                  size: 20,
                  color: hasOneTimeSecret
                      ? Colors.orange.shade800
                      : theme.colorScheme.secondary,
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    hasOneTimeSecret
                        ? 'ai_agent_setup_secret_warning'.tr
                        : 'ai_agent_setup_secret_unavailable'.tr,
                    style: theme.textTheme.bodySmall?.copyWith(height: 1.35),
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(height: 12),
          _readonlyValue(
            context,
            label: 'ai_agent_api_agent_id'.tr,
            value: controller.agentId.value,
            onCopy: () => controller.copyText(controller.agentId.value),
          ),
          const SizedBox(height: 10),
          _readonlyValue(
            context,
            label: 'ai_agent_api_endpoint'.tr,
            value: controller.apiEndpoint.value,
            onCopy: () => controller.copyText(controller.apiEndpoint.value),
          ),
          const SizedBox(height: 10),
          _readonlyValue(
            context,
            label: 'ai_agent_api_key'.tr,
            value: controller.apiKeyDisplay,
            onCopy: hasOneTimeSecret
                ? () => controller.copyText(controller.apiKey.value)
                : null,
            valueKey: const Key('agent-setup-api-key-value'),
          ),
        ],
      ),
    );
  }

  Widget _buildInstallGuideCard(BuildContext context) {
    final theme = Theme.of(context);
    return _sectionCard(
      context,
      key: const Key('agent-setup-install-guide-card'),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'ai_agent_setup_install_title'.tr,
            style: theme.textTheme.titleMedium?.copyWith(
              fontWeight: FontWeight.w700,
            ),
          ),
          const SizedBox(height: 6),
          Text(
            'ai_agent_setup_install_hint'.tr,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurface.withValues(alpha: 0.7),
              height: 1.4,
            ),
          ),
          const SizedBox(height: 16),
          if (controller.isLoadingGuides.value && controller.installGuides.isEmpty)
            const Center(
              child: Padding(
                padding: EdgeInsets.all(12),
                child: CircularProgressIndicator(),
              ),
            )
          else if (controller.installGuides.isEmpty)
            Text('ai_agent_api_install_type_unavailable'.tr)
          else
            _guideSelector(context),
          if (controller.hasInstallTask) ...[
            const SizedBox(height: 16),
            Text(
              'ai_agent_setup_task_paste_hint'.tr,
              style: theme.textTheme.bodySmall?.copyWith(height: 1.45),
            ),
            const SizedBox(height: 10),
            _taskPreview(context, controller.installTask),
            const SizedBox(height: 12),
            SizedBox(
              width: double.infinity,
              child: FilledButton.icon(
                key: const Key('agent-setup-copy-task'),
                onPressed: controller.copyInstallTask,
                icon: const Icon(Icons.assignment_outlined, size: 18),
                label: Text('ai_agent_setup_copy_task'.tr),
              ),
            ),
          ],
        ],
      ),
    );
  }

  /// A dropdown menu renders as a floating overlay sized to the free space
  /// around the field, so with this many agent types the list gets clipped and
  /// the tail becomes unreachable. A bottom sheet always gets the full width
  /// and scrolls.
  Widget _guideSelector(BuildContext context) {
    final theme = Theme.of(context);
    final selected = controller.selectedInstallGuide;
    // InkWell + InputDecorator reads as plain text to a screen reader; the
    // DropdownButtonFormField it replaced announced itself as a button.
    return Semantics(
      button: true,
      label: 'ai_agent_api_install_type_label'.tr,
      value: selected?.label ?? '',
      child: InkWell(
        key: const Key('agent-setup-guide-selector'),
        onTap: () => _openGuidePicker(context),
        borderRadius: BorderRadius.circular(12),
        child: InputDecorator(
          decoration: InputDecoration(
            labelText: 'ai_agent_api_install_type_label'.tr,
          ),
          child: Row(
            children: [
              Expanded(
                child: Text(
                  selected?.label ?? '',
                  key: const Key('agent-setup-guide-selector-label'),
                  overflow: TextOverflow.ellipsis,
                  style: theme.textTheme.bodyLarge,
                ),
              ),
              Icon(
                Icons.expand_more_rounded,
                color: theme.colorScheme.onSurface.withValues(alpha: 0.6),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _openGuidePicker(BuildContext context) async {
    final selectedType = controller.selectedClientType;
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
                    key: const Key('agent-setup-guide-sheet'),
                    shrinkWrap: true,
                    itemCount: guides.length,
                    itemBuilder: (itemContext, index) {
                      final guide = guides[index];
                      final type = guide.type.trim().toLowerCase();
                      final isSelected = type == selectedType;
                      return ListTile(
                        key: Key('agent-setup-guide-option-$type'),
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
      controller.selectInstallGuide(picked);
    }
  }

  /// The task is long by design — it is written for an agent to execute, not for
  /// the owner to memorize — so it gets a bounded, scrollable frame instead of
  /// pushing the copy button off the page.
  Widget _taskPreview(BuildContext context, String task) {
    final theme = Theme.of(context);
    return Container(
      key: const Key('agent-setup-task-preview'),
      width: double.infinity,
      constraints: const BoxConstraints(maxHeight: 260),
      decoration: BoxDecoration(
        color: theme.colorScheme.surfaceContainerHighest.withValues(alpha: 0.5),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Scrollbar(
        child: SingleChildScrollView(
          // Keyed on the type so switching rebuilds the scroll view back at the
          // top. Reusing it would keep the old offset, and since two tasks look
          // alike mid-body, a swapped task can still read as "nothing changed".
          key: ValueKey('agent-setup-task-scroll-${controller.selectedClientType}'),
          padding: const EdgeInsets.all(12),
          child: SelectableText(
            task,
            style: theme.textTheme.bodySmall?.copyWith(
              fontFamily: 'monospace',
              height: 1.45,
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildReadyCard(BuildContext context) {
    final theme = Theme.of(context);
    return _sectionCard(
      context,
      key: const Key('agent-setup-ready-card'),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(
            Icons.chat_bubble_outline_rounded,
            color: theme.colorScheme.primary,
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'ai_agent_setup_ready_title'.tr,
                  style: theme.textTheme.titleSmall?.copyWith(
                    fontWeight: FontWeight.w700,
                  ),
                ),
                const SizedBox(height: 4),
                Text(
                  'ai_agent_setup_ready_hint'.tr,
                  style: theme.textTheme.bodySmall?.copyWith(height: 1.35),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _sectionCard(
    BuildContext context, {
    required Widget child,
    Key? key,
    EdgeInsetsGeometry padding = const EdgeInsets.all(16),
  }) {
    final theme = Theme.of(context);
    return Container(
      key: key,
      width: double.infinity,
      padding: padding,
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

  Widget _readonlyValue(
    BuildContext context, {
    required String label,
    required String value,
    required VoidCallback? onCopy,
    Key? valueKey,
  }) {
    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.fromLTRB(12, 10, 6, 10),
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(12),
        border: Border.all(
          color: theme.colorScheme.outline.withValues(alpha: 0.22),
        ),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  label,
                  style: theme.textTheme.labelSmall?.copyWith(
                    color: theme.colorScheme.onSurface.withValues(alpha: 0.6),
                  ),
                ),
                const SizedBox(height: 4),
                SelectableText(
                  value.trim().isEmpty ? '-' : value,
                  key: valueKey,
                  style: theme.textTheme.bodyMedium,
                ),
              ],
            ),
          ),
          IconButton(
            onPressed: value.trim().isEmpty ? null : onCopy,
            tooltip: 'common_copy'.tr,
            icon: const Icon(Icons.copy_rounded, size: 18),
          ),
        ],
      ),
    );
  }

  Widget _buildBottomActions(BuildContext context) {
    final agent = controller.currentAgent;
    if (agent == null) {
      return const SizedBox.shrink();
    }
    final theme = Theme.of(context);
    final isApiAgent = controller.isApiAgent;
    final isOnline = controller.isOnline;
    return Material(
      color: theme.scaffoldBackgroundColor,
      elevation: 8,
      child: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 10, 16, 12),
          child: Center(
            heightFactor: 1,
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: _contentMaxWidth),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  SizedBox(
                    height: 48,
                    child: FilledButton.icon(
                      key: const Key('agent-setup-primary-action'),
                      onPressed: controller.isBusy
                          ? null
                          : controller.primaryAction,
                      icon: controller.isBusy
                          ? const SizedBox(
                              width: 18,
                              height: 18,
                              child: CircularProgressIndicator(strokeWidth: 2),
                            )
                          : Icon(
                              isApiAgent && !isOnline
                                  ? Icons.refresh_rounded
                                  : Icons.chat_bubble_rounded,
                            ),
                      label: Text(
                        isApiAgent && !isOnline
                            ? 'ai_agent_setup_check_connection'.tr
                            : 'ai_agent_setup_start_chat'.tr,
                      ),
                    ),
                  ),
                  if (!isApiAgent) ...[
                    const SizedBox(height: 8),
                    SizedBox(
                      height: 44,
                      child: OutlinedButton(
                        key: const Key('agent-setup-continue-config'),
                        onPressed: controller.isBusy
                            ? null
                            : controller.continueConfiguration,
                        child: Text('ai_agent_setup_continue_config'.tr),
                      ),
                    ),
                  ],
                  TextButton(
                    key: Key(
                      isApiAgent ? 'agent-setup-later' : 'agent-setup-done',
                    ),
                    onPressed: isApiAgent
                        ? controller.finishLater
                        : controller.finish,
                    child: Text(
                      isApiAgent ? 'ai_agent_setup_later'.tr : 'common_done'.tr,
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
