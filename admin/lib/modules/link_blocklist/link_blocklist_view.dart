import 'dart:convert';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/routes/app_routes.dart';
import '../../app/theme/app_palette.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/dialog_content_box.dart';
import '../../shared/widgets/infinite_list_view.dart';
import 'link_blocklist_controller.dart';
import 'link_blocklist_models.dart';

/// 链接黑名单管理页：规则列表 + 筛选 + CRUD + 在线测试。
class LinkBlocklistView extends GetView<LinkBlocklistController> {
  const LinkBlocklistView({super.key});

  @override
  Widget build(BuildContext context) {
    final bool compact = MediaQuery.of(context).size.width < 600;
    return AdminScaffold(
      title: '链接黑名单',
      actions: [
        IconButton(
          tooltip: '批量导入 CSV',
          onPressed: () => _showImportDialog(context),
          icon: const Icon(Icons.upload_file_outlined),
        ),
        IconButton(
          tooltip: '在线测试',
          onPressed: () => _showTestDialog(context),
          icon: const Icon(Icons.bug_report_outlined),
        ),
        IconButton(
          tooltip: '设置',
          onPressed: () => Get.toNamed(AppRoutes.linkBlocklistSettings),
          icon: const Icon(Icons.tune),
        ),
        IconButton(
          tooltip: '刷新',
          onPressed: () {
            controller.reload();
            controller.refreshStats();
          },
          icon: const Icon(Icons.refresh),
        ),
        // 窄屏只显示加号，宽屏显示带文字的按钮。
        if (compact)
          IconButton(
            tooltip: '新建规则',
            onPressed: () => _showEditDialog(context, null),
            icon: const Icon(Icons.add),
          )
        else
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 8),
            child: FilledButton.icon(
              icon: const Icon(Icons.add),
              label: const Text('新建规则'),
              onPressed: () => _showEditDialog(context, null),
            ),
          ),
      ],
      body: Column(
        children: [
          Obx(() => _StatsStrip(stats: controller.stats.value)),
          _Toolbar(c: controller, compact: compact),
          const Divider(height: 1),
          Expanded(
            child: InfiniteListView<LinkBlocklistRule>(
              controller: controller,
              emptyText: '暂无规则',
              itemBuilder: (_, rule, _) => LinkBlocklistRuleCard(
                rule: rule,
                onEdit: () => _showEditDialog(context, rule),
                onToggleEnabled: () => controller.toggleEnabled(rule),
                onDelete: () => controller.deleteRule(rule),
              ),
            ),
          ),
        ],
      ),
    );
  }

  void _showImportDialog(BuildContext context) {
    Get.dialog(_ImportDialog(controller: controller));
  }

  void _showEditDialog(BuildContext context, LinkBlocklistRule? r) {
    Get.dialog(
      _RuleEditDialog(initial: r, controller: controller),
      barrierDismissible: false,
    );
  }

  void _showTestDialog(BuildContext context) {
    Get.dialog(_TestDialog(controller: controller));
  }
}

class _Toolbar extends StatelessWidget {
  const _Toolbar({required this.c, this.compact = false});
  final LinkBlocklistController c;
  final bool compact;

  Widget _search() => TextField(
    controller: c.searchCtrl,
    decoration: InputDecoration(
      hintText: '搜索 规则值 / 备注',
      prefixIcon: const Icon(Icons.search),
      isDense: true,
      suffixIcon: IconButton(
        icon: const Icon(Icons.arrow_forward),
        onPressed: () => c.applySearch(c.searchCtrl.text),
      ),
    ),
    onSubmitted: c.applySearch,
  );

  Widget _kindFilter() => Obx(
    () => DropdownButton<String>(
      value: c.kindFilter.value.isEmpty ? null : c.kindFilter.value,
      hint: const Text('类型'),
      isDense: compact,
      items: <DropdownMenuItem<String>>[
        const DropdownMenuItem(value: '', child: Text('全部类型')),
        for (final k in kLinkRuleKinds)
          DropdownMenuItem(value: k, child: Text(k)),
      ],
      onChanged: (v) => c.setKindFilter(v ?? ''),
    ),
  );

  Widget _severityFilter() => Obx(
    () => DropdownButton<String>(
      value: c.severityFilter.value.isEmpty ? null : c.severityFilter.value,
      hint: const Text('严重度'),
      isDense: compact,
      items: <DropdownMenuItem<String>>[
        const DropdownMenuItem(value: '', child: Text('全部严重度')),
        for (final s in kLinkRuleSeverities)
          DropdownMenuItem(value: s, child: Text(s)),
      ],
      onChanged: (v) => c.setSeverityFilter(v ?? ''),
    ),
  );

