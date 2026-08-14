import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/routes/app_routes.dart';
import '../../app/themes/app_theme.dart';
import '../../data/providers/gateway_service.dart';

/// "我的"页里的「模型设置」入口行（M4，设计 §3.1）：副标题回显当前默认模型名。
/// 自带一次性拉取（getRelaySettings），不污染 settings_view 的构建逻辑；
/// 从模型设置页返回时刷新一次副标题。
class GatewaySettingsEntryTile extends StatefulWidget {
  const GatewaySettingsEntryTile({super.key});

  @override
  State<GatewaySettingsEntryTile> createState() =>
      _GatewaySettingsEntryTileState();
}

class _GatewaySettingsEntryTileState extends State<GatewaySettingsEntryTile> {
  String _defaultModel = '';

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    // GatewayService 在工程里惯例是懒注册（见 desktop 面板），这里同样兜底。
    final service = Get.isRegistered<GatewayService>()
        ? Get.find<GatewayService>()
        : Get.put(GatewayService());
    final settings = await service.getRelaySettings();
    if (!mounted) return;
    setState(() => _defaultModel = settings?.defaultModel ?? '');
  }

  @override
  Widget build(BuildContext context) {
    return ListTile(
      leading: Container(
        width: 36,
        height: 36,
        decoration: BoxDecoration(
          color: AppTheme.infoColor.withValues(alpha: 0.12),
          borderRadius: BorderRadius.circular(10),
        ),
        child: const Icon(
          Icons.tune_rounded,
          color: AppTheme.infoColor,
          size: 20,
        ),
      ),
      title: Text('gateway_model_settings_title'.tr),
      subtitle: _defaultModel.isEmpty ? null : Text(_defaultModel),
      trailing: const Icon(Icons.chevron_right_rounded),
      onTap: () async {
        await Get.toNamed(AppRoutes.gatewayModelSettings);
        await _load();
      },
    );
  }
}
