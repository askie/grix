import 'package:get/get.dart';

import '../../../data/providers/agent_service.dart';
import '../../../shared/utils/toast_util.dart';
import '../models/agent_conn_security_model.dart';

/// agent「连接安全」页控制器：拉取连接（登录）历史与黑名单，支持把某个登录 IP 加入黑名单、删除黑名单。
/// 只对 agent-api 接入类（providerType==3）有意义。
class AgentConnSecurityController extends GetxController {
  final AgentService agentService = Get.find<AgentService>();

  final isLoading = false.obs;
  final isMutating = false.obs;
  final logs = <AgentConnectionLogEntry>[].obs;
  final ipRules = <AgentIPRuleEntry>[].obs;

  final agentId = ''.obs;
  final agentName = ''.obs;
  final providerType = 0.obs;

  bool get canUse => providerType.value == 3;

  /// 已生效的黑名单 IP/CIDR 集合，用于在历史列表里标记「已封禁」。
  Set<String> get bannedTargets =>
      ipRules.where((r) => r.isBan).map((r) => r.ipCidr).toSet();

  /// 当前展示给用户的黑名单规则（只列 ban；白名单暂不在 C 端展示）。
  List<AgentIPRuleEntry> get banRules =>
      ipRules.where((r) => r.isBan).toList(growable: false);

  bool isIPBanned(String ip) => bannedTargets.contains(ip.trim());

  @override
  void onInit() {
    super.onInit();
    _loadArgs();
    _initPage();
  }

  Future<void> _initPage() async {
    await _hydrateAgentMetaIfNeeded();
    if (!canUse) {
      return;
    }
    await loadAll();
  }

  Future<void> loadAll() async {
    if (agentId.value.isEmpty || !canUse || isLoading.value) {
      return;
    }
    isLoading.value = true;
    try {
      final logsResult = await agentService.getAgentConnectionLogs(
        agentId.value,
      );
      if (logsResult.ok) {
        logs.value = logsResult.data ?? const [];
      } else {
        CustomToast.show(logsResult.message, isError: true);
      }

      final rulesResult = await agentService.getAgentIPRules(agentId.value);
      if (rulesResult.ok) {
        ipRules.value = rulesResult.data ?? const [];
      } else {
        CustomToast.show(rulesResult.message, isError: true);
      }
    } finally {
      isLoading.value = false;
    }
  }

  Future<void> _reloadRules() async {
    final rulesResult = await agentService.getAgentIPRules(agentId.value);
    if (rulesResult.ok) {
      ipRules.value = rulesResult.data ?? const [];
    }
  }

  /// 把某个 IP 加入黑名单（立即生效）。
  Future<void> banIP(String ip, {String remark = ''}) async {
    final target = ip.trim();
    if (target.isEmpty || !canUse || isMutating.value) {
      return;
    }
    if (isIPBanned(target)) {
      return;
    }
    isMutating.value = true;
    try {
      final result = await agentService.createAgentIPRule(
        agentId.value,
        ruleType: 'ban',
        ipCidr: target,
        remark: remark,
      );
      if (!result.ok) {
        CustomToast.show(result.message, isError: true);
        return;
      }
      await _reloadRules();
      CustomToast.show('ai_agent_conn_ban_success'.tr);
    } finally {
      isMutating.value = false;
    }
  }

  /// 删除一条黑名单规则。
  Future<void> deleteRule(String ruleId) async {
    final target = ruleId.trim();
    if (target.isEmpty || !canUse || isMutating.value) {
      return;
    }
    isMutating.value = true;
    try {
      final result = await agentService.deleteAgentIPRule(
        agentId.value,
        target,
      );
      if (!result.ok) {
        CustomToast.show(result.message, isError: true);
        return;
      }
      await _reloadRules();
      CustomToast.show('ai_agent_conn_unban_success'.tr);
    } finally {
      isMutating.value = false;
    }
  }

  void _loadArgs() {
    final args = Get.arguments as Map<String, dynamic>? ?? const {};
    agentId.value = args['agent_id']?.toString().trim() ?? '';
    agentName.value = args['agent_name']?.toString().trim() ?? '';
    providerType.value = _parseProviderType(args['provider_type']);

    if (agentId.value.isEmpty) {
      CustomToast.show('ai_agent_conn_target_invalid'.tr, isError: true);
      Get.back();
    }
  }

  Future<void> _hydrateAgentMetaIfNeeded() async {
    if (agentId.value.isEmpty || providerType.value != 0) {
      return;
    }
    final agent = await agentService.getAgent(agentId.value);
    if (agent == null) {
      return;
    }
    providerType.value = agent.providerType;
    if (agentName.value.isEmpty) {
      agentName.value = agent.agentName;
    }
  }

  int _parseProviderType(dynamic raw) {
    if (raw is int) {
      return raw;
    }
    if (raw is num) {
      return raw.toInt();
    }
    if (raw is String) {
      return int.tryParse(raw.trim()) ?? 0;
    }
    return 0;
  }
}
