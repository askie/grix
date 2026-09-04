import 'package:flutter/material.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:flutter_svg/flutter_svg.dart';
import 'package:get/get.dart';

import '../../app/scroll/horizontal_drag_scroll_behavior.dart';
import '../../data/providers/agent_service.dart';
import '../../shared/utils/toast_util.dart';
import '../../shared/widgets/app_dialog_style.dart';
import 'agent_client_type_meta.dart';
import 'agent_create_completion.dart';
import 'agent_installer_view.dart';
import 'agent_probe_grouping.dart';
import 'grix_connector_service.dart';

class AgentClientToolbarView extends StatefulWidget {
  const AgentClientToolbarView({
    super.key,
    required this.service,
    this.compact = false,
  });

  final GrixConnectorService service;

  /// 小图模式：单行图标，无文字，hover 显示 tooltip。
  final bool compact;

  @override
  State<AgentClientToolbarView> createState() => _AgentClientToolbarViewState();
}

class _AgentClientToolbarViewState extends State<AgentClientToolbarView> {
  bool _creating = false;

  GrixConnectorService get _service => widget.service;

  @override
  Widget build(BuildContext context) {
    if (widget.compact) return _buildCompactLayout();
    return _buildFullLayout();
  }

  // ── 小图模式 ──────────────────────────────────────────

