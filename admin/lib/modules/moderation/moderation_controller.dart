import 'package:flutter/widgets.dart';
import 'package:get/get.dart';

import '../../core/network/page_result.dart';
import '../../shared/controllers/paged_list_controller.dart';
import '../../shared/widgets/confirm_dialog.dart';
import 'moderation_models.dart';
import 'moderation_service.dart';

/// 内容审查事件列表控制器：关键词 + 仅看禁言，支持解除禁言。
class ModerationController extends PagedListController<ModerationEvent> {
  final RxString keyword = ''.obs;
  final RxBool mutedOnly = false.obs;
  final TextEditingController searchCtrl = TextEditingController();

  @override
  Future<PageResult<ModerationEvent>> fetchPage() {
    return ModerationService.listEvents(
      query: keyword.value,
      mutedOnly: mutedOnly.value,
      page: page.value,
      pageSize: pageSize.value,
    );
  }

  void applySearch(String value) {
    keyword.value = value.trim();
    reloadFromFirstPage();
  }

  void toggleMutedOnly(bool value) {
    mutedOnly.value = value;
    reloadFromFirstPage();
  }

  void resetFilters() {
    mutedOnly.value = false;
    searchCtrl.clear();
    keyword.value = '';
    reloadFromFirstPage();
  }

  Future<void> unmute(ModerationEvent e) async {
    final ok = await ConfirmDialog.show(
      title: '解除禁言',
      message: '确定解除 ${e.senderName} 在该会话的审查禁言吗？',
      confirmText: '解除',
    );
    if (!ok) return;
    await runAction(
      () => ModerationService.unmute(e.sessionId, e.senderId),
      '禁言已解除',
    );
  }

  @override
  void onClose() {
    searchCtrl.dispose();
    super.onClose();
  }
}
