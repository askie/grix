import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:permission_handler/permission_handler.dart';

import '../../app/routes/app_routes.dart';
import '../../shared/utils/toast_util.dart';

class NotificationSettingsView extends StatefulWidget {
  const NotificationSettingsView({super.key});

  @override
  State<NotificationSettingsView> createState() =>
      _NotificationSettingsViewState();
}

class _NotificationSettingsViewState extends State<NotificationSettingsView>
    with WidgetsBindingObserver {
  PermissionStatus _status = PermissionStatus.denied;
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _refreshStatus();
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      _refreshStatus();
    }
  }

  Future<void> _refreshStatus() async {
    setState(() => _loading = true);
    PermissionStatus status;
    try {
      status = await Permission.notification.status;
    } catch (_) {
      status = PermissionStatus.denied;
    }
    if (!mounted) return;
    setState(() {
      _status = status;
      _loading = false;
    });
  }

  Future<void> _requestPermission() async {
    PermissionStatus result;
    try {
      result = await Permission.notification.request();
    } catch (_) {
      CustomToast.show('settings_notification_open_settings_failed'.tr);
      return;
    }
    if (!mounted) return;
    setState(() => _status = result);
  }

  Future<void> _openSystemSettings() async {
    final opened = await openAppSettings();
    if (!opened) {
      CustomToast.show('settings_notification_open_settings_failed'.tr);
    }
  }

  String _statusText() {
    if (_status.isGranted) return 'settings_notification_status_granted'.tr;
    if (_status.isPermanentlyDenied) {
      return 'settings_notification_status_blocked'.tr;
    }
    if (_status.isDenied) return 'settings_notification_status_denied'.tr;
    if (_status.isRestricted) {
      return 'settings_notification_status_restricted'.tr;
    }
    if (_status.isLimited) return 'settings_notification_status_limited'.tr;
    if (_status.isProvisional) {
      return 'settings_notification_status_provisional'.tr;
    }
    return 'common_unknown_error'.tr;
  }

  Color _statusColor() {
    if (_status.isGranted) return const Color(0xFF1E8E3E);
    if (_status.isProvisional || _status.isLimited) {
      return const Color(0xFFB26A00);
    }
    return const Color(0xFFB3261E);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      appBar: AppBar(
        leading: IconButton(
          icon: const Icon(Icons.arrow_back_ios_rounded, size: 20),
          onPressed: () => Get.back(),
        ),
        title: Text('me_notification'.tr),
      ),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : ListView(
              padding: const EdgeInsets.all(16),
              children: [
                _InfoCard(
                  title: 'settings_notification_status'.tr,
                  subtitle: _statusText(),
                  subtitleColor: _statusColor(),
                  icon: Icons.notifications_active_outlined,
                ),
                const SizedBox(height: 12),
                _ActionCard(
                  title: 'settings_notification_request'.tr,
                  subtitle: 'settings_notification_request_hint'.tr,
                  enabled: !_status.isGranted,
                  onTap: _requestPermission,
                ),
                const SizedBox(height: 12),
                _ActionCard(
                  title: 'settings_notification_open_settings'.tr,
                  subtitle: 'settings_notification_open_settings_hint'.tr,
                  onTap: _openSystemSettings,
                ),
                const SizedBox(height: 12),
                _ActionCard(
                  title: 'agent_notif_title'.tr,
                  subtitle: 'agent_notif_entry_subtitle'.tr,
                  onTap: () => Get.toNamed(AppRoutes.agentNotificationPrefs),
                ),
                const SizedBox(height: 12),
                _ActionCard(
                  title: 'me_privacy'.tr,
                  subtitle: 'settings_notification_privacy_hint'.tr,
                  onTap: () => Get.toNamed(AppRoutes.privacy),
                ),
                const SizedBox(height: 12),
                Text(
                  'settings_notification_local_hint'.tr,
                  style: TextStyle(
                    fontSize: 13,
                    color: theme.colorScheme.secondary,
                  ),
                ),
              ],
            ),
    );
  }
}

class _InfoCard extends StatelessWidget {
  const _InfoCard({
    required this.title,
    required this.subtitle,
    required this.icon,
    this.subtitleColor,
  });

  final String title;
  final String subtitle;
  final IconData icon;
  final Color? subtitleColor;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Row(
        children: [
          Icon(icon, color: theme.colorScheme.primary),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  title,
                  style: const TextStyle(
                    fontWeight: FontWeight.w700,
                    fontSize: 15,
                  ),
                ),
                const SizedBox(height: 4),
                Text(
                  subtitle,
                  style: TextStyle(
                    fontSize: 13,
                    color: subtitleColor ?? theme.colorScheme.secondary,
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _ActionCard extends StatelessWidget {
  const _ActionCard({
    required this.title,
    required this.subtitle,
    required this.onTap,
    this.enabled = true,
  });

  final String title;
  final String subtitle;
  final VoidCallback onTap;
  final bool enabled;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Material(
      color: theme.colorScheme.surface,
      borderRadius: BorderRadius.circular(16),
      child: InkWell(
        borderRadius: BorderRadius.circular(16),
        onTap: enabled ? onTap : null,
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      title,
                      style: TextStyle(
                        fontWeight: FontWeight.w700,
                        fontSize: 15,
                        color: enabled
                            ? theme.colorScheme.onSurface
                            : theme.disabledColor,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      subtitle,
                      style: TextStyle(
                        fontSize: 13,
                        color: enabled
                            ? theme.colorScheme.secondary
                            : theme.disabledColor,
                      ),
                    ),
                  ],
                ),
              ),
              Icon(
                Icons.chevron_right_rounded,
                color: enabled
                    ? theme.colorScheme.secondary.withValues(alpha: 0.5)
                    : theme.disabledColor,
              ),
            ],
          ),
        ),
      ),
    );
  }
}
