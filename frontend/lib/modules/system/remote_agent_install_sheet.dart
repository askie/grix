import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_svg/flutter_svg.dart';
import 'package:get/get.dart';

import '../../data/models/connector_admin_model.dart';
import '../../data/providers/agent_service.dart';
import '../../data/providers/im_service.dart';
import '../../data/providers/session_service.dart';
import '../../shared/utils/toast_util.dart';
import '../../shared/widgets/app_dialog_style.dart';
import '../chat/services/chat_route_navigator.dart';
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

/// 进度回 error 时连接器不给错误码，求助消息里要有个能说清是哪一段失败的标识。
const String _installErrorCode = 'install_error';

/// 一次安装失败的现场：错误码、给用户看的错误文案、连接器的输出尾巴。
class _InstallFailure {
  const _InstallFailure({
    required this.code,
    required this.message,
    this.outputTail = '',
  });

  final String code;
  final String message;
  final String outputTail;
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
  /// 只拿还在线、且连接器声明了 connector_admin 的候选：离线的当不了通道，
  /// 列出来只会让用户选中一个必然失败的 agent。调用方已按同样口径过滤，
  /// 这里再兜一次，免得列表在弹窗打开期间被别处复用时口径走样。
  late final List<AgentModel> _channels = widget.channelCandidates
      .where((agent) => agent.online && agent.supportsConnectorAdmin)
      .toList();

