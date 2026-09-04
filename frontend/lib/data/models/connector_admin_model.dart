/// 连接器管理（手机端装/建 agent）的错误码与错误类型。
///
/// 手机端没有本机 127 admin API，装/建 agent 走 ws：客户端 → 后端 →
/// 目标主机上的 connector（connector_admin local_action）。这里的错误码来自
/// 后端 `agent_connector_admin_resp.error_code`，UI 据此给出不同提示，
/// 尤其要把「连接器太老」和一次性失败区分开。
class ConnectorAdminErrorCode {
  const ConnectorAdminErrorCode._();

  /// 该主机没有在线 agent 可当通道。
  static const String offline = 'offline';

  /// 连接器版本太老，不认识 connector_admin —— 提示用户升级连接器。
  static const String unsupported = 'unsupported';

  /// 连接器没在时限内回执。
  static const String timeout = 'timeout';

  /// 请求者不是该 agent 的主人（被共享者一律拒绝）。
  static const String forbidden = 'forbidden';

  /// 写操作触发限频。
  static const String rateLimited = 'rate_limited';

  // —— 以下由连接器回执信封 {ok:false, error_code} 原样透传 ——

  /// 连接器不认识这个 op（连接器较老，或该 op 已下线）。
  static const String unsupportedOp = 'unsupported_op';

  /// 连接器侧关掉了远程管理开关。
  static const String remoteAdminDisabled = 'remote_admin_disabled';

  /// 连接器侧远程管理暂时不可用。
  static const String remoteAdminUnavailable = 'remote_admin_unavailable';

  /// 需要用户升级连接器才能解决的错误码。
  static const Set<String> upgradeRequired = {unsupported, unsupportedOp};
}

/// 连接器管理指令失败。[code] 见 [ConnectorAdminErrorCode]，可能为空。
class ConnectorAdminException implements Exception {
  const ConnectorAdminException(this.message, {this.code = ''});

  final String message;
  final String code;

  bool get isUnsupported =>
      ConnectorAdminErrorCode.upgradeRequired.contains(code);
  bool get isOffline => code == ConnectorAdminErrorCode.offline;

  @override
  String toString() => message;
}

/// 可安装的 agent 客户端类型（连接器 GET /api/install 的一项）。
class ConnectorInstallableAgent {
  const ConnectorInstallableAgent({
    required this.agentType,
    required this.label,
    this.description = '',
    this.version = '',
    this.installed = false,
  });

  final String agentType;
  final String label;
  final String description;
  final String version;
  final bool installed;

  factory ConnectorInstallableAgent.fromJson(Map<String, dynamic> json) {
    final type = (json['agentType'] ?? json['agent_type'] ?? '')
        .toString()
        .trim();
    return ConnectorInstallableAgent(
      agentType: type,
      label: (json['label'] ?? '').toString().trim().isEmpty
          ? type
          : json['label'].toString().trim(),
      description: json['description']?.toString() ?? '',
      version: json['version']?.toString() ?? '',
      installed: json['installed'] == true,
    );
  }
}

/// list_installable 的回执：与连接器 GET /api/install 一致的对象。
class ConnectorInstallableList {
  const ConnectorInstallableList({this.platform = '', this.agents = const []});

  final String platform;
  final List<ConnectorInstallableAgent> agents;

  factory ConnectorInstallableList.fromJson(Map<String, dynamic> json) {
    final raw = json['agents'];
    return ConnectorInstallableList(
      platform: json['platform']?.toString() ?? '',
      agents: raw is List
          ? raw
                .whereType<Map>()
                .map(
                  (item) => ConnectorInstallableAgent.fromJson(
                    Map<String, dynamic>.from(item),
                  ),
                )
                .where((item) => item.agentType.isNotEmpty)
                .toList()
          : const [],
    );
  }
}

/// 安装进度（连接器 GET /api/install/:agentType）。
class ConnectorInstallProgress {
  const ConnectorInstallProgress({
    required this.status,
    this.progress,
    this.message = '',
    this.error = '',
  });

  /// pending / downloading / installing / done / error / unknown
  final String status;
  final double? progress;
  final String message;
  final String error;

  bool get isDone => status == 'done';
  bool get isError => status == 'error';

  /// unknown 既可能是「还没开始记录」也可能是「已经装完、进度记录被清了」，
  /// 不当失败处理，交给调用方按超时和探测结果收口。
  bool get isUnknown => status == 'unknown';

  factory ConnectorInstallProgress.fromJson(Map<String, dynamic> json) =>
      ConnectorInstallProgress(
        status: (json['status'] ?? 'unknown').toString(),
        progress: (json['progress'] as num?)?.toDouble(),
        message: json['message']?.toString() ?? '',
        error: json['error']?.toString() ?? '',
      );
}

/// create_agent 成功后后端回给客户端的字段（都是 REST 创建接口本来就会回的）。
class ConnectorCreatedAgent {
  const ConnectorCreatedAgent({
    required this.agentId,
    required this.agentName,
    this.clientType = '',
    this.sessionId = '',
  });

  final String agentId;
  final String agentName;
  final String clientType;
  final String sessionId;

  factory ConnectorCreatedAgent.fromJson(Map<String, dynamic> json) =>
      ConnectorCreatedAgent(
        agentId: json['agent_id']?.toString() ?? '',
        agentName: json['agent_name']?.toString() ?? '',
        clientType: json['client_type']?.toString() ?? '',
        sessionId: json['session_id']?.toString() ?? '',
      );
}
