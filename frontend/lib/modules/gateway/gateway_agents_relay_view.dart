import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../data/providers/gateway_service.dart';
import '../../shared/utils/toast_util.dart';
import 'gateway_model_picker_view.dart';

/// 移动端 Agent 模型设置列表页（M4，设计 §3.5）。
///
/// 每行：Agent 名 + client_type 标签 + Switch（真值 = 服务端 enabled/desired）+
/// 四态副文案（与桌面 M3 §2.5 同语义同文案）+ "模型：xxx"行（可点进模型选择页
/// 换模型）。开关/换模型只走服务端 desired（GatewayService.setAgentRelay，
/// POST /agents/:id/relay）——移动端没有本地连接器，不存在桌面端的降级两段式：
/// 在线 agent 由服务端即时下发，离线的上线后 sync 对齐。
///
/// 交互同构桌面 _setRelayViaServer：乐观更新 → 409 自动刷新重试 / 400 need_model
/// 引导选模型重试一次 / 503 flag 关闭刷新回落。
class GatewayAgentsRelayView extends StatefulWidget {
  const GatewayAgentsRelayView({super.key});

  @override
  State<GatewayAgentsRelayView> createState() => _GatewayAgentsRelayViewState();
}

class _GatewayAgentsRelayViewState extends State<GatewayAgentsRelayView> {
  /// GatewayService 在工程里惯例是懒注册（见 desktop 面板），这里同样兜底。
  final GatewayService _service = Get.isRegistered<GatewayService>()
      ? Get.find<GatewayService>()
      : Get.put(GatewayService());

  List<GatewayAgentRelayStateModel> _agents = const [];
  GatewayRelaySettingsModel? _settings;
  bool _loading = true;

  /// 未启用时换模型只本地暂存，等"启用"时一并带下去（同桌面 _agentModelPick）。
  final Map<String, String> _agentModelPick = {};

