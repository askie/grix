import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/theme/app_palette.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/confirm_dialog.dart';
import 'egg_service.dart';

class EggDetailView extends StatefulWidget {
  const EggDetailView({super.key});

  @override
  State<EggDetailView> createState() => _EggDetailViewState();
}

class _EggDetailViewState extends State<EggDetailView> {
  late final String eggId;
  EggDetail? _egg;
  List<EggVersion> _versions = [];
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    eggId = Get.parameters['id'] ?? (Get.arguments?['id']?.toString() ?? '');
    _load();
  }

  Future<void> _load() async {
    setState(() { _loading = true; _error = null; });
    try {
      final egg = await EggService.getEgg(eggId);
      final versions = await EggService.listVersions(eggId);
      setState(() { _egg = egg; _versions = versions; });
    } catch (e) {
      setState(() => _error = e.toString());
    } finally {
      setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: '虾蛋详情: $eggId',
      actions: [
        IconButton(
          tooltip: '编辑基础信息',
          icon: const Icon(Icons.edit_outlined),
          onPressed: () async {
            final result = await Get.toNamed('/eggs/$eggId/edit', arguments: {'id': eggId, 'isEdit': true});
            if (result == true) _load();
          },
        ),
        IconButton(tooltip: '刷新', onPressed: _load, icon: const Icon(Icons.refresh)),
      ],
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : _error != null
              ? Center(child: Column(mainAxisSize: MainAxisSize.min, children: [
                  Text(_error!, style: TextStyle(color: AppPalette.danger)),
                  TextButton(onPressed: _load, child: const Text('重试')),
                ]))
              : _buildContent(context),
    );
  }

  Widget _buildContent(BuildContext context) {
    final egg = _egg!;
    return SingleChildScrollView(
      padding: const EdgeInsets.all(16),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        // 基础信息卡
        Card(child: Padding(padding: const EdgeInsets.all(16), child: Column(
          crossAxisAlignment: CrossAxisAlignment.start, children: [
            Row(children: [
              Flexible(child: Text(egg.id, style: Theme.of(context).textTheme.titleLarge, overflow: TextOverflow.ellipsis)),
              const SizedBox(width: 8),
              Text(egg.emoji, style: const TextStyle(fontSize: 24)),
              const Spacer(),
              _StatusChip(status: egg.status),
              const SizedBox(width: 8),
              PopupMenuButton<String>(
                onSelected: (v) async {
                  try {
                    await EggService.updateEggStatus(egg.id, v);
                    Toast.success('状态已更新');
                    await _load();
                  } catch (e) { Toast.error(e.toString()); }
                },
                itemBuilder: (_) => [
                  if (egg.status != 'active') const PopupMenuItem(value: 'active', child: Text('上架')),
                  if (egg.status != 'inactive') const PopupMenuItem(value: 'inactive', child: Text('下架')),
                ],
              ),
            ]),
            const SizedBox(height: 8),
            Text('分类: ${egg.categoryId} · 颜色: ${egg.color} · 安装数: ${egg.installCount}',
                style: Theme.of(context).textTheme.bodySmall),
            if (egg.i18n.isNotEmpty) ...[
              const SizedBox(height: 8),
              const Divider(),
              ...egg.i18n.map((row) => Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text('[${row['locale']}] ${row['name'] ?? ''} — ${row['description'] ?? ''}',
                    style: Theme.of(context).textTheme.bodySmall),
              )),
            ],
          ],
        ))),
        const SizedBox(height: 16),
        // 版本管理
        Row(children: [
          Text('版本列表', style: Theme.of(context).textTheme.titleMedium),
          const Spacer(),
          FilledButton.icon(
            onPressed: () => _createVersion(context),
            icon: const Icon(Icons.add, size: 18),
            label: const Text('新建版本'),
          ),
        ]),
        const SizedBox(height: 8),
        if (_versions.isEmpty)
          const Card(child: Padding(padding: EdgeInsets.all(24), child: Center(child: Text('暂无版本'))))
        else
          ..._versions.map((v) => _VersionCard(v: v, eggId: eggId, onUpdated: _load)),
      ]),
    );
  }

  Future<void> _createVersion(BuildContext context) async {
    final versionCtrl = TextEditingController();
    final personaUrl = TextEditingController();
    final personaSize = TextEditingController();
    final skillUrl = TextEditingController();
    final skillSize = TextEditingController();
    final descZh = TextEditingController();
    final descEn = TextEditingController();

    final confirmed = await Get.dialog<bool>(AlertDialog(
      title: const Text('新建版本'),
      scrollable: true,
      content: Column(mainAxisSize: MainAxisSize.min, children: [
        TextField(controller: versionCtrl, decoration: const InputDecoration(labelText: '版本号 (整数)'), keyboardType: TextInputType.number),
        const SizedBox(height: 8),
        TextField(controller: personaUrl, decoration: const InputDecoration(labelText: 'Persona ZIP URL')),
        const SizedBox(height: 8),
        TextField(controller: personaSize, decoration: const InputDecoration(labelText: 'Persona ZIP 大小 (bytes)'), keyboardType: TextInputType.number),
        const SizedBox(height: 8),
        TextField(controller: skillUrl, decoration: const InputDecoration(labelText: 'Skill ZIP URL (可选)')),
        const SizedBox(height: 8),
        TextField(controller: skillSize, decoration: const InputDecoration(labelText: 'Skill ZIP 大小 (bytes, 可选)'), keyboardType: TextInputType.number),
        const SizedBox(height: 8),
        TextField(controller: descZh, decoration: const InputDecoration(labelText: '版本描述 (zh-CN)')),
        const SizedBox(height: 8),
        TextField(controller: descEn, decoration: const InputDecoration(labelText: 'Version desc (en-US)')),
      ]),
      actions: [
        TextButton(onPressed: () => Get.back(result: false), child: const Text('取消')),
        FilledButton(onPressed: () => Get.back(result: true), child: const Text('创建')),
      ],
    ));

    if (confirmed == true) {
      final i18n = <Map<String, dynamic>>[];
      if (descZh.text.trim().isNotEmpty) i18n.add({'locale': 'zh-CN', 'version_desc': descZh.text.trim()});
      if (descEn.text.trim().isNotEmpty) i18n.add({'locale': 'en-US', 'version_desc': descEn.text.trim()});
      try {
        await EggService.createVersion(eggId, {
          'version': int.tryParse(versionCtrl.text) ?? 0,
          'persona_zip_url': personaUrl.text.trim(),
          'persona_zip_size': int.tryParse(personaSize.text) ?? 0,
          if (skillUrl.text.trim().isNotEmpty) 'skill_zip_url': skillUrl.text.trim(),
          if (skillSize.text.trim().isNotEmpty) 'skill_zip_size': int.tryParse(skillSize.text) ?? 0,
          'i18n': i18n,
        });
        Toast.success('版本已创建');
        await _load();
      } catch (e) {
        Toast.error(e.toString());
      }
    }
    for (final c in [versionCtrl, personaUrl, personaSize, skillUrl, skillSize, descZh, descEn]) {
      c.dispose();
    }
  }
}

