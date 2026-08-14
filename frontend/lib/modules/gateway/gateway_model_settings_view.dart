import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../data/providers/gateway_service.dart';
import 'gateway_agents_relay_view.dart';
import 'gateway_default_model_view.dart';

/// 移动端「模型设置」主页（M4，设计 §3.2，v6 收窄版）：只做默认模型设置 +
/// Agent 模型设置。不做余额卡、不做消费流水、不做模型映射；不调用
/// getWallet/createTopup/listTopups/listLedger（设计 §3.0 合规红线）。
class GatewayModelSettingsView extends StatefulWidget {
  const GatewayModelSettingsView({super.key});

  @override
  State<GatewayModelSettingsView> createState() =>
      _GatewayModelSettingsViewState();
}

class _GatewayModelSettingsViewState extends State<GatewayModelSettingsView> {
  /// GatewayService 在工程里惯例是懒注册（见 desktop 面板），这里同样兜底。
  final GatewayService _service = Get.isRegistered<GatewayService>()
      ? Get.find<GatewayService>()
      : Get.put(GatewayService());

  List<GatewayModelModel> _models = const [];
  GatewayRelaySettingsModel? _settings;
  List<GatewayAgentRelayStateModel> _agents = const [];
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final results = await Future.wait([
      _service.listModels(),
      _service.getRelaySettings(),
      _service.listAgents(),
    ]);
    if (!mounted) return;
    setState(() {
      _models = results[0] as List<GatewayModelModel>;
      _settings = results[1] as GatewayRelaySettingsModel?;
      _agents = results[2] as List<GatewayAgentRelayStateModel>;
      _loading = false;
    });
  }

  GatewayModelModel? _modelInfo(String name) {
    for (final m in _models) {
      if (m.model == name) return m;
    }
    return null;
  }

  /// 默认模型行副标题：当前模型；模型被下架（不在价目表）时如实标出。
  String _defaultModelSubtitle(GatewayRelaySettingsModel settings) {
    if (settings.defaultModel.isEmpty) return 'gateway_relay_model_unset'.tr;
    final info = _modelInfo(settings.defaultModel);
    if (info == null) return 'gateway_relay_model_unavailable'.tr;
    return info.model;
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
          'gateway_model_settings_title'.tr,
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
    final settings = _settings;
    if (settings == null) {
      return ListView(
        padding: const EdgeInsets.all(24),
        children: [
          const SizedBox(height: 80),
          Text(
            'gateway_relay_settings_load_failed'.tr,
            textAlign: TextAlign.center,
            style: TextStyle(color: theme.colorScheme.onSurfaceVariant),
          ),
        ],
      );
    }

    final supportedAgents = _agents.where((a) => a.supported).toList();
    final enabledCount = supportedAgents.where((a) => a.enabled == true).length;

    return ListView(
      padding: const EdgeInsets.only(bottom: 24),
      children: [
        _buildSectionHeader(theme, 'gateway_model_settings_group_general'.tr),
        _buildSection(theme, [
          ListTile(
            title: Text('gateway_relay_default_model'.tr),
            subtitle: Text(
              _defaultModelSubtitle(settings),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),
            trailing: const Icon(Icons.chevron_right_rounded),
            onTap: () async {
              await Get.to<void>(() => const GatewayDefaultModelView());
              // 选择页可能改了默认模型，回来刷新副标题。
              await _load();
            },
          ),
        ]),

        _buildSectionHeader(theme, 'gateway_model_settings_agents_title'.tr),
        _buildSection(theme, [
          ListTile(
            title: Text('gateway_model_settings_agents_title'.tr),
            subtitle: Text(
              'gateway_model_settings_agents_summary'.trParams({
                'enabled': '$enabledCount',
                'total': '${supportedAgents.length}',
              }),
            ),
            trailing: const Icon(Icons.chevron_right_rounded),
            onTap: () async {
              await Get.to<void>(() => const GatewayAgentsRelayView());
              await _load();
            },
          ),
        ]),
      ],
    );
  }

  Widget _buildSectionHeader(ThemeData theme, String title) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
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

  Widget _buildSection(ThemeData theme, List<Widget> children) {
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