  late AgentModel _channel =
      (_channels.isEmpty ? widget.channelCandidates : _channels).first;

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
      final failure = await _runInstall(item, gen);
      if (!mounted || gen != _generation) return;
      if (failure != null) {
        setState(() {
          _busyType = '';
          _busyMessage = '';
          _busyProgress = null;
        });
        await _offerAgentHelp(item, failure);
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
  /// install_progress 拿；返回 null 表示装完，否则是这次失败的现场。
  ///
  /// 取消（弹窗关掉 / 换了通道）同样返回 null，但调用方在 await 之后先做
  /// mounted+代数判断再看返回值，取消不会被当成装完。
  Future<_InstallFailure?> _runInstall(
    ConnectorInstallableAgent item,
    int gen,
  ) async {
    try {
      await _client.install(item.agentType);
    } catch (e) {
      CustomToast.show(_describe(e));
      return _failureOf(e);
    }

    final deadline = DateTime.now().add(const Duration(minutes: 10));
    while (DateTime.now().isBefore(deadline)) {
      await _sleepBetweenPolls();
      if (!mounted || gen != _generation) return null;
      ConnectorInstallProgress progress;
      try {
        progress = await _client.installProgress(item.agentType);
      } catch (e) {
        CustomToast.show(_describe(e));
        return _failureOf(e);
      }
      if (!mounted || gen != _generation) return null;
      if (progress.isDone) return null;
      if (progress.isError) {
        final message = progress.error.isNotEmpty
            ? progress.error
            : 'agent_installer_request_failed'.tr;
        CustomToast.show(message);
        return _InstallFailure(
          code: _installErrorCode,
          message: message,
          outputTail: progress.outputTail.isNotEmpty
              ? progress.outputTail
              : progress.message,
        );
      }
      if (progress.isUnknown) {
        // 连接器装完后会清掉进度记录，之后再问只会拿到 unknown。光看进度就会一直
        // 轮到 10 分钟超时，明明已经装好了。用可安装列表交叉确认一次：装上了就收工，
        // 没装上说明只是还没开始记录，继续轮。
        if (await _isInstalledNow(item.agentType)) return null;
      }
      if (!mounted || gen != _generation) return null;
      setState(() {
        _busyProgress = progress.progress;
        if (progress.message.isNotEmpty) _busyMessage = progress.message;
      });
    }
    final timeout = 'remote_install_timeout'.tr;
    CustomToast.show(timeout);
    return _InstallFailure(
      code: ConnectorAdminErrorCode.timeout,
      message: timeout,
    );
  }

  _InstallFailure _failureOf(Object error) => _InstallFailure(
    code: error is ConnectorAdminException ? error.code : '',
    message: _describe(error),
  );

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

  /// 安装失败之后问一句要不要让通道 agent 接手：这台机器上就它能动手，
  /// 用户在手机上除了看错误码什么也做不了。确认之后跳到与它的会话，
  /// 并以主人身份把现场（在干什么、命令、前置依赖、错误）一次说清。
  Future<void> _offerAgentHelp(
    ConnectorInstallableAgent item,
    _InstallFailure failure,
  ) async {
    final channel = _channel;
    final confirmed = await showAppDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('remote_install_help_title'.tr),
        content: Text(
          'remote_install_help_confirm'.trParams({'agent': channel.agentName}),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: Text('common_cancel'.tr),
          ),
          FilledButton(
            key: const Key('remote-install-help-confirm'),
            onPressed: () => Navigator.pop(ctx, true),
            child: Text('remote_install_help_action'.tr),
          ),
        ],
      ),
    );
    if (confirmed != true) return;

    final text = _helpMessage(item, failure);
    // 后面这一段不再依赖本 widget：先关掉安装弹窗，再开会话、发消息。
    if (mounted) Navigator.of(context).pop();

    // 弹窗已经关掉，这一段再出错就没有 UI 能承接了，只能吞掉并给个 toast，
    // 否则会变成一次没人看得见的未捕获异常。
    try {
      final sessionId = await Get.find<SessionService>().openLatestSession(
        channel.id,
        2,
      );
      if (sessionId == null || sessionId.isEmpty) {
        CustomToast.show(
          'remote_install_help_open_failed'.trParams({
            'agent': channel.agentName,
          }),
        );
        return;
      }
      unawaited(
        ChatRouteNavigator.toChat(
          sessionId: sessionId,
          title: channel.agentName,
          type: 'private',
        ),
      );
      await Get.find<ImService>().sendMessage(text, sessionId);
    } catch (e) {
      CustomToast.show(
        'remote_install_help_open_failed'.trParams({
          'agent': channel.agentName,
        }),
      );
      debugPrint('remote install help handoff failed: $e');
    }
  }

  String _helpMessage(ConnectorInstallableAgent item, _InstallFailure failure) {
    final meta = systemAgentClientTypeMeta(item.agentType);
    final none = 'remote_install_help_none'.tr;
    String orNone(String value) => value.trim().isEmpty ? none : value.trim();
    return 'remote_install_help_message'.trParams({
      'host': widget.hostLabel,
      'client': meta?.label ?? item.label,
      'command': orNone(item.installCommand),
      'prereq': orNone(item.prerequisites.join(', ')),
      'code': orNone(failure.code),
      'error': orNone(failure.message),
      'tail': orNone(failure.outputTail),
    });
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
                Icon(
                  Icons.computer,
                  size: 18,
                  color: theme.colorScheme.primary,
                ),
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
            if (_channels.isNotEmpty) _buildChannelPicker(theme),
            const SizedBox(height: 8),
            Flexible(child: _buildBody(theme)),
          ],
        ),
      ),
    );
  }

  /// 通道 agent 这一行：点开是自己的底部 sheet。系统下拉菜单只能给一串纯名字，
  /// 看不出这个 agent 是什么客户端、在不在线，选错了要等到下发失败才知道。
  Widget _buildChannelPicker(ThemeData theme) {
    final meta = systemAgentClientTypeMeta(_channel.agentClientType);
    final muted = theme.colorScheme.onSurface.withValues(alpha: 0.6);
    return Padding(
      padding: const EdgeInsets.only(top: 4),
      child: InkWell(
        key: const Key('remote-install-channel-picker'),
        onTap: _busyType.isNotEmpty ? null : _pickChannel,
        borderRadius: BorderRadius.circular(8),
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 8),
          child: Row(
            children: [
              Text(
                'remote_install_channel'.tr,
                style: TextStyle(fontSize: 12, color: muted),
              ),
              const SizedBox(width: 8),
              if (meta != null) ...[
                _ClientTypeLogo(meta: meta, size: 18),
                const SizedBox(width: 6),
              ],
              Expanded(
                child: Text(
                  _channel.agentName,
                  overflow: TextOverflow.ellipsis,
                  style: theme.textTheme.bodyMedium,
                ),
              ),
              const SizedBox(width: 8),
              _OnlineDot(online: _channel.online),
              Icon(Icons.keyboard_arrow_down_rounded, size: 20, color: muted),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _pickChannel() async {
    final picked = await showModalBottomSheet<AgentModel>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      builder: (ctx) =>
          _ChannelPickerSheet(candidates: _channels, selectedId: _channel.id),
    );
    if (!mounted || picked == null || picked.id == _channel.id) return;
    setState(() => _channel = picked);
    unawaited(_loadInstallable());
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

/// 通道 agent 选择器的内容：一行一个候选，名字 + 客户端类型 + 在线点 + 选中勾。
class _ChannelPickerSheet extends StatelessWidget {
  const _ChannelPickerSheet({
    required this.candidates,
    required this.selectedId,
  });

  final List<AgentModel> candidates;
  final String selectedId;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SafeArea(
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxHeight: MediaQuery.of(context).size.height * 0.6,
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
              child: Text(
                'remote_install_channel_picker_title'.tr,
                style: theme.textTheme.titleMedium,
              ),
            ),
            Flexible(
              child: ListView.builder(
                shrinkWrap: true,
                itemCount: candidates.length,
                itemBuilder: (context, index) {
                  final agent = candidates[index];
                  final meta = systemAgentClientTypeMeta(agent.agentClientType);
                  final selected = agent.id == selectedId;
                  return ListTile(
                    key: Key('remote-install-channel-${agent.id}'),
                    leading: meta == null
                        ? const Icon(Icons.smart_toy_outlined, size: 24)
                        : _ClientTypeLogo(meta: meta, size: 24),
                    title: Text(
                      agent.agentName,
                      overflow: TextOverflow.ellipsis,
                    ),
                    subtitle: Text(
                      meta?.label ?? agent.agentClientType,
                      style: TextStyle(
                        fontSize: 12,
                        color: theme.colorScheme.onSurface.withValues(
                          alpha: 0.6,
                        ),
                      ),
                    ),
                    trailing: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        _OnlineDot(online: agent.online),
                        if (selected)
                          Icon(
                            Icons.check_rounded,
                            size: 20,
                            color: theme.colorScheme.primary,
                          ),
                      ],
                    ),
                    onTap: () => Navigator.of(context).pop(agent),
                  );
                },
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// 在线状态点。这里的候选都该是在线的，离线只会出现在数据刚变、列表还没刷新时。
class _OnlineDot extends StatelessWidget {
  const _OnlineDot({required this.online});

  final bool online;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      width: 8,
      height: 8,
      margin: const EdgeInsets.only(right: 8),
      decoration: BoxDecoration(
        shape: BoxShape.circle,
        color: online
            ? Colors.green
            : theme.colorScheme.onSurface.withValues(alpha: 0.3),
      ),
    );
  }
}

/// 客户端类型图标。SVG 先按 96px 画再缩到目标尺寸：macOS Impeller 直接以很小的
/// 像素尺寸栅格化复杂 SVG 会出问题，与 agent_client_toolbar_view 的做法一致。
class _ClientTypeLogo extends StatelessWidget {
  const _ClientTypeLogo({required this.meta, required this.size});

  final AgentClientTypeMeta meta;
  final double size;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    if (!meta.logoAsset.endsWith('.svg')) {
      return Image.asset(
        meta.logoAsset,
        width: size,
        height: size,
        fit: BoxFit.contain,
      );
    }
    return SizedBox(
      width: size,
      height: size,
      child: FittedBox(
        fit: BoxFit.contain,
        child: SvgPicture.asset(
          meta.logoAsset,
          width: 96,
          height: 96,
          fit: BoxFit.contain,
          colorFilter: meta.monochrome
              ? ColorFilter.mode(theme.colorScheme.onSurface, BlendMode.srcIn)
              : null,
        ),
      ),
    );
  }
}
