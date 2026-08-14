import 'package:get/get.dart';

import '../../core/network/page_result.dart';
import '../../shared/controllers/paged_list_controller.dart';
import 'gateway_models.dart';
import 'gateway_service.dart';

/// 对账报告：按厂商过滤，只读，用于查每一轮"厂商真实花费 vs 我方流水理论花费"的比对结果。
class GatewayReconciliationController extends PagedListController<GatewayReconciliationReport> {
  final RxString providerFilter = ''.obs;

  @override
  Future<PageResult<GatewayReconciliationReport>> fetchPage() {
    return GatewayService.listReconciliationReports(
      provider: providerFilter.value,
      page: page.value,
      pageSize: pageSize.value,
    );
  }

  void changeProvider(String value) {
    if (providerFilter.value == value) return;
    providerFilter.value = value;
    reloadFromFirstPage();
  }
}
