import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../shared/utils/toast_util.dart';
import 'agent_client_toolbar_view.dart';
import 'grix_connector_service.dart';

/// Grix Connector 状态页面
/// 显示连接状态、agent 列表、安装/启动/更新操作
class ConnectorStatusView extends StatefulWidget {
  const ConnectorStatusView({super.key});

  @override
  State<ConnectorStatusView> createState() => _ConnectorStatusViewState();
}

class _ConnectorStatusViewState extends State<ConnectorStatusView> {
  late final GrixConnectorService _service;
  bool _operating = false;

  @override
  void initState() {
    super.initState();
    _service = Get.find<GrixConnectorService>();
  }

  Future<void> _doAction(Future<bool> Function() action) async {
    setState(() => _operating = true);
    await action();
    setState(() => _operating = false);
  }

  /// 只把升级指令交给 connector，不在这里等它装完、更不在这里重启 daemon
  /// （重启会掐断本机所有 agent）。结果必须当场如实告诉用户：这个按钮此前
  /// 装完包既不重启也不提示，看上去就是"点了没反应"。
  Future<void> _doUpgrade() async {
    setState(() => _operating = true);
    try {
      final outcome = await _service.upgrade();
      if (!mounted) return;
      switch (outcome) {
        case ConnectorUpgradeOutcome.queued:
          CustomToast.show(
            'system_upgrade_queued_toast'.tr,
            isError: false,
          );
        case ConnectorUpgradeOutcome.installed:
          // 走的是直接装包那条路，没有人会去重启它——别说成"会自动生效"
          CustomToast.show('system_upgrade_installed_toast'.tr, isError: false);
        case ConnectorUpgradeOutcome.failed:
          CustomToast.show(
            'system_upgrade_failed'.trParams({
              'error': _service.lastError.value,
            }),
          );
      }
    } finally {
      // 装包那条路会抛（Process.run 失败、超时），不兜住的话 _operating 永远复位不了，
      // 整页按钮全被锁死。
      if (mounted) setState(() => _operating = false);
    }
  }

