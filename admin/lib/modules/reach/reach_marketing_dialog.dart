import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../shared/widgets/confirm_dialog.dart';
import '../../shared/widgets/dialog_content_box.dart';
import 'reach_models.dart';
import 'reach_service.dart';
import 'reach_tasks_controller.dart';

class ReachMarketingDialog extends StatefulWidget {
  const ReachMarketingDialog({super.key});

  static Future<void> show() {
    return Get.dialog(const ReachMarketingDialog(), barrierDismissible: false);
  }

  @override
  State<ReachMarketingDialog> createState() => _ReachMarketingDialogState();
}

class _ReachMarketingDialogState extends State<ReachMarketingDialog> {
  List<ReachTemplate>? _templates;
  String? _selectedTemplateId;
  final Set<String> _channels = {'in_app'};
  DateTime? _scheduledAt;
  final _testUserIdsController = TextEditingController();
  bool _busy = false;
  bool _loadingTemplates = true;

  @override
  void dispose() {
    _testUserIdsController.dispose();
    super.dispose();
  }

  List<String> _parseTestUserIds() {
    return _testUserIdsController.text
        .split(RegExp(r'[,，\s]+'))
        .map((s) => s.trim())
        .where((s) => s.isNotEmpty)
        .toList();
  }

  @override
  void initState() {
    super.initState();
    _loadTemplates();
  }

  Future<void> _loadTemplates() async {
    try {
      final result = await ReachService.listTemplates(page: 1, pageSize: 100);
      if (!mounted) return;
      setState(() {
        _templates = result.items;
        _loadingTemplates = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() => _loadingTemplates = false);
      Toast.error('加载模板失败: $e');
    }
  }

  Future<void> _create() async {
    if (_selectedTemplateId == null) {
      Toast.error('请选择模板');
      return;
    }
    if (_channels.isEmpty) {
      Toast.error('请选择至少一个通道');
      return;
    }
    final testUserIds = _parseTestUserIds();
    if (testUserIds.any((s) => int.tryParse(s) == null)) {
      Toast.error('试发用户ID必须是数字，多个用逗号分隔');
      return;
    }
    setState(() => _busy = true);
    try {
      await ReachService.createMarketingTask(
        templateId: _selectedTemplateId!,
        channels: _channels.toList(),
        audience: testUserIds.isNotEmpty
            ? {'test_user_ids': testUserIds}
            : null,
        scheduledAt: _scheduledAt,
      );
      Toast.success(testUserIds.isNotEmpty ? '试发任务已创建' : '营销任务已创建');
      if (Get.isRegistered<ReachTasksController>()) {
        Get.find<ReachTasksController>().reloadFromFirstPage();
      }
      Get.back();
    } catch (e) {
      Toast.error(e.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _pickSchedule() async {
    final date = await showDatePicker(
      context: context,
      initialDate: DateTime.now().add(const Duration(hours: 1)),
      firstDate: DateTime.now(),
      lastDate: DateTime.now().add(const Duration(days: 365)),
    );
    if (date == null || !mounted) return;
    final time = await showTimePicker(
      context: context,
      initialTime: TimeOfDay.now(),
    );
    if (time == null || !mounted) return;
    setState(() {
      _scheduledAt = DateTime(
        date.year,
        date.month,
        date.day,
        time.hour,
        time.minute,
      );
    });
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      insetPadding: kDialogInsetPadding,
      title: const Text('创建营销任务'),
      content: DialogContentBox(
        child: _loadingTemplates
            ? const SizedBox(
                height: 120,
                child: Center(child: CircularProgressIndicator()),
              )
            : SingleChildScrollView(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    DropdownButtonFormField<String>(
                      initialValue: _selectedTemplateId,
                      decoration: const InputDecoration(labelText: '选择模板 *'),
                      items: (_templates ?? [])
                          .map(
                            (t) => DropdownMenuItem(
                              value: t.id,
                              child: Text(
                                t.name,
                                overflow: TextOverflow.ellipsis,
                              ),
                            ),
                          )
                          .toList(),
                      onChanged: (v) => setState(() => _selectedTemplateId = v),
                    ),
                    const SizedBox(height: 16),
                    const Text('通道 *', style: TextStyle(fontSize: 13)),
                    const SizedBox(height: 4),
                    Wrap(
                      spacing: 8,
                      children:
                          [
                            _ChannelChip('in_app', '站内信'),
                            _ChannelChip('push', 'Push'),
                            _ChannelChip('email', '邮件'),
                          ].map((c) {
                            final selected = _channels.contains(c.key);
                            return FilterChip(
                              label: Text(c.label),
                              selected: selected,
                              labelStyle: TextStyle(
                                color: selected ? Colors.white : null,
                              ),
                              checkmarkColor: Colors.white,
                              onSelected: (on) {
                                setState(() {
                                  if (on) {
                                    _channels.add(c.key);
                                  } else {
                                    _channels.remove(c.key);
                                  }
                                });
                              },
                            );
                          }).toList(),
                    ),
                    const SizedBox(height: 12),
                    TextField(
                      controller: _testUserIdsController,
                      decoration: const InputDecoration(
                        labelText: '试发用户ID（可选）',
                        helperText: '填写后仅发给这些用户做预览，忽略订阅设置；多个用逗号分隔',
                        helperMaxLines: 2,
                      ),
                    ),
                    const SizedBox(height: 12),
                    Row(
                      children: [
                        Expanded(
                          child: Text(
                            _scheduledAt != null
                                ? '计划: ${_scheduledAt!.toString().substring(0, 16)}'
                                : '立即发送',
                            style: const TextStyle(fontSize: 13),
                          ),
                        ),
                        TextButton(
                          onPressed: _pickSchedule,
                          child: Text(_scheduledAt != null ? '修改时间' : '设定时间'),
                        ),
                        if (_scheduledAt != null)
                          TextButton(
                            onPressed: () =>
                                setState(() => _scheduledAt = null),
                            child: const Text('清除'),
                          ),
                      ],
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
          onPressed: _busy || _loadingTemplates ? null : _create,
          child: Text(_busy ? '创建中…' : '创建'),
        ),
      ],
    );
  }
}

class _ChannelChip {
  const _ChannelChip(this.key, this.label);
  final String key;
  final String label;
}
