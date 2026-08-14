class FeatureGateInfo {
  FeatureGateInfo({required this.key, required this.displayName, required this.status, required this.whitelistUserCount, required this.publicOnly});
  final String key;
  final String displayName;
  final String status; // disabled/whitelist/enabled
  final int whitelistUserCount;
  final bool publicOnly; // evaluated pre-login; whitelist mode unavailable

  String get statusText {
    switch (status) {
      case 'enabled': return '全量开启';
      case 'whitelist': return '白名单';
      case 'disabled': return '关闭';
      default: return status;
    }
  }

  factory FeatureGateInfo.fromJson(Map<String, dynamic> j) => FeatureGateInfo(
    key: (j['key'] ?? '').toString(),
    displayName: (j['display_name'] ?? '').toString(),
    status: (j['status'] ?? '').toString(),
    whitelistUserCount: (j['whitelist_user_count'] as num?)?.toInt() ?? 0,
    publicOnly: j['public_only'] == true,
  );
}

class AvailableFeature {
  AvailableFeature({required this.key, required this.displayName});
  final String key;
  final String displayName;
  factory AvailableFeature.fromJson(Map<String, dynamic> j) => AvailableFeature(
    key: (j['key'] ?? '').toString(),
    displayName: (j['display_name'] ?? '').toString(),
  );
}
