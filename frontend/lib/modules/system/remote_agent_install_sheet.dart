import 'dart:async';

import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../data/models/connector_admin_model.dart';
import '../../data/providers/agent_service.dart';
import '../../shared/utils/toast_util.dart';
import 'agent_client_type_meta.dart';
import 'agent_create_completion.dart';
import 'connector_admin_client.dart';

/// 手机端「在某台主机上装/建 agent」。
///
/// 桌面端走本机 127 admin API（agent_client_toolbar_view.dart）；手机端打不到那台
/// 机器，于是借一个该主机上在线、且连接器声明了 connector_admin 的自有 agent 当通道，
/// 指令经后端转成 local_action 下发。建完之后的收尾（中转配置 + 打开会话）与桌面端
/// 共用 [configureAndOpenNewAgent]。
///
/// [channelCandidates] 必须已经过滤成「本人 + 在线 + supportsConnectorAdmin」。
Future<void> showRemoteAgentInstallSheet({
  required BuildContext context,
  required String hostLabel,
  required List<AgentModel> channelCandidates,
}) {
  return showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    useSafeArea: true,
    builder: (ctx) => _RemoteAgentInstallSheet(
      hostLabel: hostLabel,
      channelCandidates: channelCandidates,
    ),
  );
}

class _RemoteAgentInstallSheet extends StatefulWidget {
  const _RemoteAgentInstallSheet({
    required this.hostLabel,
    required this.channelCandidates,
  });

  final String hostLabel;
  final List<AgentModel> channelCandidates;

  @override
  State<_RemoteAgentInstallSheet> createState() =>
      _RemoteAgentInstallSheetState();
}

class _RemoteAgentInstallSheetState extends State<_RemoteAgentInstallSheet> {
  late AgentModel _channel = widget.channelCandidates.first;

  bool _loading = true;
  String _loadError = '';
  List<ConnectorInstallableAgent> _installable = const [];

  /// 正在安装/创建的类型；非空时禁掉其它行，避免并发下发。
  String _busyType = '';
  String _busyMessage = '';
  double? _busyProgress;

  /// 操作代数：换通道或取消时让进行中的异步链自行终止。
  int _generation = 0;

  /// 轮询间隔的可取消等待。弹窗关掉时必须立刻放行并终止，否则会留下一个
  /// 没人要的 2s 定时器，还会在已经销毁的 State 上继续跑一轮。
  Timer? _pollTimer;
  Completer<void>? _pollWaiter;

  @override
  void initState() {
    super.initState();
    unawaited(_loadInstallable());
  }

  @override
  void dispose() {
    // 关掉弹窗就让进行中的轮询链自行终止：改代数让它下一步就退出，
    // 同时放行正卡在轮询间隔上的等待，别留下悬着的定时器。
    _generation++;
    _pollTimer?.cancel();
    _pollTimer = null;
    final waiter = _pollWaiter;
    _pollWaiter = null;
    if (waiter != null && !waiter.isCompleted) waiter.complete();
    super.dispose();
  }

  Future<void> _sleepBetweenPolls() {
    final waiter = Completer<void>();
    _pollWaiter = waiter;
    _pollTimer = Timer(const Duration(seconds: 2), () {
      if (!waiter.isCompleted) waiter.complete();
    });
    return waiter.future;
  }

  ConnectorAdminClient get _client => ConnectorAdminClient(_channel.id);

  Future<void> _loadInstallable() async {
    final gen = ++_generation;
    setState(() {
      _loading = true;
      _loadError = '';
    });
    try {
      final list = await _client.listInstallable();
      if (!mounted || gen != _generation) return;
      setState(() {
        _installable = list.agents;
        _loading = false;
      });
    } catch (e) {
      if (!mounted || gen != _generation) return;
      setState(() {
        _loading = false;
        _loadError = _describe(e);
      });
    }
  }

