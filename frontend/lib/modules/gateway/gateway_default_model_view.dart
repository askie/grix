import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../data/providers/gateway_service.dart';
import '../../shared/utils/toast_util.dart';
import 'gateway_model_picker_view.dart';

/// 移动端默认模型选择页（M4，设计 §3.3）：全屏单选，点选即整体保存
/// PUT /v1/gateway/relay-settings（保留现有 model_map 不动），成功 toast
/// gateway_relay_saved，失败 toast 并由选择页还原选中态。
class GatewayDefaultModelView extends StatefulWidget {
  const GatewayDefaultModelView({super.key});

  @override
  State<GatewayDefaultModelView> createState() =>
      _GatewayDefaultModelViewState();
}

class _GatewayDefaultModelViewState extends State<GatewayDefaultModelView> {
  /// GatewayService 在工程里惯例是懒注册（见 desktop 面板），这里同样兜底。
  final GatewayService _service = Get.isRegistered<GatewayService>()
      ? Get.find<GatewayService>()
      : Get.put(GatewayService());

  GatewayRelaySettingsModel? _settings;
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final settings = await _service.getRelaySettings();
    if (!mounted) return;
    setState(() {
      _settings = settings;
      _loading = false;
    });
  }

  Future<bool> _save(String model) async {
    final settings = _settings;
    if (settings == null) return false;
    final ok = await _service.putRelaySettings(
      defaultModel: model,
      // 整体保存语义：映射表原样带上，本页只改兜底模型。
      modelMap: settings.modelMap,
    );
    if (ok) {
      CustomToast.show('gateway_relay_saved'.tr, isError: false);
    } else {
      CustomToast.show('gateway_relay_save_failed'.tr);
    }
    return ok;
  }

  @override
  Widget build(BuildContext context) {
    if (_loading) {
      return Scaffold(
        appBar: AppBar(title: Text('gateway_relay_default_model'.tr)),
        body: const Center(child: CircularProgressIndicator()),
      );
    }
    final settings = _settings;
    if (settings == null) {
      return Scaffold(
        appBar: AppBar(title: Text('gateway_relay_default_model'.tr)),
        body: Center(
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Text(
              'gateway_relay_settings_load_failed'.tr,
              textAlign: TextAlign.center,
              style: TextStyle(
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            ),
          ),
        ),
      );
    }
    return GatewayModelPickerView(
      title: 'gateway_relay_default_model'.tr,
      hint: 'gateway_relay_default_model_hint'.tr,
      currentModel: settings.defaultModel,
      onSave: _save,
    );
  }
}
