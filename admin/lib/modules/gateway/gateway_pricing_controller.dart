import 'package:get/get.dart';

import '../../core/network/page_result.dart';
import '../../shared/controllers/paged_list_controller.dart';
import 'gateway_models.dart';
import 'gateway_service.dart';

/// 价目表：按厂商过滤，支持新增一条价目规则（会自动收口同厂商同模型上一条生效规则）。
class GatewayPricingController extends PagedListController<GatewayPricingRule> {
  final RxString providerFilter = ''.obs;

  @override
  Future<PageResult<GatewayPricingRule>> fetchPage() {
    return GatewayService.listPricingRules(
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

  /// 退休一条价目规则：此后不再参与计价，也不再出现在用户端的可选模型清单里。
  /// 用于清掉历史探测留下的废规则（上游不认的模型别名，留在表里会被用户选中然后报错）。
  Future<void> retireRule(String id) {
    return runAction(() => GatewayService.retirePricingRule(id), '该价目规则已退休');
  }

  Future<void> createRule({
    required String provider,
    required String model,
    required String cached,
    required String uncached,
    required String output,
    required String sourceCurrency,
    required String fxRate,
    int? windowStartMin,
    int? windowEndMin,
  }) {
    return runAction(
      () => GatewayService.createPricingRule(
        provider: provider,
        model: model,
        cached: cached,
        uncached: uncached,
        output: output,
        sourceCurrency: sourceCurrency,
        fxRate: fxRate,
        windowStartMin: windowStartMin,
        windowEndMin: windowEndMin,
      ),
      '价目规则已生效',
    );
  }
}