  /// 一行的完整流程：未装先装并轮询进度，装好后起名字，再让后端建 agent。
  Future<void> _installAndCreate(ConnectorInstallableAgent item) async {
    if (_busyType.isNotEmpty) return;
    final gen = ++_generation;

    if (!item.installed) {
      setState(() {
        _busyType = item.agentType;
        _busyProgress = null;
        _busyMessage = 'agent_installer_installing'.trParams({
          'name': item.label,
        });
      });
      final installed = await _runInstall(item, gen);
      if (!mounted || gen != _generation) return;
      if (!installed) {
        setState(() {
          _busyType = '';
          _busyMessage = '';
          _busyProgress = null;
        });
        return;
      }
      setState(() {
        _busyMessage = 'agent_installer_done'.tr;
        _busyProgress = 1;
      });
      // 装完把列表刷成"已安装"，用户中途退出再进来状态也是对的。
      unawaited(_refreshInstalledState(item.agentType));
    } else {
      setState(() {
        _busyType = item.agentType;
        _busyMessage = '';
        _busyProgress = null;
      });
    }

    final usedNames = Get.find<AgentService>().agents
        .where((agent) => agent.hostname == _channel.hostname)
        .map((agent) => agent.agentName);
    final sameTypeCount = Get.find<AgentService>().agents
        .where(
          (agent) =>
              agent.hostname == _channel.hostname &&
              agent.agentClientType == item.agentType,
        )
        .length;

    if (!mounted || gen != _generation) return;
    final name = await promptNewAgentName(
      context: context,
      typeLabel: item.label,
      initialName: defaultAgentNameFor(
        clientType: item.agentType,
        usedNames: usedNames,
        sameTypeCount: sameTypeCount,
      ),
    );
    if (!mounted || gen != _generation) return;
    if (name == null || name.isEmpty) {
      setState(() {
        _busyType = '';
        _busyMessage = '';
        _busyProgress = null;
      });
      return;
    }

    setState(() => _busyMessage = 'remote_install_creating'.tr);
    final ConnectorCreatedAgent created;
    try {
      created = await _client.createAgent(
        agentName: name,
        clientType: item.agentType,
      );
    } catch (e) {
      if (!mounted || gen != _generation) return;
      setState(() {
        _busyType = '';
        _busyMessage = '';
        _busyProgress = null;
      });
      CustomToast.show(_describe(e));
      return;
    }

    if (!mounted || gen != _generation) return;
    // 收尾在弹窗关掉之后继续跑：configureAndOpenNewAgent 不依赖本 widget 的
    // context，也不能被 mounted 判断挡掉，否则中转配置和跳会话会被静默跳过。
    Navigator.of(context).pop();
    CustomToast.show(
      'system_agent_created_starting'.trParams({'name': name}),
      isError: false,
    );
    await Get.find<AgentService>().loadAgents();
    await configureAndOpenNewAgent(
      agentId: created.agentId,
      agentName: created.agentName.isEmpty ? name : created.agentName,
      clientType: item.agentType,
    );
  }

  /// 触发安装并轮询进度。连接器是异步受理，install 立刻返回，进度靠
  /// install_progress 拿；返回 true 表示装完。
  Future<bool> _runInstall(ConnectorInstallableAgent item, int gen) async {
    try {
      await _client.install(item.agentType);
    } catch (e) {
      CustomToast.show(_describe(e));
      return false;
    }

    final deadline = DateTime.now().add(const Duration(minutes: 10));
    while (DateTime.now().isBefore(deadline)) {
      await _sleepBetweenPolls();
      if (!mounted || gen != _generation) return false;
      ConnectorInstallProgress progress;
      try {
        progress = await _client.installProgress(item.agentType);
      } catch (e) {
        CustomToast.show(_describe(e));
        return false;
      }
      if (!mounted || gen != _generation) return false;
      if (progress.isDone) return true;
      if (progress.isError) {
        CustomToast.show(
          progress.error.isNotEmpty
              ? progress.error
              : 'agent_installer_request_failed'.tr,
        );
        return false;
      }
      if (progress.isUnknown) {
        // 连接器装完后会清掉进度记录，之后再问只会拿到 unknown。光看进度就会一直
        // 轮到 10 分钟超时，明明已经装好了。用可安装列表交叉确认一次：装上了就收工，
        // 没装上说明只是还没开始记录，继续轮。
        if (await _isInstalledNow(item.agentType)) return true;
      }
      if (!mounted || gen != _generation) return false;
      setState(() {
        _busyProgress = progress.progress;
        if (progress.message.isNotEmpty) _busyMessage = progress.message;
      });
    }
    CustomToast.show('remote_install_timeout'.tr);
    return false;
  }

  Future<bool> _isInstalledNow(String agentType) async {
    try {
      final list = await _client.listInstallable();
      return list.agents.any(
        (item) => item.agentType == agentType && item.installed,
      );
    } catch (_) {
      // 交叉确认失败不改变判断：继续按进度轮询。
      return false;
    }
  }

  Future<void> _refreshInstalledState(String agentType) async {
    try {
      final list = await _client.listInstallable();
      if (!mounted) return;
      setState(() => _installable = list.agents);
    } catch (_) {
      // 刷新失败不影响主流程：这一行的状态下次打开时会对齐。
      if (!mounted) return;
      setState(() {
        _installable = _installable
            .map(
              (item) => item.agentType == agentType
                  ? ConnectorInstallableAgent(
                      agentType: item.agentType,
                      label: item.label,
                      description: item.description,
                      version: item.version,
                      installed: true,
                    )
                  : item,
            )
            .toList();
      });
    }
  }

