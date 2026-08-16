import 'dart:async';

import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:uuid/uuid.dart';

import '../../../data/providers/agent_service.dart';
import '../../../data/providers/egg_market_service.dart';
import '../../../data/providers/im_service.dart';
import '../../chat/services/chat_route_navigator.dart';
import '../../../shared/utils/toast_util.dart';

class EggMarketController extends GetxController {
  EggMarketController({Uuid? uuid}) : _uuid = uuid ?? const Uuid();

  static const int _hotEggPageSize = 20;
  static const int _searchPageSize = 30;
  static const double _loadMoreTriggerThreshold = 240;
  static const Duration _scrollToTopDuration = Duration(milliseconds: 260);

  final EggMarketService eggMarketService = Get.find<EggMarketService>();
  final AgentService agentService = Get.find<AgentService>();
  final ImService imService = Get.find<ImService>();
  final Uuid _uuid;

  final keywordController = TextEditingController();
  final searchFocusNode = FocusNode();
  final scrollController = ScrollController();

  final hotEggs = <EggMarketEggModel>[].obs;
  final resultEggs = <EggMarketEggModel>[].obs;

  final currentKeyword = ''.obs;
  final searched = false.obs;
  final isLoading = false.obs;
  final isLoadingMore = false.obs;
  final isInstalling = false.obs;
  final hotHasMore = true.obs;
  final resultHasMore = false.obs;

  int _hotPage = 0;
  int _resultPage = 0;

  @override
  void onInit() {
    super.onInit();
    scrollController.addListener(_handleScroll);
    unawaited(refreshAll());
  }

  @override
  void onClose() {
    scrollController
      ..removeListener(_handleScroll)
      ..dispose();
    searchFocusNode.dispose();
    keywordController.dispose();
    super.onClose();
  }

  void dismissSearchKeyboard() {
    if (searchFocusNode.hasFocus) {
      searchFocusNode.unfocus();
    }
    FocusManager.instance.primaryFocus?.unfocus();
  }

  Future<void> submitSearch() async {
    dismissSearchKeyboard();
    await search();
    dismissSearchKeyboard();
  }

  void clearSearch() {
    if (isLoading.value) {
      return;
    }
    final hadKeyword = keywordController.text.trim().isNotEmpty;
    final hadSearchResults = searched.value || resultEggs.isNotEmpty;
    keywordController.clear();
    currentKeyword.value = '';
    searched.value = false;
    _resultPage = 0;
    resultHasMore.value = false;
    resultEggs.clear();
    if (!hadKeyword && !hadSearchResults) {
      return;
    }
    _jumpToTopIfPossible();
    _scheduleAutoLoadMoreCheck();
  }

  Future<void> refreshAll() async {
    if (isLoading.value) {
      return;
    }
    isLoading.value = true;
    try {
      await Future.wait<void>([
        _loadHotEggs(reset: true),
        agentService.loadAgents(),
      ]);
      if (searched.value) {
        await _loadSearchEggs(keyword: currentKeyword.value, reset: true);
      }
    } catch (error) {
      CustomToast.show(_eggErrorToast(error), isError: true);
    } finally {
      isLoading.value = false;
      _scheduleAutoLoadMoreCheck();
    }
  }

  Future<void> search() async {
    if (isLoading.value) {
      return;
    }

    final keyword = keywordController.text.trim();
    currentKeyword.value = keyword;
    searched.value = keyword.isNotEmpty;
    _resultPage = 0;
    resultHasMore.value = false;

    if (keyword.isEmpty) {
      resultEggs.clear();
      _jumpToTopIfPossible();
      _scheduleAutoLoadMoreCheck();
      return;
    }

    isLoading.value = true;
    try {
      await _loadSearchEggs(keyword: keyword, reset: true);
    } catch (error) {
      CustomToast.show(_eggErrorToast(error), isError: true);
    } finally {
      isLoading.value = false;
      _jumpToTopIfPossible();
      _scheduleAutoLoadMoreCheck();
    }
  }