  Widget _enabledFilter() => Obx(
    () => DropdownButton<bool?>(
      value: c.enabledFilter.value,
      hint: const Text('状态'),
      isDense: compact,
      items: const <DropdownMenuItem<bool?>>[
        DropdownMenuItem(value: null, child: Text('全部状态')),
        DropdownMenuItem(value: true, child: Text('启用')),
        DropdownMenuItem(value: false, child: Text('禁用')),
      ],
      onChanged: c.setEnabledFilter,
    ),
  );

  @override
  Widget build(BuildContext context) {
    if (compact) {
      // 窄屏：搜索独占一行，筛选项紧凑换行排列。
      return Padding(
        padding: const EdgeInsets.fromLTRB(12, 8, 12, 8),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            _search(),
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              runSpacing: 4,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [_kindFilter(), _severityFilter(), _enabledFilter()],
            ),
          ],
        ),
      );
    }
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
      child: Wrap(
        runSpacing: 8,
        spacing: 12,
        crossAxisAlignment: WrapCrossAlignment.center,
        children: [
          SizedBox(width: 280, child: _search()),
          _kindFilter(),
          _severityFilter(),
          _enabledFilter(),
        ],
      ),
    );
  }
}

/// 单条规则卡片。
///
/// 公开化（去掉前导下划线）以便 widget 测试。操作由调用方通过 callback 注入，
/// 不再内部 `Get.find` controller —— 这样测试不用搭起整套分页 controller。
class LinkBlocklistRuleCard extends StatelessWidget {
  const LinkBlocklistRuleCard({
    super.key,
    required this.rule,
    required this.onEdit,
    required this.onToggleEnabled,
    required this.onDelete,
  });
  final LinkBlocklistRule rule;
  final VoidCallback onEdit;
  final VoidCallback onToggleEnabled;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) {
    final isMalicious = rule.severity == 'malicious';

    final severityBadge = Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      decoration: BoxDecoration(
        color: isMalicious ? Colors.red.shade50 : Colors.amber.shade50,
        borderRadius: BorderRadius.circular(4),
      ),
      child: Text(
        rule.severity,
        style: TextStyle(
          color: isMalicious ? Colors.red : Colors.amber.shade800,
          fontSize: 12,
          fontWeight: FontWeight.w600,
        ),
      ),
    );

    final actions = Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Transform.scale(
          scale: 0.85,
          child: Switch(
            value: rule.enabled,
            onChanged: (_) => onToggleEnabled(),
            materialTapTargetSize: MaterialTapTargetSize.shrinkWrap,
          ),
        ),
        IconButton(
          tooltip: '编辑',
          onPressed: onEdit,
          icon: const Icon(Icons.edit_outlined, size: 18),
          visualDensity: VisualDensity.compact,
          padding: const EdgeInsets.all(6),
          constraints: const BoxConstraints(),
        ),
        const SizedBox(width: 4),
        IconButton(
          tooltip: '删除',
          onPressed: onDelete,
          icon: const Icon(Icons.delete_outline, size: 18),
          visualDensity: VisualDensity.compact,
          padding: const EdgeInsets.all(6),
          constraints: const BoxConstraints(),
        ),
      ],
    );

    final infoColumn = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        Wrap(
          spacing: 8,
          runSpacing: 4,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            severityBadge,
            Text(
              rule.value,
              style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w600),
            ),
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
              decoration: BoxDecoration(
                color: Colors.grey.shade100,
                borderRadius: BorderRadius.circular(4),
              ),
              child: Text(rule.kind, style: const TextStyle(fontSize: 11)),
            ),
            Text(
              '· ${rule.source}',
              style: const TextStyle(
                fontSize: 11,
                color: AppPalette.textTertiary,
              ),
            ),
          ],
        ),
        if (rule.note.isNotEmpty)
          Padding(
            padding: const EdgeInsets.only(top: 4),
            child: Text(
              rule.note,
              style: const TextStyle(
                fontSize: 12,
                color: AppPalette.textSecondary,
              ),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),
          ),
        Padding(
          padding: const EdgeInsets.only(top: 4),
          child: Text(
            '命中 ${rule.hitCount} 次'
            '${rule.lastHitAt != null ? ' · 最近 ${_fmt(rule.lastHitAt!)}' : ''}',
            style: const TextStyle(
              fontSize: 11,
              color: AppPalette.textTertiary,
            ),
          ),
        ),
      ],
    );

    return Card(
      margin: const EdgeInsets.symmetric(vertical: 4),
      shape: const RoundedRectangleBorder(),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
        child: LayoutBuilder(
          builder: (ctx, cons) {
            final narrow = cons.maxWidth < 560;
            if (narrow) {
              return Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  infoColumn,
                  const SizedBox(height: 4),
                  Align(alignment: Alignment.centerRight, child: actions),
                ],
              );
            }
            return Row(
              crossAxisAlignment: CrossAxisAlignment.center,
              children: [
                Expanded(child: infoColumn),
                actions,
              ],
            );
          },
        ),
      ),
    );
  }

  String _fmt(DateTime t) {
    final local = t.toLocal();
    return '${local.year}-${_pad(local.month)}-${_pad(local.day)} '
        '${_pad(local.hour)}:${_pad(local.minute)}';
  }

  String _pad(int n) => n.toString().padLeft(2, '0');
}

