import 'package:get/get.dart';

import '../../../data/providers/agent_service.dart';
import '../../../shared/utils/toast_util.dart';

typedef AgentScopeToastShower = void Function(String message, {bool isError});

class AgentScopeOption {
  const AgentScopeOption({
    required this.scope,
    required this.label,
    required this.description,
  });

  final String scope;
  final String label;
  final String description;
}

class _ScopeI18nMeta {
  const _ScopeI18nMeta({required this.labelKey, required this.descKey});

  final String labelKey;
  final String descKey;
}

class AgentScopeController extends GetxController {
  AgentScopeController({AgentScopeToastShower? showToast})
    : _showToast = showToast ?? CustomToast.show;

  final AgentService agentService = Get.find<AgentService>();
  final AgentScopeToastShower _showToast;

  final isLoading = false.obs;
  final isSaving = false.obs;
  final selectedScopes = <String>[].obs;
  final availableScopes = <String>[].obs;
  final availableScopeItems = <AgentScopeItem>[].obs;

  final agentId = ''.obs;
  final agentName = ''.obs;
  final providerType = 0.obs;

  bool get canConfigure => providerType.value == 3;

  static const Map<String, _ScopeI18nMeta> _knownScopeI18n = {
    'agent.api.create': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_agent_api_create',
      descKey: 'ai_agent_scope_agent_api_create_desc',
    ),
    'agent.category.list': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_agent_category_list',
      descKey: 'ai_agent_scope_agent_category_list_desc',
    ),
    'agent.category.create': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_agent_category_create',
      descKey: 'ai_agent_scope_agent_category_create_desc',
    ),
    'agent.category.update': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_agent_category_update',
      descKey: 'ai_agent_scope_agent_category_update_desc',
    ),
    'agent.category.assign': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_agent_category_assign',
      descKey: 'ai_agent_scope_agent_category_assign_desc',
    ),
    'session.search': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_session_search',
      descKey: 'ai_agent_scope_session_search_desc',
    ),
    'contact.search': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_contact_search',
      descKey: 'ai_agent_scope_contact_search_desc',
    ),
    'group.create': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_group_create',
      descKey: 'ai_agent_scope_group_create_desc',
    ),
    'group.member.add': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_group_member_add',
      descKey: 'ai_agent_scope_group_member_add_desc',
    ),
    'group.member.remove': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_group_member_remove',
      descKey: 'ai_agent_scope_group_member_remove_desc',
    ),
    'group.member.role.update': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_group_member_role_update',
      descKey: 'ai_agent_scope_group_member_role_update_desc',
    ),
    'group.dissolve': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_group_dissolve',
      descKey: 'ai_agent_scope_group_dissolve_desc',
    ),
    'group.speaking.update': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_group_speaking_update',
      descKey: 'ai_agent_scope_group_speaking_update_desc',
    ),
    'agent.dispatch': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_agent_dispatch',
      descKey: 'ai_agent_scope_agent_dispatch_desc',
    ),
    'session.send': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_session_send',
      descKey: 'ai_agent_scope_session_send_desc',
    ),
    'owner.call': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_owner_call',
      descKey: 'ai_agent_scope_owner_call_desc',
    ),
    'agent.introduction.update': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_agent_introduction_update',
      descKey: 'ai_agent_scope_agent_introduction_update_desc',
    ),
    'agent.task.query': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_agent_task_query',
      descKey: 'ai_agent_scope_agent_task_query_desc',
    ),
    'conversation.audit.read': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_conversation_audit_read',
      descKey: 'ai_agent_scope_conversation_audit_read_desc',
    ),
    'media.upload': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_media_upload',
      descKey: 'ai_agent_scope_media_upload_desc',
    ),
    'app.local_search': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_app_local_search',
      descKey: 'ai_agent_scope_app_local_search_desc',
    ),
    'app.open_chat': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_app_open_chat',
      descKey: 'ai_agent_scope_app_open_chat_desc',
    ),
    'app.open_page': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_app_open_page',
      descKey: 'ai_agent_scope_app_open_page_desc',
    ),
    'widget.visitor.ban': _ScopeI18nMeta(
      labelKey: 'ai_agent_scope_widget_visitor_ban',
      descKey: 'ai_agent_scope_widget_visitor_ban_desc',
    ),
  };

  List<AgentScopeOption> get scopeOptions {
    final resolvedScopes = _resolvedScopeOrder();
    final backendItems = _availableScopeItemMap();
    return resolvedScopes
        .map((scope) {
          final backendItem = backendItems[scope];
          if (backendItem != null) {
            final fallback = _fallbackScopeOption(scope);
            return AgentScopeOption(
              scope: scope,
              label: backendItem.label.isEmpty
                  ? fallback.label
                  : backendItem.label,
              description: backendItem.description.isEmpty
                  ? fallback.description
                  : backendItem.description,
            );
          }
          return _fallbackScopeOption(scope);
        })
        .toList(growable: false);
  }

  AgentScopeOption _fallbackScopeOption(String scope) {
    final meta = _knownScopeI18n[scope];
    return AgentScopeOption(
      scope: scope,
      label: meta == null ? scope : meta.labelKey.tr,
      description: meta == null ? scope : meta.descKey.tr,
    );
  }

  @override
  void onInit() {
    super.onInit();
    _loadArgs();
    _initPage();
  }

  Future<void> _initPage() async {
    await _hydrateAgentMetaIfNeeded();
    if (!canConfigure) {
      return;
    }
    await loadScopes();
  }

  void toggleScope(String scope, bool checked) {
    if (!canConfigure) {
      return;
    }
    final current = selectedScopes.toList(growable: true);
    if (checked) {
      if (!current.contains(scope)) {
        current.add(scope);
      }
    } else {
      current.remove(scope);
    }
    selectedScopes.value = _normalizeScopeList(current);
  }

  bool isSelected(String scope) {
    return selectedScopes.contains(scope);
  }

  void selectAllScopes() {
    if (!canConfigure) {
      return;
    }
    selectedScopes.value = _resolvedAvailableScopes();
  }

  void clearScopes() {
    if (!canConfigure) {
      return;
    }
    selectedScopes.clear();
  }

  Future<void> loadScopes() async {
    if (agentId.value.isEmpty || !canConfigure || isLoading.value) {
      return;
    }
    isLoading.value = true;
    try {
      final result = await agentService.getAgentScopes(agentId.value);
      if (!result.ok) {
        _showToast(result.message, isError: true);
        return;
      }

      final scopeConfig = result.data ?? const AgentScopeConfig();
      availableScopes.value = _normalizeScopeList(scopeConfig.availableScopes);
      availableScopeItems.value = _normalizeScopeItems(
        scopeConfig.availableScopeItems,
      );
      selectedScopes.value = _normalizeScopeList(scopeConfig.scopes);
    } finally {
      isLoading.value = false;
    }
  }

  Future<void> saveScopes() async {
    if (agentId.value.isEmpty || !canConfigure || isSaving.value) {
      return;
    }
    isSaving.value = true;
    try {
      final result = await agentService.replaceAgentScopes(
        agentId.value,
        selectedScopes.toList(),
      );
      if (!result.ok) {
        _showToast(result.message, isError: true);
        return;
      }

      final scopeConfig = result.data ?? const AgentScopeConfig();
      availableScopes.value = _normalizeScopeList(scopeConfig.availableScopes);
      availableScopeItems.value = _normalizeScopeItems(
        scopeConfig.availableScopeItems,
      );
      selectedScopes.value = _normalizeScopeList(scopeConfig.scopes);
      _showToast('ai_agent_scope_save_success'.tr, isError: false);
      Get.back(result: true);
    } finally {
      isSaving.value = false;
    }
  }

  void _loadArgs() {
    final args = Get.arguments as Map<String, dynamic>? ?? const {};
    agentId.value = args['agent_id']?.toString().trim() ?? '';
    agentName.value = args['agent_name']?.toString().trim() ?? '';
    providerType.value = _parseProviderType(args['provider_type']);

    if (agentId.value.isEmpty) {
      _showToast('ai_agent_scope_target_invalid'.tr, isError: true);
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

  List<String> _resolvedScopeOrder() {
    final result = _resolvedAvailableScopes().toList(growable: true);
    final seen = <String>{...result};
    for (final selected in selectedScopes) {
      if (selected.isEmpty || seen.contains(selected)) {
        continue;
      }
      seen.add(selected);
      result.add(selected);
    }
    return result;
  }

  List<String> _resolvedAvailableScopes() {
    return _normalizeScopeList(availableScopes);
  }

  Map<String, AgentScopeItem> _availableScopeItemMap() {
    return {
      for (final item in _normalizeScopeItems(availableScopeItems))
        item.scope: item,
    };
  }

  List<String> _normalizeScopeList(List<String> scopes) {
    if (scopes.isEmpty) {
      return const [];
    }
    final seen = <String>{};
    final normalized = <String>[];
    for (final scope in scopes) {
      final value = scope.trim();
      if (value.isEmpty || seen.contains(value)) {
        continue;
      }
      seen.add(value);
      normalized.add(value);
    }
    return normalized;
  }

  List<AgentScopeItem> _normalizeScopeItems(List<AgentScopeItem> items) {
    if (items.isEmpty) {
      return const [];
    }
    final seen = <String>{};
    final normalized = <AgentScopeItem>[];
    for (final item in items) {
      final scope = item.scope.trim();
      if (scope.isEmpty || seen.contains(scope)) {
        continue;
      }
      seen.add(scope);
      normalized.add(
        AgentScopeItem(
          scope: scope,
          label: item.label.trim(),
          description: item.description.trim(),
        ),
      );
    }
    return normalized;
  }
}
