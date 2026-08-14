import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../shared/widgets/confirm_dialog.dart';
import '../../shared/widgets/dialog_content_box.dart';
import 'reach_models.dart';
import 'reach_service.dart';
import 'reach_templates_controller.dart';

class ReachTemplateDialog extends StatefulWidget {
  const ReachTemplateDialog({super.key, this.template});
  final ReachTemplate? template;

  static Future<void> show({ReachTemplate? template}) {
    return Get.dialog(
      ReachTemplateDialog(template: template),
      barrierDismissible: false,
    );
  }

  @override
  State<ReachTemplateDialog> createState() => _ReachTemplateDialogState();
}

class _ReachTemplateDialogState extends State<ReachTemplateDialog> {
  late final TextEditingController _nameCtrl;
  late final TextEditingController _titleCtrl;
  late final TextEditingController _inAppCtrl;
  late final TextEditingController _pushCtrl;
  late final TextEditingController _emailCtrl;
  bool _busy = false;

  bool get _isEdit => widget.template != null;

  @override
  void initState() {
    super.initState();
    final t = widget.template;
    _nameCtrl = TextEditingController(text: t?.name ?? '');
    _titleCtrl = TextEditingController(text: t?.title ?? '');
    _inAppCtrl = TextEditingController(text: t?.inAppBody ?? '');
    _pushCtrl = TextEditingController(text: t?.pushBody ?? '');
    _emailCtrl = TextEditingController(text: t?.emailHtml ?? '');
  }

  @override
  void dispose() {
    _nameCtrl.dispose();
    _titleCtrl.dispose();
    _inAppCtrl.dispose();
    _pushCtrl.dispose();
    _emailCtrl.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    final name = _nameCtrl.text.trim();
    if (name.isEmpty) {
      Toast.error('模板名称不能为空');
      return;
    }
    setState(() => _busy = true);
    try {
      if (_isEdit) {
        await ReachService.updateTemplate(
          widget.template!.id,
          name: name,
          title: _titleCtrl.text.trim(),
          inAppBody: _inAppCtrl.text,
          pushBody: _pushCtrl.text,
          emailHtml: _emailCtrl.text,
        );
        Toast.success('模板已更新');
      } else {
        await ReachService.createTemplate(
          name: name,
          title: _titleCtrl.text.trim(),
          inAppBody: _inAppCtrl.text,
          pushBody: _pushCtrl.text,
          emailHtml: _emailCtrl.text,
        );
        Toast.success('模板已创建');
      }
      if (Get.isRegistered<ReachTemplatesController>()) {
        Get.find<ReachTemplatesController>().reloadFromFirstPage();
      }
      Get.back();
    } catch (e) {
      Toast.error(e.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      insetPadding: kDialogInsetPadding,
      title: Text(_isEdit ? '编辑模板' : '新建模板'),
      content: DialogContentBox(
        maxWidth: 520,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: _nameCtrl,
                autofocus: !_isEdit,
                decoration: const InputDecoration(labelText: '名称 *'),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: _titleCtrl,
                decoration: const InputDecoration(labelText: '标题'),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: _inAppCtrl,
                maxLines: 3,
                decoration: const InputDecoration(
                  labelText: '站内信内容',
                  alignLabelWithHint: true,
                ),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: _pushCtrl,
                maxLines: 2,
                decoration: const InputDecoration(
                  labelText: 'Push 内容',
                  alignLabelWithHint: true,
                ),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: _emailCtrl,
                maxLines: 5,
                decoration: const InputDecoration(
                  labelText: '邮件 HTML',
                  alignLabelWithHint: true,
                ),
              ),
            ],
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: _busy ? null : () => Get.back(),
          child: const Text('取消'),
        ),
        FilledButton(
          onPressed: _busy ? null : _save,
          child: Text(_busy ? '保存中…' : '保存'),
        ),
      ],
    );
  }
}
