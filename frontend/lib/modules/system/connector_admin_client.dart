import 'package:get/get.dart';

import '../../data/models/connector_admin_model.dart';
import '../../data/providers/im_service.dart';

/// 手机端的「连接器管理」客户端。
///
/// 桌面端直接打本机 connector 的 127.0.0.1 admin API；手机端打不到，于是借一台
/// 该主机上在线的、属于自己的 agent 当通道：客户端 → 后端 → connector
/// （connector_admin local_action）。这里只做协议编解码，权限与路由都在后端。
class ConnectorAdminClient {
  ConnectorAdminClient(this.channelAgentId);

  /// 当通道用的在线 agent；它决定指令落到哪台机器上。
  final String channelAgentId;

  ImService get _im => Get.find<ImService>();

  Future<ConnectorInstallableList> listInstallable() async {
    final result = await _im.requestConnectorAdmin(
      agentId: channelAgentId,
      op: 'list_installable',
    );
    if (result is! Map) return const ConnectorInstallableList();
    return ConnectorInstallableList.fromJson(Map<String, dynamic>.from(result));
  }

  /// 触发安装。连接器是异步受理：立即回 {agentType, status:"started"}，
  /// 已经在装则回 in_progress；两种都表示"已受理"，进度用 [installProgress] 轮询。
  Future<void> install(String agentType) async {
    await _im.requestConnectorAdmin(
      agentId: channelAgentId,
      op: 'install',
      args: {'agent_type': agentType},
    );
  }

  Future<ConnectorInstallProgress> installProgress(String agentType) async {
    final result = await _im.requestConnectorAdmin(
      agentId: channelAgentId,
      op: 'install_progress',
      args: {'agent_type': agentType},
    );
    if (result is! Map) {
      return const ConnectorInstallProgress(status: 'unknown');
    }
    return ConnectorInstallProgress.fromJson(Map<String, dynamic>.from(result));
  }

  /// 建 agent：后端一次性完成「建 Agent 行 → 下发 add_agent 给该主机的连接器」，
  /// 中途失败会把刚建的行删掉，不留孤儿。
  Future<ConnectorCreatedAgent> createAgent({
    required String agentName,
    required String clientType,
  }) async {
    final result = await _im.requestConnectorAdmin(
      agentId: channelAgentId,
      op: 'create_agent',
      args: {'agent_name': agentName, 'client_type': clientType},
    );
    if (result is! Map) {
      throw const ConnectorAdminException('invalid create_agent response');
    }
    return ConnectorCreatedAgent.fromJson(Map<String, dynamic>.from(result));
  }
}