  /// 重启会掐断本机所有 agent 的在途任务，必须先跟用户确认
  Future<void> _confirmRestart() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('system_restart_connector'.tr),
        content: Text('system_restart_connector_confirm'.tr),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(false),
            child: Text('common_cancel'.tr),
          ),
          FilledButton(
            onPressed: () => Navigator.of(ctx).pop(true),
            child: Text('common_confirm'.tr),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    setState(() => _operating = true);
    try {
      final ok = await _service.restartDaemon();
      if (!mounted) return;
      if (ok) {
        CustomToast.show('system_restart_done'.tr, isError: false);
      } else {
        CustomToast.show('system_restart_failed'.trParams({
          'error': _service.lastError.value,
        }));
      }
    } finally {
      if (mounted) setState(() => _operating = false);
    }
  }

  /// 已下发、等 connector 自己升完的过渡态：按钮换成说明，避免用户反复点
  Widget _upgradeQueuedHint(ThemeData theme) {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: theme.colorScheme.primary.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(10),
      ),
      child: Row(
        children: [
          Icon(
            Icons.schedule_rounded,
            size: 18,
            color: theme.colorScheme.primary,
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              'system_upgrade_queued_hint'.trParams({
                'version': _service.latestVersion.value,
              }),
              style: TextStyle(
                color: theme.colorScheme.onSurface.withValues(alpha: 0.8),
                fontSize: 13,
              ),
            ),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Obx(() {
      return ListView(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 12),
        children: [
          // 连接状态卡片
          _buildStatusCard(theme),
          const SizedBox(height: 16),
          // 操作按钮区（运行中时隐藏，有更新时除外）
          if (!_service.isRunning.value || _service.hasUpdate)
            _buildActionsCard(theme),
          const SizedBox(height: 16),
          AgentClientToolbarView(service: _service),
        ],
      );
    });
  }

  Widget _buildStatusCard(ThemeData theme) {
    final running = _service.isRunning.value;
    final installed = _service.isInstalled.value;

    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                width: 10,
                height: 10,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: running
                      ? Colors.green
                      : installed
                      ? Colors.orange
                      : Colors.red,
                ),
              ),
              const SizedBox(width: 10),
              Text(
                running
                    ? 'system_status_running'.tr
                    : installed
                    ? 'system_status_stopped'.tr
                    : 'system_status_not_installed'.tr,
                style: TextStyle(
                  fontSize: 16,
                  fontWeight: FontWeight.w600,
                  color: theme.colorScheme.onSurface,
                ),
              ),
              const Spacer(),
              if (running)
                IconButton(
                  icon: const Icon(Icons.restart_alt_rounded, size: 20),
                  onPressed: _operating ? null : _confirmRestart,
                  tooltip: 'system_restart_connector'.tr,
                ),
              IconButton(
                icon: const Icon(Icons.refresh, size: 20),
                onPressed: _operating ? null : () => _service.checkAll(),
                tooltip: 'system_refresh'.tr,
              ),
            ],
          ),
          if (running) ...[
            const SizedBox(height: 12),
            _infoRow('PID', '${_service.pid.value}'),
            _infoRow(
              'system_uptime'.tr,
              _formatUptime(_service.uptime.value),
            ),
            _infoRow(
              'system_agent_count'.tr,
              '${_service.agents.length}',
            ),
          ],
          if (installed) ...[
            const SizedBox(height: 8),
            _infoRow(
              'system_installed_version'.tr,
              _service.installedVersion.value.isEmpty
                  ? '—'
                  : _service.installedVersion.value,
            ),
            if (_service.latestVersion.value.isNotEmpty)
              _infoRow(
                'system_latest_version'.tr,
                _service.latestVersion.value,
              ),
          ],
        ],
      ),
    );
  }

  Widget _buildActionsCard(ThemeData theme) {
    final installed = _service.isInstalled.value;
    final running = _service.isRunning.value;

    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (!installed)
            _actionButton(
              theme,
              icon: Icons.download_rounded,
              label: 'system_install_connector'.tr,
              onPressed: () => _doAction(_service.install),
            ),
          if (installed && !running)
            _actionButton(
              theme,
              icon: Icons.play_arrow_rounded,
              label: 'system_start'.tr,
              onPressed: () => _doAction(_service.start),
            ),
          if (installed && _service.hasUpdate)
            Padding(
              padding: EdgeInsets.only(top: !running ? 8 : 0),
              child: _service.upgradeQueued
                  ? _upgradeQueuedHint(theme)
                  : _actionButton(
                      theme,
                      icon: Icons.system_update_rounded,
                      label: 'system_update_to'.trParams({
                        'version': _service.latestVersion.value,
                      }),
                      onPressed: _doUpgrade,
                    ),
            ),
          if (installed && !_service.hasUpdate && running)
            Text(
              'system_all_running_well'.tr,
              style: TextStyle(
                color: theme.colorScheme.onSurface.withValues(alpha: 0.6),
                fontSize: 14,
              ),
              textAlign: TextAlign.center,
            ),
        ],
      ),
    );
  }

  Widget _actionButton(
    ThemeData theme, {
    required IconData icon,
    required String label,
    required VoidCallback onPressed,
  }) {
    return FilledButton.icon(
      onPressed: _operating ? null : onPressed,
      icon: _operating
          ? const SizedBox(
              width: 16,
              height: 16,
              child: CircularProgressIndicator(strokeWidth: 2),
            )
          : Icon(icon, size: 18),
      label: Text(label),
      style: FilledButton.styleFrom(
        minimumSize: const Size.fromHeight(44),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
      ),
    );
  }

  Widget _infoRow(String label, String value) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 2),
      child: Row(
        children: [
          Text(label, style: TextStyle(fontSize: 13, color: Colors.grey[600])),
          const Spacer(),
          Text(
            value,
            style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w500),
          ),
        ],
      ),
    );
  }

  String _formatUptime(int seconds) {
    if (seconds < 60) return '${seconds}s';
    if (seconds < 3600) return '${seconds ~/ 60}m';
    final h = seconds ~/ 3600;
    final m = (seconds % 3600) ~/ 60;
    return '${h}h ${m}m';
  }
}
