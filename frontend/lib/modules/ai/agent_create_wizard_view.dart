import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';

import 'controllers/agent_create_wizard_controller.dart';

class AgentCreateWizardView extends GetView<AgentCreateWizardController> {
  const AgentCreateWizardView({super.key});

  @override
  Widget build(BuildContext context) {
    return Obx(() {
      final choosingType = controller.step.value == 0;
      return Scaffold(
        appBar: AppBar(
          leading: BackButton(
            onPressed: choosingType ? Get.back : controller.chooseAnotherType,
          ),
          title: Text(
            choosingType
                ? 'ai_agent_create'.tr
                : 'ai_agent_create_required_title'.tr,
          ),
        ),
        body: choosingType
            ? _buildTypePicker(context)
            : _buildCreateForm(context),
        bottomNavigationBar: choosingType ? null : _buildBottomAction(context),
      );
    });
  }

  Widget _buildTypePicker(BuildContext context) {
    final theme = Theme.of(context);
    return SafeArea(
      child: SingleChildScrollView(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 720),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'ai_agent_create_choose_title'.tr,
                  style: theme.textTheme.headlineSmall?.copyWith(
                    fontWeight: FontWeight.w700,
                  ),
                ),
                const SizedBox(height: 8),
                Text(
                  'ai_agent_create_choose_hint'.tr,
                  style: theme.textTheme.bodyMedium?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                    height: 1.45,
                  ),
                ),
                const SizedBox(height: 20),
                _buildTypeCard(
                  context,
                  providerType: 3,
                  icon: Icons.terminal_rounded,
                  title: 'ai_agent_create_api_title'.tr,
                  description: 'ai_agent_create_api_desc'.tr,
                  recommended: true,
                ),
                const SizedBox(height: 12),
                _buildTypeCard(
                  context,
                  providerType: 1,
                  icon: Icons.cloud_outlined,
                  title: 'ai_agent_create_remote_title'.tr,
                  description: 'ai_agent_create_remote_desc'.tr,
                ),
                const SizedBox(height: 12),
                _buildTypeCard(
                  context,
                  providerType: 2,
                  icon: Icons.computer_rounded,
                  title: 'ai_agent_create_local_title'.tr,
                  description: 'ai_agent_create_local_desc'.tr,
                ),
                const SizedBox(height: 12),
                _buildTypeCard(
                  context,
                  providerType: 4,
                  icon: Icons.graphic_eq_rounded,
                  title: 'ai_agent_create_voice_title'.tr,
                  description: 'ai_agent_create_voice_desc'.tr,
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildTypeCard(
    BuildContext context, {
    required int providerType,
    required IconData icon,
    required String title,
    required String description,
    bool recommended = false,
  }) {
    final theme = Theme.of(context);
    return Material(
      color: theme.colorScheme.surface,
      borderRadius: BorderRadius.circular(16),
      child: InkWell(
        key: Key('agent-create-type-$providerType'),
        onTap: () => controller.selectProviderType(providerType),
        borderRadius: BorderRadius.circular(16),
        child: Container(
          width: double.infinity,
          padding: const EdgeInsets.all(16),
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(16),
            border: Border.all(
              color: recommended
                  ? theme.colorScheme.primary.withValues(alpha: 0.45)
                  : theme.colorScheme.outlineVariant,
            ),
          ),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Container(
                width: 44,
                height: 44,
                decoration: BoxDecoration(
                  color: theme.colorScheme.primaryContainer,
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Icon(icon, color: theme.colorScheme.onPrimaryContainer),
              ),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Expanded(
                          child: Text(
                            title,
                            style: theme.textTheme.titleMedium?.copyWith(
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                        ),
                        if (recommended)
                          Container(
                            padding: const EdgeInsets.symmetric(
                              horizontal: 8,
                              vertical: 3,
                            ),
                            decoration: BoxDecoration(
                              color: theme.colorScheme.primaryContainer,
                              borderRadius: BorderRadius.circular(10),
                            ),
                            child: Text(
                              'ai_agent_create_recommended'.tr,
                              style: theme.textTheme.labelSmall?.copyWith(
                                color: theme.colorScheme.onPrimaryContainer,
                                fontWeight: FontWeight.w600,
                              ),
                            ),
                          ),
                      ],
                    ),
                    const SizedBox(height: 6),
                    Text(
                      description,
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                        height: 1.4,
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 8),
              Icon(
                Icons.chevron_right_rounded,
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildCreateForm(BuildContext context) {
    final theme = Theme.of(context);
    final type = controller.providerType.value;
    return SafeArea(
      bottom: false,
      child: SingleChildScrollView(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 720),
            child: Form(
              key: controller.formKey,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  _buildSelectedTypeHeader(context, type),
                  const SizedBox(height: 20),
                  Text(
                    'ai_agent_create_required_hint'.tr,
                    style: theme.textTheme.bodyMedium?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                  const SizedBox(height: 16),
                  TextFormField(
                    key: const Key('agent-create-name-field'),
                    controller: controller.nameController,
                    autofocus: true,
                    decoration: InputDecoration(
                      labelText: 'ai_agent_name'.tr,
                      hintText: 'ai_agent_name_hint'.tr,
                    ),
                    validator: controller.validateAgentName,
                    inputFormatters: [LengthLimitingTextInputFormatter(100)],
                    textInputAction: TextInputAction.next,
                  ),
                  const SizedBox(height: 16),
                  _buildProviderFields(context, type),
                  const SizedBox(height: 20),
                  _buildOptionalProfile(context),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildSelectedTypeHeader(BuildContext context, int type) {
    final theme = Theme.of(context);
    final metadata = _typeMetadata(type);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: theme.colorScheme.primaryContainer.withValues(alpha: 0.45),
        borderRadius: BorderRadius.circular(14),
      ),
      child: Row(
        children: [
          Icon(metadata.icon, color: theme.colorScheme.primary),
          const SizedBox(width: 12),
          Expanded(
            child: Text(
              metadata.title,
              style: theme.textTheme.titleSmall?.copyWith(
                fontWeight: FontWeight.w700,
              ),
            ),
          ),
          TextButton(
            key: const Key('agent-create-change-type'),
            onPressed: controller.chooseAnotherType,
            child: Text('ai_agent_create_change_type'.tr),
          ),
        ],
      ),
    );
  }

  Widget _buildProviderFields(BuildContext context, int type) {
    switch (type) {
      case 1:
        return Column(
          children: [
            TextFormField(
              key: const Key('agent-create-remote-provider-field'),
              controller: controller.modelProviderController,
              decoration: InputDecoration(
                labelText: 'ai_agent_model_provider'.tr,
                hintText: 'ai_agent_model_provider_hint'.tr,
              ),
            ),
            const SizedBox(height: 16),
            TextFormField(
              key: const Key('agent-create-prompt-field'),
              controller: controller.promptController,
              decoration: InputDecoration(
                labelText: 'ai_agent_system_prompt'.tr,
                hintText: 'ai_agent_system_prompt_hint'.tr,
                alignLabelWithHint: true,
              ),
              minLines: 3,
              maxLines: 5,
            ),
          ],
        );
      case 2:
        return Column(
          children: [
            TextFormField(
              key: const Key('agent-create-local-endpoint-field'),
              controller: controller.localEndpointController,
              decoration: InputDecoration(
                labelText: 'ai_agent_local_endpoint'.tr,
                hintText: 'ai_agent_local_endpoint_hint'.tr,
              ),
              validator: controller.validateLocalEndpoint,
            ),
            const SizedBox(height: 16),
            TextFormField(
              key: const Key('agent-create-local-model-field'),
              controller: controller.localModelController,
              decoration: InputDecoration(
                labelText: 'ai_agent_model_name'.tr,
                hintText: 'ai_agent_model_name_hint'.tr,
              ),
            ),
          ],
        );
      case 3:
        return _buildInfoCard(
          context,
          icon: Icons.terminal_rounded,
          text: 'ai_agent_create_api_next_hint'.tr,
        );
      case 4:
        return _buildVoiceFields(context);
      default:
        return const SizedBox.shrink();
    }
  }

  Widget _buildVoiceFields(BuildContext context) {
    return Obx(() {
      if (controller.voiceModelsLoading.value) {
        return const Padding(
          padding: EdgeInsets.symmetric(vertical: 24),
          child: LinearProgressIndicator(),
        );
      }
      final options = controller.voiceModels;
      return Column(
        children: [
          DropdownButtonFormField<String>(
            key: const Key('agent-create-voice-model-field'),
            initialValue: controller.selectedVoiceModelId.value.isEmpty
                ? null
                : controller.selectedVoiceModelId.value,
            isExpanded: true,
            decoration: InputDecoration(labelText: 'ai_voice_model_label'.tr),
            items: options
                .map(
                  (option) => DropdownMenuItem<String>(
                    value: option.id,
                    child: Text(option.label, overflow: TextOverflow.ellipsis),
                  ),
                )
                .toList(),
            onChanged: controller.selectVoiceModel,
            validator: controller.validateVoiceModel,
          ),
          if (options.isEmpty) ...[
            const SizedBox(height: 8),
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton.icon(
                onPressed: controller.loadVoiceModels,
                icon: const Icon(Icons.refresh_rounded),
                label: Text('common_retry'.tr),
              ),
            ),
          ],
          const SizedBox(height: 16),
          TextFormField(
            key: const Key('agent-create-voice-key-field'),
            controller: controller.voiceApiKeyController,
            obscureText: true,
            decoration: InputDecoration(
              labelText: 'ai_voice_api_key_label'.tr,
              hintText: 'ai_voice_api_key_hint'.tr,
            ),
            validator: controller.validateVoiceApiKey,
          ),
        ],
      );
    });
  }

  Widget _buildInfoCard(
    BuildContext context, {
    required IconData icon,
    required String text,
  }) {
    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: theme.colorScheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(12),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, size: 20, color: theme.colorScheme.primary),
          const SizedBox(width: 10),
          Expanded(
            child: Text(
              text,
              style: theme.textTheme.bodySmall?.copyWith(height: 1.45),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildOptionalProfile(BuildContext context) {
    final theme = Theme.of(context);
    return Obx(() {
      final expanded = controller.showOptionalProfile.value;
      return Container(
        width: double.infinity,
        padding: const EdgeInsets.all(14),
        decoration: BoxDecoration(
          border: Border.all(color: theme.colorScheme.outlineVariant),
          borderRadius: BorderRadius.circular(14),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            InkWell(
              key: const Key('agent-create-optional-profile-toggle'),
              onTap: () => controller.showOptionalProfile.toggle(),
              child: Row(
                children: [
                  const Icon(Icons.person_outline_rounded, size: 20),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          'ai_agent_create_optional_profile'.tr,
                          style: theme.textTheme.titleSmall?.copyWith(
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                        const SizedBox(height: 2),
                        Text(
                          'ai_agent_create_optional_profile_hint'.tr,
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: theme.colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ],
                    ),
                  ),
                  Icon(
                    expanded
                        ? Icons.expand_less_rounded
                        : Icons.expand_more_rounded,
                  ),
                ],
              ),
            ),
            if (expanded) ...[
              const SizedBox(height: 14),
              TextFormField(
                key: const Key('agent-create-introduction-field'),
                controller: controller.introductionController,
                decoration: InputDecoration(
                  labelText: 'ai_agent_introduction'.tr,
                  hintText: 'ai_agent_introduction_hint'.tr,
                  alignLabelWithHint: true,
                ),
                minLines: 2,
                maxLines: 4,
                inputFormatters: [LengthLimitingTextInputFormatter(3072)],
              ),
            ],
          ],
        ),
      );
    });
  }

  Widget _buildBottomAction(BuildContext context) {
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 10, 16, 16),
        child: Center(
          heightFactor: 1,
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 720),
            child: SizedBox(
              width: double.infinity,
              height: 48,
              child: Obx(
                () => ElevatedButton.icon(
                  key: const Key('agent-create-submit'),
                  onPressed: controller.isSubmitting.value
                      ? null
                      : controller.submit,
                  icon: controller.isSubmitting.value
                      ? const SizedBox(
                          width: 16,
                          height: 16,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Icon(Icons.arrow_forward_rounded),
                  label: Text(
                    controller.isSubmitting.value
                        ? 'common_saving'.tr
                        : 'ai_agent_create_and_continue'.tr,
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }

  _AgentCreateTypeMetadata _typeMetadata(int type) {
    switch (type) {
      case 1:
        return _AgentCreateTypeMetadata(
          Icons.cloud_outlined,
          'ai_agent_create_remote_title'.tr,
        );
      case 2:
        return _AgentCreateTypeMetadata(
          Icons.computer_rounded,
          'ai_agent_create_local_title'.tr,
        );
      case 4:
        return _AgentCreateTypeMetadata(
          Icons.graphic_eq_rounded,
          'ai_agent_create_voice_title'.tr,
        );
      case 3:
      default:
        return _AgentCreateTypeMetadata(
          Icons.terminal_rounded,
          'ai_agent_create_api_title'.tr,
        );
    }
  }
}

class _AgentCreateTypeMetadata {
  const _AgentCreateTypeMetadata(this.icon, this.title);

  final IconData icon;
  final String title;
}
