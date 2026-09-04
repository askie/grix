import 'dart:async';

import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../../app/routes/app_routes.dart';
import '../../../data/providers/agent_category_service.dart';
import '../../../data/providers/agent_service.dart';
import '../../../data/providers/im_service.dart';
import '../../../shared/utils/toast_util.dart';
import '../models/agent_editor_result.dart';
import '../widgets/agent_share_sheet.dart';

typedef AgentRouteOpener =
    Future<T?> Function<T>(
      String route, {
      dynamic arguments,
      Map<String, String>? parameters,
    });
typedef AgentResultToastShower = void Function(String message, {bool isError});

class AgentsController extends GetxController {
  AgentsController({
    AgentService? agentService,
    AgentCategoryService? categoryService,
    ImService? imService,
    AgentRouteOpener? openRoute,
    AgentResultToastShower? showToast,
    RxInt? homeTabIndex,
  }) : agentService = agentService ?? Get.find<AgentService>(),
       categoryService = categoryService ?? Get.find<AgentCategoryService>(),
       imService = imService ?? Get.find<ImService>(),
       _openRoute = openRoute ?? _defaultOpenRoute,
       _showToast = showToast ?? CustomToast.show,
       _homeTabIndex = homeTabIndex;

  final AgentService agentService;
  final AgentCategoryService categoryService;
  final ImService imService;
  final AgentRouteOpener _openRoute;
  final AgentResultToastShower _showToast;
  final RxInt? _homeTabIndex;
  Worker? _homeTabWorker;

  /// 视图模式：0=按分类目录，1=按电脑
  static const String _viewModePrefsKey = 'agents_view_mode';
  final RxInt viewMode = 0.obs;

  /// 切换视图模式
  void toggleViewMode() {
    viewMode.value = viewMode.value == 0 ? 1 : 0;
    _persistViewMode(viewMode.value);
  }

  Future<void> _persistViewMode(int mode) async {
    try {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setInt(_viewModePrefsKey, mode);
    } catch (_) {}
  }

  Future<void> _restoreViewMode() async {
    try {
      final prefs = await SharedPreferences.getInstance();
      viewMode.value = prefs.getInt(_viewModePrefsKey) ?? 0;
    } catch (_) {}
  }

  /// 按宿主机名称分组
  Map<String, List<AgentModel>> groupByHostname(List<AgentModel> agents) {
    final map = <String, List<AgentModel>>{};
    for (final agent in agents) {
      final key = agent.hostname.isEmpty ? '' : agent.hostname;
      map.putIfAbsent(key, () => []).add(agent);
    }
    return map;
  }

  static Future<T?> _defaultOpenRoute<T>(
    String route, {
    dynamic arguments,
    Map<String, String>? parameters,
  }) {
    return Get.toNamed<T>(
          route,
          arguments: arguments,
          parameters: parameters,
        ) ??
        Future<T?>.value(null);
  }

  @override
  void onInit() {
    super.onInit();
    unawaited(_restoreViewMode());
    final homeTabIndex = _homeTabIndex;
    if (homeTabIndex == null) {
      unawaited(_loadInitialAgentsPage());
      return;
    }
    _homeTabWorker = ever<int>(homeTabIndex, _handleHomeTabChanged);
    if (homeTabIndex.value == HomeTab.agents.index) {
      triggerRefreshForVisiblePage();
    } else {
      unawaited(categoryService.restoreCachedCategories());
    }
  }

  @override
  void onClose() {
    _homeTabWorker?.dispose();
    super.onClose();
  }

  void loadAgents() {
    agentService.loadAgents();
  }

  Future<void> refreshAgents() async {
    imService.refreshAgentOnlineStates();
    await categoryService.restoreCachedCategories();
    final syncTask = categoryService.syncCategoriesFromRemote();
    await agentService.loadAgents();
    await syncTask;
  }

  Future<void> _loadInitialAgentsPage() async {
    await categoryService.restoreCachedCategories();
    await agentService.loadAgents();
    unawaited(categoryService.syncCategoriesFromRemote());
  }

  void triggerRefreshForVisiblePage() {
    final homeTabIndex = _homeTabIndex;
    if (homeTabIndex != null && homeTabIndex.value != HomeTab.agents.index) {
      return;
    }
    unawaited(refreshAgents());
  }

  void _handleHomeTabChanged(int index) {
    if (index != HomeTab.agents.index) {
      return;
    }
    triggerRefreshForVisiblePage();
  }