  String _describe(Object error) {
    if (error is ConnectorAdminException) {
      if (error.isUnsupported) return 'remote_install_upgrade_connector'.tr;
      if (error.isOffline) return 'remote_install_host_offline'.tr;
      return error.message;
    }
    return userFacingError(error, fallback: 'ai_agents_create_failed'.tr);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 16),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.computer, size: 18, color: theme.colorScheme.primary),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    'remote_install_title'.trParams({'host': widget.hostLabel}),
                    style: theme.textTheme.titleMedium,
                  ),
                ),
                IconButton(
                  icon: const Icon(Icons.close),
                  onPressed: () => Navigator.of(context).pop(),
                ),
              ],
            ),
            if (widget.channelCandidates.length > 1) _buildChannelPicker(theme),
            const SizedBox(height: 8),
            Flexible(child: _buildBody(theme)),
          ],
        ),
      ),
    );
  }

  Widget _buildChannelPicker(ThemeData theme) {
    return Padding(
      padding: const EdgeInsets.only(top: 4),
      child: Row(
        children: [
          Text(
            'remote_install_channel'.tr,
            style: TextStyle(
              fontSize: 12,
              color: theme.colorScheme.onSurface.withValues(alpha: 0.6),
            ),
          ),
          const SizedBox(width: 8),
          Expanded(
            child: DropdownButton<String>(
              isExpanded: true,
              value: _channel.id,
              underline: const SizedBox.shrink(),
              items: widget.channelCandidates
                  .map(
                    (agent) => DropdownMenuItem<String>(
                      value: agent.id,
                      child: Text(
                        agent.agentName,
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                  )
                  .toList(),
              onChanged: _busyType.isNotEmpty
                  ? null
                  : (value) {
                      final next = widget.channelCandidates.firstWhere(
                        (agent) => agent.id == value,
                        orElse: () => _channel,
                      );
                      if (next.id == _channel.id) return;
                      setState(() => _channel = next);
                      unawaited(_loadInstallable());
                    },
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildBody(ThemeData theme) {
    if (_loading) {
      return const Padding(
        padding: EdgeInsets.symmetric(vertical: 32),
        child: Center(child: CircularProgressIndicator()),
      );
    }
    if (_loadError.isNotEmpty) {
      return Padding(
        padding: const EdgeInsets.symmetric(vertical: 24),
        child: Column(
          children: [
            Text(
              _loadError,
              textAlign: TextAlign.center,
              style: TextStyle(color: theme.colorScheme.error, fontSize: 13),
            ),
            const SizedBox(height: 12),
            OutlinedButton.icon(
              onPressed: _loadInstallable,
              icon: const Icon(Icons.refresh, size: 18),
              label: Text('common_retry'.tr),
            ),
          ],
        ),
      );
    }
    if (_installable.isEmpty) {
      return Padding(
        padding: const EdgeInsets.symmetric(vertical: 32),
        child: Center(
          child: Text(
            'remote_install_empty'.tr,
            style: TextStyle(
              fontSize: 13,
              color: theme.colorScheme.onSurface.withValues(alpha: 0.6),
            ),
          ),
        ),
      );
    }
    return ListView.builder(
      shrinkWrap: true,
      itemCount: _installable.length,
      itemBuilder: (context, index) => _buildRow(theme, _installable[index]),
    );
  }

  Widget _buildRow(ThemeData theme, ConnectorInstallableAgent item) {
    final busy = _busyType == item.agentType;
    final meta = systemAgentClientTypeMeta(item.agentType);
    final blocked = _busyType.isNotEmpty && !busy;
    return ListTile(
      key: Key('remote-install-${item.agentType}'),
      contentPadding: EdgeInsets.zero,
      enabled: !blocked,
      title: Text(meta?.label ?? item.label),
      subtitle: busy && _busyMessage.isNotEmpty
          ? Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(_busyMessage, style: const TextStyle(fontSize: 12)),
                const SizedBox(height: 4),
                LinearProgressIndicator(value: _busyProgress),
              ],
            )
          : Text(
              item.installed
                  ? 'remote_install_installed'.tr
                  : 'remote_install_not_installed'.tr,
              style: TextStyle(
                fontSize: 12,
                color: theme.colorScheme.onSurface.withValues(alpha: 0.6),
              ),
            ),
      trailing: busy
          ? const SizedBox(
              width: 20,
              height: 20,
              child: CircularProgressIndicator(strokeWidth: 2),
            )
          : Icon(
              item.installed ? Icons.add_rounded : Icons.download_rounded,
              size: 20,
            ),
      onTap: blocked || busy ? null : () => _installAndCreate(item),
    );
  }
}
