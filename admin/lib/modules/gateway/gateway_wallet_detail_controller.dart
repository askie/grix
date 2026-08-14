import 'package:get/get.dart';

import '../../shared/widgets/confirm_dialog.dart';
import 'gateway_models.dart';
import 'gateway_service.dart';

/// 钱包详情：余额、虚拟Key列表、消费流水、充值流水；充值/发Key/吊销Key 都在这一页操作。
class GatewayWalletDetailController extends GetxController {
  GatewayWalletDetailController(this.walletId);

  final String walletId;

  final Rxn<GatewayWallet> wallet = Rxn<GatewayWallet>();
  final RxList<GatewayVirtualKey> keys = <GatewayVirtualKey>[].obs;
  final RxList<GatewayLedgerEntry> ledger = <GatewayLedgerEntry>[].obs;
  final RxList<GatewayTopupRecord> topups = <GatewayTopupRecord>[].obs;

  final RxBool loading = false.obs;
  final RxnString error = RxnString();

  @override
  void onInit() {
    super.onInit();
    load();
  }

  Future<void> load() async {
    loading.value = true;
    error.value = null;
    try {
      final detail = await GatewayService.walletDetail(walletId);
      wallet.value = detail.wallet;
      keys.assignAll(detail.keys);
      final ledgerPage = await GatewayService.listLedger(walletId, pageSize: 50);
      ledger.assignAll(ledgerPage.items);
      final topupPage = await GatewayService.listTopups(walletId, pageSize: 50);
      topups.assignAll(topupPage.items);
    } catch (e) {
      error.value = e.toString();
    } finally {
      loading.value = false;
    }
  }

  Future<void> topup({
    required String sourceCurrency,
    required String sourceAmount,
    required String fxRate,
    required String reference,
  }) async {
    try {
      final w = await GatewayService.topup(
        walletId,
        sourceCurrency: sourceCurrency,
        sourceAmount: sourceAmount,
        fxRate: fxRate,
        reference: reference,
      );
      wallet.value = w;
      Toast.success('充值成功，当前余额 ${w.balance} USD');
      await load();
    } catch (e) {
      Toast.error(e.toString());
    }
  }

  /// 发一把新Key；明文只返回这一次，调用方负责展示给管理员当场保存。
  Future<String?> issueKey(String label) async {
    try {
      final result = await GatewayService.issueVirtualKey(walletId, label);
      await load();
      return result.plainKey;
    } catch (e) {
      Toast.error(e.toString());
      return null;
    }
  }

  Future<void> revokeKey(String keyId) async {
    try {
      await GatewayService.revokeVirtualKey(keyId);
      Toast.success('Key已吊销');
      await load();
    } catch (e) {
      Toast.error(e.toString());
    }
  }
}