// ---------- 编辑对话框 ----------

class _RuleEditDialog extends StatefulWidget {
  const _RuleEditDialog({required this.controller, this.initial});
  final LinkBlocklistController controller;
  final LinkBlocklistRule? initial;

  @override
  State<_RuleEditDialog> createState() => _RuleEditDialogState();
}

class _RuleEditDialogState extends State<_RuleEditDialog> {
  late String _kind;
  late String _severity;
  late String _source;
  late bool _enabled;
  late TextEditingController _valueCtrl;
  late TextEditingController _noteCtrl;

  @override
  void initState() {
    super.initState();
    final r = widget.initial;
    _kind = r?.kind ?? 'domain';
    _severity = r?.severity ?? 'malicious';
    _source = r?.source ?? 'manual';
    _enabled = r?.enabled ?? true;
    _valueCtrl = TextEditingController(text: r?.value ?? '');
    _noteCtrl = TextEditingController(text: r?.note ?? '');
  }

  @override
  void dispose() {
    _valueCtrl.dispose();
    _noteCtrl.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    final value = _valueCtrl.text.trim();
    if (value.isEmpty) return;
    Get.back();
    if (widget.initial == null) {
      await widget.controller.createRule(
        kind: _kind,
        value: value,
        severity: _severity,
        source: _source,
        enabled: _enabled,
        note: _noteCtrl.text.trim(),
      );
    } else {
      await widget.controller.updateRule(
        widget.initial!.id,
        kind: _kind,
        value: value,
        severity: _severity,
        source: _source,
        enabled: _enabled,
        note: _noteCtrl.text.trim(),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      insetPadding: kDialogInsetPadding,
      scrollable: true,
      title: Text(widget.initial == null ? '新建规则' : '编辑规则'),
      content: DialogContentBox(
        maxWidth: 460,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            LayoutBuilder(
              builder: (context, constraints) {
                final kindField = DropdownButtonFormField<String>(
                  initialValue: _kind,
                  decoration: const InputDecoration(labelText: '类型'),
                  items: [
                    for (final k in kLinkRuleKinds)
                      DropdownMenuItem(value: k, child: Text(k)),
                  ],
                  onChanged: (v) => setState(() => _kind = v ?? _kind),
                );
                final severityField = DropdownButtonFormField<String>(
                  initialValue: _severity,
                  decoration: const InputDecoration(labelText: '严重度'),
                  items: [
                    for (final s in kLinkRuleSeverities)
                      DropdownMenuItem(value: s, child: Text(s)),
                  ],
                  onChanged: (v) => setState(() => _severity = v ?? _severity),
                );
                if (constraints.maxWidth < 360) {
                  return Column(
                    children: [
                      kindField,
                      const SizedBox(height: 12),
                      severityField,
                    ],
                  );
                }
                return Row(
                  children: [
                    Expanded(child: kindField),
                    const SizedBox(width: 12),
                    Expanded(child: severityField),
                  ],
                );
              },
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _valueCtrl,
              decoration: InputDecoration(
                labelText: _valueLabel,
                hintText: _valueHint,
              ),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _noteCtrl,
              decoration: const InputDecoration(labelText: '备注（可选）'),
            ),
            const SizedBox(height: 12),
            Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: TextEditingController(text: _source),
                    decoration: const InputDecoration(labelText: '来源'),
                    onChanged: (v) => _source = v.trim(),
                  ),
                ),
                const SizedBox(width: 12),
                Row(
                  children: [
                    const Text('启用'),
                    Switch(
                      value: _enabled,
                      onChanged: (v) => setState(() => _enabled = v),
                    ),
                  ],
                ),
              ],
            ),
          ],
        ),
      ),
      actions: [
        TextButton(onPressed: () => Get.back(), child: const Text('取消')),
        FilledButton(onPressed: _submit, child: const Text('保存')),
      ],
    );
  }

  String get _valueLabel {
    switch (_kind) {
      case 'wildcard':
        return '通配域名（如 *.evil.com）';
      case 'regex':
        return '正则表达式';
      case 'keyword':
        return 'URL 关键词';
      default:
        return '域名（如 evil.com）';
    }
  }

  String get _valueHint {
    switch (_kind) {
      case 'wildcard':
        return '*.example.com';
      case 'regex':
        return r'^bad-site\.com/.*phish.*';
      case 'keyword':
        return 'phish-kit';
      default:
        return 'example.com';
    }
  }
}

