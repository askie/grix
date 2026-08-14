import 'package:flutter/widgets.dart';
import 'package:get/get.dart';

import '../../core/network/page_result.dart';
import '../../shared/controllers/paged_list_controller.dart';
import 'connector_service.dart';

class ConnectorReportsController extends PagedListController<ConnectorUpgradeReport> {
  final RxString keyword = ''.obs;
  final RxString statusFilter = ''.obs;
  final RxString typeFilter = ''.obs;
  final TextEditingController searchCtrl = TextEditingController();

  @override
  Future<PageResult<ConnectorUpgradeReport>> fetchPage() async {
    final r = await ConnectorService.listReports(
      clientType: typeFilter.value.isEmpty ? null : typeFilter.value,
      toVersion: keyword.value,
      status: statusFilter.value.isEmpty ? null : statusFilter.value,
      page: page.value,
      pageSize: pageSize.value,
    );
    return PageResult(
      items: r.reports,
      total: r.total,
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

  void changeType(String value) {
    if (typeFilter.value == value) return;
    typeFilter.value = value;
    reloadFromFirstPage();
  }

  int get activeFilterCount => (statusFilter.value.isNotEmpty ? 1 : 0) + (typeFilter.value.isNotEmpty ? 1 : 0);

  void resetFilters() {
    statusFilter.value = '';
    typeFilter.value = '';
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
