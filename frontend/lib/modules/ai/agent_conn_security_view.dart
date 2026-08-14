import 'package:flutter/material.dart';
import 'package:get/get.dart';

import 'controllers/agent_conn_security_controller.dart';
import 'models/agent_conn_security_model.dart';

/// agent「连接安全」页：查看登录历史，并可把某个登录 IP 加入黑名单（立即生效）。
class AgentConnSecurityView extends GetView<AgentConnSecurityController> {
  const AgentConnSecurityView({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      appBar: AppBar(title: Text('ai_agent_conn_title'.tr)),
      body: Obx(() {
        if (!controller.canUse) {
          return _buildUnsupported(theme);
        }
        if (controller.isLoading.value) {
          return const Center(child: CircularProgressIndicator());
        }
        return RefreshIndicator(
          onRefresh: controller.loadAll,
          child: ListView(
            padding: const EdgeInsets.all(16),
            children: [
              _buildHeader(theme),
              const SizedBox(height: 16),
              _buildBanSection(theme),
              const SizedBox(height: 20),
              _buildHistorySection(theme),
            ],
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
            'ai_agent_conn_hint'.tr,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.secondary,
              height: 1.4,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildBanSection(ThemeData theme) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _sectionTitle(theme, 'ai_agent_conn_blocklist_title'.tr),
        const SizedBox(height: 8),
        Obx(() {
          final rules = controller.banRules;
          if (rules.isEmpty) {
            return _emptyHint(theme, 'ai_agent_conn_blocklist_empty'.tr);
          }
          return Column(
            children: rules.map((r) => _buildRuleTile(theme, r)).toList(),
          );
        }),
      ],
    );
  }

  Widget _buildRuleTile(ThemeData theme, AgentIPRuleEntry rule) {
    return Card(
      margin: const EdgeInsets.only(bottom: 8),
      child: ListTile(
        leading: Icon(Icons.block, color: theme.colorScheme.error),
        title: Text(
          rule.ipCidr,
          style: const TextStyle(fontWeight: FontWeight.w600),
        ),
        subtitle: rule.remark.isEmpty
            ? null
            : Text(rule.remark, maxLines: 2, overflow: TextOverflow.ellipsis),
        trailing: Obx(
          () => TextButton(
            onPressed: controller.isMutating.value
                ? null
                : () => controller.deleteRule(rule.id),
            child: Text('ai_agent_conn_unban'.tr),
          ),
        ),
      ),
    );
  }

  Widget _buildHistorySection(ThemeData theme) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _sectionTitle(theme, 'ai_agent_conn_history_title'.tr),
        const SizedBox(height: 8),
        Obx(() {
          final items = controller.logs;
          if (items.isEmpty) {
            return _emptyHint(theme, 'ai_agent_conn_history_empty'.tr);
          }
          return Column(
            children: items.map((e) => _buildLogTile(theme, e)).toList(),
          );
        }),
      ],
    );
  }

  Widget _buildLogTile(ThemeData theme, AgentConnectionLogEntry log) {
    final ip = log.clientIP;
    return Card(
      margin: const EdgeInsets.only(bottom: 8),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(12, 10, 12, 8),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    ip.isEmpty ? '-' : ip,
                    style: const TextStyle(
                      fontWeight: FontWeight.w700,
                      fontSize: 15,
                    ),
                  ),
                ),
                if (log.isOnline)
                  _badge(
                    theme,
                    'ai_agent_conn_online'.tr,
                    theme.colorScheme.primary,
                  )
                else
                  _badge(
                    theme,
                    'ai_agent_conn_offline'.tr,
                    theme.colorScheme.outline,
                  ),
                if (log.geoChanged) ...[
                  const SizedBox(width: 6),
                  _badge(theme, 'ai_agent_conn_geo_changed'.tr, Colors.orange),
                ],
              ],
            ),
            const SizedBox(height: 6),
            _metaLine(
              theme,
              Icons.place_outlined,
              log.ipLocation.isEmpty
                  ? 'ai_agent_conn_unknown'.tr
                  : log.ipLocation,
            ),
            if (log.clientType.isNotEmpty)
              _metaLine(theme, Icons.devices_other_outlined, log.clientType),
            _metaLine(
              theme,
              Icons.schedule_outlined,
              _formatConnectedLine(log),
            ),
            const SizedBox(height: 4),
            Align(
              alignment: Alignment.centerRight,
              child: Obx(() {
                final banned = controller.isIPBanned(ip);
                if (ip.isEmpty) {
                  return const SizedBox.shrink();
                }
                if (banned) {
                  return Text(
                    'ai_agent_conn_already_banned'.tr,
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.error,
                      fontWeight: FontWeight.w600,
                    ),
                  );
                }
                return TextButton.icon(
                  onPressed: controller.isMutating.value
                      ? null
                      : () => _confirmBan(ip),
                  icon: Icon(
                    Icons.block,
                    size: 18,
                    color: theme.colorScheme.error,
                  ),
                  label: Text(
                    'ai_agent_conn_ban'.tr,
                    style: TextStyle(color: theme.colorScheme.error),
                  ),
                );
              }),
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _confirmBan(String ip) async {
    final confirmed = await Get.dialog<bool>(
      AlertDialog(
        title: Text('ai_agent_conn_ban_confirm_title'.tr),
        content: Text('ai_agent_conn_ban_confirm_body'.trParams({'ip': ip})),
        actions: [
          TextButton(
            onPressed: () => Get.back(result: false),
            child: Text('common_cancel'.tr),
          ),
          TextButton(
            onPressed: () => Get.back(result: true),
            child: Text('ai_agent_conn_ban'.tr),
          ),
        ],
      ),
    );
    if (confirmed == true) {
      await controller.banIP(ip);
    }
  }

  Widget _sectionTitle(ThemeData theme, String text) {
    return Text(
      text,
      style: theme.textTheme.titleSmall?.copyWith(fontWeight: FontWeight.w700),
    );
  }

  Widget _emptyHint(ThemeData theme, String text) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 12),
      child: Text(
        text,
        style: theme.textTheme.bodySmall?.copyWith(
          color: theme.colorScheme.secondary,
        ),
      ),
    );
  }

  Widget _metaLine(ThemeData theme, IconData icon, String text) {
    return Padding(
      padding: const EdgeInsets.only(top: 2),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, size: 14, color: theme.colorScheme.outline),
          const SizedBox(width: 6),
          Expanded(
            child: Text(
              text,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _badge(ThemeData theme, String text, Color color) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Text(
        text,
        style: TextStyle(
          fontSize: 11,
          fontWeight: FontWeight.w600,
          color: color,
        ),
      ),
    );
  }

  Widget _buildUnsupported(ThemeData theme) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Text(
          'ai_agent_conn_provider_unsupported'.tr,
          style: theme.textTheme.bodyMedium?.copyWith(
            color: theme.colorScheme.secondary,
          ),
          textAlign: TextAlign.center,
        ),
      ),
    );
  }

  String _formatConnectedLine(AgentConnectionLogEntry log) {
    final connected = _fmtTime(log.connectedAt);
    if (log.isOnline) {
      return connected;
    }
    final disconnected = _fmtTime(log.disconnectedAt);
    return '$connected → $disconnected';
  }

  String _fmtTime(DateTime? t) {
    if (t == null) {
      return '-';
    }
    String two(int n) => n.toString().padLeft(2, '0');
    return '${t.year}-${two(t.month)}-${two(t.day)} '
        '${two(t.hour)}:${two(t.minute)}';
  }
}