// ---------- 在线测试对话框 ----------

class _TestDialog extends StatefulWidget {
  const _TestDialog({required this.controller});
  final LinkBlocklistController controller;

  @override
  State<_TestDialog> createState() => _TestDialogState();
}

class _TestDialogState extends State<_TestDialog> {
  final TextEditingController _ctrl = TextEditingController();
  LinkTestResult? _result;
  bool _loading = false;

  @override
  void dispose() {
    _ctrl.dispose();
    super.dispose();
  }

  Future<void> _test() async {
    final url = _ctrl.text.trim();
    if (url.isEmpty) return;
    setState(() => _loading = true);
    final r = await widget.controller.testUrl(url);
    setState(() {
      _result = r;
      _loading = false;
    });
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      insetPadding: kDialogInsetPadding,
      scrollable: true,
      title: const Text('在线测试'),
      content: DialogContentBox(
        maxWidth: 460,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: _ctrl,
              decoration: const InputDecoration(
                labelText: '输入 URL 测试是否命中规则',
                hintText: 'http://example.com/path',
              ),
              onSubmitted: (_) => _test(),
            ),
            const SizedBox(height: 12),
            if (_loading)
              const Padding(
                padding: EdgeInsets.all(16),
                child: CircularProgressIndicator(),
              )
            else if (_result != null)
              _ResultPanel(result: _result!),
          ],
        ),
      ),
      actions: [
        TextButton(onPressed: () => Get.back(), child: const Text('关闭')),
        FilledButton(onPressed: _test, child: const Text('测试')),
      ],
    );
  }
}

class _ResultPanel extends StatelessWidget {
  const _ResultPanel({required this.result});
  final LinkTestResult result;

  @override
  Widget build(BuildContext context) {
    Color color;
    switch (result.verdict) {
      case 'malicious':
        color = Colors.red;
        break;
      case 'suspicious':
        color = Colors.amber.shade800;
        break;
      default:
        color = Colors.green;
    }
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            result.verdict.toUpperCase(),
            style: TextStyle(
              fontWeight: FontWeight.w700,
              color: color,
              fontSize: 16,
            ),
          ),
          const SizedBox(height: 8),
          _kv('规范化 host', result.canonicalHost),
          if (result.reason.isNotEmpty) _kv('原因', result.reason),
          if (result.ruleSource.isNotEmpty) _kv('来源', result.ruleSource),
          if (result.ruleId > 0) _kv('命中规则 ID', '${result.ruleId}'),
        ],
      ),
    );
  }

  Widget _kv(String k, String v) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 2),
    child: Text('$k：$v', style: const TextStyle(fontSize: 13)),
  );
}

// ---------- 顶部统计条 ----------

class _StatsStrip extends StatelessWidget {
  const _StatsStrip({required this.stats});
  final LinkBlocklistStats? stats;

