import 'package:flutter/widgets.dart';
import 'package:get/get.dart';

import '../../core/network/page_result.dart';
import '../../shared/controllers/paged_list_controller.dart';
import '../../shared/widgets/confirm_dialog.dart';
import 'gateway_models.dart';
import 'gateway_service.dart';

/// 网关钱包列表：可按 owner_id 精确搜索，支持选用户发Key（钱包按需自动创建）。
class GatewayWalletsController extends PagedListController<GatewayWallet> {
  final RxString ownerIdFilter = ''.obs;
  final TextEditingController searchCtrl = TextEditingController();

  @override
  Future<PageResult<GatewayWallet>> fetchPage() {
    return GatewayService.listWallets(
      ownerId: ownerIdFilter.value,
      page: page.value,
      pageSize: pageSize.value,
    );
  }

  void applySearch(String value) {
    ownerIdFilter.value = value.trim();
    reloadFromFirstPage();
  }

  /// 给指定用户发一把虚拟Key：钱包不存在时后端自动创建（get-or-create）。
  /// 成功返回明文Key（仅此一次可见），失败提示错误并返回 null。
  Future<String?> issueKeyForUser(String ownerId, String label) async {
    try {
      final wallet = await GatewayService.createWallet(ownerId);
      final issued = await GatewayService.issueVirtualKey(wallet.id, label);
      await reloadFromFirstPage();
      return issued.plainKey;
    } catch (e) {
      Toast.error(e.toString());
      return null;
    }
  }

  @override
  void onClose() {
    searchCtrl.dispose();
    super.onClose();
  }
}
