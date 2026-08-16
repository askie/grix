import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:intl/intl.dart';

import '../../app/themes/app_theme.dart';
import '../../data/models/login_device_session_model.dart';
import '../../data/providers/device_management_service.dart';
import 'services/logout_flow_service.dart';
import '../../shared/utils/toast_util.dart';
import '../../shared/widgets/app_dialog_style.dart';

class DeviceManagementView extends StatefulWidget {
  const DeviceManagementView({
    super.key,
    this.service,
    this.onCurrentDeviceLogout,
  });

  final DeviceManagementService? service;
  final Future<void> Function()? onCurrentDeviceLogout;

  @override
  State<DeviceManagementView> createState() => _DeviceManagementViewState();
}

class _DeviceManagementViewState extends State<DeviceManagementView> {
  late final DeviceManagementService _service;
  List<LoginDeviceSessionModel> _sessions = const <LoginDeviceSessionModel>[];
  final Set<String> _removingSessionIds = <String>{};
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _service = widget.service ?? DeviceManagementService();
    _loadSessions();
  }

  Future<void> _loadSessions() async {
    if (mounted) {
      setState(() => _loading = true);
    }

    try {
      final items = await _service.fetchSessions();
      if (!mounted) return;
      setState(() {
        _sessions = items;
        _loading = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() => _loading = false);
      final message = error is String && error.trim().isNotEmpty
          ? error
          : 'device_management_load_failed'.tr;
      CustomToast.show(message);
    }
  }

  Future<void> _confirmRemove(LoginDeviceSessionModel session) async {
    await showAppGetDialog<void>(
      AlertDialog(
        title: Text('device_management_remove_title'.tr),
        content: Text('device_management_remove_confirm'.tr),
        actions: [
          TextButton(
            onPressed: () => Get.back(),
            child: Text('common_cancel'.tr),
          ),
          ElevatedButton(
            onPressed: () async {
              Get.back();
              await _removeSession(session);
            },
            child: Text('device_management_remove_action'.tr),
          ),
        ],
      ),
    );
  }

  Future<void> _removeSession(LoginDeviceSessionModel session) async {
    final sessionId = session.sessionId.trim();
    if (sessionId.isEmpty || _removingSessionIds.contains(sessionId)) {
      return;
    }

    setState(() => _removingSessionIds.add(sessionId));
    final result = await _service.removeSession(sessionId);
    if (!mounted) return;

    setState(() => _removingSessionIds.remove(sessionId));
    if (!result.ok) {
      CustomToast.show(result.message);
      return;
    }

    CustomToast.show('device_management_remove_success'.tr, isError: false);
    await _loadSessions();
  }

  Future<void> _confirmLogout() async {
    await (widget.onCurrentDeviceLogout ?? showLogoutConfirmDialog)();
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
        title: Text('device_management_title'.tr),
      ),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : RefreshIndicator(
              onRefresh: _loadSessions,
              child: ListView(
                padding: const EdgeInsets.all(16),
                children: [
                  if (_sessions.isEmpty) _EmptyStateCard(),
                  for (final session in _sessions) ...[
                    _DeviceSessionCard(
                      session: session,
                      removing: _removingSessionIds.contains(
                        session.sessionId.trim(),
                      ),
                      onLogout: session.current ? _confirmLogout : null,
                      onRemove: session.current
                          ? null
                          : () => _confirmRemove(session),
                    ),
                    const SizedBox(height: 12),
                  ],
                  Text(
                    'device_management_hint'.tr,
                    style: TextStyle(
                      fontSize: 13,
                      color: theme.colorScheme.secondary,
                    ),
                  ),
                ],
              ),
            ),
    );
  }
}

class _EmptyStateCard extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 16),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        children: [
          Icon(
            Icons.devices_outlined,
            size: 36,
            color: theme.colorScheme.secondary,
          ),
          const SizedBox(height: 12),
          Text(
            'device_management_empty'.tr,
            style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w700),
            textAlign: TextAlign.center,
          ),
        ],
      ),
    );
  }
}

class _DeviceSessionCard extends StatelessWidget {
  const _DeviceSessionCard({
    required this.session,
    required this.removing,
    required this.onLogout,
    required this.onRemove,
  });