  bool isApiAgentOnline(AgentModel agent) {
    if (agent.providerType != 3) {
      return false;
    }
    final agentId = agent.id.trim();
    if (agentId.isEmpty) {
      return agent.online;
    }
    if (imService.hasAgentChannelState(agentId)) {
      return imService.isAgentChannelOnline(agentId);
    }
    return agent.online;
  }

  String providerDisplayLabel(AgentModel agent) {
    if (agent.providerType == 2) return 'ai_provider_local'.tr;
    if (agent.providerType == 3) {
      final label = _resolveClientTypeLabel(agent.agentClientType);
      if (label != null) return label;
      return agent.agentClientType.isNotEmpty
          ? agent.agentClientType
          : 'ai_provider_agent_api'.tr;
    }
    return agent.modelProvider.isNotEmpty
        ? agent.modelProvider
        : 'ai_provider_remote'.tr;
  }

  String? _resolveClientTypeLabel(String raw) {
    switch (raw.trim().toLowerCase()) {
      case '':
        return null;
      case 'claude':
        return 'ai_agent_client_type_claude'.tr;
      case 'codex':
        return 'ai_agent_client_type_codex'.tr;
      case 'gemini':
        return 'ai_agent_client_type_gemini'.tr;
      case 'hermes':
        return 'ai_agent_client_type_hermes'.tr;
      case 'openclaw':
        return 'ai_agent_client_type_openclaw'.tr;
      case 'qwen':
        return 'ai_agent_client_type_qwen'.tr;
      case 'reasonix':
        return 'ai_agent_client_type_reasonix'.tr;
      case 'deepseek':
        return 'ai_agent_client_type_deepseek'.tr;
      case 'opencode':
        return 'ai_agent_client_type_opencode'.tr;
      case 'kiro':
        return 'ai_agent_client_type_kiro'.tr;
      case 'copilot':
        return 'ai_agent_client_type_copilot'.tr;
      case 'kimi':
        return 'ai_agent_client_type_kimi'.tr;
      case 'acp':
        return 'ai_agent_client_type_acp'.tr;
      default:
        return raw.trim();
    }
  }

  Future<void> openAgentCreate() async {
    final result = await _openRoute(AppRoutes.agentCreate);
    loadAgents();
    _showEditorResultToast(result);
  }

  Future<void> openAgentQuickOnboard() async {
    await _openRoute(AppRoutes.agentQuickSetup);
    loadAgents();
  }

  Future<void> openAgentEdit(AgentModel agent) async {
    final result = await _openRoute(
      AppRoutes.agentEdit,
      arguments: {'agent_id': agent.id, 'agent': agent},
    );
    loadAgents();
    _showEditorResultToast(result);
  }

  Future<void> openAgentProfile(AgentModel agent) async {
    final agentId = agent.id.trim();
    if (agentId.isEmpty) {
      return;
    }

    final agentName = agent.agentName.trim();
    final sessionId = agent.sessionId.trim();
    final arguments = <String, dynamic>{
      'peer_id': agentId,
      'peer_type': '2',
      'nickname': agentName,
      'introduction': agent.introduction.trim(),
      'avatar_url': agent.avatarUrl.trim(),
      'title': agentName,
    };
    final parameters = <String, String>{'peer_id': agentId, 'peer_type': '2'};
    if (sessionId.isNotEmpty) {
      arguments['session_id'] = sessionId;
      parameters['session_id'] = sessionId;
    }

    await _openRoute(
      AppRoutes.accountInfo,
      arguments: arguments,
      parameters: parameters,
    );
  }

  Future<void> openAgentScopes(AgentModel agent) async {
    await _openRoute(
      AppRoutes.agentScopes,
      arguments: {
        'agent_id': agent.id,
        'agent_name': agent.agentName,
        'provider_type': agent.providerType,
      },
    );
    loadAgents();
  }

  /// 打开「连接安全」页：查看登录历史、把某个登录 IP 加入黑名单。仅 agent 主人可用。
  Future<void> openAgentConnSecurity(AgentModel agent) async {
    await _openRoute(
      AppRoutes.agentConnSecurity,
      arguments: {
        'agent_id': agent.id,
        'agent_name': agent.agentName,
        'provider_type': agent.providerType,
      },
    );
  }

  /// 打开「共享管理」面板：仅 agent 主人可用（被共享的 agent 不能再共享）。
  void openAgentShare(AgentModel agent) {
    if (!agentService.isOwnedByMe(agent)) return;
    final ctx = Get.context;
    if (ctx == null) return;
    showAgentShareSheet(ctx, agent);
  }

