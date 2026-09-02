/// 大模型计费网关：钱包、虚拟Key、消费流水、充值流水、价目表、对账报告。
/// 后端所有大整数ID字段都以JSON字符串传输（避免前端数字精度丢失），这里统一按 String 接收。
class GatewayWallet {
  GatewayWallet({
    required this.id,
    required this.ownerId,
    required this.balance,
    required this.createdAt,
  });

  final String id;
  final String ownerId;
  final String balance;
  final DateTime createdAt;

  factory GatewayWallet.fromJson(Map<String, dynamic> j) => GatewayWallet(
    id: (j['id'] ?? '').toString(),
    ownerId: (j['owner_id'] ?? '').toString(),
    balance: (j['balance'] ?? '0').toString(),
    createdAt:
        DateTime.tryParse((j['created_at'] ?? '').toString()) ?? DateTime.now(),
  );
}

class GatewayVirtualKey {
  GatewayVirtualKey({
    required this.id,
    required this.walletId,
    required this.keyHint,
    required this.label,
    required this.status,
    required this.createdAt,
    this.revokedAt,
  });

  final String id;
  final String walletId;
  final String keyHint;
  final String label;
  final String status; // active/revoked
  final DateTime createdAt;
  final DateTime? revokedAt;

  bool get isActive => status == 'active';

  factory GatewayVirtualKey.fromJson(Map<String, dynamic> j) =>
      GatewayVirtualKey(
        id: (j['id'] ?? '').toString(),
        walletId: (j['wallet_id'] ?? '').toString(),
        keyHint: (j['key_hint'] ?? '').toString(),
        label: (j['label'] ?? '').toString(),
        status: (j['status'] ?? '').toString(),
        createdAt:
            DateTime.tryParse((j['created_at'] ?? '').toString()) ??
            DateTime.now(),
        revokedAt: j['revoked_at'] == null
            ? null
            : DateTime.tryParse(j['revoked_at'].toString()),
      );
}

class GatewayLedgerEntry {
  GatewayLedgerEntry({
    required this.id,
    required this.provider,
    required this.model,
    required this.promptTokens,
    required this.cachedTokens,
    required this.completionTokens,
    required this.cost,
    required this.balanceAfter,
    required this.status,
    required this.createdAt,
  });

  final String id;
  final String provider;
  final String model;
  final int promptTokens;
  final int cachedTokens;
  final int completionTokens;
  final String cost;
  final String? balanceAfter;
  final String status; // settled/failed/rejected_insufficient_balance
  final DateTime createdAt;

  factory GatewayLedgerEntry.fromJson(Map<String, dynamic> j) =>
      GatewayLedgerEntry(
        id: (j['id'] ?? '').toString(),
        provider: (j['provider'] ?? '').toString(),
        model: (j['model'] ?? '').toString(),
        promptTokens: (j['prompt_tokens'] as num?)?.toInt() ?? 0,
        cachedTokens: (j['cached_tokens'] as num?)?.toInt() ?? 0,
        completionTokens: (j['completion_tokens'] as num?)?.toInt() ?? 0,
        cost: (j['cost'] ?? '0').toString(),
        balanceAfter: j['balance_after']?.toString(),
        status: (j['status'] ?? '').toString(),
        createdAt:
            DateTime.tryParse((j['created_at'] ?? '').toString()) ??
            DateTime.now(),
      );
}

class GatewayTopupRecord {
  GatewayTopupRecord({
    required this.id,
    required this.sourceCurrency,
    required this.sourceAmount,
    required this.fxRateUsed,
    required this.creditedAmount,
    required this.channel,
    required this.reference,
    required this.createdAt,
  });

  final String id;
  final String sourceCurrency;
  final String sourceAmount;
  final String fxRateUsed;
  final String creditedAmount;
  final String channel;
  final String reference;
  final DateTime createdAt;

  factory GatewayTopupRecord.fromJson(Map<String, dynamic> j) =>
      GatewayTopupRecord(
        id: (j['id'] ?? '').toString(),
        sourceCurrency: (j['source_currency'] ?? '').toString(),
        sourceAmount: (j['source_amount'] ?? '0').toString(),
        fxRateUsed: (j['fx_rate_used'] ?? '0').toString(),
        creditedAmount: (j['credited_amount'] ?? '0').toString(),
        channel: (j['payment_channel'] ?? '').toString(),
        reference: (j['payment_reference'] ?? '').toString(),
        createdAt:
            DateTime.tryParse((j['created_at'] ?? '').toString()) ??
            DateTime.now(),
      );
}

class GatewayPricingRule {
  GatewayPricingRule({
    required this.id,
    required this.provider,
    required this.model,
    required this.cachedInputPricePerM,
    required this.uncachedInputPricePerM,
    required this.outputPricePerM,
    required this.sourceCurrency,
    required this.createdBy,
    required this.effectiveFrom,
    this.effectiveTo,
    this.dailyWindowStartMin,
    this.dailyWindowEndMin,
  });

