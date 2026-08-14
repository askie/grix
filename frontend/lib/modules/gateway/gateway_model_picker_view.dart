import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../data/providers/gateway_service.dart';

/// 移动端全屏模型选择页（M4，设计 §3.3）：默认模型选择与 Agent 换模型共用。
///
/// 点选即保存：选中态先乐观打勾，[onSave] 返回 false 时还原选中（提示文案由
/// onSave 内部负责）；保存成功即返回上一页。空清单展示 gateway_relay_no_models。
///
/// 合规红线（设计 §3.0）：本页是移动端独立实现，不得 import 桌面面板
/// （gateway_relay_panel_view.dart），不得引用任何充值相关 i18n key。
class GatewayModelPickerView extends StatefulWidget {
  const GatewayModelPickerView({
    super.key,
    required this.title,
    required this.currentModel,
    required this.onSave,
    this.hint,
  });

  /// 页面标题（如 gateway_relay_pick_default_model / gateway_relay_pick_agent_model）。
  final String title;

  /// 当前生效的模型名，进页时打勾的那一行。
  final String currentModel;

  /// 点选某一行的保存动作；返回 false 时选择页还原选中态。
  final Future<bool> Function(String model) onSave;

  /// 顶部说明文案（默认模型选择页传 gateway_relay_default_model_hint）。
  final String? hint;

  @override
  State<GatewayModelPickerView> createState() => _GatewayModelPickerViewState();
}

class _GatewayModelPickerViewState extends State<GatewayModelPickerView> {
  /// GatewayService 在工程里惯例是懒注册（见 desktop 面板），这里同样兜底。
  final GatewayService _service = Get.isRegistered<GatewayService>()
      ? Get.find<GatewayService>()
      : Get.put(GatewayService());

  List<GatewayModelModel> _models = const [];
  bool _loading = true;
  bool _saving = false;
  late String _selected = widget.currentModel;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final models = await _service.listModels();
    if (!mounted) return;
    setState(() {
      _models = models;
      _loading = false;
    });
  }

  Future<void> _select(String model) async {
    if (_saving || model == _selected) return;
    final previous = _selected;
    setState(() {
      _selected = model;
      _saving = true;
    });
    final ok = await widget.onSave(model);
    if (!mounted) return;
    if (ok) {
      Get.back();
      return;
    }
    setState(() {
      _selected = previous;
      _saving = false;
    });
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
          widget.title,
          style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w600),
        ),
      ),
      body: _buildBody(theme),
    );
  }

  Widget _buildBody(ThemeData theme) {
    if (_loading) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_models.isEmpty) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Text(
            'gateway_relay_no_models'.tr,
            textAlign: TextAlign.center,
            style: TextStyle(color: theme.colorScheme.onSurfaceVariant),
          ),
        ),
      );
    }
    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 24),
      children: [
        if (widget.hint != null) ...[
          Text(
            widget.hint!,
            style: TextStyle(
              fontSize: 11,
              height: 1.5,
              color: theme.colorScheme.onSurfaceVariant.withValues(alpha: 0.8),
            ),
          ),
          const SizedBox(height: 12),
        ],
        for (final m in _models)
          ListTile(
            dense: true,
            contentPadding: EdgeInsets.zero,
            title: Text(m.model, style: const TextStyle(fontSize: 14)),
            subtitle: m.provider.isEmpty
                ? null
                : Text(
                    m.provider,
                    style: TextStyle(
                      fontSize: 12,
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
            trailing: _selected == m.model
                ? Icon(Icons.check_rounded, color: theme.colorScheme.primary)
                : null,
            onTap: () => _select(m.model),
          ),
      ],
    );
  }
}
