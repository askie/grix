import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../data/providers/skill_library_service.dart';
import '../../shared/utils/toast_util.dart';
import '../../shared/widgets/app_dialog_style.dart';

/// 自定义技能库管理弹层。
/// 直接读写平台技能库（/v1/skills）；保存后由各机器 connector 自动同步到本地。
/// 入口与位置由调用方决定；本组件只负责库的增删改查 UI。
class SkillLibrarySheet extends StatelessWidget {
  const SkillLibrarySheet._();

  static Future<void> show(BuildContext context) {
    final service = Get.isRegistered<SkillLibraryService>()
        ? Get.find<SkillLibraryService>()
        : Get.put(SkillLibraryService());
    service.refresh();
    return showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      isScrollControlled: true,
      builder: (_) => const SkillLibrarySheet._(),
    );
  }

  SkillLibraryService get _service => Get.find<SkillLibraryService>();

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxHeight: MediaQuery.sizeOf(context).height * 0.8,
        ),
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Text(
                    'skill_library_title'.tr,
                    style: const TextStyle(
                      fontSize: 17,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  const Spacer(),
                  TextButton.icon(
                    onPressed: () => _openEditor(context),
                    icon: const Icon(Icons.add, size: 18),
                    label: Text('skill_library_new'.tr),
                  ),
                ],
              ),
              Text(
                'skill_library_subtitle'.tr,
                style: TextStyle(
                  fontSize: 12,
                  color: Theme.of(context).hintColor,
                ),
              ),
              const SizedBox(height: 8),
              Flexible(
                child: Obx(() {
                  if (_service.loading.value && _service.skills.isEmpty) {
                    return const Padding(
                      padding: EdgeInsets.all(32),
                      child: Center(child: CircularProgressIndicator()),
                    );
                  }
                  if (_service.skills.isEmpty) {
                    return Padding(
                      padding: const EdgeInsets.all(32),
                      child: Center(child: Text('skill_library_empty'.tr)),
                    );
                  }
                  return ListView.separated(
                    shrinkWrap: true,
                    itemCount: _service.skills.length,
                    separatorBuilder: (_, __) => const Divider(height: 1),
                    itemBuilder: (_, i) {
                      final s = _service.skills[i];
                      return ListTile(
                        contentPadding: EdgeInsets.zero,
                        title: Text(s.name),
                        subtitle: Text(
                          'skill_library_version'.trParams({
                            'n': '${s.version}',
                          }),
                          style: const TextStyle(fontSize: 12),
                        ),
                        // 系统内置技能（owner_id=0）只读：不给删除入口，点开只看不改。
                        trailing: s.isSystem
                            ? const Icon(Icons.lock_outline, size: 18)
                            : IconButton(
                                icon: const Icon(
                                  Icons.delete_outline,
                                  size: 20,
                                ),
                                tooltip: 'skill_library_delete'.tr,
                                onPressed: () => _confirmDelete(context, s),
                              ),
                        onTap: () => _openEditor(context, existing: s),
                      );
                    },
                  );
                }),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _confirmDelete(BuildContext context, UserSkillModel s) async {
    final ok = await showAppConfirmDialog(
      context: context,
      title: 'skill_library_delete'.tr,
      message: 'skill_library_delete_confirm'.trParams({'name': s.name}),
      cancelText: 'skill_library_cancel'.tr,
      confirmText: 'skill_library_confirm'.tr,
      isDestructive: true,
    );
    if (ok) {
      final done = await _service.remove(s.id);
      if (!done) _toast('skill_library_op_failed'.tr);
    }
  }

  Future<void> _openEditor(
    BuildContext context, {
    UserSkillModel? existing,
  }) async {
    // 编辑已有技能先拉全文（列表不含正文）。
    UserSkillModel? full = existing;
    if (existing != null && existing.content.isEmpty) {
      full = await _service.fetchContent(existing.id) ?? existing;
    }
    if (!context.mounted) return;
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (_) => _SkillEditor(
        service: _service,
        existing: full,
        readOnly: existing?.isSystem ?? false,
      ),
    );
  }

  void _toast(String msg) {
    CustomToast.show(msg, isError: true);
  }
}

class _SkillEditor extends StatefulWidget {
  const _SkillEditor({
    required this.service,
    this.existing,
    this.readOnly = false,
  });

  final SkillLibraryService service;
  final UserSkillModel? existing;

  /// 系统内置技能只读查看，不提供保存。
  final bool readOnly;

  @override
  State<_SkillEditor> createState() => _SkillEditorState();
}

class _SkillEditorState extends State<_SkillEditor> {
  late final TextEditingController _name = TextEditingController(
    text: widget.existing?.name ?? '',
  );
  late final TextEditingController _content = TextEditingController(
    text: widget.existing?.content ?? '',
  );
  bool _saving = false;

  @override
  void dispose() {
    _name.dispose();
    _content.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    final name = _name.text.trim();
    final content = _content.text;
    if (name.isEmpty || content.trim().isEmpty) return;
    setState(() => _saving = true);
    final s = widget.existing;
    final ok = s == null
        ? await widget.service.create(name, content)
        : await widget.service.update(s.id, name, content);
    if (!mounted) return;
    setState(() => _saving = false);
    if (ok) {
      Navigator.pop(context);
    } else {
      CustomToast.show('skill_library_op_failed'.tr, isError: true);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: EdgeInsets.only(
        left: 16,
        right: 16,
        bottom: MediaQuery.viewInsetsOf(context).bottom + 16,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            widget.existing == null
                ? 'skill_library_new'.tr
                : 'skill_library_edit'.tr,
            style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _name,
            maxLength: 100,
            readOnly: widget.readOnly,
            decoration: InputDecoration(
              labelText: 'skill_library_name'.tr,
              border: const OutlineInputBorder(),
              isDense: true,
            ),
          ),
          const SizedBox(height: 8),
          TextField(
            controller: _content,
            minLines: 6,
            maxLines: 14,
            readOnly: widget.readOnly,
            decoration: InputDecoration(
              labelText: 'skill_library_content'.tr,
              hintText: 'skill_library_content_hint'.tr,
              border: const OutlineInputBorder(),
              alignLabelWithHint: true,
            ),
          ),
          const SizedBox(height: 12),
          if (!widget.readOnly)
            FilledButton(
              onPressed: _saving ? null : _save,
              child: _saving
                  ? const SizedBox(
                      width: 18,
                      height: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : Text('skill_library_save'.tr),
            ),
        ],
      ),
    );
  }
}
