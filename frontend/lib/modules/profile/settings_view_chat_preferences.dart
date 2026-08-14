part of 'settings_view.dart';

extension _SettingsViewChatPreferences on SettingsView {
  Widget _buildDefaultAgentTile({
    required BuildContext context,
    required AgentService agentService,
    required UserSettingsService userSettingsService,
  }) {
    final theme = Theme.of(context);
    final selectableAgents = agentService.allAccessibleAgents
        .where((agent) => agent.status == 1)
        .toList();
    final currentValue = userSettingsService.autoDelegateAgentId.value.trim();
    final selectedValue = selectableAgents.any((a) => a.id == currentValue)
        ? currentValue
        : '';
    final isBusy =
        userSettingsService.isLoading.value ||
        userSettingsService.isSaving.value;
    String selectedLabel = 'settings_chat_default_agent_none'.tr;
    for (final agent in selectableAgents) {
      if (agent.id == selectedValue) {
        selectedLabel = agent.agentName;
        break;
      }
    }

    return ListTile(
      leading: Container(
        width: 36,
        height: 36,
        decoration: BoxDecoration(
          color: AppTheme.warningColor.withValues(alpha: 0.12),
          borderRadius: BorderRadius.circular(10),
        ),
        child: const Icon(
          Icons.smart_toy_outlined,
          color: AppTheme.warningColor,
          size: 20,
        ),
      ),
      title: Text('settings_chat_default_agent'.tr),
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (isBusy)
            SizedBox(
              width: 14,
              height: 14,
              child: CircularProgressIndicator(
                strokeWidth: 2,
                color: theme.primaryColor,
              ),
            ),
          if (isBusy) const SizedBox(width: 8),
          ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 150),
            child: Text(
              selectedLabel,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                fontSize: 13,
                color: theme.colorScheme.secondary,
              ),
              textAlign: TextAlign.right,
            ),
          ),
          Icon(
            Icons.chevron_right_rounded,
            color: theme.colorScheme.secondary.withValues(alpha: 0.4),
          ),
        ],
      ),
      onTap: isBusy
          ? null
          : () => _showDefaultAgentSheet(
              context: context,
              userSettingsService: userSettingsService,
              selectableAgents: selectableAgents,
            ),
    );
  }

  Widget _buildFriendAddSettingTile({
    required BuildContext context,
    required UserSettingsService userSettingsService,
  }) {
    final theme = Theme.of(context);
    final currentSetting = userSettingsService.friendAddSetting.value;
    final isBusy =
        userSettingsService.isLoading.value ||
        userSettingsService.isSaving.value;

    return ListTile(
      leading: Container(
        width: 36,
        height: 36,
        decoration: BoxDecoration(
          color: AppTheme.primaryColor.withValues(alpha: 0.12),
          borderRadius: BorderRadius.circular(10),
        ),
        child: const Icon(
          Icons.person_add_alt_rounded,
          color: AppTheme.primaryColor,
          size: 20,
        ),
      ),
      title: Text('settings_chat_friend_add'.tr),
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (isBusy)
            SizedBox(
              width: 14,
              height: 14,
              child: CircularProgressIndicator(
                strokeWidth: 2,
                color: theme.primaryColor,
              ),
            ),
          if (isBusy) const SizedBox(width: 8),
          Text(
            _friendAddSettingLabel(currentSetting),
            style: TextStyle(fontSize: 13, color: theme.colorScheme.secondary),
          ),
          Icon(
            Icons.chevron_right_rounded,
            color: theme.colorScheme.secondary.withValues(alpha: 0.4),
          ),
        ],
      ),
      onTap: isBusy
          ? null
          : () => _showFriendAddSettingSheet(
              context: context,
              userSettingsService: userSettingsService,
            ),
    );
  }

  Widget _buildAllowGroupInviteTile({
    required BuildContext context,
    required UserSettingsService userSettingsService,
  }) {
    final theme = Theme.of(context);
    final currentValue = userSettingsService.allowGroupInvite.value;
    final isBusy =
        userSettingsService.isLoading.value ||
        userSettingsService.isSaving.value;

    return ListTile(
      leading: Container(
        width: 36,
        height: 36,
        decoration: BoxDecoration(
          color: AppTheme.infoColor.withValues(alpha: 0.12),
          borderRadius: BorderRadius.circular(10),
        ),
        child: const Icon(
          Icons.group_add_rounded,
          color: AppTheme.infoColor,
          size: 20,
        ),
      ),
      title: Text('settings_chat_allow_group_invite'.tr),
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (isBusy)
            SizedBox(
              width: 14,
              height: 14,
              child: CircularProgressIndicator(
                strokeWidth: 2,
                color: theme.primaryColor,
              ),
            ),
          if (isBusy) const SizedBox(width: 8),
          Text(
            _allowGroupInviteLabel(currentValue),
            style: TextStyle(fontSize: 13, color: theme.colorScheme.secondary),
          ),
          Icon(
            Icons.chevron_right_rounded,
            color: theme.colorScheme.secondary.withValues(alpha: 0.4),
          ),
        ],
      ),
      onTap: isBusy
          ? null
          : () => _showAllowGroupInviteSheet(
              context: context,
              userSettingsService: userSettingsService,
            ),
    );
  }

  String _friendAddSettingLabel(int setting) {
    switch (setting) {
      case UserSettingsService.friendAddSettingAutoApprove:
        return 'settings_chat_friend_add_auto_approve'.tr;
      case UserSettingsService.friendAddSettingForbidden:
        return 'settings_chat_friend_add_forbidden'.tr;
      case UserSettingsService.friendAddSettingNeedApproval:
      default:
        return 'settings_chat_friend_add_need_approval'.tr;
    }
  }

  String _allowGroupInviteLabel(bool allowGroupInvite) {
    return allowGroupInvite
        ? 'settings_chat_allow_group_invite_allow'.tr
        : 'settings_chat_allow_group_invite_reject'.tr;
  }

  Future<void> _showFriendAddSettingSheet({
    required BuildContext context,
    required UserSettingsService userSettingsService,
  }) async {
    await showModalBottomSheet<void>(
      context: context,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (sheetContext) {
        return SafeArea(
          child: Obx(() {
            final currentSetting = userSettingsService.friendAddSetting.value;
            final isBusy =
                userSettingsService.isLoading.value ||
                userSettingsService.isSaving.value;
            final activeColor = Theme.of(sheetContext).primaryColor;

            return Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const SizedBox(height: 8),
                Container(
                  width: 40,
                  height: 4,
                  decoration: BoxDecoration(
                    color: Theme.of(
                      sheetContext,
                    ).colorScheme.outline.withValues(alpha: 0.25),
                    borderRadius: BorderRadius.circular(2),
                  ),
                ),
                const SizedBox(height: 8),
                ListTile(
                  title: Text('settings_chat_friend_add_need_approval'.tr),
                  trailing:
                      currentSetting ==
                          UserSettingsService.friendAddSettingNeedApproval
                      ? Icon(Icons.check_rounded, color: activeColor)
                      : null,
                  onTap: isBusy
                      ? null
                      : () async {
                          final applied = await _onFriendAddSettingSelected(
                            userSettingsService: userSettingsService,
                            currentSetting: currentSetting,
                            nextSetting: UserSettingsService
                                .friendAddSettingNeedApproval,
                          );
                          if (applied && (Get.isBottomSheetOpen ?? false)) {
                            Get.back();
                          }
                        },
                ),
                ListTile(
                  title: Text('settings_chat_friend_add_auto_approve'.tr),
                  trailing:
                      currentSetting ==
                          UserSettingsService.friendAddSettingAutoApprove
                      ? Icon(Icons.check_rounded, color: activeColor)
                      : null,
                  onTap: isBusy
                      ? null
                      : () async {
                          final applied = await _onFriendAddSettingSelected(
                            userSettingsService: userSettingsService,
                            currentSetting: currentSetting,
                            nextSetting:
                                UserSettingsService.friendAddSettingAutoApprove,
                          );
                          if (applied && (Get.isBottomSheetOpen ?? false)) {
                            Get.back();
                          }
                        },
                ),
                ListTile(
                  title: Text('settings_chat_friend_add_forbidden'.tr),
                  trailing:
                      currentSetting ==
                          UserSettingsService.friendAddSettingForbidden
                      ? Icon(Icons.check_rounded, color: activeColor)
                      : null,
                  onTap: isBusy
                      ? null
                      : () async {
                          final applied = await _onFriendAddSettingSelected(
                            userSettingsService: userSettingsService,
                            currentSetting: currentSetting,
                            nextSetting:
                                UserSettingsService.friendAddSettingForbidden,
                          );
                          if (applied && (Get.isBottomSheetOpen ?? false)) {
                            Get.back();
                          }
                        },
                ),
                const SizedBox(height: 8),
              ],
            );
          }),
        );
      },
    );
  }

  Future<bool> _onFriendAddSettingSelected({
    required UserSettingsService userSettingsService,
    required int currentSetting,
    required int nextSetting,
  }) async {
    if (currentSetting == nextSetting) {
      return true;
    }
    final ok = await userSettingsService.updateFriendAddSetting(nextSetting);
    if (!ok) {
      CustomToast.show('settings_chat_friend_add_save_failed'.tr);
      return false;
    }
    return true;
  }

  Future<void> _showAllowGroupInviteSheet({
    required BuildContext context,
    required UserSettingsService userSettingsService,
  }) async {
    await showModalBottomSheet<void>(
      context: context,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (sheetContext) {
        return SafeArea(
          child: Obx(() {
            final currentValue = userSettingsService.allowGroupInvite.value;
            final isBusy =
                userSettingsService.isLoading.value ||
                userSettingsService.isSaving.value;
            final activeColor = Theme.of(sheetContext).primaryColor;

            return Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const SizedBox(height: 8),
                Container(
                  width: 40,
                  height: 4,
                  decoration: BoxDecoration(
                    color: Theme.of(
                      sheetContext,
                    ).colorScheme.outline.withValues(alpha: 0.25),
                    borderRadius: BorderRadius.circular(2),
                  ),
                ),
                const SizedBox(height: 8),
                ListTile(
                  title: Text('settings_chat_allow_group_invite_allow'.tr),
                  trailing: currentValue
                      ? Icon(Icons.check_rounded, color: activeColor)
                      : null,
                  onTap: isBusy
                      ? null
                      : () async {
                          final applied = await _onAllowGroupInviteSelected(
                            userSettingsService: userSettingsService,
                            currentValue: currentValue,
                            nextValue: true,
                          );
                          if (applied && (Get.isBottomSheetOpen ?? false)) {
                            Get.back();
                          }
                        },
                ),
                ListTile(
                  title: Text('settings_chat_allow_group_invite_reject'.tr),
                  trailing: !currentValue
                      ? Icon(Icons.check_rounded, color: activeColor)
                      : null,
                  onTap: isBusy
                      ? null
                      : () async {
                          final applied = await _onAllowGroupInviteSelected(
                            userSettingsService: userSettingsService,
                            currentValue: currentValue,
                            nextValue: false,
                          );
                          if (applied && (Get.isBottomSheetOpen ?? false)) {
                            Get.back();
                          }
                        },
                ),
                const SizedBox(height: 8),
              ],
            );
          }),
        );
      },
    );
  }

  Future<bool> _onAllowGroupInviteSelected({
    required UserSettingsService userSettingsService,
    required bool currentValue,
    required bool nextValue,
  }) async {
    if (currentValue == nextValue) {
      return true;
    }
    final ok = await userSettingsService.updateAllowGroupInvite(nextValue);
    if (!ok) {
      CustomToast.show('settings_chat_allow_group_invite_save_failed'.tr);
      return false;
    }
    return true;
  }

  Future<void> _showDefaultAgentSheet({
    required BuildContext context,
    required UserSettingsService userSettingsService,
    required List<AgentModel> selectableAgents,
  }) async {
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (sheetContext) {
        return SafeArea(
          child: Obx(() {
            final currentValue = userSettingsService.autoDelegateAgentId.value
                .trim();
            final selectedValue =
                selectableAgents.any((a) => a.id == currentValue)
                ? currentValue
                : '';
            final isBusy =
                userSettingsService.isLoading.value ||
                userSettingsService.isSaving.value;
            final activeColor = Theme.of(sheetContext).primaryColor;

            return ConstrainedBox(
              constraints: BoxConstraints(
                maxHeight: MediaQuery.of(sheetContext).size.height * 0.6,
              ),
              child: SingleChildScrollView(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                const SizedBox(height: 8),
                Container(
                  width: 40,
                  height: 4,
                  decoration: BoxDecoration(
                    color: Theme.of(
                      sheetContext,
                    ).colorScheme.outline.withValues(alpha: 0.25),
                    borderRadius: BorderRadius.circular(2),
                  ),
                ),
                const SizedBox(height: 8),
                ListTile(
                  title: Text('settings_chat_default_agent_none'.tr),
                  trailing: selectedValue.isEmpty
                      ? Icon(Icons.check_rounded, color: activeColor)
                      : null,
                  onTap: isBusy
                      ? null
                      : () async {
                          final applied = await _onDefaultAgentSelected(
                            context: sheetContext,
                            userSettingsService: userSettingsService,
                            currentAgentID: selectedValue,
                            nextAgentID: '',
                          );
                          if (applied && (Get.isBottomSheetOpen ?? false)) {
                            Get.back();
                          }
                        },
                ),
                ...selectableAgents.map(
                  (agent) => ListTile(
                    title: Text(
                      agent.agentName,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                    trailing: selectedValue == agent.id
                        ? Icon(Icons.check_rounded, color: activeColor)
                        : null,
                    onTap: isBusy
                        ? null
                        : () async {
                            final applied = await _onDefaultAgentSelected(
                              context: sheetContext,
                              userSettingsService: userSettingsService,
                              currentAgentID: selectedValue,
                              nextAgentID: agent.id,
                            );
                            if (applied && (Get.isBottomSheetOpen ?? false)) {
                              Get.back();
                            }
                          },
                  ),
                ),
                const SizedBox(height: 8),
                ],
              ),
              ),
            );
          }),
        );
      },
    );
  }

  Future<bool> _onDefaultAgentSelected({
    required BuildContext context,
    required UserSettingsService userSettingsService,
    required String currentAgentID,
    required String nextAgentID,
  }) async {
    final currentID = currentAgentID.trim();
    final nextID = nextAgentID.trim();
    if (currentID == nextID) {
      return true;
    }

    if (nextID.isNotEmpty) {
      final confirmed = await _confirmDefaultAgentSelection(context);
      if (!confirmed) {
        return false;
      }
    }

    final ok = await userSettingsService.updateAutoDelegateAgentId(
      nextID.isEmpty ? null : nextID,
    );
    if (!ok) {
      CustomToast.show('settings_chat_default_agent_save_failed'.tr);
      return false;
    }
    return true;
  }

  Future<bool> _confirmDefaultAgentSelection(BuildContext context) {
    return showAppConfirmDialog(
      context: context,
      title: 'common_confirm'.tr,
      message: 'settings_chat_default_agent_confirm_message'.tr,
    );
  }

  Widget _buildVoiceDefaultAgentTile({
    required BuildContext context,
    required AgentService agentService,
    required UserSettingsService userSettingsService,
  }) {
    final theme = Theme.of(context);
    final selectableAgents = agentService.allAccessibleAgents
        .where((agent) => agent.status == 1 && agent.providerType == 4)
        .toList();
    final currentValue = userSettingsService.voiceAutoDelegateAgentId.value.trim();
    final selectedValue = selectableAgents.any((a) => a.id == currentValue)
        ? currentValue
        : '';
    final isBusy =
        userSettingsService.isLoading.value || userSettingsService.isSaving.value;
    String selectedLabel = 'settings_chat_voice_default_agent_none'.tr;
    for (final agent in selectableAgents) {
      if (agent.id == selectedValue) {
        selectedLabel = agent.agentName;
        break;
      }
    }
    return ListTile(
      leading: Container(
        width: 36,
        height: 36,
        decoration: BoxDecoration(
          color: AppTheme.infoColor.withValues(alpha: 0.12),
          borderRadius: BorderRadius.circular(10),
        ),
        child: const Icon(Icons.support_agent_rounded,
            color: AppTheme.infoColor, size: 20),
      ),
      title: Text('settings_chat_voice_default_agent'.tr),
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (isBusy)
            SizedBox(
              width: 14,
              height: 14,
              child: CircularProgressIndicator(
                  strokeWidth: 2, color: theme.primaryColor),
            ),
          if (isBusy) const SizedBox(width: 8),
          ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 150),
            child: Text(
              selectedLabel,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(fontSize: 13, color: theme.colorScheme.secondary),
              textAlign: TextAlign.right,
            ),
          ),
          Icon(Icons.chevron_right_rounded,
              color: theme.colorScheme.secondary.withValues(alpha: 0.4)),
        ],
      ),
      onTap: isBusy
          ? null
          : () => _showVoiceDefaultAgentSheet(
                context: context,
                userSettingsService: userSettingsService,
                selectableAgents: selectableAgents,
              ),
    );
  }

  Future<void> _showVoiceDefaultAgentSheet({
    required BuildContext context,
    required UserSettingsService userSettingsService,
    required List<AgentModel> selectableAgents,
  }) async {
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (sheetContext) {
        return SafeArea(
          child: Obx(() {
            final currentValue =
                userSettingsService.voiceAutoDelegateAgentId.value.trim();
            final selectedValue =
                selectableAgents.any((a) => a.id == currentValue)
                    ? currentValue
                    : '';
            final isBusy = userSettingsService.isLoading.value ||
                userSettingsService.isSaving.value;
            final activeColor = Theme.of(sheetContext).primaryColor;
            return ConstrainedBox(
              constraints: BoxConstraints(
                maxHeight: MediaQuery.of(sheetContext).size.height * 0.6,
              ),
              child: SingleChildScrollView(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                  const SizedBox(height: 8),
                  Container(
                    width: 40,
                    height: 4,
                    decoration: BoxDecoration(
                      color: Theme.of(sheetContext)
                          .colorScheme
                          .outline
                          .withValues(alpha: 0.25),
                      borderRadius: BorderRadius.circular(2),
                    ),
                  ),
                  const SizedBox(height: 8),
                  if (selectableAgents.isEmpty)
                    Padding(
                      padding: const EdgeInsets.all(16),
                      child: Text('settings_chat_voice_default_agent_empty'.tr,
                          style: TextStyle(color: Theme.of(sheetContext).colorScheme.secondary)),
                    ),
                  ListTile(
                    title: Text('settings_chat_voice_default_agent_none'.tr),
                    trailing: selectedValue.isEmpty
                        ? Icon(Icons.check_rounded, color: activeColor)
                        : null,
                    onTap: isBusy
                        ? null
                        : () async {
                            final ok = await userSettingsService
                                .updateVoiceAutoDelegateAgentId(null);
                            if (!ok) {
                              CustomToast.show(
                                  'settings_chat_voice_default_agent_save_failed'.tr);
                            } else if (Get.isBottomSheetOpen ?? false) {
                              Get.back();
                            }
                          },
                  ),
                  ...selectableAgents.map(
                    (agent) => ListTile(
                      title: Text(agent.agentName,
                          maxLines: 1, overflow: TextOverflow.ellipsis),
                      trailing: selectedValue == agent.id
                          ? Icon(Icons.check_rounded, color: activeColor)
                          : null,
                      onTap: isBusy
                          ? null
                          : () async {
                              final ok = await userSettingsService
                                  .updateVoiceAutoDelegateAgentId(agent.id);
                              if (!ok) {
                                CustomToast.show(
                                    'settings_chat_voice_default_agent_save_failed'.tr);
                              } else if (Get.isBottomSheetOpen ?? false) {
                                Get.back();
                              }
                            },
                    ),
                  ),
                  const SizedBox(height: 8),
                ],
              ),
              ),
            );
          }),
        );
      },
    );
  }

  // 语音大脑（owner 主动呼出的语音通道）：选一个 type=4 语音大模型。
  // 与"语音托管"(访客来电自动接单)各存各的，互不影响。
  Widget _buildVoiceBrainAgentTile({
    required BuildContext context,
    required AgentService agentService,
    required UserSettingsService userSettingsService,
  }) {
    final theme = Theme.of(context);
    final selectableAgents = agentService.allAccessibleAgents
        .where((agent) => agent.status == 1 && agent.providerType == 4)
        .toList();
    final currentValue = userSettingsService.voiceBrainAgentId.value.trim();
    final selectedValue = selectableAgents.any((a) => a.id == currentValue)
        ? currentValue
        : '';
    final isBusy =
        userSettingsService.isLoading.value || userSettingsService.isSaving.value;
    String selectedLabel = 'settings_chat_voice_brain_none'.tr;
    for (final agent in selectableAgents) {
      if (agent.id == selectedValue) {
        selectedLabel = agent.agentName;
        break;
      }
    }
    return ListTile(
      leading: Container(
        width: 36,
        height: 36,
        decoration: BoxDecoration(
          color: AppTheme.primaryColor.withValues(alpha: 0.12),
          borderRadius: BorderRadius.circular(10),
        ),
        child: const Icon(Icons.record_voice_over_rounded,
            color: AppTheme.primaryColor, size: 20),
      ),
      title: Text('settings_chat_voice_brain'.tr),
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (isBusy)
            SizedBox(
              width: 14,
              height: 14,
              child: CircularProgressIndicator(
                  strokeWidth: 2, color: theme.primaryColor),
            ),
          if (isBusy) const SizedBox(width: 8),
          ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 150),
            child: Text(
              selectedLabel,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(fontSize: 13, color: theme.colorScheme.secondary),
              textAlign: TextAlign.right,
            ),
          ),
          Icon(Icons.chevron_right_rounded,
              color: theme.colorScheme.secondary.withValues(alpha: 0.4)),
        ],
      ),
      onTap: isBusy
          ? null
          : () => _showVoiceBrainAgentSheet(
                context: context,
                userSettingsService: userSettingsService,
                selectableAgents: selectableAgents,
              ),
    );
  }

  // 语音大脑工作模式开关：true=豆包实时互动，false=STT+TTS 念稿兜底。
  Widget _buildVoiceBrainRealtimeTile({
    required BuildContext context,
    required UserSettingsService userSettingsService,
  }) {
    final theme = Theme.of(context);
    final isBusy =
        userSettingsService.isLoading.value || userSettingsService.isSaving.value;
    final currentValue = userSettingsService.voiceBrainRealtime.value;

    return ListTile(
      leading: Container(
        width: 36,
        height: 36,
        decoration: BoxDecoration(
          color: AppTheme.primaryColor.withValues(alpha: 0.12),
          borderRadius: BorderRadius.circular(10),
        ),
        child: const Icon(Icons.sensors_rounded,
            color: AppTheme.primaryColor, size: 20),
      ),
      title: Text('settings_chat_voice_brain_realtime'.tr),
      subtitle: Text(
        currentValue
            ? 'settings_chat_voice_brain_realtime_on_hint'.tr
            : 'settings_chat_voice_brain_realtime_off_hint'.tr,
        style: TextStyle(fontSize: 12, color: theme.colorScheme.secondary),
      ),
      trailing: Switch(
        value: currentValue,
        onChanged: isBusy
            ? null
            : (value) async {
                final ok =
                    await userSettingsService.updateVoiceBrainRealtime(value);
                if (!ok) {
                  CustomToast.show(
                      'settings_chat_voice_brain_realtime_save_failed'.tr);
                }
              },
      ),
    );
  }

  Future<void> _showVoiceBrainAgentSheet({
    required BuildContext context,
    required UserSettingsService userSettingsService,
    required List<AgentModel> selectableAgents,
  }) async {
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (sheetContext) {
        return SafeArea(
          child: Obx(() {
            final currentValue =
                userSettingsService.voiceBrainAgentId.value.trim();
            final selectedValue =
                selectableAgents.any((a) => a.id == currentValue)
                    ? currentValue
                    : '';
            final isBusy = userSettingsService.isLoading.value ||
                userSettingsService.isSaving.value;
            final activeColor = Theme.of(sheetContext).primaryColor;
            return ConstrainedBox(
              constraints: BoxConstraints(
                maxHeight: MediaQuery.of(sheetContext).size.height * 0.6,
              ),
              child: SingleChildScrollView(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                  const SizedBox(height: 8),
                  Container(
                    width: 40,
                    height: 4,
                    decoration: BoxDecoration(
                      color: Theme.of(sheetContext)
                          .colorScheme
                          .outline
                          .withValues(alpha: 0.25),
                      borderRadius: BorderRadius.circular(2),
                    ),
                  ),
                  const SizedBox(height: 8),
                  if (selectableAgents.isEmpty)
                    Padding(
                      padding: const EdgeInsets.all(16),
                      child: Text('settings_chat_voice_brain_empty'.tr,
                          style: TextStyle(color: Theme.of(sheetContext).colorScheme.secondary)),
                    ),
                  ListTile(
                    title: Text('settings_chat_voice_brain_none'.tr),
                    trailing: selectedValue.isEmpty
                        ? Icon(Icons.check_rounded, color: activeColor)
                        : null,
                    onTap: isBusy
                        ? null
                        : () async {
                            final ok = await userSettingsService
                                .updateVoiceBrainAgentId(null);
                            if (!ok) {
                              CustomToast.show(
                                  'settings_chat_voice_brain_save_failed'.tr);
                            } else if (Get.isBottomSheetOpen ?? false) {
                              Get.back();
                            }
                          },
                  ),
                  ...selectableAgents.map(
                    (agent) => ListTile(
                      title: Text(agent.agentName,
                          maxLines: 1, overflow: TextOverflow.ellipsis),
                      trailing: selectedValue == agent.id
                          ? Icon(Icons.check_rounded, color: activeColor)
                          : null,
                      onTap: isBusy
                          ? null
                          : () async {
                              final ok = await userSettingsService
                                  .updateVoiceBrainAgentId(agent.id);
                              if (!ok) {
                                CustomToast.show(
                                    'settings_chat_voice_brain_save_failed'.tr);
                              } else if (Get.isBottomSheetOpen ?? false) {
                                Get.back();
                              }
                            },
                    ),
                  ),
                  const SizedBox(height: 8),
                ],
              ),
              ),
            );
          }),
        );
      },
    );
  }
}
