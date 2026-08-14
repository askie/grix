import 'package:flutter/widgets.dart';
import 'package:get/get.dart';

import '../../core/network/page_result.dart';
import '../../shared/controllers/paged_list_controller.dart';
import '../../shared/widgets/confirm_dialog.dart';
import 'visitor_ban_item.dart';
import 'visitor_ban_service.dart';

/// Widget 访客封禁列表控制器。
class VisitorBansController extends PagedListController<VisitorBanItem> {
  final RxString statusFilter = 'banned'.obs;
  final RxString keyword = ''.obs;
  final TextEditingController searchCtrl = TextEditingController();

  @override
  Future<PageResult<VisitorBanItem>> fetchPage() {
    return VisitorBanService.list(
      query: keyword.value,
      status: statusFilter.value,
      page: page.value,
      pageSize: pageSize.value,
    );
  }

  void applySearch(String value) {
    keyword.value = value.trim();
    reloadFromFirstPage();
  }

  void changeStatus(String status) {
    if (statusFilter.value == status) return;
    statusFilter.value = status;
    reloadFromFirstPage();
  }

  void resetFilters() {
    statusFilter.value = 'banned';
    searchCtrl.clear();
    keyword.value = '';
    reloadFromFirstPage();
  }

  Future<void> unban(VisitorBanItem item) async {
    final ok = await ConfirmDialog.show(
      title: '解封访客',
      message:
          '确定要解封 ${item.visitorDisplayName} 吗？\n同一站点下该访客的封禁会话和对应 IP 封禁规则会一并解除。',
      confirmText: '解封',
    );
    if (!ok) return;
    await runAction(() => VisitorBanService.unban(item.sessionId), '访客已解封');
  }

  @override
  void onClose() {
    searchCtrl.dispose();
    super.onClose();
  }
}
