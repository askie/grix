import 'package:get/get.dart';

import '../../core/network/page_result.dart';
import '../../shared/controllers/paged_list_controller.dart';
import 'gateway_models.dart';
import 'gateway_service.dart';

/// 上游厂商凭据：按厂商过滤，支持新增(密文落库)、启用/停用、删除。
/// 明文只在新增时提交一次，之后系统只存密文，列表只回末4位。
class GatewayCredentialsController
    extends PagedListController<GatewayUpstreamCredential> {
  final RxString providerFilter = ''.obs;

  @override
  Future<PageResult<GatewayUpstreamCredential>> fetchPage() {
    return GatewayService.listUpstreamCredentials(
      provider: providerFilter.value,
    );
  }

  void changeProvider(String value) {
    if (providerFilter.value == value) return;
    providerFilter.value = value;
    reloadFromFirstPage();
  }

  Future<void> createCredential({
    required String provider,
    required String purpose,
    required String apiKey,
    String apiSecret = '',
    String baseUrl = '',
    String region = '',
    String label = '',
  }) {
    return runAction(
      () => GatewayService.createUpstreamCredential(
        provider: provider,
        purpose: purpose,
        apiKey: apiKey,
        apiSecret: apiSecret,
        baseUrl: baseUrl,
        region: region,
        label: label,
      ),
      '凭据已添加（最长15秒后各网关副本自动生效）',
    );
  }

  Future<void> setEnabled(String id, bool enabled) {
    return runAction(
      () => GatewayService.setUpstreamCredentialEnabled(id, enabled),
      // 停用/启用不是即时生效：各网关副本有最长15秒的凭据缓存，紧急吊销泄露Key时要知道这个窗口。
      enabled ? '已启用（最长15秒后各网关副本生效）' : '已停用（最长15秒后各网关副本停止使用该Key）',
    );
  }

  Future<void> deleteCredential(String id) {
    return runAction(
      () => GatewayService.deleteUpstreamCredential(id),
      '已删除（最长15秒后各网关副本停止使用该Key）',
    );
  }
}