  Future<void> scrollToTop() async {
    if (!scrollController.hasClients) {
      return;
    }
    final minScrollExtent = scrollController.position.minScrollExtent;
    if (scrollController.offset <= minScrollExtent) {
      return;
    }
    await scrollController.animateTo(
      minScrollExtent,
      duration: _scrollToTopDuration,
      curve: Curves.easeOutCubic,
    );
  }

  Future<void> loadMoreIfNeeded() async {
    if (isLoading.value || isLoadingMore.value) {
      return;
    }

    if (searched.value) {
      if (!resultHasMore.value) {
        return;
      }
    } else if (!hotHasMore.value) {
      return;
    }

    isLoadingMore.value = true;
    try {
      if (searched.value) {
        await _loadSearchEggs(keyword: currentKeyword.value, reset: false);
      } else {
        await _loadHotEggs(reset: false);
      }
    } catch (error) {
      CustomToast.show(_eggErrorToast(error), isError: true);
    } finally {
      isLoadingMore.value = false;
      _scheduleAutoLoadMoreCheck();
    }
  }

  Future<void> _loadHotEggs({required bool reset}) async {
    final nextPage = reset ? 1 : _hotPage + 1;
    final result = await eggMarketService.searchEggs(
      keyword: '',
      page: nextPage,
      pageSize: _hotEggPageSize,
    );
    if (reset) {
      hotEggs.assignAll(result.list);
    } else {
      hotEggs.addAll(result.list);
    }
    _hotPage = result.page > 0 ? result.page : nextPage;
    hotHasMore.value = result.hasMore;
  }

  Future<void> _loadSearchEggs({
    required String keyword,
    required bool reset,
  }) async {
    if (keyword.trim().isEmpty) {
      resultEggs.clear();
      _resultPage = 0;
      resultHasMore.value = false;
      return;
    }

    final nextPage = reset ? 1 : _resultPage + 1;
    final result = await eggMarketService.searchEggs(
      keyword: keyword,
      page: nextPage,
      pageSize: _searchPageSize,
    );
    if (reset) {
      resultEggs.assignAll(result.list);
    } else {
      resultEggs.addAll(result.list);
    }
    _resultPage = result.page > 0 ? result.page : nextPage;
    resultHasMore.value = result.hasMore;
  }

  void _handleScroll() {
    if (!scrollController.hasClients) {
      return;
    }
    final position = scrollController.position;
    if (position.extentAfter > _loadMoreTriggerThreshold) {
      return;
    }
    unawaited(loadMoreIfNeeded());
  }