  Widget _buildCompactLayout() {
    final theme = Theme.of(context);

    return Obx(() {
      final groups = buildAgentProbeGroups(
        _service.probeResults,
        installedClients: _service.installedClients,
      );

      if (groups.isEmpty) return const SizedBox.shrink();

      return Container(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
        decoration: BoxDecoration(
          border: Border(
            bottom: BorderSide(
              color: theme.colorScheme.outline.withValues(alpha: 0.12),
              width: 0.5,
            ),
          ),
        ),
        child: ScrollConfiguration(
          // 桌面/Web 全局滚动默认禁鼠标拖动（保文字选中）；横向图标条需单独放开。
          behavior: const HorizontalDragScrollBehavior(),
          child: SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            child: Row(
              children: [
                for (final group in groups)
                  Padding(
                    padding: const EdgeInsets.only(right: 4),
                    child: _buildCompactIcon(theme, group),
                  ),
              ],
            ),
          ),
        ),
      );
    });
  }

  Widget _buildCompactIcon(ThemeData theme, AgentProbeGroup group) {
    final count = group.results.length;
    return Tooltip(
      message: group.meta.label,
      preferBelow: true,
      child: InkWell(
        onTap: () => _showAgentTypeDialog(group.meta),
        borderRadius: BorderRadius.zero,
        child: SizedBox(
          width: 40,
          height: 40,
          child: Stack(
            children: [
              Center(
                child: _buildClientLogo(
                  group.meta,
                  size: 36,
                  showBackground: false,
                ),
              ),
              if (count > 0)
                Positioned(
                  right: 0,
                  bottom: 0,
                  child: Container(
                    constraints: const BoxConstraints(
                      minWidth: 16,
                      minHeight: 16,
                    ),
                    padding: const EdgeInsets.symmetric(horizontal: 3),
                    decoration: BoxDecoration(
                      color: theme.colorScheme.surfaceContainerHighest
                          .withValues(alpha: 0.85),
                      shape: BoxShape.circle,
                    ),
                    alignment: Alignment.center,
                    child: Text(
                      '$count',
                      style: TextStyle(
                        fontSize: count >= 10 ? 8 : 9,
                        fontWeight: FontWeight.w700,
                        color: theme.colorScheme.onSurface,
                        height: 1,
                      ),
                    ),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }

  // ── 大图模式（原有布局）──────────────────────────────────

  Widget _buildFullLayout() {
    final theme = Theme.of(context);

    return Obx(() {
      final groups = buildAgentProbeGroups(
        _service.probeResults,
        installedClients: _service.installedClients,
      );
      final loading = _service.probeLoading.value;

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
                Icon(
                  Icons.apps_rounded,
                  size: 18,
                  color: theme.colorScheme.primary,
                ),
                const SizedBox(width: 8),
                Text(
                  'system_agent_toolbar'.tr,
                  style: TextStyle(
                    fontSize: 14,
                    fontWeight: FontWeight.w600,
                    color: theme.colorScheme.onSurface,
                  ),
                ),
                const Spacer(),
                if (loading)
                  const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                else
                  IconButton(
                    icon: const Icon(Icons.refresh, size: 18),
                    onPressed: () => _service.probeAll(fresh: true),
                    tooltip: 'system_re_probe'.tr,
                    padding: EdgeInsets.zero,
                    constraints: const BoxConstraints(
                      minWidth: 32,
                      minHeight: 32,
                    ),
                  ),
              ],
            ),
            const SizedBox(height: 12),
            if (groups.isEmpty && !loading)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 8),
                child: Text(
                  _service.isRunning.value
                      ? 'system_no_agent_commands'.tr
                      : 'system_connector_not_running'.tr,
                  style: TextStyle(
                    fontSize: 13,
                    color: theme.colorScheme.onSurface.withValues(alpha: 0.5),
                  ),
                ),
              ),
            if (groups.isNotEmpty)
              Wrap(
                spacing: 10,
                runSpacing: 10,
                children: [
                  for (final group in groups)
                    _buildAgentTypeButton(theme, group),
                ],
              ),
          ],
        ),
      );
    });
  }

  Widget _buildAgentTypeButton(ThemeData theme, AgentProbeGroup group) {
    final statusColor = _probeStatusColor(group.status);
    final deployedCount = _agentsForType(group.meta.clientType).length;
    final subtitle = group.results.isEmpty && group.installedClient != null
        ? (group.installedClient!.installed
              ? 'system_command_installed_agent_zero'.tr
              : 'system_not_installed_click_install'.tr)
        : 'system_deployed_probe'.trParams({
            'deployed': '$deployedCount',
            'probed': '${group.results.length}',
          });

    return InkWell(
      borderRadius: BorderRadius.circular(10),
      onTap: () => _showAgentTypeDialog(group.meta),
      child: Container(
        width: 146,
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          border: Border.all(
            color: theme.colorScheme.outline.withValues(alpha: 0.18),
          ),
          borderRadius: BorderRadius.circular(10),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            Row(
              children: [
                _buildClientLogo(group.meta, size: 34),
                const Spacer(),
                Container(
                  width: 9,
                  height: 9,
                  decoration: BoxDecoration(
                    color: statusColor,
                    shape: BoxShape.circle,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 10),
            Text(
              group.meta.label,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
            ),
            const SizedBox(height: 3),
            Text(
              subtitle,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                fontSize: 11,
                color: theme.colorScheme.onSurface.withValues(alpha: 0.55),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildClientLogo(
    AgentClientTypeMeta meta, {
    double size = 28,
    bool showBackground = true,
  }) {
    final theme = Theme.of(context);
    final plateless = meta.selfContained;
    final logoSize = showBackground && !plateless ? size * 0.62 : size;
    final ColorFilter? monoFilter = meta.monochrome
        ? ColorFilter.mode(theme.colorScheme.onSurface, BlendMode.srcIn)
        : null;
    final Widget image;
    if (meta.logoAsset.endsWith('.svg')) {
      // Render SVG at a larger internal size and let FittedBox scale down.
      // macOS Impeller has issues rasterizing complex SVGs at very small
      // pixel sizes; rendering at 96px then scaling avoids that.
      image = SizedBox(
        width: logoSize,
        height: logoSize,
        child: FittedBox(
          fit: BoxFit.contain,
          child: SvgPicture.asset(
            meta.logoAsset,
            width: 96,
            height: 96,
            fit: BoxFit.contain,
            colorFilter: monoFilter,
          ),
        ),
      );
    } else {
      image = Image.asset(
        meta.logoAsset,
        width: logoSize,
        height: logoSize,
        fit: BoxFit.contain,
      );
    }
    if (!showBackground) {
      return SizedBox(
        width: size,
        height: size,
        child: Center(child: image),
      );
    }
    if (plateless) {
      return ClipOval(
        child: SizedBox(width: size, height: size, child: image),
      );
    }
    return Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        color: theme.colorScheme.surfaceContainerLow,
        shape: BoxShape.circle,
      ),
      alignment: Alignment.center,
      child: image,
    );
  }

  void _showAgentTypeDialog(AgentClientTypeMeta meta) {
    showAppDialog<void>(
      context: context,
      builder: (ctx) {
        final theme = Theme.of(ctx);
        return AlertDialog(
          title: Row(
            children: [
              _buildClientLogo(meta, size: 28),
              const SizedBox(width: 10),
              Expanded(child: Text(meta.label)),
            ],
          ),
          content: SizedBox(
            width: resolveDialogConstraints(
              ctx,
              size: AppDialogSize.wide,
            ).maxWidth,
            child: Obx(() {
              final agents = _agentsForType(meta.clientType);
              final probes = _probeResultsForType(meta.clientType);
              final installedClient = _installedClientForType(meta.clientType);
              final probeByName = {
                for (final probe in probes)
                  if (probe.agentName.trim().isNotEmpty)
                    probe.agentName.trim(): probe,
              };
              final probeOnly = probes
                  .where((probe) => !_hasAgentNamed(agents, probe.agentName))
                  .toList();
              final emptyText = installedClient == null
                  ? 'system_no_deployed_agents'.trParams({'label': meta.label})
                  : installedClient.installed
                  ? 'system_installed_no_deployed'.trParams({
                      'label': meta.label,
                    })
                  : 'system_label_not_installed_hint'.trParams({
                      'label': meta.label,
                    });

              return Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  if (agents.isEmpty && probeOnly.isEmpty)
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 12),
                      child: Text(
                        emptyText,
                        style: TextStyle(
                          fontSize: 13,
                          color: theme.colorScheme.onSurface.withValues(
                            alpha: 0.55,
                          ),
                        ),
                      ),
                    )
                  else
                    ConstrainedBox(
                      constraints: const BoxConstraints(maxHeight: 360),
                      child: SingleChildScrollView(
                        child: Column(
                          children: [
                            for (final agent in agents)
                              _buildAgentTile(
                                theme,
                                agent,
                                probeByName[_agentName(agent)],
                              ),
                            for (final probe in probeOnly)
                              _buildProbeOnlyTile(theme, probe),
                          ],
                        ),
                      ),
                    ),
                ],
              );
            }),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(ctx),
              child: Text('system_close'.tr),
            ),
            Obx(() {
              final installedClient = _installedClientForType(meta.clientType);

              // Show install button if: no client info OR client exists but not installed
              final showInstallButton =
                  installedClient == null || !installedClient.installed;

              if (showInstallButton) {
                return OutlinedButton.icon(
                  onPressed: () {
                    Navigator.pop(ctx);
                    showAgentInstallerDialog(
                      context: context,
                      service: _service,
                      meta: meta,
                      onInstalled: () {
                        _service.probeAll(fresh: true);
                      },
                    );
                  },
                  icon: const Icon(Icons.download_rounded, size: 18),
                  label: Text('system_install'.tr),
                );
              }
              return const SizedBox.shrink();
            }),
            FilledButton.icon(
              onPressed: _creating
                  ? null
                  : () {
                      FocusManager.instance.primaryFocus?.unfocus();
                      Navigator.pop(ctx);
                      Future<void>.delayed(
                        const Duration(milliseconds: 120),
                        () {
                          if (mounted) {
                            _showAddAgentForTypeDialog(meta);
                          }
                        },
                      );
                    },
              icon: const Icon(Icons.add_rounded, size: 18),
              label: Text('system_add_new'.tr),
            ),
          ],
        );
      },
    );
  }

  Widget _buildAgentTile(
    ThemeData theme,
    Map<String, dynamic> agent,
    AgentProbeResult? probe,
  ) {
    final name = _agentName(agent);
    final alive = agent['alive'] == true;
    final busy = agent['busy'] == true;
    final adapterType = agent['adapterType']?.toString() ?? '';
    final pool = agent['pool'] as Map<String, dynamic>?;
    final statusColor = probe != null
        ? _probeStatusColor(probe.status)
        : (alive ? (busy ? Colors.orange : Colors.green) : Colors.red);
    final statusLabel = !alive
        ? 'common_offline'.tr
        : (busy ? 'system_agent_busy'.tr : 'system_agent_ready'.tr);
    final poolPart = pool == null
        ? ''
        : ' · ${'system_pool_ready'.trParams({'ready': '${pool['ready']}', 'total': '${pool['total']}'})}';

    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 7),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: const EdgeInsets.only(top: 6),
            child: _statusDot(statusColor),
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  name,
                  style: const TextStyle(
                    fontSize: 14,
                    fontWeight: FontWeight.w500,
                  ),
                ),
                Text(
                  '$adapterType · $statusLabel$poolPart',
                  style: TextStyle(
                    fontSize: 12,
                    color: theme.colorScheme.onSurface.withValues(alpha: 0.6),
                  ),
                ),
                if (probe != null) _probeDetailLines(theme, probe),
              ],
            ),
          ),
          IconButton(
            icon: const Icon(Icons.restart_alt_rounded, size: 18),
            tooltip: 'system_restart'.tr,
            onPressed: name.isEmpty ? null : () => _service.restartAgent(name),
          ),
          IconButton(
            icon: Icon(
              Icons.delete_outline_rounded,
              size: 18,
              color: theme.colorScheme.error,
            ),
            tooltip: 'system_remove'.tr,
            onPressed: name.isEmpty ? null : () => _confirmRemoveAgent(name),
          ),
        ],
      ),
    );
  }

  Widget _buildProbeOnlyTile(ThemeData theme, AgentProbeResult probe) {
    final statusColor = _probeStatusColor(probe.status);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 7),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: const EdgeInsets.only(top: 6),
            child: _statusDot(statusColor),
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  probe.agentName,
                  style: const TextStyle(
                    fontSize: 14,
                    fontWeight: FontWeight.w500,
                  ),
                ),
                Text(
                  _probeStatusLabel(probe.status),
                  style: TextStyle(
                    fontSize: 12,
                    color: theme.colorScheme.onSurface.withValues(alpha: 0.6),
                  ),
                ),
                _probeDetailLines(theme, probe),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _statusDot(Color color) {
    return Container(
      width: 8,
      height: 8,
      decoration: BoxDecoration(shape: BoxShape.circle, color: color),
    );
  }

  Widget _probeDetailLines(ThemeData theme, AgentProbeResult result) {
    final lines = <String>[];
    final cli = result.cli;
    if (cli != null) {
      if (cli.installed) {
        lines.add(
          'system_cli_installed'.trParams({
            'command': cli.command,
            'version': cli.version,
          }).trim(),
        );
        if (cli.path.isNotEmpty) {
          lines.add('system_cli_path'.trParams({'path': cli.path}));
        }
      } else {
        lines.add('system_cli_not_installed'.tr);
      }
    }

    final proc = result.process;
    if (proc != null) {
      final procParts = <String>[];
      if (proc.started) procParts.add('system_process_started'.tr);
      if (proc.alive) procParts.add('system_status_running'.tr);
      if (proc.busy) procParts.add('system_agent_busy'.tr);
      if (procParts.isNotEmpty) {
        lines.add(
          'system_process'.trParams({'details': procParts.join(' · ')}),
        );
      } else if (proc.started == false) {
        lines.add('system_process_line_not_started'.tr);
      }
    }

    final conv = result.conversation;
    if (conv != null && conv.attempted) {
      if (conv.ok) {
        lines.add(
          conv.latencyMs != null
              ? 'system_conversation_ok_latency'.trParams({
                  'latencyMs': '${conv.latencyMs}',
                })
              : 'system_conversation_ok'.tr,
        );
      } else {
        lines.add('system_conversation_failed'.tr);
      }
    }

    if (lines.isEmpty) return const SizedBox.shrink();

    return Padding(
      padding: const EdgeInsets.only(top: 3),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: lines
            .map(
              (line) => Text(
                line,
                style: TextStyle(
                  fontSize: 11,
                  color: theme.colorScheme.onSurface.withValues(alpha: 0.55),
                  fontFamily: 'monospace',
                  fontFamilyFallback: AppTheme.textFontFallbackOrNull,
                ),
              ),
            )
            .toList(),
      ),
    );
  }

  Future<void> _showAddAgentForTypeDialog(AgentClientTypeMeta meta) async {
    final name = await promptNewAgentName(
      context: context,
      typeLabel: meta.label,
      initialName: _defaultAgentName(meta.clientType),
    );
    if (name == null || name.isEmpty || _creating || !mounted) return;
    await _createAgentFlow(name, meta.clientType);
  }

  Future<void> _createAgentFlow(String name, String clientType) async {
    setState(() => _creating = true);

    try {
      final agentService = Get.find<AgentService>();
      final agent = await agentService.createAgent({
        'agent_name': name,
        'provider_type': 3,
        'agent_client_type': clientType,
      });

      if (agent == null) {
        CustomToast.show(_agentServiceError(agentService));
        return;
      }

      final wsUrl = agent.apiEndpoint.isNotEmpty
          ? agent.apiEndpoint
          : 'wss://grix.dhf.pub/v1/agent-api/ws';

      final added = await _service.addAgent({
        'name': name,
        'ws_url': wsUrl,
        'agent_id': agent.id,
        'api_key': agent.apiKey,
        'client_type': clientType,
      });

      if (!added) {
        CustomToast.show(_connectorError());
        return;
      }

      CustomToast.show(
        'system_agent_created_starting'.trParams({'name': name}),
        isError: false,
      );
      await Future.delayed(const Duration(seconds: 3));
      await _service.checkHealth();
      await _service.probeAll(fresh: true);

      if (mounted) {
        await configureAndOpenNewAgent(
          agentId: agent.id,
          agentName: name,
          clientType: clientType,
        );
      }
    } catch (e) {
      CustomToast.show(
        userFacingError(e, fallback: 'system_create_agent_failed'.tr),
      );
    } finally {
      if (mounted) setState(() => _creating = false);
    }
  }

  String _agentServiceError(AgentService agentService) {
    final message = agentService.lastOperationError.trim();
    return message.isEmpty ? 'system_remote_create_failed'.tr : message;
  }

  String _connectorError() {
    final message = _service.lastError.value.trim();
    return message.isEmpty ? 'system_local_add_failed'.tr : message;
  }

  List<Map<String, dynamic>> _agentsForType(String clientType) {
    final probeNames = _probeResultsForType(clientType)
        .map((probe) => probe.agentName.trim())
        .where((name) => name.isNotEmpty)
        .toSet();
    return _service.agents.where((agent) {
      final type = _agentClientType(agent);
      if (type == clientType) return true;
      return probeNames.contains(_agentName(agent));
    }).toList();
  }

  List<AgentProbeResult> _probeResultsForType(String clientType) {
    return _service.probeResults
        .where(
          (probe) =>
              !isHiddenAgentProbeResult(probe) &&
              probe.clientType.trim().toLowerCase() == clientType,
        )
        .toList();
  }

  InstalledClientCommand? _installedClientForType(String clientType) {
    final normalizedType = clientType.trim().toLowerCase();
    for (final client in _service.installedClients) {
      if (isHiddenInstalledClientCommand(client)) continue;
      if (client.clientType.trim().toLowerCase() == normalizedType) {
        return client;
      }
    }
    return null;
  }

  String _agentClientType(Map<String, dynamic> agent) {
    return (agent['clientType'] ??
            agent['client_type'] ??
            agent['client-type'] ??
            '')
        .toString()
        .trim()
        .toLowerCase();
  }

  String _agentName(Map<String, dynamic> agent) {
    return (agent['name'] ?? agent['agent_name'] ?? '').toString().trim();
  }

  bool _hasAgentNamed(List<Map<String, dynamic>> agents, String name) {
    final normalized = name.trim();
    if (normalized.isEmpty) return false;
    return agents.any((agent) => _agentName(agent) == normalized);
  }

  String _defaultAgentName(String clientType) {
    return defaultAgentNameFor(
      clientType: clientType,
      usedNames: _service.agents.map(_agentName),
      sameTypeCount: _agentsForType(clientType).length,
    );
  }

  void _confirmRemoveAgent(String name) async {
    final ok = await showAppConfirmDialog(
      context: context,
      title: 'system_remove_agent'.tr,
      message: 'system_confirm_remove_agent'.trParams({'name': name}),
      confirmText: 'system_remove'.tr,
      isDestructive: true,
    );
    if (ok) {
      await _service.removeAgent(name);
      await _service.probeAll(fresh: true);
    }
  }

  Color _probeStatusColor(String status) {
    switch (status) {
      case 'healthy':
        return Colors.green;
      case 'degraded':
        return Colors.orange;
      case 'installed':
        return Colors.green;
      case 'not_installed':
        return Colors.grey;
      case 'unavailable':
        return Colors.grey;
      case 'error':
        return Colors.red;
      default:
        return Colors.grey;
    }
  }

  String _probeStatusLabel(String status) {
    switch (status) {
      case 'healthy':
        return 'system_probe_healthy'.tr;
      case 'degraded':
        return 'system_probe_degraded'.tr;
      case 'installed':
        return 'system_probe_installed'.tr;
      case 'not_installed':
        return 'system_status_not_installed'.tr;
      case 'unavailable':
        return 'system_probe_unavailable'.tr;
      case 'error':
        return 'system_probe_error'.tr;
      default:
        return status;
    }
  }
}