  /// 各 agent 乐观锁 revision：来自写操作应答（GET 列表不带 revision，
  /// 首次写走 last-write-wins，之后带 expected_revision）。
  final Map<String, int> _agentRevisions = {};
  final Set<String> _toggling = {};

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final results = await Future.wait([
      _service.listAgents(),
      _service.getRelaySettings(),
    ]);
    if (!mounted) return;
    setState(() {
      // 接不了中转的类型（BYOK/绑定自己账号）不展示——开关对它无意义。
      _agents = (results[0] as List<GatewayAgentRelayStateModel>)
          .where((a) => a.supported)
          .toList();
      _settings = results[1] as GatewayRelaySettingsModel?;
      _loading = false;
    });
  }

  static bool _isNativeProviderType(String clientType) =>
      GatewayService.nativeProviderClientTypes.contains(clientType);

  /// 该 agent 当前生效/待生效的中转模型：本地刚选的 > 服务端回显的上次选择 > 兜底模型。
  String _effectiveModelFor(GatewayAgentRelayStateModel a) {
    final pick = _agentModelPick[a.agentId];
    if (pick != null && pick.isNotEmpty) return pick;
    if (a.relayModel.isNotEmpty) return a.relayModel;
    return _settings?.defaultModel ?? '';
  }

  Future<void> _toggleAgentRelay(
    GatewayAgentRelayStateModel agent,
    bool enable,
  ) async {
    if (_toggling.contains(agent.agentId)) return;
    setState(() => _toggling.add(agent.agentId));
    final ok = await _setRelayViaServer(agent, enable);
    if (!mounted) return;
    setState(() => _toggling.remove(agent.agentId));
    if (ok) await _load();
  }

  /// 点"模型：xxx"行：进同款模型选择页换模型。已启用时选完立即写服务端 desired
  /// （POST {enabled:true, model:新值}，服务端重签下发）；未启用时只本地暂存，
  /// 等"启用"时一并带下去（同桌面 _pickAgentModel 的服务端路径）。
  Future<bool> _pickAgentModel(
    GatewayAgentRelayStateModel agent,
    String model, {
    required bool relayOn,
  }) async {
    if (!relayOn) {
      setState(() => _agentModelPick[agent.agentId] = model);
      return true;
    }
    if (_toggling.contains(agent.agentId)) return false;
    setState(() => _toggling.add(agent.agentId));
    final ok = await _setRelayViaServer(agent, true, model: model);
    if (!mounted) return false;
    setState(() => _toggling.remove(agent.agentId));
    if (ok) await _load();
    return ok;
  }

  /// 写服务端 desired。乐观更新（Switch 立即反映期望态，写时 applied 先置 false
  /// 进入"待生效"），失败/冲突再刷新回滚（桌面 _setRelayViaServer 的移动端移植，
  /// 去掉不存在的连接器降级路径）。
  Future<bool> _setRelayViaServer(
    GatewayAgentRelayStateModel agent,
    bool enable, {
    String? model,
    bool modelRetried = false,
  }) async {
    // 原生配置类型开启必须带模型（服务端 400 need_model）：先按现有逻辑解析，
    // 解析不出（无暂存、无回显、无兜底）时引导进模型选择页。
    var effectiveModel = model;
    if (enable && (effectiveModel == null || effectiveModel.isEmpty)) {
      final pick = _agentModelPick[agent.agentId];
      if (pick != null && pick.isNotEmpty) {
        effectiveModel = pick;
      } else if (_isNativeProviderType(agent.clientType)) {
        final resolved = _effectiveModelFor(agent);
        if (resolved.isNotEmpty) {
          effectiveModel = resolved;
        } else {
          final picked = await _showAgentModelPicker(agent, relayOn: enable);
          if (picked == null || picked.isEmpty) {
            CustomToast.show('gateway_relay_need_model'.tr);
            return false;
          }
          _agentModelPick[agent.agentId] = picked;
          effectiveModel = picked;
        }
      }
    }

    // 乐观更新：desired 变更即让旧 actual 失效（服务端同步把 applied 置 false、
    // applied_at 置空）——显式重建而不是 copyWith，不留中间态。
    final index = _agents.indexWhere((e) => e.agentId == agent.agentId);
    if (index >= 0) {
      setState(() {
        final cur = _agents[index];
        final next = [..._agents];
        next[index] = GatewayAgentRelayStateModel(
          agentId: cur.agentId,
          agentName: cur.agentName,
          clientType: cur.clientType,
          supported: cur.supported,
          configured: cur.configured,
          hostName: cur.hostName,
          relayModel: effectiveModel ?? cur.relayModel,
          enabled: enable,
          applied: false,
          stateKnown: cur.stateKnown,
        );
        _agents = next;
      });
    }

    final r = await _service.setAgentRelay(
      agent.agentId,
      enabled: enable,
      model: effectiveModel,
      expectedRevision: _agentRevisions[agent.agentId],
    );
    if (!mounted) return false;
    switch (r.status) {
      case GatewaySetRelayStatus.ok:
        if (r.state != null) _agentRevisions[agent.agentId] = r.state!.revision;
        _agentModelPick.remove(agent.agentId); // 已落服务端，回显以服务端为准
        CustomToast.show(
          enable
              ? (effectiveModel == null || effectiveModel.isEmpty
                    ? 'gateway_relay_enabled_pending'.trParams({
                        'name': agent.agentName,
                      })
                    : 'gateway_relay_enabled_with_model_pending'.trParams({
                        'name': agent.agentName,
                        'model': effectiveModel,
                      }))
              : 'gateway_relay_disabled_pending'.trParams({
                  'name': agent.agentName,
                }),
          isError: false,
        );
        return true;
      case GatewaySetRelayStatus.conflict:
        if (r.state != null) _agentRevisions[agent.agentId] = r.state!.revision;
        // 自动刷新最新 state（连带回滚乐观更新），提示用户按最新状态重试。
        await _load();
        CustomToast.show('gateway_relay_conflict_retry'.tr);
        return false;
      case GatewaySetRelayStatus.needModel:
        if (!modelRetried) {
          final picked = await _showAgentModelPicker(agent, relayOn: enable);
          if (picked != null && picked.isNotEmpty) {
            _agentModelPick[agent.agentId] = picked;
            return _setRelayViaServer(
              agent,
              enable,
              model: picked,
              modelRetried: true,
            );
          }
        }
        CustomToast.show('gateway_relay_need_model'.tr);
        await _load();
        return false;
      case GatewaySetRelayStatus.disabled:
        // 服务端 flag 被关：GET 回落旧语义（扩展字段缺席），刷新后开关禁用。
        CustomToast.show('gateway_relay_server_unsupported'.tr);
        await _load();
        return false;
      case GatewaySetRelayStatus.failed:
        CustomToast.show(
          enable
              ? 'gateway_relay_enable_apply_failed'.tr
              : 'gateway_relay_disable_failed'.tr,
        );
        await _load(); // 回滚乐观更新
        return false;
    }
  }

  /// 推全屏模型选择页并等待选择结果（用于 need_model 引导等"只取一个模型名"
  /// 的场景）；换模型的主路径在列表行内直接推页，不走这里。
  Future<String?> _showAgentModelPicker(
    GatewayAgentRelayStateModel agent, {
    required bool relayOn,
  }) async {
    String? picked;
    await Get.to<void>(
      () => GatewayModelPickerView(
        title: 'gateway_relay_pick_agent_model'.trParams({
          'name': _agentPickerName(agent),
        }),
        currentModel: _effectiveModelFor(agent),
        onSave: (model) async {
          picked = model;
          return true;
        },
      ),
    );
    return picked;
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
        title: Text(
          'gateway_model_settings_agents_title'.tr,
          style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w600),
        ),
      ),
      body: RefreshIndicator(onRefresh: _load, child: _buildBody(theme)),
    );
  }

  Widget _buildBody(ThemeData theme) {
    if (_loading) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_agents.isEmpty) {
      return ListView(
        padding: const EdgeInsets.all(24),
        children: [
          const SizedBox(height: 80),
          Text(
            'gateway_model_settings_no_agents'.tr,
            textAlign: TextAlign.center,
            style: TextStyle(color: theme.colorScheme.onSurfaceVariant),
          ),
        ],
      );
    }
    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 24),
      children: [
        for (final group in _groupByHost()) ...[
          _buildHostHeader(theme, group.key),
          for (final a in group.value) _buildAgentTile(theme, a),
        ],
      ],
    );
  }

  /// 按所在机器归类：同名 Agent 分散在多台机器时扁平列表无法区分，必须先分组。
  /// 机器名按不区分大小写的字母序排列，组内保持服务端的 created_at 顺序；
  /// 没上报过机器名的（老版本 connector / 从未连过）归入"未知设备"并置底。
  List<MapEntry<String, List<GatewayAgentRelayStateModel>>> _groupByHost() {
    final groups = <String, List<GatewayAgentRelayStateModel>>{};
    for (final a in _agents) {
      groups.putIfAbsent(a.hostName, () => []).add(a);
    }
    final hosts = groups.keys.where((h) => h.isNotEmpty).toList()
      ..sort((a, b) => a.toLowerCase().compareTo(b.toLowerCase()));
    if (groups.containsKey('')) hosts.add('');
    return [for (final h in hosts) MapEntry(h, groups[h]!)];
  }

  /// 展示用机器名：macOS 的 hostname 常带 .local 后缀（gcf-Mac-mini.local），
  /// 展示时裁掉更像"机器"；分组与匹配仍用服务端原值。
  static String _hostLabel(String host) {
    if (host.isEmpty) return 'gateway_relay_agent_host_unknown'.tr;
    const suffix = '.local';
    if (host.length > suffix.length && host.toLowerCase().endsWith(suffix)) {
      return host.substring(0, host.length - suffix.length);
    }
    return host;
  }

  /// 进模型选择页时的 Agent 标识：带上机器名，避免同名 Agent 进去后分不清在改哪个。
  String _agentPickerName(GatewayAgentRelayStateModel a) {
    if (a.hostName.isEmpty) return a.agentName;
    return '${a.agentName} · ${_hostLabel(a.hostName)}';
  }

  Widget _buildHostHeader(ThemeData theme, String host) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(0, 12, 0, 4),
      child: Row(
        children: [
          Icon(
            Icons.computer_rounded,
            size: 14,
            color: theme.colorScheme.secondary,
          ),
          const SizedBox(width: 6),
          Expanded(
            child: Text(
              _hostLabel(host),
              style: TextStyle(
                fontSize: 12,
                fontWeight: FontWeight.w600,
                color: theme.colorScheme.secondary,
                letterSpacing: 0.5,
              ),
              overflow: TextOverflow.ellipsis,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildAgentTile(ThemeData theme, GatewayAgentRelayStateModel a) {
    final busy = _toggling.contains(a.agentId);

    // Switch 真值 = 服务端 enabled（desired）。扩展字段缺席（服务端 flag 关）时
    // 移动端没有连接器名单可回落，禁用开关并如实说明，不能把未知当成"关"。
    final serverState = a.enabled != null;
    final on = a.enabled == true;
    final switchEnabled = !busy && serverState;

    // 状态行四态（设计 §2.5/§3.5，与桌面同文案）：
    // 已开启(绿) / 待生效(黄) / 设备离线或旧版连接器(蓝灰) / 已关闭(灰)。
    // 移动端读不到连接器版本，!stateKnown 统一按"设备离线"陈述。
    final String note;
    final Color noteColor;
    if (serverState) {
      final stateKnown = a.stateKnown == true;
      final applied = a.applied == true;
      if (on && applied) {
        note = 'gateway_relay_agent_on'.tr;
        noteColor = Colors.green;
      } else if (on && stateKnown) {
        note = 'gateway_relay_agent_pending'.tr;
        noteColor = Colors.amber.shade800;
      } else if (!stateKnown) {
        note = 'gateway_relay_agent_device_offline'.tr;
        noteColor = Colors.blueGrey;
      } else {
        note = 'gateway_relay_agent_off'.tr;
        noteColor = theme.colorScheme.onSurfaceVariant;
      }
    } else {
      note = 'gateway_relay_server_unsupported'.tr;
      noteColor = theme.colorScheme.onSurfaceVariant;
    }

    final effectiveModel = _effectiveModelFor(a);

    return ListTile(
      dense: true,
      contentPadding: EdgeInsets.zero,
      title: Text(
        'gateway_relay_agent_title'.trParams({
          'name': a.agentName,
          'type': a.clientType,
        }),
        style: const TextStyle(fontSize: 14),
      ),
      subtitle: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(note, style: TextStyle(fontSize: 11, color: noteColor)),
          InkWell(
            onTap: switchEnabled
                ? () => Get.to<void>(
                    () => GatewayModelPickerView(
                      title: 'gateway_relay_pick_agent_model'.trParams({
                        'name': _agentPickerName(a),
                      }),
                      currentModel: effectiveModel,
                      onSave: (model) => _pickAgentModel(a, model, relayOn: on),
                    ),
                  )
                : null,
            child: Padding(
              padding: const EdgeInsets.only(top: 2),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Flexible(
                    child: Text(
                      effectiveModel.isEmpty
                          ? 'gateway_relay_model_unset'.tr
                          : 'gateway_relay_model_selected'.trParams({
                              'model': effectiveModel,
                            }),
                      style: TextStyle(
                        fontSize: 11,
                        color: switchEnabled
                            ? theme.colorScheme.primary
                            : theme.colorScheme.onSurfaceVariant,
                        fontWeight: FontWeight.w600,
                      ),
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                  Icon(
                    Icons.chevron_right_rounded,
                    size: 14,
                    color: switchEnabled
                        ? theme.colorScheme.primary
                        : theme.colorScheme.onSurfaceVariant,
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
      trailing: busy
          ? const SizedBox(
              width: 18,
              height: 18,
              child: CircularProgressIndicator(strokeWidth: 2),
            )
          : Switch.adaptive(
              value: on,
              onChanged: switchEnabled ? (v) => _toggleAgentRelay(a, v) : null,
            ),
    );
  }
}
