import 'dart:async';

import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter/material.dart';
import 'package:get/get.dart';
import '../../data/providers/feature_flag_service.dart';
import '../system/skill_library_sheet.dart';

import '../../app/locale/locale_change_coordinator.dart';
import '../../app/locale/locale_service.dart';
import '../../app/routes/app_routes.dart';
import '../../app/settings/chat_background_service.dart';
import '../../app/settings/chat_font_size_service.dart';
import '../../app/settings/theme_preference_service.dart';
import '../../app/themes/app_theme.dart';
import '../../data/providers/agent_service.dart';
import '../../data/providers/auth_service.dart';
import '../../data/providers/user_settings_service.dart';
import '../gateway/gateway_settings_entry_tile.dart';
import '../../shared/utils/toast_util.dart';
import '../../shared/widgets/app_dialog_style.dart';

part 'settings_view_chat_appearance.dart';
part 'settings_view_chat_preferences.dart';

class SettingsView extends StatelessWidget {
  const SettingsView({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final chatFontSizeService = Get.isRegistered<ChatFontSizeService>()
        ? Get.find<ChatFontSizeService>()
        : null;
    final chatBackgroundService = Get.isRegistered<ChatBackgroundService>()
        ? Get.find<ChatBackgroundService>()
        : null;
    final themePreferenceService = Get.find<ThemePreferenceService>();
    final agentService = Get.find<AgentService>();
    final userSettingsService = Get.find<UserSettingsService>();
    chatBackgroundService?.ensureSyncedWithCurrentUser();
    unawaited(
      _ensureChatSettingsLoaded(
        agentService: agentService,
        userSettingsService: userSettingsService,
        forceRefreshAgents: true,
      ),
    );

    return Scaffold(
      appBar: AppBar(
        leading: IconButton(
          icon: const Icon(Icons.arrow_back_ios_rounded, size: 20),
          onPressed: () => Get.back(),
        ),
        title: Text(
          'me_account_settings'.tr,
          style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w600),
        ),
      ),
      body: ListView(
        children: [
          const SizedBox(height: 12),

          _buildSectionHeader(context, 'settings_appearance'.tr),
          Obx(
            () => _buildSection(context, [
              ListTile(
                leading: Container(
                  width: 36,
                  height: 36,
                  decoration: BoxDecoration(
                    color: AppTheme.warningColor.withValues(alpha: 0.12),
                    borderRadius: BorderRadius.circular(10),
                  ),
                  child: const Icon(
                    Icons.brightness_6_outlined,
                    color: AppTheme.warningColor,
                    size: 20,
                  ),
                ),
                title: Text('settings_theme_dark'.tr),
                trailing: Switch.adaptive(
                  value: themePreferenceService.isDarkMode,
                  onChanged: (enabled) {
                    unawaited(
                      themePreferenceService.setDarkModeEnabled(enabled),
                    );
                  },
                  activeThumbColor: theme.primaryColor,
                  activeTrackColor: theme.primaryColor.withValues(alpha: 0.5),
                ),
                onTap: () {
                  unawaited(themePreferenceService.toggle());
                },
              ),
              ListTile(
                leading: Container(
                  width: 36,
                  height: 36,
                  decoration: BoxDecoration(
                    color: AppTheme.primaryColor.withValues(alpha: 0.12),
                    borderRadius: BorderRadius.circular(10),
                  ),
                  child: const Icon(
                    Icons.language_rounded,
                    color: AppTheme.primaryColor,
                    size: 20,
                  ),
                ),
                title: Text('settings_language'.tr),
                trailing: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(
                      LocaleService.currentNativeLabel(Get.locale),
                      style: TextStyle(
                        fontSize: 13,
                        color: theme.colorScheme.secondary,
                      ),
                    ),
                    const SizedBox(width: 4),
                    const Icon(Icons.chevron_right_rounded),
                  ],
                ),
                onTap: () => _showLanguagePicker(context),
              ),
            ]),
          ),

          _buildSectionHeader(context, 'settings_general'.tr),
          _buildSection(context, [
            _buildPhoneBindTile(context),
            Divider(
              indent: 56,
              color: theme.colorScheme.outline.withValues(alpha: 0.15),
            ),
            // 多账号切换入口仅移动端展示：桌面端走"账号实例"多窗口方案。
            if (!kIsWeb && GetPlatform.isMobile) ...[
              ListTile(
                leading: Container(
                  width: 36,
                  height: 36,
                  decoration: BoxDecoration(
                    color: AppTheme.primaryColor.withValues(alpha: 0.12),
                    borderRadius: BorderRadius.circular(10),
                  ),
                  child: const Icon(
                    Icons.switch_account_rounded,
                    color: AppTheme.primaryColor,
                    size: 20,
                  ),
                ),
                title: Text('account_switch_title'.tr),
                subtitle: Text('account_switch_entry_subtitle'.tr),
                trailing: const Icon(Icons.chevron_right_rounded),
                onTap: () => Get.toNamed(AppRoutes.accountSwitch),
              ),
              Divider(
                indent: 56,
                color: theme.colorScheme.outline.withValues(alpha: 0.15),
              ),
            ],
            ListTile(
              leading: Container(
                width: 36,
                height: 36,
                decoration: BoxDecoration(
                  color: AppTheme.successColor.withValues(alpha: 0.12),
                  borderRadius: BorderRadius.circular(10),
                ),
                child: const Icon(
                  Icons.devices_rounded,
                  color: AppTheme.successColor,
                  size: 20,
                ),
              ),
              title: Text('device_management_title'.tr),
              subtitle: Text('device_management_subtitle'.tr),
              trailing: const Icon(Icons.chevron_right_rounded),
              onTap: () => Get.toNamed(AppRoutes.deviceManagement),
            ),
            Divider(
              indent: 56,
              color: theme.colorScheme.outline.withValues(alpha: 0.15),
            ),
            // 「模型设置」入口（M4）：默认模型 + Agent 模型设置，无平台门控。
            const GatewaySettingsEntryTile(),
          ]),

          _buildSectionHeader(context, 'settings_external_integrations'.tr),
          _buildSection(context, [
            ListTile(
              leading: Container(
                width: 36,
                height: 36,
                decoration: BoxDecoration(
                  color: AppTheme.primaryDark.withValues(alpha: 0.12),
                  borderRadius: BorderRadius.circular(10),
                ),
                child: const Icon(
                  Icons.language_rounded,
                  color: AppTheme.primaryDark,
                  size: 20,
                ),
              ),
              title: Text('settings_widget_sites'.tr),
              trailing: const Icon(Icons.chevron_right_rounded),
              onTap: () => Get.toNamed(AppRoutes.widgetSites),
            ),
            Divider(
              indent: 56,
              color: theme.colorScheme.outline.withValues(alpha: 0.15),
            ),
            ListTile(
              leading: Container(
                width: 36,
                height: 36,
                decoration: BoxDecoration(
                  color: AppTheme.infoColor.withValues(alpha: 0.12),
                  borderRadius: BorderRadius.circular(10),
                ),
                child: const Icon(
                  Icons.hub_outlined,
                  color: AppTheme.infoColor,
                  size: 20,
                ),
              ),
              title: Text('settings_webhook_integrations'.tr),
              trailing: const Icon(Icons.chevron_right_rounded),
              onTap: () => Get.toNamed(AppRoutes.webhookIntegrations),
            ),
            Divider(
              indent: 56,
              color: theme.colorScheme.outline.withValues(alpha: 0.15),
            ),
            ListTile(
              leading: Container(
                width: 36,
                height: 36,
                decoration: BoxDecoration(
                  color: AppTheme.primaryDark.withValues(alpha: 0.12),
                  borderRadius: BorderRadius.circular(10),
                ),
                child: const Icon(
                  Icons.extension_outlined,
                  color: AppTheme.primaryDark,
                  size: 20,
                ),
              ),
              title: Text('settings_skill_library'.tr),
              subtitle: Text('skill_library_subtitle'.tr),
              trailing: const Icon(Icons.chevron_right_rounded),
              onTap: () => SkillLibrarySheet.show(context),
            ),
          ]),

          // 聊天设置
          _buildSectionHeader(context, 'settings_chat'.tr),
          Obx(
            () => _buildSection(context, [
              _buildDefaultAgentTile(
                context: context,
                agentService: agentService,
                userSettingsService: userSettingsService,
              ),
              if (Get.find<FeatureFlagService>().isEnabled('voice_delegate')) ...[
                Divider(
                  indent: 56,
                  color: theme.colorScheme.outline.withValues(alpha: 0.15),
                ),
                _buildVoiceDefaultAgentTile(
                  context: context,
                  agentService: agentService,
                  userSettingsService: userSettingsService,
                ),
              ],
              if (Get.find<FeatureFlagService>().isEnabled('voice_brain')) ...[
                Divider(
                  indent: 56,
                  color: theme.colorScheme.outline.withValues(alpha: 0.15),
                ),
                _buildVoiceBrainAgentTile(
                  context: context,
                  agentService: agentService,
                  userSettingsService: userSettingsService,
                ),
                // 仅在已选语音大脑时才显示模式开关，没选就没意义
                if (userSettingsService.voiceBrainAgentId.value.trim().isNotEmpty) ...[
                  Divider(
                    indent: 56,
                    color: theme.colorScheme.outline.withValues(alpha: 0.15),
                  ),
                  _buildVoiceBrainRealtimeTile(
                    context: context,
                    userSettingsService: userSettingsService,
                  ),
                ],
              ],
              Divider(
                indent: 56,
                color: theme.colorScheme.outline.withValues(alpha: 0.15),
              ),
              _buildFriendAddSettingTile(
                context: context,
                userSettingsService: userSettingsService,
              ),
              Divider(
                indent: 56,
                color: theme.colorScheme.outline.withValues(alpha: 0.15),
              ),
              _buildAllowGroupInviteTile(
                context: context,
                userSettingsService: userSettingsService,
              ),
              Divider(
                indent: 56,
                color: theme.colorScheme.outline.withValues(alpha: 0.15),
              ),
              _buildFontSizeTile(
                context: context,
                service: chatFontSizeService,
              ),
              Divider(
                indent: 56,
                color: theme.colorScheme.outline.withValues(alpha: 0.15),
              ),
              _buildChatBackgroundTile(
                context: context,
                service: chatBackgroundService,
              ),
            ]),
          ),

          const SizedBox(height: 16),
        ],
      ),
    );
  }

  Future<void> _ensureChatSettingsLoaded({
    required AgentService agentService,
    required UserSettingsService userSettingsService,
    bool forceRefreshAgents = false,
  }) async {
    await userSettingsService.ensureSyncedWithCurrentUser();
    if (forceRefreshAgents || !agentService.hasLoaded.value) {
      await agentService.loadAgents();
    }
  }

  Widget _buildPhoneBindTile(BuildContext context) {
    final authService = Get.find<AuthService>();
    return Obx(() {
      final user = authService.user;
      final phone = user?.phoneE164 ?? '';
      final bound = phone.isNotEmpty;
      return ListTile(
        leading: Container(
          width: 36,
          height: 36,
          decoration: BoxDecoration(
            color: AppTheme.primaryColor.withValues(alpha: 0.12),
            borderRadius: BorderRadius.circular(10),
          ),
          child: const Icon(
            Icons.phone_iphone_rounded,
            color: AppTheme.primaryColor,
            size: 20,
          ),
        ),
        title: Text(bound ? 'phone_bind_already_title'.tr : 'phone_bind_not_yet_title'.tr),
        subtitle: Text(
          bound ? phone : 'phone_bind_not_yet_subtitle'.tr,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
        ),
        trailing: const Icon(Icons.chevron_right_rounded),
        onTap: () =>
            Get.toNamed(AppRoutes.phoneLogin, arguments: {'mode': 'bind'}),
      );
    });
  }

  Widget _buildSectionHeader(BuildContext context, String title) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      child: Text(
        title,
        style: TextStyle(
          fontSize: 12,
          fontWeight: FontWeight.w600,
          color: theme.colorScheme.secondary,
          letterSpacing: 0.5,
        ),
      ),
    );
  }

  Widget _buildSection(BuildContext context, List<Widget> children) {
    final theme = Theme.of(context);
    return Container(
      margin: const EdgeInsets.symmetric(horizontal: 12),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(children: children),
    );
  }

  Future<void> _showLanguagePicker(BuildContext context) async {
    final current = Get.locale;
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (ctx) {
        return SafeArea(
          child: ConstrainedBox(
            constraints: BoxConstraints(
              maxHeight: MediaQuery.of(ctx).size.height * 0.7,
            ),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const SizedBox(height: 12),
                Container(
                  width: 36,
                  height: 4,
                  decoration: BoxDecoration(
                    color: Theme.of(
                      ctx,
                    ).colorScheme.outline.withValues(alpha: 0.3),
                    borderRadius: BorderRadius.circular(2),
                  ),
                ),
                const SizedBox(height: 12),
                Flexible(
                  child: ListView(
                    shrinkWrap: true,
                    padding: const EdgeInsets.only(bottom: 8),
                    children: LocaleService.supportedLocales.map((entry) {
                      final isSelected =
                          current?.languageCode == entry.locale.languageCode &&
                          (entry.locale.countryCode == null ||
                              current?.countryCode == entry.locale.countryCode);
                      return ListTile(
                        title: Text(entry.nativeLabel),
                        subtitle: Text(entry.label),
                        trailing: isSelected
                            ? Icon(
                                Icons.check_rounded,
                                color: Theme.of(ctx).primaryColor,
                              )
                            : null,
                        onTap: () async {
                          Navigator.of(ctx).pop();
                          final ok = await LocaleChangeCoordinator.changeLocale(
                            entry.locale,
                          );
                          if (!ok) {
                            CustomToast.show('common_error'.tr);
                          }
                        },
                      );
                    }).toList(),
                  ),
                ),
              ],
            ),
          ),
        );
      },
    );
  }
}