  void _scheduleAutoLoadMoreCheck() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (isClosed ||
          isLoading.value ||
          isLoadingMore.value ||
          !scrollController.hasClients) {
        return;
      }
      final position = scrollController.position;
      if (position.extentAfter > _loadMoreTriggerThreshold) {
        return;
      }
      unawaited(loadMoreIfNeeded());
    });
  }

  void _jumpToTopIfPossible() {
    if (!scrollController.hasClients) {
      return;
    }
    final minScrollExtent = scrollController.position.minScrollExtent;
    if (scrollController.offset == minScrollExtent) {
      return;
    }
    scrollController.jumpTo(minScrollExtent);
  }

  /// Step 2: 根据孵化类型返回可选 Agent 列表。
  /// 智能体 → 可创建 Agent 的主 Agent（isMain=true）
  /// 技能 → 所有活跃 API Agent
  List<AgentModel> agentsForHatchType(String hatchType) {
    return agentService.agents.where((agent) {
      if (agent.providerType != 3 || agent.status != 1) return false;
      final ct = agent.agentClientType.trim().toLowerCase();
      if (hatchType == EggHatchType.agent) {
        return agent.isMain && ct.isNotEmpty;
      }
      return ct.isNotEmpty;
    }).toList();
  }

  /// Step 3: 按 clientType 过滤已有 Agent（不含 isMain 的限制）。
  List<AgentModel> targetAgentsForClientType(String clientType) {
    final normalized = clientType.trim().toLowerCase();
    return agentService.agents.where((agent) {
      if (agent.providerType != 3 || agent.status != 1) return false;
      return agent.agentClientType.trim().toLowerCase() == normalized;
    }).toList();
  }

  Future<void> installEgg({
    required EggMarketEggModel egg,
    required String installMode,
    String? targetAgentID,
    String? executorAgentID,
    bool isSkillInstall = false,
  }) async {
    if (isInstalling.value) return;
    if (installMode == EggInstallMode.existingAgent &&
        (targetAgentID == null || targetAgentID.trim().isEmpty)) {
      CustomToast.show('eggs_pond_install_existing_required'.tr, isError: true);
      return;
    }

    isInstalling.value = true;
    try {
      final idempotencyKey =
          'egg_${DateTime.now().millisecondsSinceEpoch}_${_uuid.v4()}';
      final accepted = await _startEggInstall(
        egg: egg,
        installMode: installMode,
        idempotencyKey: idempotencyKey,
        targetAgentID: targetAgentID,
        executorAgentID: executorAgentID,
      );
      if (accepted == null) return;

      await _openInstallChat(accepted);
      CustomToast.show(
        isSkillInstall
            ? 'eggs_pond_install_started_skill'.tr
            : 'eggs_pond_install_started'.tr,
        isError: false,
      );
      await _loadHotEggs(reset: true);
    } catch (error) {
      CustomToast.show(_eggErrorToast(error), isError: true);
    } finally {
      isInstalling.value = false;
    }
  }

  Future<EggInstallAcceptModel?> _startEggInstall({
    required EggMarketEggModel egg,
    required String installMode,
    required String idempotencyKey,
    String? targetAgentID,
    String? executorAgentID,
  }) async {
    final accepted = await eggMarketService.installEgg(
      eggID: egg.id,
      version: egg.version,
      idempotencyKey: idempotencyKey,
      installMode: installMode,
      targetAgentID: targetAgentID,
      executorAgentID: executorAgentID,
    );
    if (!accepted.requiresExecutorSelection) return accepted;

    // 自动选择第一个候选 executor（不再弹对话框）
    final validCandidates = accepted.candidates
        .where((c) => c.agentID.trim().isNotEmpty)
        .toList(growable: false);
    if (validCandidates.isEmpty) {
      throw Exception('eggs_pond_install_choose_executor_missing'.tr);
    }
    return _startEggInstall(
      egg: egg,
      installMode: installMode,
      idempotencyKey: idempotencyKey,
      targetAgentID: targetAgentID,
      executorAgentID: validCandidates.first.agentID,
    );
  }

  Future<void> _openInstallChat(EggInstallAcceptModel accepted) async {
    final sessionID = accepted.sessionID.trim();
    if (sessionID.isEmpty) {
      throw Exception('eggs_pond_install_open_chat_failed'.tr);
    }

    final title = _resolveExecutorAgentTitle(accepted.executorAgentID);
    if (!imService.hasSessionDisplayTitleById(sessionID)) {
      await imService.bindSessionDisplayTitle(
        sessionID,
        title,
        type: 'private',
      );
    }
    await ChatRouteNavigator.toChat(
      sessionId: sessionID,
      title: title,
      type: 'private',
    );
  }

  String _resolveExecutorAgentTitle(String executorAgentID) {
    final normalizedAgentID = executorAgentID.trim();
    if (normalizedAgentID.isNotEmpty) {
      final idx = agentService.agents.indexWhere(
        (agent) => agent.id.trim() == normalizedAgentID,
      );
      if (idx >= 0) {
        final name = agentService.agents[idx].agentName.trim();
        if (name.isNotEmpty) {
          return name;
        }
      }
    }
    return 'eggs_pond_install_dialog_title'.tr;
  }
}

String _eggErrorToast(Object error) {
  final text = error.toString();
  const prefix = 'Exception: ';
  return text.startsWith(prefix) ? text.substring(prefix.length) : text;
}
