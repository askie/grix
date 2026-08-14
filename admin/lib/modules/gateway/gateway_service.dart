import '../../core/network/api_client.dart';
import '../../core/network/page_result.dart';
import 'gateway_models.dart';

/// 大模型计费网关管理 API（对接 /admin/api/gateway/*）。
class GatewayService {
  static Future<PageResult<GatewayWallet>> listWallets({
    String? ownerId,
    int page = 1,
    int pageSize = 20,
  }) async {
    final data = await ApiClient.instance.get('/gateway/wallets', query: {
      if (ownerId != null && ownerId.isNotEmpty) 'owner_id': ownerId,
      'page': page,
      'page_size': pageSize,
    });
    return PageResult.fromData(data, GatewayWallet.fromJson);
  }

  static Future<GatewayWallet> createWallet(String ownerId) async {
    final data = await ApiClient.instance.post('/gateway/wallets', data: {'owner_id': ownerId});
    final map = (data as Map).cast<String, dynamic>();
    return GatewayWallet.fromJson((map['wallet'] as Map).cast<String, dynamic>());
  }

  static Future<({GatewayWallet wallet, List<GatewayVirtualKey> keys})> walletDetail(String id) async {
    final data = await ApiClient.instance.get('/gateway/wallets/$id');
    final map = (data as Map).cast<String, dynamic>();
    final wallet = GatewayWallet.fromJson((map['wallet'] as Map).cast<String, dynamic>());
    final keys = ((map['virtual_keys'] as List?) ?? [])
        .map((e) => GatewayVirtualKey.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
    return (wallet: wallet, keys: keys);
  }

  static Future<GatewayWallet> topup(
    String walletId, {
    required String sourceCurrency,
    required String sourceAmount,
    required String fxRate,
    String channel = 'manual',
    String reference = '',
  }) async {
    final data = await ApiClient.instance.post('/gateway/wallets/$walletId/topup', data: {
      'source_currency': sourceCurrency,
      'source_amount': sourceAmount,
      'fx_rate': fxRate,
      'channel': channel,
      'reference': reference,
    });
    final map = (data as Map).cast<String, dynamic>();
    return GatewayWallet.fromJson((map['wallet'] as Map).cast<String, dynamic>());
  }

  static Future<PageResult<GatewayLedgerEntry>> listLedger(
    String walletId, {
    int page = 1,
    int pageSize = 20,
  }) async {
    final data = await ApiClient.instance.get('/gateway/wallets/$walletId/ledger', query: {
      'page': page,
      'page_size': pageSize,
    });
    return PageResult.fromData(data, GatewayLedgerEntry.fromJson);
  }

  static Future<PageResult<GatewayTopupRecord>> listTopups(
    String walletId, {
    int page = 1,
    int pageSize = 20,
  }) async {
    final data = await ApiClient.instance.get('/gateway/wallets/$walletId/topups', query: {
      'page': page,
      'page_size': pageSize,
    });
    return PageResult.fromData(data, GatewayTopupRecord.fromJson);
  }

  /// 发一把新的虚拟Key；返回明文Key，只在这一次拿得到，之后系统只存哈希。
  static Future<({String plainKey, GatewayVirtualKey key})> issueVirtualKey(
    String walletId,
    String label,
  ) async {
    final data = await ApiClient.instance.post('/gateway/wallets/$walletId/keys', data: {'label': label});
    final map = (data as Map).cast<String, dynamic>();
    return (
      plainKey: (map['virtual_key'] ?? '').toString(),
      key: GatewayVirtualKey.fromJson((map['key'] as Map).cast<String, dynamic>()),
    );
  }

  static Future<void> revokeVirtualKey(String keyId) {
    return ApiClient.instance.post('/gateway/keys/$keyId/revoke');
  }

  static Future<PageResult<GatewayPricingRule>> listPricingRules({
    String? provider,
    int page = 1,
    int pageSize = 20,
  }) async {
    final data = await ApiClient.instance.get('/gateway/pricing-rules', query: {
      if (provider != null && provider.isNotEmpty) 'provider': provider,
      'page': page,
      'page_size': pageSize,
    });
    return PageResult.fromData(data, GatewayPricingRule.fromJson);
  }

  /// 退休一条价目规则：此后不再参与计价，也不再出现在用户端的可选模型清单里。
  /// 用来清掉历史探测留下的废规则（上游不认的模型别名，用户选中即报错）。
  static Future<void> retirePricingRule(String id) async {
    await ApiClient.instance.post('/gateway/pricing-rules/$id/retire');
  }

  static Future<GatewayPricingRule> createPricingRule({
    required String provider,
    required String model,
    required String cached,
    required String uncached,
    required String output,
    required String sourceCurrency,
    required String fxRate,
    int? windowStartMin,
    int? windowEndMin,
  }) async {
    final data = await ApiClient.instance.post('/gateway/pricing-rules', data: {
      'provider': provider,
      'model': model,
      'cached': cached,
      'uncached': uncached,
      'output': output,
      'source_currency': sourceCurrency,
      'fx_rate': fxRate,
      // 两者都为null后端识别为全天兜底价；有值则是分时价。
      'window_start_min': windowStartMin,
      'window_end_min': windowEndMin,
    });
    final map = (data as Map).cast<String, dynamic>();
    return GatewayPricingRule.fromJson((map['rule'] as Map).cast<String, dynamic>());
  }

  /// 列出上游厂商凭据（后端一次性返回全部，包成单页 PageResult 复用列表控件）。
  static Future<PageResult<GatewayUpstreamCredential>> listUpstreamCredentials({String? provider}) async {
    final data = await ApiClient.instance.get('/gateway/upstream-credentials', query: {
      if (provider != null && provider.isNotEmpty) 'provider': provider,
    });
    final map = (data as Map).cast<String, dynamic>();
    final raw = (map['items'] as List?) ?? const [];
    final items = raw
        .map((e) => GatewayUpstreamCredential.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
    // pageSize 取 items.length+1，保证列表控件判定"无更多"、不再翻页。
    return PageResult(items: items, total: items.length, page: 1, pageSize: items.length + 1);
  }

  static Future<void> createUpstreamCredential({
    required String provider,
    required String purpose,
    required String apiKey,
    String apiSecret = '',
    String baseUrl = '',
    String region = '',
    String label = '',
  }) {
    return ApiClient.instance.post('/gateway/upstream-credentials', data: {
      'provider': provider,
      'purpose': purpose,
      'api_key': apiKey,
      'api_secret': apiSecret,
      'base_url': baseUrl,
      'region': region,
      'label': label,
    });
  }

  static Future<void> setUpstreamCredentialEnabled(String id, bool enabled) {
    return ApiClient.instance.post('/gateway/upstream-credentials/$id/enabled', data: {'enabled': enabled});
  }

  static Future<void> deleteUpstreamCredential(String id) {
    return ApiClient.instance.delete('/gateway/upstream-credentials/$id');
  }

  static Future<PageResult<GatewayReconciliationReport>> listReconciliationReports({
    String? provider,
    int page = 1,
    int pageSize = 20,
  }) async {
    final data = await ApiClient.instance.get('/gateway/reconciliation-reports', query: {
      if (provider != null && provider.isNotEmpty) 'provider': provider,
      'page': page,
      'page_size': pageSize,
    });
    return PageResult.fromData(data, GatewayReconciliationReport.fromJson);
  }
}