  @override
  Widget build(BuildContext context) {
    final s = stats;
    if (s == null) {
      return const SizedBox(height: 0);
    }
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 12, 16, 0),
      child: Wrap(
        spacing: 12,
        runSpacing: 8,
        children: [
          _StatChip(
            label: '今日拦截',
            value: '${s.blockedToday}',
            color: Colors.red,
          ),
          _StatChip(
            label: '7日拦截',
            value: '${s.blocked7d}',
            color: Colors.red.shade400,
          ),
          _StatChip(
            label: '30日拦截',
            value: '${s.blocked30d}',
            color: Colors.red.shade300,
          ),
          _StatChip(
            label: '今日提示',
            value: '${s.warnedToday}',
            color: Colors.amber.shade700,
          ),
          _StatChip(
            label: '启用规则',
            value: '${s.activeRulesCount}',
            color: Colors.green,
          ),
          _StatChip(
            label: '禁用规则',
            value: '${s.disabledRulesCount}',
            color: Colors.grey,
          ),
        ],
      ),
    );
  }
}

class _StatChip extends StatelessWidget {
  const _StatChip({
    required this.label,
    required this.value,
    required this.color,
  });
  final String label;
  final String value;
  final Color color;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: color.withValues(alpha: 0.3), width: 1),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(label, style: TextStyle(fontSize: 12, color: color)),
          const SizedBox(width: 8),
          Text(
            value,
            style: TextStyle(
              fontSize: 14,
              fontWeight: FontWeight.w700,
              color: color,
            ),
          ),
        ],
      ),
    );
  }
}

// ---------- CSV 批量导入对话框 ----------

class _ImportDialog extends StatefulWidget {
  const _ImportDialog({required this.controller});
  final LinkBlocklistController controller;

  @override
  State<_ImportDialog> createState() => _ImportDialogState();
}

class _ImportDialogState extends State<_ImportDialog> {
  static const int _importMaxBytes = 5 * 1024 * 1024;

  final TextEditingController _ctrl = TextEditingController();
  bool _importing = false;

  // 文件模式状态
  bool _useFile = true;
  bool _picking = false;
  String? _fileCSV;
  String? _fileName;
  int? _fileSize;
  int? _fileTotalLines;
  int? _fileValidLines;
  List<String>? _filePreview;
  String? _fileError;

  @override
  void dispose() {
    _ctrl.dispose();
    super.dispose();
  }

  Future<void> _pickFile() async {
    setState(() {
      _picking = true;
      _fileError = null;
    });
    try {
      final res = await FilePicker.platform.pickFiles(
        type: FileType.custom,
        allowedExtensions: const ['csv', 'txt'],
        withData: true,
      );
      if (res == null || res.files.isEmpty) return;
      final f = res.files.single;
      final bytes = f.bytes;
      if (bytes == null) {
        setState(() => _fileError = '读取文件失败：没有数据');
        return;
      }
      if (bytes.length > _importMaxBytes) {
        setState(
          () => _fileError = '文件 ${_humanSize(bytes.length)} 超过 5MB 上限，请拆分后再导入',
        );
        return;
      }
      String text;
      try {
        text = utf8.decode(bytes);
      } on FormatException {
        text = utf8.decode(bytes, allowMalformed: true);
      }
      // 统计行数（与后端 parser 口径一致：跳空行、跳#开头注释；首行 kind 表头由后端自动跳过）
      final lines = const LineSplitter().convert(text);
      int valid = 0;
      final preview = <String>[];
      for (final raw in lines) {
        final s = raw.trim();
        if (s.isEmpty || s.startsWith('#')) continue;
        valid++;
        if (preview.length < 3) preview.add(s);
      }
      setState(() {
        _fileCSV = text;
        _fileName = f.name;
        _fileSize = bytes.length;
        _fileTotalLines = lines.length;
        _fileValidLines = valid;
        _filePreview = preview;
      });
    } finally {
      if (mounted) setState(() => _picking = false);
    }
  }

  Future<void> _doImport() async {
    final csv = _useFile ? (_fileCSV ?? '') : _ctrl.text.trim();
    if (csv.isEmpty) return;
    setState(() => _importing = true);
    Get.back();
    await widget.controller.importCSV(csv);
  }

  bool get _canImport {
    if (_importing) return false;
    if (_useFile) return _fileCSV != null && _fileCSV!.isNotEmpty;
    return _ctrl.text.trim().isNotEmpty;
  }

  static String _humanSize(int n) {
    if (n < 1024) return '${n}B';
    if (n < 1024 * 1024) return '${(n / 1024).toStringAsFixed(1)}KB';
    return '${(n / 1024 / 1024).toStringAsFixed(2)}MB';
  }