  final String id;
  final String provider;
  final String model;
  final String cachedInputPricePerM;
  final String uncachedInputPricePerM;
  final String outputPricePerM;
  final String sourceCurrency;
  final String createdBy; // manual/auto_reconciliation
  final DateTime effectiveFrom;
  final DateTime? effectiveTo;
  // 分时定价时段：北京时间当日分钟数[0,1440)；两者都为null表示全天兜底价。
  final int? dailyWindowStartMin;
  final int? dailyWindowEndMin;

  bool get isActive => effectiveTo == null;

  /// 时段的人类可读描述（北京时间）。
  String get windowLabel {
    if (dailyWindowStartMin == null || dailyWindowEndMin == null) return '全天';
    String fmt(int m) =>
        '${(m ~/ 60).toString().padLeft(2, '0')}:${(m % 60).toString().padLeft(2, '0')}';
    return '北京 ${fmt(dailyWindowStartMin!)}-${fmt(dailyWindowEndMin!)}';
  }

  factory GatewayPricingRule.fromJson(Map<String, dynamic> j) =>
      GatewayPricingRule(
        id: (j['id'] ?? '').toString(),
        provider: (j['provider'] ?? '').toString(),
        model: (j['model'] ?? '').toString(),
        cachedInputPricePerM: (j['cached_input_price_per_m'] ?? '0').toString(),
        uncachedInputPricePerM: (j['uncached_input_price_per_m'] ?? '0')
            .toString(),
        outputPricePerM: (j['output_price_per_m'] ?? '0').toString(),
        sourceCurrency: (j['source_currency'] ?? '').toString(),
        createdBy: (j['created_by'] ?? '').toString(),
        effectiveFrom:
            DateTime.tryParse((j['effective_from'] ?? '').toString()) ??
            DateTime.now(),
        effectiveTo: j['effective_to'] == null
            ? null
            : DateTime.tryParse(j['effective_to'].toString()),
        dailyWindowStartMin: (j['daily_window_start_min'] as num?)?.toInt(),
        dailyWindowEndMin: (j['daily_window_end_min'] as num?)?.toInt(),
      );
}

/// 上游厂商官方凭据（推理转发Key / 对账AK-SK）。后端只回末4位(keyHint)，绝不回明文/密文。
class GatewayUpstreamCredential {
  GatewayUpstreamCredential({
    required this.id,
    required this.provider,
    required this.purpose,
    required this.keyHint,
    required this.baseUrl,
    required this.region,
    required this.label,
    required this.enabled,
    required this.createdAt,
  });

  final String id;
  final String provider; // deepseek / volcano_ark
  final String purpose; // inference(推理转发) / reconcile(对账)
  final String keyHint; // 明文末4位
  final String baseUrl;
  final String region;
  final String label;
  final bool enabled;
  final DateTime createdAt;

  bool get isInference => purpose == 'inference';

  factory GatewayUpstreamCredential.fromJson(Map<String, dynamic> j) =>
      GatewayUpstreamCredential(
        id: (j['id'] ?? '').toString(),
        provider: (j['provider'] ?? '').toString(),
        purpose: (j['purpose'] ?? 'inference').toString(),
        keyHint: (j['key_hint'] ?? '').toString(),
        baseUrl: (j['base_url'] ?? '').toString(),
        region: (j['region'] ?? '').toString(),
        label: (j['label'] ?? '').toString(),
        enabled: j['enabled'] == true,
        createdAt:
            DateTime.tryParse((j['created_at'] ?? '').toString()) ??
            DateTime.now(),
      );
}

class GatewayReconciliationReport {
  GatewayReconciliationReport({
    required this.id,
    required this.provider,
    required this.windowStart,
    required this.windowEnd,
    required this.vendorActualCost,
    required this.ledgerExpectedCost,
    required this.diff,
    required this.diffRatio,
    required this.status,
    required this.autoAdjusted,
    required this.createdAt,
  });

  final String id;
  final String provider;
  final DateTime windowStart;
  final DateTime windowEnd;
  final String vendorActualCost;
  final String ledgerExpectedCost;
  final String diff;
  final String? diffRatio;
  final String status; // ok/warning/critical
  final bool autoAdjusted;
  final DateTime createdAt;

  factory GatewayReconciliationReport.fromJson(
    Map<String, dynamic> j,
  ) => GatewayReconciliationReport(
    id: (j['id'] ?? '').toString(),
    provider: (j['provider'] ?? '').toString(),
    windowStart:
        DateTime.tryParse((j['window_start'] ?? '').toString()) ??
        DateTime.now(),
    windowEnd:
        DateTime.tryParse((j['window_end'] ?? '').toString()) ?? DateTime.now(),
    vendorActualCost: (j['vendor_actual_cost'] ?? '0').toString(),
    ledgerExpectedCost: (j['ledger_expected_cost'] ?? '0').toString(),
    diff: (j['diff'] ?? '0').toString(),
    diffRatio: j['diff_ratio']?.toString(),
    status: (j['status'] ?? '').toString(),
    autoAdjusted: j['auto_adjusted'] == true,
    createdAt:
        DateTime.tryParse((j['created_at'] ?? '').toString()) ?? DateTime.now(),
  );
}
