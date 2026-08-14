import 'package:flutter/widgets.dart';
import 'package:get/get.dart';

import '../../core/network/page_result.dart';
import '../../shared/controllers/paged_list_controller.dart';
import 'report_models.dart';
import 'report_service.dart';

/// 举报列表控制器：关键词 + 状态 + 对象类型 + 原因 多维筛选。
class ReportsController extends PagedListController<ReportListItem> {
  final RxString statusFilter = ''.obs; // ''/pending/review/resolved
  final RxString targetTypeFilter = ''.obs; // ''/user/group
  final RxString keyword = ''.obs;
  final RxString reasonCode = ''.obs;

  final TextEditingController searchCtrl = TextEditingController();

  @override
  Future<PageResult<ReportListItem>> fetchPage() {
    return ReportService.list(
      query: keyword.value,
      status: statusFilter.value,
      targetType: targetTypeFilter.value,
      reasonCode: reasonCode.value,
      page: page.value,
      pageSize: pageSize.value,
    );
  }

  void applySearch(String value) {
    keyword.value = value.trim();
    reloadFromFirstPage();
  }

  void changeStatus(String value) {
    if (statusFilter.value == value) return;
    statusFilter.value = value;
    reloadFromFirstPage();
  }

  void changeTargetType(String value) {
    if (targetTypeFilter.value == value) return;
    targetTypeFilter.value = value;
    reloadFromFirstPage();
  }

  void changeReasonCode(String value) {
    if (reasonCode.value == value) return;
    reasonCode.value = value;
    reloadFromFirstPage();
  }

  /// 当前激活的筛选条件数量（用于角标）。
  int get activeFilterCount {
    int count = 0;
    if (statusFilter.value.isNotEmpty) count++;
    if (targetTypeFilter.value.isNotEmpty) count++;
    if (reasonCode.value.isNotEmpty) count++;
    return count;
  }

  void resetFilters() {
    statusFilter.value = '';
    targetTypeFilter.value = '';
    reasonCode.value = '';
    searchCtrl.clear();
    keyword.value = '';
    reloadFromFirstPage();
  }

  @override
  void onClose() {
    searchCtrl.dispose();
    super.onClose();
  }
}