  /// Moves [agent] to the category identified by [targetCategoryId].
  /// Sort order is set to the end of the target category's agents.
  Future<void> moveAgentToCategory(
    AgentModel agent,
    String targetCategoryId,
  ) async {
    // 分类/排序属管理操作：被共享给我的 agent 不可拖动调整（后端也会拒）。
    if (!agentService.isOwnedByMe(agent)) return;
    final sameCategory = agentService.agents.where(
      (a) => a.categoryId == targetCategoryId,
    );
    final maxSort = sameCategory.isEmpty
        ? 0
        : sameCategory.map((a) => a.sortOrder).reduce((a, b) => a > b ? a : b) +
              1;

    final ok = await agentService.batchSortAgents([
      {
        'agent_id': agent.id,
        'category_id': targetCategoryId == '0' ? '0' : targetCategoryId,
        'sort_order': maxSort,
      },
    ]);
    if (ok) {
      await refreshAgents();
    } else {
      _showToast('ai_agents_move_failed'.tr, isError: true);
    }
  }

  /// Reorders agents within the same category.
  /// [dragAgent] is the agent being dragged, [targetIndex] is the insertion
  /// index among the category's agents (excluding the dragged agent).
  Future<void> reorderAgentInCategory(
    AgentModel dragAgent,
    String categoryId,
    int targetIndex,
  ) async {
    // 排序属管理操作：被共享给我的 agent 不可拖动调整。
    if (!agentService.isOwnedByMe(dragAgent)) return;
    final sameCategory =
        agentService.agents.where((a) => a.categoryId == categoryId).toList()
          ..sort((a, b) {
            final cmp = a.sortOrder.compareTo(b.sortOrder);
            return cmp != 0 ? cmp : a.createdAt.compareTo(b.createdAt);
          });

    // Remove dragged agent from list, re-insert at target position
    final without = sameCategory.where((a) => a.id != dragAgent.id).toList();
    final clamped = targetIndex.clamp(0, without.length);
    without.insert(clamped, dragAgent);

    final items = <Map<String, dynamic>>[];
    for (var i = 0; i < without.length; i++) {
      items.add({
        'agent_id': without[i].id,
        'category_id': categoryId == '0' ? '0' : categoryId,
        'sort_order': i,
      });
    }

    final ok = await agentService.batchSortAgents(items);
    if (ok) {
      await refreshAgents();
    } else {
      _showToast('ai_agents_sort_failed'.tr, isError: true);
    }
  }

  /// Reorders categories by updating their sort_order.
  /// [orderedIds] is the list of category IDs in the desired order.
  Future<void> reorderCategories(List<String> orderedIds) async {
    for (var i = 0; i < orderedIds.length; i++) {
      final id = orderedIds[i];
      final cat = categoryService.categories.firstWhereOrNull(
        (c) => c.id == id,
      );
      if (cat == null) continue;
      await categoryService.updateCategory(
        id,
        name: cat.name,
        parentId: cat.parentId,
        sortOrder: i,
      );
    }
    await refreshAgents();
  }

  /// Moves a category to a new parent, inserting at a given position.
  Future<void> moveCategoryToParent(
    String categoryId,
    String newParentId,
    int insertIndex,
  ) async {
    final cat = categoryService.categories.firstWhereOrNull(
      (c) => c.id == categoryId,
    );
    if (cat == null) return;

    // Get siblings under new parent (excluding the moved category)
    final siblings =
        categoryService.categories
            .where((c) => c.parentId == newParentId && c.id != categoryId)
            .toList()
          ..sort((a, b) => a.sortOrder.compareTo(b.sortOrder));

    // Insert at position
    siblings.insert(insertIndex.clamp(0, siblings.length), cat);

    // Update all siblings' sort_order
    for (var i = 0; i < siblings.length; i++) {
      await categoryService.updateCategory(
        siblings[i].id,
        name: siblings[i].name,
        parentId: newParentId,
        sortOrder: i,
      );
    }
    await refreshAgents();
  }

  /// Updates a category's name.
  Future<void> updateCategoryName(String categoryId, String newName) async {
    final cat = categoryService.categories.firstWhereOrNull(
      (c) => c.id == categoryId,
    );
    if (cat == null) return;
    await categoryService.updateCategory(
      categoryId,
      name: newName,
      parentId: cat.parentId,
      sortOrder: cat.sortOrder,
    );
    await refreshAgents();
  }

  void _showEditorResultToast(Object? result) {
    if (result is! AgentEditorResult) {
      return;
    }
    _showToast(result.toastKey.tr, isError: false);
  }
}
