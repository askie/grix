import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/profile/instance_manager.dart';
import '../../app/profile/profile_instance_info.dart';
import '../../platform/desktop/desktop_autostart_service.dart';
import '../../platform/desktop/desktop_window_service.dart';
import 'connector_status_view.dart';
import 'gateway_relay_panel_view.dart';

/// 桌面端系统设置页面（顶部 TabBar 容器）
/// 包含：设置 tab、状态 tab、大模型配置 tab
class SystemSettingsView extends StatefulWidget {
  const SystemSettingsView({super.key});

  @override
  State<SystemSettingsView> createState() => _SystemSettingsViewState();
}

class _SystemSettingsViewState extends State<SystemSettingsView>
    with SingleTickerProviderStateMixin {
  late final TabController _tabController;
  int _tabIndex = 0;

  @override
  void initState() {
    super.initState();
    _tabController = TabController(length: 3, vsync: this)
      ..addListener(() {
        if (_tabIndex != _tabController.index) {
          setState(() => _tabIndex = _tabController.index);
        }
      });
  }

  @override
  void dispose() {
    _tabController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        automaticallyImplyLeading: false,
        title: Text(
          'system_settings_title'.tr,
          style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w600),
        ),
        bottom: TabBar(
          controller: _tabController,
          tabs: [
            Tab(text: 'system_tab_status'.tr),
            Tab(text: 'system_tab_settings'.tr),
            Tab(text: 'system_tab_providers'.tr),
          ],
        ),
      ),
      body: TabBarView(
        controller: _tabController,
        children: [
          const ConnectorStatusView(),
          const SystemSettingsTab(),
          GatewayRelayPanelView(isActive: _tabIndex == 2),
        ],
      ),
    );
  }
}

/// 系统设置内容（账号实例、窗口行为、启动设置）
class SystemSettingsTab extends StatefulWidget {
  const SystemSettingsTab({super.key});

  @override
  State<SystemSettingsTab> createState() => _SettingsTabState();
}

class _SettingsTabState extends State<SystemSettingsTab> {
  late bool _closeToTray;
  late bool _autoStart;
  List<ProfileInstanceInfo> _instances = const [];

  @override
  void initState() {
    super.initState();
    final windowService = Get.find<DesktopWindowService>();
    final autostartService = Get.find<DesktopAutostartService>();
    _closeToTray = windowService.closeToTray;
    _autoStart = autostartService.isEnabled;
    _reloadInstances();
  }

  Future<void> _reloadInstances() async {
    if (!InstanceManager.isSupported) return;
    final list = await InstanceManager.listInstances();
    if (mounted) setState(() => _instances = list);
  }

  String _instanceSubtitle(ProfileInstanceInfo info) {
    final parts = <String>[info.name];
    if (info.isCurrent) {
      parts.add('system_instance_current'.tr);
    } else if (info.running) {
      parts.add('system_instance_running'.tr);
    }
    return parts.join(' · ');
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return ListView(
      children: [
        const SizedBox(height: 12),
        if (InstanceManager.isSupported) ...[
          _buildSectionHeader(context, 'system_account_instances'.tr),
          _buildSection(context, [
            for (final info in _instances)
              ListTile(
                leading: Container(
                  width: 36,
                  height: 36,
                  decoration: BoxDecoration(
                    color: theme.colorScheme.primary.withValues(alpha: 0.12),
                    borderRadius: BorderRadius.circular(10),
                  ),
                  child: Icon(
                    info.isCurrent
                        ? Icons.person_rounded
                        : Icons.person_outline_rounded,
                    color: theme.colorScheme.primary,
                    size: 20,
                  ),
                ),
                title: Text(
                  info.nickname?.trim().isNotEmpty == true
                      ? info.nickname!.trim()
                      : 'system_instance_not_logged_in'.tr,
                ),
                subtitle: Text(_instanceSubtitle(info)),
                trailing: info.isCurrent
                    ? null
                    : const Icon(Icons.open_in_new_rounded, size: 18),
                onTap: info.isCurrent
                    ? null
                    : () async {
                        await InstanceManager.openInstance(info.name);
                        await _reloadInstances();
                      },
              ),
            ListTile(
              leading: Container(
                width: 36,
                height: 36,
                decoration: BoxDecoration(
                  color: theme.colorScheme.tertiary.withValues(alpha: 0.12),
                  borderRadius: BorderRadius.circular(10),
                ),
                child: Icon(
                  Icons.person_add_alt_1_rounded,
                  color: theme.colorScheme.tertiary,
                  size: 20,
                ),
              ),
              title: Text('system_add_account_window'.tr),
              subtitle: Text('system_add_account_window_desc'.tr),
              onTap: () async {
                await InstanceManager.launchNewInstance();
                // 给新进程一点抢锁/写端口的时间，再刷新运行状态。
                await Future.delayed(const Duration(seconds: 1));
                await _reloadInstances();
              },
            ),
          ]),
          const SizedBox(height: 16),
        ],
        _buildSectionHeader(context, 'system_window_behavior'.tr),
        _buildSection(context, [
          SwitchListTile.adaptive(
            secondary: Container(
              width: 36,
              height: 36,
              decoration: BoxDecoration(
                color: theme.colorScheme.primary.withValues(alpha: 0.12),
                borderRadius: BorderRadius.circular(10),
              ),
              child: Icon(
                Icons.minimize_rounded,
                color: theme.colorScheme.primary,
                size: 20,
              ),
            ),
            title: Text('system_close_to_tray'.tr),
            subtitle: Text('system_close_to_tray_desc'.tr),
            value: _closeToTray,
            onChanged: (value) {
              setState(() => _closeToTray = value);
              Get.find<DesktopWindowService>().closeToTray = value;
            },
          ),
        ]),
        const SizedBox(height: 16),
        _buildSectionHeader(context, 'system_startup'.tr),
        _buildSection(context, [
          SwitchListTile.adaptive(
            secondary: Container(
              width: 36,
              height: 36,
              decoration: BoxDecoration(
                color: theme.colorScheme.tertiary.withValues(alpha: 0.12),
                borderRadius: BorderRadius.circular(10),
              ),
              child: Icon(
                Icons.power_settings_new_rounded,
                color: theme.colorScheme.tertiary,
                size: 20,
              ),
            ),
            title: Text('system_auto_start'.tr),
            subtitle: Text('system_auto_start_desc'.tr),
            value: _autoStart,
            onChanged: (value) async {
              setState(() => _autoStart = value);
              await Get.find<DesktopAutostartService>().setEnabled(value);
            },
          ),
        ]),
        const SizedBox(height: 16),
      ],
    );
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
}
