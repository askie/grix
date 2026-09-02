import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/confirm_dialog.dart';
import 'egg_service.dart';
import 'eggs_controller.dart';

/// 参数：{'id': '...', 'isEdit': true/false}（路由参数）
class EggFormView extends StatefulWidget {
  const EggFormView({super.key});

  @override
  State<EggFormView> createState() => _EggFormViewState();
}

class _EggFormViewState extends State<EggFormView> {
  late final bool isEdit;
  late final String? eggId;

  final _idCtrl = TextEditingController();
  final _colorCtrl = TextEditingController(text: '#D97706');
  final _emojiCtrl = TextEditingController(text: '🌍');
  String _categoryId = '';

  final _nameZh = TextEditingController();
  final _descZh = TextEditingController();
  final _vibeZh = TextEditingController();
  final _nameEn = TextEditingController();
  final _descEn = TextEditingController();
  final _vibeEn = TextEditingController();

  bool _loading = false;
  EggsController get _controller => Get.find<EggsController>();

  @override
  void initState() {
    super.initState();
    final args = Get.arguments as Map<String, dynamic>?;
    isEdit = args?['isEdit'] == true;
    eggId = args?['id']?.toString();
    if (isEdit && eggId != null) _loadDetail();
    if (_controller.categories.isEmpty) _controller.loadCategories();
  }

  Future<void> _loadDetail() async {
    setState(() => _loading = true);
    try {
      final egg = await EggService.getEgg(eggId!);
      _colorCtrl.text = egg.color;
      _emojiCtrl.text = egg.emoji;
      _categoryId = egg.categoryId;
      for (final row in egg.i18n) {
        final locale = (row['locale'] ?? '').toString();
        if (locale == 'zh-CN') {
          _nameZh.text = (row['name'] ?? '').toString();
          _descZh.text = (row['description'] ?? '').toString();
          _vibeZh.text = (row['vibe'] ?? '').toString();
        } else if (locale == 'en-US') {
          _nameEn.text = (row['name'] ?? '').toString();
          _descEn.text = (row['description'] ?? '').toString();
          _vibeEn.text = (row['vibe'] ?? '').toString();
        }
      }
    } catch (e) {
      Toast.error(e.toString());
    } finally {
      setState(() => _loading = false);
    }
  }

