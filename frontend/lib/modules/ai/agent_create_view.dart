import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';
import '../../data/providers/agent_service.dart';
import '../../data/providers/feature_flag_service.dart';
import '../../app/themes/app_theme.dart';
import '../../shared/utils/app_external_links.dart';
import '../../shared/utils/toast_util.dart';
import '../../shared/widgets/app_dialog_style.dart';
import '../../shared/widgets/multi_locale_text_field.dart';
import '../../shared/widgets/session_avatar.dart';
import 'controllers/agent_category_manage_controller.dart';
import 'controllers/agent_create_controller.dart';
import 'widgets/agent_introduction_expanded_input.dart';
import 'widgets/contact_agent_picker_sheet.dart';

class AgentCreateView extends GetView<AgentCreateController> {
  const AgentCreateView({super.key});

  AgentCategoryManageController _ensureCategoryManageController() {
    if (Get.isRegistered<AgentCategoryManageController>()) {
      return Get.find<AgentCategoryManageController>();
    }
    return Get.put(AgentCategoryManageController());
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      appBar: AppBar(
        title: Text(
          controller.isEditMode ? 'ai_agent_edit'.tr : 'ai_agent_create'.tr,
        ),
        actions: [
          Obx(
            () => TextButton(
              onPressed: controller.isLoading.value ? null : controller.save,
              child: AnimatedSwitcher(
                duration: const Duration(milliseconds: 180),
                switchInCurve: Curves.easeOut,
                switchOutCurve: Curves.easeIn,
                child: _buildSaveButtonChild(
                  context,
                  controller.saveButtonState.value,
                ),
              ),
            ),
          ),
        ],
      ),
      body: SingleChildScrollView(
        padding: const EdgeInsets.all(16),
        child: Form(
          key: controller.formKey,
          child: Obx(() {
            final isAgentApi = controller.providerType.value == 3;
            final hasCreatedApiAgent = controller.apiAgentId.value
                .trim()
                .isNotEmpty;

            return Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (isAgentApi) ...[
                  _buildStepCard(
                    context,
                    stepNumber: 1,
                    title: 'ai_agent_step_basic_title'.tr,
                    description: hasCreatedApiAgent
                        ? 'ai_agent_step_basic_done'.tr
                        : 'ai_agent_step_basic_pending'.tr,
                    isCompleted: hasCreatedApiAgent,
                    key: const ValueKey('agent-api-step-1'),
                  ),
                  const SizedBox(height: 12),
                ],
                _buildProfileSection(context),
                const SizedBox(height: 20),

                // Agent Name
                TextFormField(
                  controller: controller.nameController,
                  decoration: InputDecoration(
                    labelText: 'ai_agent_name'.tr,
                    hintText: 'ai_agent_name_hint'.tr,
                  ),
                  validator: controller.validateAgentName,
                  inputFormatters: [LengthLimitingTextInputFormatter(100)],
                ),
                const SizedBox(height: 16),
                Align(
                  alignment: Alignment.centerRight,
                  child: TextButton.icon(
                    key: const Key('agent_insert_id_button'),
                    onPressed: () => _insertContact(context),
                    icon: const Icon(Icons.person_add_alt_1, size: 18),
                    label: Text('ai_agent_insert_id'.tr),
                  ),
                ),
                Stack(
                  children: [
                    TextFormField(
                      key: const Key('agent_introduction_field'),
                      controller: controller.introductionController,
                      decoration: InputDecoration(
                        labelText: 'ai_agent_introduction'.tr,
                        hintText: 'ai_agent_introduction_hint'.tr,
                        alignLabelWithHint: true,
                        // 右侧留给放大按钮。
                        contentPadding: const EdgeInsets.fromLTRB(
                          12,
                          16,
                          40,
                          12,
                        ),
                      ),
                      minLines: 3,
                      maxLines: 5,
                      inputFormatters: [LengthLimitingTextInputFormatter(3072)],
                    ),
                    PositionedDirectional(
                      top: 8,
                      end: 8,
                      child: Material(
                        color: Theme.of(
                          context,
                        ).colorScheme.surface.withValues(alpha: 0.88),
                        shape: const CircleBorder(),
                        child: InkWell(
                          key: const Key('agent_introduction_expand_button'),
                          customBorder: const CircleBorder(),
                          onTap: () => openAgentIntroductionExpandedEditor(
                            textController: controller.introductionController,
                            onInsertContact: _insertContact,
                            maxLength: 3072,
                          ),
                          child: Tooltip(
                            message: 'chat_input_expand'.tr,
                            child: Padding(
                              padding: const EdgeInsets.all(5),
                              child: Icon(
                                Icons.open_in_full_rounded,
                                size: 14,
                                color: Theme.of(
                                  context,
                                ).colorScheme.secondary.withValues(alpha: 0.7),
                              ),
                            ),
                          ),
                        ),
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 16),
                _buildCategoryField(context),
                const SizedBox(height: 20),

                Obx(() {
                  final currentType = controller.providerType.value;
                  if (currentType == 1 || currentType == 2) {
                    return const SizedBox.shrink();
                  }
                  return Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        'ai_agent_provider_type'.tr,
                        style: TextStyle(
                          fontSize: 14,
                          fontWeight: FontWeight.w600,
                          color: theme.colorScheme.onSurface.withValues(
                            alpha: 0.7,
                          ),
                        ),
                      ),
                      const SizedBox(height: 8),
                      SegmentedButton<int>(
                        segments: [
                          ButtonSegment(
                            value: 3,
                            label: Text('ai_provider_agent_api'.tr),
                            icon: const Icon(
                              Icons.settings_ethernet_rounded,
                              size: 18,
                            ),
                          ),
                          if (Get.find<FeatureFlagService>().isEnabled(
                            'agent_voice_llm',
                          ))
                            ButtonSegment(
                              value: 4,
                              label: Text('ai_provider_voice'.tr),
                              icon: const Icon(
                                Icons.graphic_eq_rounded,
                                size: 18,
                              ),
                            ),
                        ],
                        selected: {currentType},
                        onSelectionChanged: (v) =>
                            controller.providerType.value = v.first,
                      ),
                      const SizedBox(height: 20),
                    ],
                  );
                }),

                // Input fields based on provider type
                Obx(() {
                  if (controller.providerType.value == 1) {
                    return Column(
                      children: [
                        TextFormField(
                          controller: controller.providerController,
                          decoration: InputDecoration(
                            labelText: 'ai_agent_model_provider'.tr,
                            hintText: 'ai_agent_model_provider_hint'.tr,
                          ),
                        ),
                        const SizedBox(height: 16),
                        TextFormField(
                          controller: controller.promptController,
                          decoration: InputDecoration(
                            labelText: 'ai_agent_system_prompt'.tr,
                            hintText: 'ai_agent_system_prompt_hint'.tr,
                            alignLabelWithHint: true,
                          ),
                          maxLines: 5,
                        ),
                      ],
                    );
                  } else if (controller.providerType.value == 2) {
                    return Column(
                      children: [
                        TextFormField(
                          controller: controller.endpointController,
                          decoration: InputDecoration(
                            labelText: 'ai_agent_local_endpoint'.tr,
                            hintText: 'ai_agent_local_endpoint_hint'.tr,
                          ),
                          validator: (v) {
                            if (controller.providerType.value == 2 &&
                                (v == null || v.trim().isEmpty)) {
                              return 'ai_agent_endpoint_required'.tr;
                            }
                            return null;
                          },
                        ),
                        const SizedBox(height: 16),
                        TextFormField(
                          controller: controller.modelNameController,
                          decoration: InputDecoration(
                            labelText: 'ai_agent_model_name'.tr,
                            hintText: 'ai_agent_model_name_hint'.tr,
                          ),
                        ),
                        const SizedBox(height: 16),
                        // Context file editor entry
                        ListTile(
                          contentPadding: EdgeInsets.zero,
                          leading: const Icon(Icons.description_outlined),
                          title: Text('ai_agent_context_file'.tr),
                          subtitle: Text('ai_agent_context_file_hint'.tr),
                          trailing: const Icon(Icons.chevron_right_rounded),
                          onTap: () {
                            if (controller.isEditMode &&
                                controller.editAgentId != null) {
                              Get.toNamed(
                                '/agent/context',
                                arguments: {'agent_id': controller.editAgentId},
                              );
                            } else {
                              CustomToast.show(
                                'ai_agent_save_first'.tr,
                                isError: true,
                              );
                            }
                          },
                        ),
                      ],
                    );
                  } else if (controller.providerType.value == 4) {
                    return _buildVoiceSection(context);
                  }
                  return _buildAgentApiSection(context);
                }),
                Obx(() {
                  if (controller.apiAgentId.value.trim().isEmpty) {
                    return const SizedBox.shrink();
                  }
                  return Column(
                    children: [
                      const SizedBox(height: 12),
                      _buildDangerZone(context),
                    ],
                  );
                }),
              ],
            );
          }),
        ),
      ),
    );
  }

  Future<void> _insertContact(BuildContext context) async {
    final pick = await showContactAgentPickerSheet(context);
    if (pick == null || pick.id.trim().isEmpty) {
      return;
    }
    controller.insertContactIntoIntroduction(pick);
  }

  Widget _buildVoiceSection(BuildContext context) {
    final theme = Theme.of(context);
    final hintText = controller.voiceApiKeyHint.value.trim();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Obx(() {
          final options = controller.voiceModelOptions;
          if (options.isEmpty) {
            if (controller.voiceModelsLoading.value) {
              return const Padding(
                padding: EdgeInsets.symmetric(vertical: 12),
                child: LinearProgressIndicator(),
              );
            }
            return Padding(
              padding: const EdgeInsets.symmetric(vertical: 8),
              child: Text(
                'ai_voice_model_empty'.tr,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.error,
                ),
              ),
            );
          }
          final selectedId = controller.selectedVoiceModelId.value;
          return DropdownButtonFormField<String>(
            key: const Key('voice_model_field'),
            initialValue: selectedId.isEmpty ? null : selectedId,
            isExpanded: true,
            decoration: InputDecoration(labelText: 'ai_voice_model_label'.tr),
            items: options
                .map(
                  (o) => DropdownMenuItem(
                    value: o.id,
                    child: Text(o.label, overflow: TextOverflow.ellipsis),
                  ),
                )
                .toList(),
            onChanged: (v) {
              if (v == null) return;
              controller.selectVoiceModel(v);
            },
            validator: (v) =>
                (controller.providerType.value == 4 && (v == null || v.isEmpty))
                ? 'ai_voice_model_required'.tr
                : null,
          );
        }),
        const SizedBox(height: 16),
        // 模型：默认带出所选清单项的推荐模型，允许用户改成厂商新版本。
        TextFormField(
          key: const Key('voice_model_name_field'),
          controller: controller.voiceModelController,
          decoration: InputDecoration(
            labelText: 'ai_voice_model_name_label'.tr,
            helperText: 'ai_voice_model_name_hint'.tr,
          ),
          validator: (v) =>
              (controller.providerType.value == 4 &&
                  (v == null || v.trim().isEmpty))
              ? 'ai_voice_model_required'.tr
              : null,
        ),
        const SizedBox(height: 16),
        Obx(() {
          final presets = controller.availableVoicePresets;
          if (presets.isEmpty) {
            return TextFormField(
              controller: controller.voiceIdController,
              decoration: InputDecoration(
                labelText: 'ai_voice_voice_label'.tr,
                hintText: 'alloy',
              ),
            );
          }
          final currentId = controller.voiceIdController.text.trim();
          final isPreset = presets.any((p) => p.id == currentId);
          final dropdownValue = isPreset ? currentId : '__custom__';
          return Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              DropdownButtonFormField<String>(
                key: ValueKey(
                  'voice_preset_${controller.selectedVoiceModelId.value}',
                ),
                initialValue: dropdownValue,
                isExpanded: true,
                decoration: InputDecoration(
                  labelText: 'ai_voice_voice_label'.tr,
                ),
                items: [
                  ...presets.map(
                    (p) => DropdownMenuItem(
                      value: p.id,
                      child: Text(
                        '${p.id}（${p.label}）',
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                  ),
                  DropdownMenuItem(
                    value: '__custom__',
                    child: Text(
                      'ai_voice_custom_voice'.tr,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                ],
                onChanged: (v) {
                  if (v == null) return;
                  if (v == '__custom__') {
                    controller.voiceIdController.clear();
                  } else {
                    controller.voiceIdController.text = v;
                  }
                  controller.availableVoicePresets.refresh();
                },
              ),
              if (!isPreset) ...[
                const SizedBox(height: 8),
                TextFormField(
                  controller: controller.voiceIdController,
                  decoration: InputDecoration(
                    labelText: 'ai_voice_custom_voice_hint'.tr,
                    hintText: 'alloy',
                  ),
                ),
              ],
            ],
          );
        }),
        const SizedBox(height: 16),
        TextFormField(
          key: const Key('voice_api_key_field'),
          controller: controller.voiceApiKeyController,
          obscureText: true,
          decoration: InputDecoration(
            labelText: 'ai_voice_api_key_label'.tr,
            hintText: hintText.isNotEmpty
                ? 'ai_voice_api_key_set_hint'.trParams({'hint': hintText})
                : 'ai_voice_api_key_hint'.tr,
          ),
          validator: (v) {
            // 创建时必填；编辑时已存在 hint 则可留空保持
            if (controller.providerType.value == 4 &&
                hintText.isEmpty &&
                (v == null || v.trim().isEmpty)) {
              return 'ai_voice_api_key_required'.tr;
            }
            return null;
          },
        ),
        const SizedBox(height: 16),
        TextFormField(
          controller: controller.promptController,
          decoration: InputDecoration(
            labelText: 'ai_agent_system_prompt'.tr,
            alignLabelWithHint: true,
          ),
          maxLines: 5,
        ),
        const SizedBox(height: 16),
        Text(
          'ai_voice_welcome_label'.tr,
          style: theme.textTheme.bodyMedium?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
        const SizedBox(height: 6),
        MultiLocaleTextField(
          initial: controller.voiceWelcomeI18n,
          minLines: 1,
          maxLines: 2,
          onChanged: (v) => controller.voiceWelcomeI18n.value = v,
        ),
        const SizedBox(height: 16),
        // 护栏：单通话时长上限 / 每日次数上限（0=不限）
        TextFormField(
          key: const Key('voice_max_call_seconds_field'),
          controller: controller.voiceMaxCallSecondsController,
          keyboardType: TextInputType.number,
          inputFormatters: [FilteringTextInputFormatter.digitsOnly],
          decoration: InputDecoration(
            labelText: 'ai_voice_max_call_seconds'.tr,
            hintText: '0',
          ),
        ),
        const SizedBox(height: 16),
        TextFormField(
          key: const Key('voice_daily_call_limit_field'),
          controller: controller.voiceDailyCallLimitController,
          keyboardType: TextInputType.number,
          inputFormatters: [FilteringTextInputFormatter.digitsOnly],
          decoration: InputDecoration(
            labelText: 'ai_voice_daily_call_limit'.tr,
            hintText: '0',
          ),
        ),
        const SizedBox(height: 16),
        TextFormField(
          key: const Key('voice_max_concurrent_calls_field'),
          controller: controller.voiceMaxConcurrentCallsController,
          keyboardType: TextInputType.number,
          inputFormatters: [
            FilteringTextInputFormatter.digitsOnly,
            LengthLimitingTextInputFormatter(2),
          ],
          decoration: InputDecoration(
            labelText: 'ai_voice_max_concurrent_calls'.trParams({
              'max': '${AgentCreateController.kVoiceMaxConcurrentCallsMax}',
            }),
            hintText: '2',
          ),
        ),
        // 仅编辑已有 agent 时展示实时状态；判断放在 Obx 外，
        // 否则创建模式下 builder 不读取任何 Rx 会触发 GetX 断言。
        if (controller.editAgentId != null &&
            controller.editAgentId!.isNotEmpty)
          Obx(() {
            final stats = controller.voiceStats.value;
            final text = stats == null
                ? '—'
                : 'ai_voice_queue_status'.trParams({
                    'active': '${stats.active}',
                    'queued': '${stats.queued}',
                  });
            return Row(
              children: [
                Expanded(
                  child: Text(
                    text,
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ),
                IconButton(
                  key: const Key('voice_stats_refresh_button'),
                  tooltip: 'ai_voice_queue_refresh'.tr,
                  iconSize: 18,
                  onPressed: controller.voiceStatsLoading.value
                      ? null
                      : controller.refreshVoiceStats,
                  icon: const Icon(Icons.refresh),
                ),
              ],
            );
          }),
        const SizedBox(height: 8),
        Obx(
          () => SwitchListTile(
            key: const Key('voice_allow_visitor_switch'),
            contentPadding: EdgeInsets.zero,
            title: Text('ai_voice_allow_visitor'.tr),
            subtitle: Text('ai_voice_allow_visitor_hint'.tr),
            value: controller.voiceAllowVisitor.value,
            onChanged: (v) => controller.voiceAllowVisitor.value = v,
          ),
        ),
        const SizedBox(height: 16),
        // 测试拨打：仅 Web/桌面显示（iOS/Android 隐藏）
        Obx(() {
          if (!controller.canTestCall) {
            return const SizedBox.shrink();
          }
          return SizedBox(
            width: double.infinity,
            child: OutlinedButton.icon(
              key: const Key('voice_test_call_button'),
              onPressed: controller.isLoading.value
                  ? null
                  : controller.testCall,
              icon: const Icon(Icons.call_rounded, size: 18),
              label: Text('ai_voice_test_call'.tr),
              style: OutlinedButton.styleFrom(
                foregroundColor: theme.colorScheme.primary,
              ),
            ),
          );
        }),
      ],
    );
  }

  Widget _buildSaveButtonChild(
    BuildContext context,
    AgentSaveButtonState state,
  ) {
    final theme = Theme.of(context);
    switch (state) {
      case AgentSaveButtonState.saving:
        return Row(
          key: const ValueKey('agent-save-button-saving'),
          mainAxisSize: MainAxisSize.min,
          children: [
            const SizedBox(
              width: 16,
              height: 16,
              child: CircularProgressIndicator(strokeWidth: 2),
            ),
            const SizedBox(width: 8),
            Text('common_saving'.tr),
          ],
        );
      case AgentSaveButtonState.saved:
        return Row(
          key: const ValueKey('agent-save-button-saved'),
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.check_rounded,
              size: 18,
              color: theme.colorScheme.primary,
            ),
            const SizedBox(width: 4),
            Text('common_saved'.tr),
          ],
        );
      case AgentSaveButtonState.idle:
        return Text(
          'common_save'.tr,
          key: const ValueKey('agent-save-button-idle'),
        );
    }
  }

  Widget _buildProfileSection(BuildContext context) {
    final theme = Theme.of(context);
    return Obx(() {
      final pendingAvatarBytes = controller.pendingAvatarBytes.value;
      final avatarUrl = controller.avatarUrl.value.trim();

      return ValueListenableBuilder<TextEditingValue>(
        valueListenable: controller.nameController,
        builder: (context, value, _) {
          final avatarTitle = value.text.trim().isEmpty
              ? 'ai_agent_name_hint'.tr
              : value.text.trim();

          return Container(
            width: double.infinity,
            padding: const EdgeInsets.all(14),
            decoration: BoxDecoration(
              color: theme.colorScheme.surface,
              borderRadius: BorderRadius.circular(14),
              border: Border.all(
                color: theme.colorScheme.outline.withValues(alpha: 0.12),
              ),
            ),
            child: Row(
              children: [
                _buildEditableAvatar(
                  context,
                  avatarTitle: avatarTitle,
                  avatarUrl: avatarUrl,
                  pendingAvatarBytes: pendingAvatarBytes,
                ),
                const SizedBox(width: 14),
                Expanded(
                  child: Text(
                    'ai_agent_profile'.tr,
                    style: theme.textTheme.titleMedium?.copyWith(
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
              ],
            ),
          );
        },
      );
    });
  }

  Widget _buildCategoryField(BuildContext context) {
    return Obx(() {
      final categoryId = controller.categoryId.value;
      final controllerManage = _ensureCategoryManageController();
      final nodes = controllerManage.treeNodes;
      final categoryName = _resolveCategoryName(nodes, categoryId);

      return TextFormField(
        key: ValueKey('agent-category-field-$categoryId'),
        initialValue: categoryName,
        readOnly: true,
        showCursor: false,
        enableInteractiveSelection: false,
        decoration: InputDecoration(
          labelText: 'ai_agent_category_title'.tr,
          suffixIcon: const Icon(Icons.chevron_right_rounded),
        ),
        onTap: () {
          _showCategoryTreePicker(context, nodes);
        },
      );
    });
  }

  String _resolveCategoryName(List<CategoryNode> nodes, String categoryId) {
    if (categoryId == '0' || categoryId.isEmpty) {
      return 'ai_agent_category_root'.tr;
    }

    CategoryNode? findNode(List<CategoryNode> list, String id) {
      for (final node in list) {
        if (node.model.id == id) {
          return node;
        }
        final child = findNode(node.children, id);
        if (child != null) {
          return child;
        }
      }
      return null;
    }

    return findNode(nodes, categoryId)?.model.name ??
        'ai_agent_category_root'.tr;
  }

  Widget _buildEditableAvatar(
    BuildContext context, {
    required String avatarTitle,
    required String avatarUrl,
    required Uint8List? pendingAvatarBytes,
  }) {
    return GestureDetector(
      onTap: controller.isLoading.value ? null : controller.showAvatarEditSheet,
      child: Stack(
        clipBehavior: Clip.none,
        children: [
          _buildAvatarVisual(
            avatarTitle: avatarTitle,
            avatarUrl: avatarUrl,
            pendingAvatarBytes: pendingAvatarBytes,
          ),
          Positioned(
            right: -2,
            bottom: -2,
            child: _buildAvatarCameraBadge(context),
          ),
        ],
      ),
    );
  }

  Widget _buildAvatarVisual({
    required String avatarTitle,
    required String avatarUrl,
    required Uint8List? pendingAvatarBytes,
  }) {
    if (pendingAvatarBytes != null) {
      return ClipRRect(
        borderRadius: BorderRadius.circular(18),
        child: Image.memory(
          pendingAvatarBytes,
          width: 72,
          height: 72,
          fit: BoxFit.cover,
        ),
      );
    }

    return SessionAvatar(
      isGroup: false,
      avatarTitle: avatarTitle,
      avatarColor: AppTheme.getAvatarColor(
        controller.apiAgentId.value.isNotEmpty
            ? controller.apiAgentId.value
            : avatarTitle,
      ),
      avatarUrl: avatarUrl,
      size: 72,
      borderRadius: 18,
    );
  }

  Widget _buildAvatarCameraBadge(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      width: 24,
      height: 24,
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        shape: BoxShape.circle,
        border: Border.all(color: theme.scaffoldBackgroundColor, width: 1.5),
      ),
      child: Icon(
        Icons.camera_alt_rounded,
        size: 14,
        color: theme.colorScheme.secondary,
      ),
    );
  }

  Widget _buildCopyAllButton() {
    final copyPayload = _buildApiCredentialsCopyPayload();

    return OutlinedButton.icon(
      onPressed: copyPayload.isEmpty
          ? null
          : () => _copyToClipboard(copyPayload),
      icon: const Icon(Icons.copy_all_rounded, size: 18),
      label: Text('ai_agent_api_copy_all'.tr),
    );
  }

  Widget _buildAgentApiSection(BuildContext context) {
    final theme = Theme.of(context);
    final hasCreatedApiAgent = controller.apiAgentId.value.trim().isNotEmpty;
    if (!hasCreatedApiAgent) {
      return _buildStepCard(
        context,
        stepNumber: 2,
        title: 'ai_agent_step_install_title'.tr,
        description: 'ai_agent_step_install_pending'.tr,
        isLocked: true,
        key: const ValueKey('agent-api-step-2-locked'),
      );
    }
    final keyDisplay = controller.apiKey.value.isNotEmpty
        ? controller.apiKey.value
        : (controller.apiKeyHint.value.isNotEmpty
              ? '••••••••${controller.apiKeyHint.value}'
              : '');

    return _buildStepCard(
      context,
      stepNumber: 2,
      title: 'ai_agent_step_install_title'.tr,
      description: 'ai_agent_step_install_hint'.tr,
      key: const ValueKey('agent-api-step-2-ready'),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _buildInstallGuide(context, theme),
          const SizedBox(height: 12),
          _buildReadonlyField(
            context,
            label: 'ai_agent_api_agent_id'.tr,
            value: controller.apiAgentId.value,
            copyValue: controller.apiAgentId.value,
          ),
          const SizedBox(height: 12),
          _buildReadonlyField(
            context,
            label: 'ai_agent_api_endpoint'.tr,
            value: controller.apiEndpoint.value,
            copyValue: controller.apiEndpoint.value,
          ),
          const SizedBox(height: 12),
          _buildReadonlyField(
            context,
            label: 'ai_agent_api_key'.tr,
            value: keyDisplay,
            copyValue: controller.apiKey.value.isNotEmpty
                ? controller.apiKey.value
                : '',
          ),
          const SizedBox(height: 12),
          if (controller.isEditMode && controller.editAgentId != null)
            Wrap(
              spacing: 12,
              runSpacing: 8,
              children: [
                OutlinedButton.icon(
                  onPressed: controller.isLoading.value
                      ? null
                      : controller.rotateApiKey,
                  icon: const Icon(Icons.refresh_rounded),
                  label: Text('ai_agent_api_rotate'.tr),
                ),
                _buildCopyAllButton(),
              ],
            ),
        ],
      ),
    );
  }

  Widget _buildStepCard(
    BuildContext context, {
    required int stepNumber,
    required String title,
    required String description,
    bool isCompleted = false,
    bool isLocked = false,
    Widget? child,
    Key? key,
  }) {
    final theme = Theme.of(context);
    final borderColor = isCompleted
        ? theme.colorScheme.primary.withValues(alpha: 0.35)
        : theme.colorScheme.outline.withValues(alpha: 0.2);
    final backgroundColor = isCompleted
        ? theme.colorScheme.primary.withValues(alpha: 0.05)
        : theme.colorScheme.surface;

    return Container(
      key: key,
      width: double.infinity,
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: backgroundColor,
        borderRadius: BorderRadius.circular(14),
        border: Border.all(color: borderColor),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Container(
                width: 28,
                height: 28,
                alignment: Alignment.center,
                decoration: BoxDecoration(
                  color: theme.colorScheme.primary.withValues(alpha: 0.12),
                  shape: BoxShape.circle,
                ),
                child: Text(
                  '$stepNumber',
                  style: theme.textTheme.labelLarge?.copyWith(
                    color: theme.colorScheme.primary,
                    fontWeight: FontWeight.w700,
                  ),
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      title,
                      style: theme.textTheme.titleSmall?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      description,
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurface.withValues(
                          alpha: 0.72,
                        ),
                      ),
                    ),
                  ],
                ),
              ),
              if (isCompleted)
                Icon(
                  Icons.check_circle_rounded,
                  color: theme.colorScheme.primary,
                )
              else if (isLocked)
                Icon(
                  Icons.lock_outline_rounded,
                  color: theme.colorScheme.onSurface.withValues(alpha: 0.55),
                ),
            ],
          ),
          if (child != null) ...[const SizedBox(height: 14), child],
        ],
      ),
    );
  }

  Widget _buildInstallGuide(BuildContext context, ThemeData theme) {
    return Obx(() {
      final guides = controller.apiInstallGuides;
      final selectedGuide = controller.selectedApiInstallGuide;
      final selectedType = selectedGuide?.type.trim();
      final isLoading = controller.apiInstallGuidesLoading.value;

      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          TextFormField(
            key: ValueKey('agent_api_install_type_field-${selectedType ?? ''}'),
            initialValue: selectedGuide?.label ?? '',
            readOnly: true,
            showCursor: false,
            enableInteractiveSelection: false,
            decoration: InputDecoration(
              labelText: 'ai_agent_api_install_type_label'.tr,
              hintText: isLoading
                  ? 'ai_agent_api_install_type_loading'.tr
                  : 'ai_agent_api_install_type_hint'.tr,
              suffixIcon: const Icon(Icons.chevron_right_rounded),
            ),
            onTap: guides.isEmpty
                ? null
                : () => _showInstallTypePicker(context, guides, selectedType),
          ),
          const SizedBox(height: 8),
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Icon(
                Icons.info_outline_rounded,
                size: 14,
                color: theme.colorScheme.onSurface.withValues(alpha: 0.6),
              ),
              const SizedBox(width: 4),
              Expanded(
                child: Text(
                  'ai_agent_api_install_type_auto_note'.tr,
                  style: TextStyle(
                    fontSize: 12,
                    color: theme.colorScheme.onSurface.withValues(alpha: 0.6),
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),
          if (selectedGuide == null)
            Text(
              isLoading
                  ? 'ai_agent_api_install_type_loading'.tr
                  : 'ai_agent_api_install_type_unavailable'.tr,
              style: TextStyle(
                fontSize: 13,
                color: theme.colorScheme.onSurface.withValues(alpha: 0.7),
              ),
            )
          else ...[
            if (selectedGuide.intro.trim().isNotEmpty) ...[
              Text(
                selectedGuide.intro,
                style: TextStyle(
                  fontSize: 13,
                  color: theme.colorScheme.onSurface.withValues(alpha: 0.7),
                ),
              ),
              const SizedBox(height: 4),
            ],
            if (selectedGuide.isLink)
              _buildLinkField(
                context,
                value: selectedGuide.linkLabel.trim().isNotEmpty
                    ? selectedGuide.linkLabel.trim()
                    : _resolveGuideTemplate(selectedGuide.linkUrl),
                link: _resolveGuideTemplate(selectedGuide.linkUrl),
                openButtonKey: const Key('agent_api_install_guide_link_button'),
              )
            else
              _buildReadonlyField(
                context,
                value: _resolveGuideTemplate(selectedGuide.contentTemplate),
                copyValue: _resolveGuideTemplate(selectedGuide.contentTemplate),
                copyButtonKey: const Key('agent_api_install_guide_copy_button'),
              ),
          ],
        ],
      );
    });
  }

  void _showInstallTypePicker(
    BuildContext context,
    List<AgentApiInstallGuide> guides,
    String? selectedType,
  ) {
    Get.bottomSheet(
      Container(
        constraints: BoxConstraints(
          maxHeight: MediaQuery.of(context).size.height * 0.6,
        ),
        padding: const EdgeInsets.only(top: 16),
        decoration: BoxDecoration(
          color: Theme.of(context).scaffoldBackgroundColor,
          borderRadius: const BorderRadius.vertical(top: Radius.circular(16)),
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: Text(
                'ai_agent_api_install_type_hint'.tr,
                style: const TextStyle(
                  fontSize: 18,
                  fontWeight: FontWeight.bold,
                ),
              ),
            ),
            const SizedBox(height: 8),
            const Divider(height: 1),
            Flexible(
              child: ListView.builder(
                shrinkWrap: true,
                itemCount: guides.length,
                itemBuilder: (context, index) {
                  final guide = guides[index];
                  final type = guide.type.trim();
                  final isSelected = type == selectedType;
                  return ListTile(
                    title: Text(guide.label),
                    subtitle: guide.intro.trim().isNotEmpty
                        ? Text(
                            guide.intro,
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                          )
                        : null,
                    trailing: isSelected
                        ? Icon(
                            Icons.check_rounded,
                            color: Theme.of(context).colorScheme.primary,
                          )
                        : null,
                    onTap: () {
                      controller.selectApiInstallGuide(type);
                      Get.back();
                    },
                  );
                },
              ),
            ),
          ],
        ),
      ),
      isScrollControlled: true,
      useRootNavigator: true,
    );
  }

  Widget _buildReadonlyField(
    BuildContext context, {
    String label = '',
    required String value,
    required String copyValue,
    Key? copyButtonKey,
  }) {
    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        border: Border.all(
          color: theme.colorScheme.outline.withValues(alpha: 0.3),
        ),
        borderRadius: BorderRadius.circular(10),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (label.isNotEmpty) ...[
                  Text(
                    label,
                    style: TextStyle(
                      fontSize: 12,
                      color: theme.colorScheme.onSurface.withValues(
                        alpha: 0.65,
                      ),
                    ),
                  ),
                  const SizedBox(height: 6),
                ],
                SelectableText(
                  value.isNotEmpty ? value : '-',
                  style: const TextStyle(fontSize: 13),
                ),
              ],
            ),
          ),
          IconButton(
            key: copyButtonKey,
            onPressed: copyValue.isEmpty
                ? null
                : () => _copyToClipboard(copyValue),
            icon: const Icon(Icons.copy_rounded, size: 18),
          ),
        ],
      ),
    );
  }

  Widget _buildLinkField(
    BuildContext context, {
    required String value,
    required String link,
    Key? openButtonKey,
  }) {
    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        border: Border.all(
          color: theme.colorScheme.outline.withValues(alpha: 0.3),
        ),
        borderRadius: BorderRadius.circular(10),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Expanded(
            child: TextButton(
              onPressed: () => _openExternalLink(link),
              style: TextButton.styleFrom(
                padding: EdgeInsets.zero,
                minimumSize: Size.zero,
                tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                alignment: Alignment.centerLeft,
              ),
              child: Text(
                value,
                style: TextStyle(
                  fontSize: 13,
                  decoration: TextDecoration.underline,
                  decorationColor: theme.colorScheme.primary,
                ),
              ),
            ),
          ),
          IconButton(
            key: openButtonKey,
            onPressed: () => _openExternalLink(link),
            icon: const Icon(Icons.open_in_new_rounded, size: 18),
          ),
        ],
      ),
    );
  }

  String _buildApiCredentialsCopyPayload() {
    if (controller.apiKey.value.trim().isEmpty) {
      return '';
    }

    final selectedGuide = controller.selectedApiInstallGuide;
    if (selectedGuide != null) {
      final copyTemplate = selectedGuide.copyTemplate.trim();
      if (copyTemplate.isNotEmpty) {
        return _resolveGuideTemplate(copyTemplate);
      }
      if (!selectedGuide.isLink &&
          selectedGuide.contentTemplate.trim().isNotEmpty) {
        return _resolveGuideTemplate(selectedGuide.contentTemplate);
      }
    }

    final agentId = controller.apiAgentId.value.trim();
    final endpoint = controller.apiEndpoint.value.trim();
    final secretKey = controller.apiKey.value.trim();
    if (agentId.isEmpty || endpoint.isEmpty || secretKey.isEmpty) {
      return '';
    }
    return [
      '${'ai_agent_api_agent_id'.tr}:',
      agentId,
      '',
      '${'ai_agent_api_endpoint'.tr}:',
      endpoint,
      '',
      '${'ai_agent_api_key'.tr}:',
      secretKey,
    ].join('\n');
  }

  String _resolveGuideTemplate(String template) {
    if (template.trim().isEmpty) {
      return '';
    }

    var resolved = template;
    final replacements = <String, String>{
      // The backend task writes the Agent name into the connector config, so a
      // missed substitution here would land a literal "{{agent_name}}" in the
      // user's agents.json.
      'agent_name': _resolveGuidePlaceholderValue(
        controller.nameController.text,
        'ai_agent_name'.tr,
      ),
      'agent_id': _resolveGuidePlaceholderValue(
        controller.apiAgentId.value,
        'ai_agent_api_agent_id'.tr,
      ),
      'api_endpoint': _resolveGuidePlaceholderValue(
        controller.apiEndpoint.value,
        'ai_agent_api_endpoint'.tr,
      ),
      'api_key': _resolveGuidePlaceholderValue(
        controller.apiKey.value,
        'ai_agent_api_key'.tr,
      ),
    };

    replacements.forEach((key, value) {
      resolved = resolved.replaceAll('{{$key}}', value);
    });
    return resolved;
  }

  String _resolveGuidePlaceholderValue(String rawValue, String label) {
    final normalizedValue = rawValue.trim();
    if (normalizedValue.isNotEmpty) {
      return normalizedValue;
    }
    return '<$label>';
  }

  Future<void> _copyToClipboard(String value) async {
    await Clipboard.setData(ClipboardData(text: value));
    CustomToast.show('ai_agent_api_copied'.tr, isError: false);
  }

  Future<void> _openExternalLink(String link) async {
    final opened = await AppExternalLinks.open(link);
    if (opened) {
      return;
    }
    CustomToast.show('ai_agent_api_guide_open_failed'.tr);
  }

  Widget _buildDangerZone(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(10),
        color: AppTheme.errorColor.withValues(alpha: 0.04),
        border: Border.all(color: AppTheme.errorColor.withValues(alpha: 0.25)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'ai_agent_danger_zone'.tr,
            style: theme.textTheme.titleSmall?.copyWith(
              color: AppTheme.errorColor,
              fontWeight: FontWeight.w700,
            ),
          ),
          const SizedBox(height: 8),
          Text(
            'ai_agents_delete_confirm'.tr,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurface.withValues(alpha: 0.72),
            ),
          ),
          const SizedBox(height: 10),
          Obx(
            () => SizedBox(
              height: 36,
              child: OutlinedButton.icon(
                onPressed: controller.isLoading.value
                    ? null
                    : () => _confirmDelete(context),
                icon: const Icon(Icons.delete_outline_rounded, size: 16),
                style: OutlinedButton.styleFrom(
                  foregroundColor: AppTheme.errorColor,
                  side: BorderSide(
                    color: AppTheme.errorColor.withValues(alpha: 0.6),
                  ),
                ),
                label: Text('ai_agent_delete'.tr),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Future<void> _confirmDelete(BuildContext context) async {
    final confirmed =
        await showAppDialog<bool>(
          context: context,
          builder: (ctx) => AlertDialog(
            title: Text('common_confirm'.tr),
            content: Text('ai_agents_delete_confirm'.tr),
            actions: [
              TextButton(
                onPressed: () => Navigator.of(ctx).pop(false),
                child: Text('common_cancel'.tr),
              ),
              TextButton(
                onPressed: () => Navigator.of(ctx).pop(true),
                child: Text(
                  'common_delete'.tr,
                  style: const TextStyle(color: AppTheme.errorColor),
                ),
              ),
            ],
          ),
        ) ??
        false;
    if (!confirmed) {
      return;
    }
    await controller.deleteCurrentAgent();
  }

  void _showCategoryTreePicker(BuildContext context, List<CategoryNode> nodes) {
    Get.bottomSheet(
      Container(
        constraints: BoxConstraints(
          maxHeight: MediaQuery.of(context).size.height * 0.6,
        ),
        padding: const EdgeInsets.only(top: 16),
        decoration: BoxDecoration(
          color: Theme.of(context).scaffoldBackgroundColor,
          borderRadius: const BorderRadius.vertical(top: Radius.circular(16)),
        ),
        child: Column(
          children: [
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: Text(
                'ai_agent_category_select'.tr,
                style: const TextStyle(
                  fontSize: 18,
                  fontWeight: FontWeight.bold,
                ),
              ),
            ),
            const SizedBox(height: 8),
            ListTile(
              leading: const Icon(Icons.account_tree_outlined),
              title: Text('ai_agent_category_root'.tr),
              onTap: () {
                controller.categoryId.value = '0';
                Get.back();
              },
            ),
            const Divider(height: 1),
            Expanded(
              child: ListView(
                children: nodes
                    .map((node) => _buildCategoryPickerItem(context, node, 0))
                    .expand((e) => e)
                    .toList(),
              ),
            ),
          ],
        ),
      ),
      isScrollControlled: true,
      useRootNavigator: true,
    );
  }

  List<Widget> _buildCategoryPickerItem(
    BuildContext context,
    CategoryNode node,
    int depth,
  ) {
    return [
      ListTile(
        contentPadding: EdgeInsets.only(left: 16.0 + depth * 24.0, right: 16.0),
        leading: Icon(depth == 0 ? Icons.folder : Icons.folder_open),
        title: Text(node.model.name),
        onTap: () {
          controller.categoryId.value = node.model.id;
          Get.back();
        },
      ),
      ...node.children
          .map((child) => _buildCategoryPickerItem(context, child, depth + 1))
          .expand((e) => e),
    ];
  }
}