class _VersionCard extends StatelessWidget {
  const _VersionCard({required this.v, required this.eggId, required this.onUpdated});
  final EggVersion v;
  final String eggId;
  final VoidCallback onUpdated;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.only(bottom: 8),
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Row(children: [
            Text('v${v.version}', style: Theme.of(context).textTheme.titleMedium),
            const Spacer(),
            TextButton(onPressed: () => _edit(context), child: const Text('编辑描述')),
          ]),
          if (v.personaZipUrl.isNotEmpty)
            Text('Persona: ${v.personaZipUrl}', style: Theme.of(context).textTheme.bodySmall, overflow: TextOverflow.ellipsis),
          if (v.skillZipUrl.isNotEmpty)
            Text('Skill: ${v.skillZipUrl}', style: Theme.of(context).textTheme.bodySmall, overflow: TextOverflow.ellipsis),
          if (v.versionDesc.isNotEmpty)
            Padding(padding: const EdgeInsets.only(top: 4), child: Text(v.versionDesc, style: Theme.of(context).textTheme.bodySmall)),
        ]),
      ),
    );
  }

  Future<void> _edit(BuildContext context) async {
    final descZh = TextEditingController();
    final descEn = TextEditingController();
    for (final row in v.i18n) {
      final locale = (row['locale'] ?? '').toString();
      final desc = (row['version_desc'] ?? '').toString();
      if (locale == 'zh-CN') descZh.text = desc;
      if (locale == 'en-US') descEn.text = desc;
    }
    final confirmed = await Get.dialog<bool>(AlertDialog(
      title: Text('编辑版本 v${v.version} 描述'),
      content: Column(mainAxisSize: MainAxisSize.min, children: [
        TextField(controller: descZh, decoration: const InputDecoration(labelText: '版本描述 (zh-CN)')),
        const SizedBox(height: 8),
        TextField(controller: descEn, decoration: const InputDecoration(labelText: 'Version desc (en-US)')),
      ]),
      actions: [
        TextButton(onPressed: () => Get.back(result: false), child: const Text('取消')),
        FilledButton(onPressed: () => Get.back(result: true), child: const Text('保存')),
      ],
    ));
    if (confirmed == true) {
      final i18n = <Map<String, dynamic>>[];
      if (descZh.text.trim().isNotEmpty) i18n.add({'locale': 'zh-CN', 'version_desc': descZh.text.trim()});
      if (descEn.text.trim().isNotEmpty) i18n.add({'locale': 'en-US', 'version_desc': descEn.text.trim()});
      try {
        await EggService.updateVersion(eggId, v.version, {'i18n': i18n});
        Toast.success('描述已更新');
        onUpdated();
      } catch (e) {
        Toast.error(e.toString());
      }
    }
    descZh.dispose();
    descEn.dispose();
  }
}

class _StatusChip extends StatelessWidget {
  const _StatusChip({required this.status});
  final String status;
  @override
  Widget build(BuildContext context) {
    final active = status == 'active';
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: active ? AppPalette.successSoft : AppPalette.border,
        borderRadius: BorderRadius.circular(6),
      ),
      child: Text(active ? '上架' : '下架',
          style: TextStyle(fontSize: 12, color: active ? AppPalette.success : AppPalette.textSecondary, fontWeight: FontWeight.w600)),
    );
  }
}