  @override
  void dispose() {
    for (final c in [
      _idCtrl,
      _colorCtrl,
      _emojiCtrl,
      _nameZh,
      _descZh,
      _vibeZh,
      _nameEn,
      _descEn,
      _vibeEn,
    ]) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _submit() async {
    final i18n = <Map<String, dynamic>>[];
    if (_nameZh.text.trim().isNotEmpty || _descZh.text.trim().isNotEmpty) {
      i18n.add({
        'locale': 'zh-CN',
        'name': _nameZh.text.trim(),
        'description': _descZh.text.trim(),
        'vibe': _vibeZh.text.trim(),
      });
    }
    if (_nameEn.text.trim().isNotEmpty || _descEn.text.trim().isNotEmpty) {
      i18n.add({
        'locale': 'en-US',
        'name': _nameEn.text.trim(),
        'description': _descEn.text.trim(),
        'vibe': _vibeEn.text.trim(),
      });
    }
    setState(() => _loading = true);
    try {
      if (isEdit) {
        await EggService.updateEgg(eggId!, {
          'category_id': _categoryId,
          'color': _colorCtrl.text.trim(),
          'emoji': _emojiCtrl.text.trim(),
          'i18n': i18n,
        });
        Toast.success('已保存');
        Get.back(result: true);
      } else {
        await EggService.createEgg({
          'id': _idCtrl.text.trim(),
          'category_id': _categoryId,
          'color': _colorCtrl.text.trim(),
          'emoji': _emojiCtrl.text.trim(),
          'i18n': i18n,
        });
        Toast.success('虾蛋已创建');
        Get.back(result: true);
      }
    } catch (e) {
      Toast.error(e.toString());
    } finally {
      setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: isEdit ? '编辑虾蛋' : '创建虾蛋',
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : SingleChildScrollView(
              padding: const EdgeInsets.all(24),
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 640),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    if (!isEdit) ...[
                      TextField(
                        controller: _idCtrl,
                        decoration: const InputDecoration(
                          labelText: 'Egg ID (唯一标识)',
                          border: OutlineInputBorder(),
                        ),
                      ),
                      const SizedBox(height: 16),
                    ],
                    Obx(() {
                      final cats = _controller.categories;
                      if (cats.isEmpty) return const SizedBox.shrink();
                      // 若 categoryId 未设置，用第一个分类做默认（在 build 之外不能 setState，改为 DropdownButtonFormField 处理）
                      final currentValue =
                          (_categoryId.isEmpty ||
                              !cats.any((c) => c.id == _categoryId))
                          ? cats.first.id
                          : _categoryId;
                      return DropdownButtonFormField<String>(
                        value: currentValue,
                        decoration: const InputDecoration(
                          labelText: '分类',
                          border: OutlineInputBorder(),
                        ),
                        items: cats
                            .map(
                              (c) => DropdownMenuItem(
                                value: c.id,
                                child: Text('${c.name} (${c.code})'),
                              ),
                            )
                            .toList(),
                        onChanged: (v) {
                          if (v != null) setState(() => _categoryId = v);
                        },
                      );
                    }),
                    const SizedBox(height: 16),
                    Row(
                      children: [
                        Expanded(
                          child: TextField(
                            controller: _colorCtrl,
                            decoration: const InputDecoration(
                              labelText: '颜色 (HEX)',
                              border: OutlineInputBorder(),
                            ),
                          ),
                        ),
                        const SizedBox(width: 16),
                        Expanded(
                          child: TextField(
                            controller: _emojiCtrl,
                            decoration: const InputDecoration(
                              labelText: 'Emoji',
                              border: OutlineInputBorder(),
                            ),
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 24),
                    Text(
                      '中文 (zh-CN)',
                      style: Theme.of(context).textTheme.titleSmall,
                    ),
                    const SizedBox(height: 8),
                    TextField(
                      controller: _nameZh,
                      decoration: const InputDecoration(
                        labelText: '名称',
                        border: OutlineInputBorder(),
                      ),
                    ),
                    const SizedBox(height: 8),
                    TextField(
                      controller: _descZh,
                      maxLines: 3,
                      decoration: const InputDecoration(
                        labelText: '描述',
                        border: OutlineInputBorder(),
                      ),
                    ),
                    const SizedBox(height: 8),
                    TextField(
                      controller: _vibeZh,
                      decoration: const InputDecoration(
                        labelText: 'Vibe',
                        border: OutlineInputBorder(),
                      ),
                    ),
                    const SizedBox(height: 24),
                    Text(
                      '英文 (en-US)',
                      style: Theme.of(context).textTheme.titleSmall,
                    ),
                    const SizedBox(height: 8),
                    TextField(
                      controller: _nameEn,
                      decoration: const InputDecoration(
                        labelText: 'Name',
                        border: OutlineInputBorder(),
                      ),
                    ),
                    const SizedBox(height: 8),
                    TextField(
                      controller: _descEn,
                      maxLines: 3,
                      decoration: const InputDecoration(
                        labelText: 'Description',
                        border: OutlineInputBorder(),
                      ),
                    ),
                    const SizedBox(height: 8),
                    TextField(
                      controller: _vibeEn,
                      decoration: const InputDecoration(
                        labelText: 'Vibe',
                        border: OutlineInputBorder(),
                      ),
                    ),
                    const SizedBox(height: 32),
                    FilledButton(
                      onPressed: _loading ? null : _submit,
                      child: Text(isEdit ? '保存修改' : '创建虾蛋'),
                    ),
                  ],
                ),
              ),
            ),
    );
  }
}
