import 'package:flutter/material.dart';
import 'package:get/get.dart';

import 'controllers/agent_scope_controller.dart';

class AgentScopeView extends GetView<AgentScopeController> {
  const AgentScopeView({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      appBar: AppBar(title: Text('ai_agent_scope_title'.tr)),
      body: Obx(() {
        if (!controller.canConfigure) {
          return _buildUnsupported(theme);
        }

        if (controller.isLoading.value) {
          return const Center(child: CircularProgressIndicator());
        }

        return ListView(
          padding: const EdgeInsets.all(16),
          children: [
            _buildHeader(theme),
            const SizedBox(height: 12),
            _buildBatchActions(),
            const SizedBox(height: 8),
            ...controller.scopeOptions.map(
              (option) => _buildScopeTile(theme, option),
            ),
          ],
        );
      }),
      bottomNavigationBar: Obx(() {
        if (!controller.canConfigure) {
          return const SizedBox.shrink();
        }
        return SafeArea(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 16),
            child: SizedBox(
              height: 44,
              child: ElevatedButton(
                onPressed: controller.isSaving.value
                    ? null
                    : controller.saveScopes,
                child: controller.isSaving.value
                    ? const SizedBox(
                        width: 16,
                        height: 16,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : Text('common_save'.tr),
              ),
            ),
          ),
        );
      }),
    );
  }

  Widget _buildHeader(ThemeData theme) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(12),
        color: theme.colorScheme.surface,
        border: Border.all(
          color: theme.colorScheme.outline.withValues(alpha: 0.15),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            controller.agentName.value.isEmpty
                ? '-'
                : controller.agentName.value,
            style: theme.textTheme.titleMedium?.copyWith(
              fontWeight: FontWeight.w700,
            ),
          ),
          const SizedBox(height: 6),
          Text(
            '${'ai_agent_api_agent_id'.tr}: ${controller.agentId.value}',
            style: theme.textTheme.bodySmall,
          ),
          const SizedBox(height: 4),
          Text(
            'ai_agent_scope_hint'.tr,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.secondary,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildBatchActions() {
    return Row(
      children: [
        TextButton(
          onPressed: controller.selectAllScopes,
          child: Text('ai_agent_scope_select_all'.tr),
        ),
        TextButton(
          onPressed: controller.clearScopes,
          child: Text('ai_agent_scope_clear_all'.tr),
        ),
      ],
    );
  }

  Widget _buildScopeTile(ThemeData theme, AgentScopeOption option) {
    return Card(
      margin: const EdgeInsets.only(bottom: 8),
      child: Obx(() {
        final checked = controller.isSelected(option.scope);
        return CheckboxListTile(
          value: checked,
          onChanged: (value) =>
              controller.toggleScope(option.scope, value ?? false),
          title: Text(option.label),
          subtitle: Text(option.description),
          controlAffinity: ListTileControlAffinity.trailing,
          activeColor: theme.colorScheme.primary,
        );
      }),
    );
  }

  Widget _buildUnsupported(ThemeData theme) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Text(
          'ai_agent_scope_provider_unsupported'.tr,
          style: theme.textTheme.bodyMedium?.copyWith(
            color: theme.colorScheme.secondary,
          ),
          textAlign: TextAlign.center,
        ),
      ),
    );
  }
}
