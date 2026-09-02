import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../shared/widgets/confirm_dialog.dart';
import '../../shared/widgets/dialog_content_box.dart';
import 'reach_models.dart';
import 'reach_service.dart';

/// 编辑待发送公告草稿的中英双语文案。
class ReachAnnouncementDialog extends StatefulWidget {
  const ReachAnnouncementDialog({
    super.key,
    required this.taskId,
    required this.content,
  });

  final String taskId;
  final ReachAnnouncementContent content;

  /// 返回 true 表示保存成功，调用方可刷新详情。
  static Future<bool> show({
    required String taskId,
    required ReachAnnouncementContent content,
  }) async {
    final result = await Get.dialog<bool>(
      ReachAnnouncementDialog(taskId: taskId, content: content),
      barrierDismissible: false,
    );
    return result == true;
  }

  @override
  State<ReachAnnouncementDialog> createState() =>
      _ReachAnnouncementDialogState();
}

class _LocaleFields {
  _LocaleFields(ReachAnnouncementLocale locale)
    : title = TextEditingController(text: locale.title),
      body = TextEditingController(text: locale.body),
      emailSubject = TextEditingController(text: locale.emailSubject),
      emailIntro = TextEditingController(text: locale.emailIntro);

  final TextEditingController title;
  final TextEditingController body;
  final TextEditingController emailSubject;
  final TextEditingController emailIntro;

  ReachAnnouncementLocale toLocale() => ReachAnnouncementLocale(
    title: title.text.trim(),
    body: body.text,
    emailSubject: emailSubject.text.trim(),
    emailIntro: emailIntro.text.trim(),
  );

  void dispose() {
    title.dispose();
    body.dispose();
    emailSubject.dispose();
    emailIntro.dispose();
  }
}

class _ReachAnnouncementDialogState extends State<ReachAnnouncementDialog>
    with SingleTickerProviderStateMixin {
  late final TabController _tabCtrl;
  late final _LocaleFields _zh;
  late final _LocaleFields _en;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _tabCtrl = TabController(length: 2, vsync: this);
    _zh = _LocaleFields(widget.content.zh);
    _en = _LocaleFields(widget.content.en);
  }

  @override
  void dispose() {
    _tabCtrl.dispose();
    _zh.dispose();
    _en.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    if (_zh.title.text.trim().isEmpty || _en.title.text.trim().isEmpty) {
      Toast.error('中英文标题均不能为空');
      return;
    }
    setState(() => _busy = true);
    try {
      await ReachService.updateTaskContent(
        widget.taskId,
        ReachAnnouncementContent(zh: _zh.toLocale(), en: _en.toLocale()),
      );
      Toast.success('文案已保存');
      Get.back(result: true);
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
      scrollable: true,
      title: const Text('编辑公告文案'),
      content: DialogContentBox(
        maxWidth: 560,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TabBar(
              controller: _tabCtrl,
              tabs: const [
                Tab(text: '中文'),
                Tab(text: 'English'),
              ],
            ),
            const SizedBox(height: 12),
            SizedBox(
              height: 360,
              child: TabBarView(
                controller: _tabCtrl,
                children: [_buildLocaleForm(_zh), _buildLocaleForm(_en)],
              ),
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: _busy ? null : () => Get.back(result: false),
          child: const Text('取消'),
        ),
        FilledButton(
          onPressed: _busy ? null : _save,
          child: Text(_busy ? '保存中…' : '保存'),
        ),
      ],
    );
  }

  Widget _buildLocaleForm(_LocaleFields fields) {
    return SingleChildScrollView(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          TextField(
            controller: fields.title,
            decoration: const InputDecoration(labelText: '标题（站内信首行）*'),
          ),
          const SizedBox(height: 12),
          TextField(
            controller: fields.body,
            maxLines: 6,
            decoration: const InputDecoration(
              labelText: '正文（更新内容，站内信与邮件共用）',
              alignLabelWithHint: true,
            ),
          ),
          const SizedBox(height: 12),
          TextField(
            controller: fields.emailSubject,
            decoration: const InputDecoration(labelText: '邮件主题（留空则用标题）'),
          ),
          const SizedBox(height: 12),
          TextField(
            controller: fields.emailIntro,
            maxLines: 2,
            decoration: const InputDecoration(
              labelText: '邮件导语',
              alignLabelWithHint: true,
            ),
          ),
        ],
      ),
    );
  }
}
