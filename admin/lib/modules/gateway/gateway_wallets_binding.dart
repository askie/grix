import 'package:get/get.dart';

import 'gateway_credentials_controller.dart';
import 'gateway_pricing_controller.dart';
import 'gateway_reconciliation_controller.dart';
import 'gateway_wallet_detail_controller.dart';
import 'gateway_wallets_controller.dart';

class GatewayWalletsBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<GatewayWalletsController>(() => GatewayWalletsController());
  }
}

class GatewayWalletDetailBinding extends Bindings {
  @override
  void dependencies() {
    final id = Get.parameters['id'] ?? '';
    Get.lazyPut<GatewayWalletDetailController>(
      () => GatewayWalletDetailController(id),
    );
  }
}

class GatewayPricingRulesBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<GatewayPricingController>(() => GatewayPricingController());
  }
}

class GatewayReconciliationReportsBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<GatewayReconciliationController>(
      () => GatewayReconciliationController(),
    );
  }
}

class GatewayUpstreamCredentialsBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<GatewayCredentialsController>(
      () => GatewayCredentialsController(),
    );
  }
}