  Widget _buildFilePane() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        LayoutBuilder(
          builder: (context, constraints) {
            final button = FilledButton.tonalIcon(
              onPressed: _picking || _importing ? null : _pickFile,
              icon: const Icon(Icons.upload_file, size: 18),
              label: Text(
                _picking
                    ? '读取中...'
                    : (_fileCSV == null ? '选择 CSV/TXT 文件' : '重新选择文件'),
              ),
            );
            const hint = Text(
              '支持 CSV/TXT，UTF-8 编码，单文件 ≤ 5MB',
              style: TextStyle(fontSize: 12, color: Colors.black54),
            );
            if (constraints.maxWidth < 360) {
              return Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [button, const SizedBox(height: 8), hint],
              );
            }
            return Row(
              children: [
                button,
                const SizedBox(width: 12),
                const Expanded(child: hint),
              ],
            );
          },
        ),
        if (_fileError != null) ...[
          const SizedBox(height: 10),
          Container(
            padding: const EdgeInsets.all(10),
            decoration: BoxDecoration(
              color: Colors.red.withValues(alpha: 0.06),
              borderRadius: BorderRadius.circular(6),
            ),
            child: Text(
              _fileError!,
              style: const TextStyle(fontSize: 12, color: Colors.red),
            ),
          ),
        ],
        if (_fileCSV != null) ...[
          const SizedBox(height: 12),
          Container(
            padding: const EdgeInsets.all(12),
            decoration: BoxDecoration(
              color: Colors.green.withValues(alpha: 0.05),
              border: Border.all(color: Colors.green.withValues(alpha: 0.2)),
              borderRadius: BorderRadius.circular(6),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    const Icon(
                      Icons.check_circle,
                      size: 16,
                      color: Colors.green,
                    ),
                    const SizedBox(width: 6),
                    Expanded(
                      child: Text(
                        _fileName ?? '',
                        style: const TextStyle(
                          fontSize: 13,
                          fontWeight: FontWeight.w600,
                        ),
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 6),
                Text(
                  '大小 ${_humanSize(_fileSize ?? 0)} · 共 ${_fileTotalLines ?? 0} 行 · 待导入 ${_fileValidLines ?? 0} 条（已扣除空行与 # 注释）',
                  style: const TextStyle(fontSize: 12, color: Colors.black54),
                ),
                if ((_filePreview ?? []).isNotEmpty) ...[
                  const SizedBox(height: 8),
                  const Text(
                    '前 3 条预览：',
                    style: TextStyle(fontSize: 11, color: Colors.black45),
                  ),
                  const SizedBox(height: 4),
                  for (final line in _filePreview!)
                    Padding(
                      padding: const EdgeInsets.only(top: 2),
                      child: Text(
                        line,
                        style: const TextStyle(
                          fontFamily: 'monospace',
                          fontSize: 11.5,
                          color: Colors.black87,
                        ),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                ],
              ],
            ),
          ),
        ],
      ],
    );
  }

  Widget _buildPastePane() {
    return TextField(
      controller: _ctrl,
      onChanged: (_) => setState(() {}),
      maxLines: 14,
      minLines: 10,
      style: const TextStyle(fontFamily: 'monospace', fontSize: 12),
      decoration: const InputDecoration(hintText: '粘贴 CSV 内容...'),
    );
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      insetPadding: kDialogInsetPadding,
      scrollable: true,
      title: const Text('批量导入 CSV'),
      content: DialogContentBox(
        maxWidth: 580,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              '格式：kind,value,severity,source,enabled,note（每行一条）\n'
              '示例：domain,evil.com,malicious,antifraud,true,反诈库',
              style: TextStyle(fontSize: 12, color: Colors.black54),
            ),
            const SizedBox(height: 12),
            SegmentedButton<bool>(
              segments: const [
                ButtonSegment(
                  value: true,
                  icon: Icon(Icons.upload_file, size: 16),
                  label: Text('选文件'),
                ),
                ButtonSegment(
                  value: false,
                  icon: Icon(Icons.content_paste, size: 16),
                  label: Text('粘贴文本'),
                ),
              ],
              selected: {_useFile},
              onSelectionChanged: _importing
                  ? null
                  : (s) => setState(() => _useFile = s.first),
              showSelectedIcon: false,
            ),
            const SizedBox(height: 12),
            _useFile ? _buildFilePane() : _buildPastePane(),
          ],
        ),
      ),
      actions: [
        TextButton(onPressed: () => Get.back(), child: const Text('取消')),
        FilledButton(
          onPressed: _canImport ? _doImport : null,
          child: Text(_importing ? '导入中...' : '开始导入'),
        ),
      ],
    );
  }
}
