import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/theme/app_palette.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/confirm_dialog.dart';
import 'connector_service.dart';

/// 参数通过 Get.arguments 传入：{'releaseId': '...', 'releaseVersion': '...'}
class ConnectorRolloutView extends StatefulWidget {
  const ConnectorRolloutView({super.key});

  @override
  State<ConnectorRolloutView> createState() => _ConnectorRolloutViewState();
}

class _ConnectorRolloutViewState extends State<ConnectorRolloutView> {
  late final String releaseId;
  late final String releaseVersion;
  List<ConnectorRolloutRule> _rules = [];
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    final args = Get.arguments as Map<String, dynamic>?;
    releaseId = args?['releaseId']?.toString() ?? '';
    releaseVersion = args?['releaseVersion']?.toString() ?? releaseId;
    _load();
  }

  Future<void> _load() async {
    setState(() { _loading = true; _error = null; });
    try {
      final rules = await ConnectorService.listRules(releaseId);
      setState(() => _rules = rules);
    } catch (e) {
      setState(() => _error = e.toString());
    } finally {
      setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: 'Rollout 灰度规则: $releaseVersion',
      actions: [
        IconButton(tooltip: '新建规则', onPressed: () => _create(context), icon: const Icon(Icons.add)),
        IconButton(tooltip: '刷新', onPressed: _load, icon: const Icon(Icons.refresh)),
      ],
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : _error != null
              ? Center(child: Column(mainAxisSize: MainAxisSize.min, children: [
                  Text(_error!, style: TextStyle(color: AppPalette.danger)),
                  TextButton(onPressed: _load, child: const Text('重试')),
                ]))
              : _rules.isEmpty
                  ? const Center(child: Text('暂无灰度规则'))
                  : ListView.separated(
                      padding: const EdgeInsets.all(16),
                      itemCount: _rules.length,
                      separatorBuilder: (_, _) => const SizedBox(height: 8),
                      itemBuilder: (_, i) => _RuleCard(rule: _rules[i], onChanged: _load),
                    ),
    );
  }

  Future<void> _create(BuildContext context) async {
    final typeCtrl = TextEditingController(text: 'percentage');
    final valueCtrl = TextEditingController(text: '{"percent":10}');
    final priorityCtrl = TextEditingController(text: '0');

    final confirmed = await Get.dialog<bool>(AlertDialog(
      title: const Text('新建灰度规则'),
      scrollable: true,
      content: Column(mainAxisSize: MainAxisSize.min, children: [
        TextField(controller: typeCtrl, decoration: const InputDecoration(labelText: '规则类型 (percentage / agent_list)')),
        const SizedBox(height: 8),
        TextField(controller: valueCtrl, maxLines: 3, decoration: const InputDecoration(labelText: '规则值 JSON (如 {"percent":10} 或 {"agent_ids":[123]})')),
        const SizedBox(height: 8),
        TextField(controller: priorityCtrl, decoration: const InputDecoration(labelText: '优先级'), keyboardType: TextInputType.number),
      ]),
      actions: [
        TextButton(onPressed: () => Get.back(result: false), child: const Text('取消')),
        FilledButton(onPressed: () => Get.back(result: true), child: const Text('创建')),
      ],
    ));
    if (confirmed == true) {
      dynamic ruleValue;
      try {
        ruleValue = jsonDecode(valueCtrl.text.trim());
      } catch (_) {
        ruleValue = null;
      }
      if (ruleValue is! Map) {
        Toast.error('规则值必须是合法 JSON 对象，例如 {"percent":10}');
      } else {
        try {
          await ConnectorService.createRule({
            'release_id': releaseId,
            'rule_type': typeCtrl.text.trim(),
            'rule_value': ruleValue,
            'priority': int.tryParse(priorityCtrl.text) ?? 0,
          });
          Toast.success('规则已创建');
          await _load();
        } catch (e) {
          Toast.error(e.toString());
        }
      }
    }
    for (final c in [typeCtrl, valueCtrl, priorityCtrl]) { c.dispose(); }
  }
}

class _RuleCard extends StatelessWidget {
  const _RuleCard({required this.rule, required this.onChanged});
  final ConnectorRolloutRule rule;
  final VoidCallback onChanged;

  @override
  Widget build(BuildContext context) {
    final active = rule.isActive;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Row(children: [
          Expanded(child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text(rule.ruleType, style: Theme.of(context).textTheme.titleSmall),
            const SizedBox(height: 2),
            Text('值: ${rule.ruleValue} · 优先级: ${rule.priority}',
                style: Theme.of(context).textTheme.bodySmall, overflow: TextOverflow.ellipsis),
          ])),
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
            decoration: BoxDecoration(
              color: active ? AppPalette.successSoft : AppPalette.border,
              borderRadius: BorderRadius.circular(6),
            ),
            child: Text(rule.statusLabel,
                style: TextStyle(fontSize: 12, color: active ? AppPalette.success : AppPalette.textSecondary, fontWeight: FontWeight.w600)),
          ),
          const SizedBox(width: 8),
          PopupMenuButton<String>(
            onSelected: (v) => _handleAction(v),
            itemBuilder: (_) => [
              PopupMenuItem(value: 'toggle', child: Text(active ? '禁用' : '启用')),
              const PopupMenuItem(value: 'delete', child: Text('删除', style: TextStyle(color: Colors.red))),
            ],
          ),
        ]),
      ),
    );
  }

  Future<void> _handleAction(String action) async {
    if (action == 'toggle') {
      try {
        await ConnectorService.toggleRule(rule.id, rule.isActive ? 0 : 1);
        Toast.success('已更新');
        onChanged();
      } catch (e) { Toast.error(e.toString()); }
    } else if (action == 'delete') {
      final ok = await ConfirmDialog.show(title: '删除规则', message: '确定删除此灰度规则吗？', confirmText: '删除', danger: true);
      if (!ok) return;
      try {
        await ConnectorService.deleteRule(rule.id);
        Toast.success('已删除');
        onChanged();
      } catch (e) { Toast.error(e.toString()); }
    }
  }
}