  final LoginDeviceSessionModel session;
  final bool removing;
  final VoidCallback? onLogout;
  final VoidCallback? onRemove;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Container(
                width: 44,
                height: 44,
                decoration: BoxDecoration(
                  color: _statusColor(session.online).withValues(alpha: 0.12),
                  borderRadius: BorderRadius.circular(14),
                ),
                child: Icon(
                  _platformIcon(session.platform),
                  color: _statusColor(session.online),
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Wrap(
                      spacing: 8,
                      runSpacing: 8,
                      crossAxisAlignment: WrapCrossAlignment.center,
                      children: [
                        Text(
                          _platformLabel(session.platform),
                          style: const TextStyle(
                            fontSize: 15,
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                        _StatusChip(
                          label: session.online
                              ? 'device_management_status_online'.tr
                              : 'device_management_status_offline'.tr,
                          color: _statusColor(session.online),
                        ),
                        if (session.current)
                          _StatusChip(
                            label: 'device_management_current_device'.tr,
                            color: AppTheme.primaryColor,
                          ),
                        if (session.current && onLogout != null)
                          OutlinedButton(
                            onPressed: onLogout,
                            style: OutlinedButton.styleFrom(
                              foregroundColor: AppTheme.errorColor,
                              side: const BorderSide(
                                color: AppTheme.errorColor,
                              ),
                              minimumSize: const Size(0, 30),
                              padding: const EdgeInsets.symmetric(
                                horizontal: 12,
                                vertical: 6,
                              ),
                              tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                              visualDensity: VisualDensity.compact,
                            ),
                            child: Text('me_logout'.tr),
                          ),
                      ],
                    ),
                    const SizedBox(height: 10),
                    _MetaLine(
                      label: 'device_management_device_id'.tr,
                      value: session.deviceId,
                    ),
                    _MetaLine(
                      label: 'device_management_login_time'.tr,
                      value: _formatTime(session.createdAt),
                    ),
                    _MetaLine(
                      label: 'device_management_last_seen'.tr,
                      value: _formatTime(session.lastSeenAt),
                    ),
                  ],
                ),
              ),
            ],
          ),
          if (onRemove != null) ...[
            const SizedBox(height: 16),
            Align(
              alignment: Alignment.centerRight,
              child: OutlinedButton(
                onPressed: removing ? null : onRemove,
                style: OutlinedButton.styleFrom(
                  foregroundColor: AppTheme.errorColor,
                  side: const BorderSide(color: AppTheme.errorColor),
                ),
                child: removing
                    ? const SizedBox(
                        width: 16,
                        height: 16,
                        child: CircularProgressIndicator(
                          strokeWidth: 2,
                          valueColor: AlwaysStoppedAnimation<Color>(
                            AppTheme.errorColor,
                          ),
                        ),
                      )
                    : Text('device_management_remove_action'.tr),
              ),
            ),
          ],
        ],
      ),
    );
  }

  String _platformLabel(String platform) {
    switch (platform.trim().toLowerCase()) {
      case 'ios':
        return 'device_management_platform_ios'.tr;
      case 'android':
      case 'android_fcm':
      case 'android_jpush':
        return 'device_management_platform_android'.tr;
      case 'macos':
        return 'device_management_platform_macos'.tr;
      case 'windows':
        return 'device_management_platform_windows'.tr;
      case 'linux':
        return 'device_management_platform_linux'.tr;
      case 'web':
        return 'device_management_platform_web'.tr;
      default:
        return platform.trim().isEmpty
            ? 'common_unknown_error'.tr
            : platform.trim();
    }
  }

  IconData _platformIcon(String platform) {
    switch (platform.trim().toLowerCase()) {
      case 'ios':
        return Icons.phone_iphone_rounded;
      case 'android':
      case 'android_fcm':
      case 'android_jpush':
        return Icons.android_rounded;
      case 'macos':
      case 'windows':
      case 'linux':
        return Icons.laptop_mac_rounded;
      case 'web':
        return Icons.language_rounded;
      default:
        return Icons.devices_other_rounded;
    }
  }

  Color _statusColor(bool online) {
    return online ? AppTheme.successColor : const Color(0xFF6B7280);
  }

  String _formatTime(DateTime? value) {
    if (value == null) {
      return '--';
    }
    return DateFormat('yyyy-MM-dd HH:mm').format(value);
  }
}

class _MetaLine extends StatelessWidget {
  const _MetaLine({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.only(top: 4),
      child: Text(
        '$label: $value',
        style: TextStyle(fontSize: 13, color: theme.colorScheme.secondary),
      ),
    );
  }
}

class _StatusChip extends StatelessWidget {
  const _StatusChip({required this.label, required this.color});

  final String label;
  final Color color;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        label,
        style: TextStyle(
          fontSize: 12,
          fontWeight: FontWeight.w600,
          color: color,
        ),
      ),
    );
  }
}
