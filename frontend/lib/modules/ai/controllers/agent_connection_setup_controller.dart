import 'dart:async';

import 'package:flutter/services.dart';
import 'package:get/get.dart';

import '../../../app/routes/app_routes.dart';
import '../../../data/providers/agent_service.dart';
import '../../../shared/utils/toast_util.dart';
import '../../chat/services/chat_route_navigator.dart';
import '../models/agent_install_task.dart';

typedef AgentSetupClipboardWriter = Future<void> Function(String text);
typedef AgentSetupChatOpener =
    Future<String?> Function({
      required String peerId,
      required int peerType,
      required String fallbackTitle,
    });
typedef AgentSetupRouteOpener =
    Future<dynamic> Function(String route, {Object? arguments});
typedef AgentSetupPageCloser = void Function({Object? result});
typedef AgentSetupToastShower = void Function(String message, {bool isError});

/// Owns the short post-creation journey for an Agent.
///
/// The API key deliberately lives outside [agent]. A refreshed Agent response
/// normally contains only `api_key_hint`; keeping the original creation secret
/// here prevents a status refresh from erasing the one-time credential.
///
/// The install task itself is authored by the backend (one `copy_template` per
/// client type). The three install paths — grix-connector, the OpenClaw plugin
/// and the Hermes plugin — write different config files with different field
/// names, and keeping the text server-side means a wording fix ships without a
/// client release.
class AgentConnectionSetupController extends GetxController {
  AgentConnectionSetupController({
    AgentService? agentService,
    Map<String, dynamic>? initialArguments,
    AgentSetupClipboardWriter? writeClipboard,
    AgentSetupChatOpener? openChat,
    AgentSetupRouteOpener? openRoute,
    AgentSetupPageCloser? closePage,
    AgentSetupToastShower? showToast,
  }) : agentService = agentService ?? Get.find<AgentService>(),
       _initialArguments = initialArguments,
       _writeClipboard = writeClipboard ?? _defaultWriteClipboard,
       _openChat = openChat ?? _defaultOpenChat,
       _openRoute = openRoute ?? _defaultOpenRoute,
       _closePage = closePage ?? _defaultClosePage,
       _showToast = showToast ?? CustomToast.show;

  final AgentService agentService;
  final Map<String, dynamic>? _initialArguments;
  final AgentSetupClipboardWriter _writeClipboard;
  final AgentSetupChatOpener _openChat;
  final AgentSetupRouteOpener _openRoute;
  final AgentSetupPageCloser _closePage;
  final AgentSetupToastShower _showToast;

  final agent = Rxn<AgentModel>();
  final agentId = ''.obs;
  final apiEndpoint = ''.obs;
  final apiKey = ''.obs;
  final apiKeyHint = ''.obs;
  final installGuides = <AgentApiInstallGuide>[].obs;
  final selectedInstallGuideType = ''.obs;

  final isLoadingAgent = false.obs;
  final isRefreshing = false.obs;
  final isLoadingGuides = false.obs;
  final isNavigating = false.obs;
  final loadFailed = false.obs;

  String _preferredInstallGuideType = '';
  String _defaultInstallGuideType = '';
  bool _initialized = false;

  AgentModel? get currentAgent => agent.value;
  bool get isApiAgent => currentAgent?.providerType == 3;
  bool get isOnline => currentAgent?.online == true;
  bool get isBusy =>
      isLoadingAgent.value || isRefreshing.value || isNavigating.value;

  AgentApiInstallGuide? get selectedInstallGuide {
    final selected = _normalizeGuideType(selectedInstallGuideType.value);
    if (selected.isEmpty) {
      return null;
    }
    for (final guide in installGuides) {
      if (_normalizeGuideType(guide.type) == selected) {
        return guide;
      }
    }
    return null;
  }

  String get selectedClientType =>
      _normalizeGuideType(selectedInstallGuideType.value);

  /// The one-time secret only lives in memory right after creation. Without it
  /// the task would carry a masked key, so copying stays disabled and the page
  /// tells the owner to rotate the key instead.
  bool get hasOneTimeSecret => apiKey.value.trim().isNotEmpty;

  bool get hasInstallTask => installTask.isNotEmpty;

  /// The backend task with its placeholders resolved. Empty when anything the
  /// task depends on is missing — never a half-filled task that still carries a
  /// literal `{{api_key}}`.
  String get installTask => resolveAgentInstallTask(
    template: selectedInstallGuide?.copyTemplate ?? '',
    agentName: currentAgent?.agentName ?? '',
    agentId: agentId.value,
    apiKey: apiKey.value,
    apiEndpoint: apiEndpoint.value,
  );

  String get apiKeyDisplay {
    final secret = apiKey.value.trim();
    if (secret.isNotEmpty) {
      return secret;
    }
    final hint = apiKeyHint.value.trim();
    if (hint.isNotEmpty) {
      return '••••••••$hint';
    }
    return '';
  }

  @override
  void onInit() {
    super.onInit();
    unawaited(initialize());
  }

  Future<void> initialize() async {
    if (_initialized) {
      return;
    }
    _initialized = true;

    final arguments = _readArguments();
    final preloadedAgent = arguments['agent'];
    if (preloadedAgent is AgentModel) {
      _applyAgent(preloadedAgent);
    }

    final requestedAgentId =
        arguments['agent_id']?.toString().trim() ??
        (preloadedAgent is AgentModel ? preloadedAgent.id.trim() : '');
    if (agentId.value.isEmpty) {
      agentId.value = requestedAgentId;
    }

    if (currentAgent == null && requestedAgentId.isNotEmpty) {
      await _loadAgent(requestedAgentId, initialLoad: true);
    }
    if (isApiAgent) {
      await loadInstallGuides();
    }
  }

  Future<void> refreshStatus() async {
    final id = agentId.value.trim();
    if (id.isEmpty || isRefreshing.value) {
      return;
    }
    isRefreshing.value = true;
    loadFailed.value = false;
    try {
      final refreshed = await agentService.getAgent(id);
      if (refreshed == null) {
        loadFailed.value = true;
        return;
      }
      _applyAgent(refreshed);
      if (isApiAgent && installGuides.isEmpty) {
        await loadInstallGuides();
      }
    } finally {
      isRefreshing.value = false;
    }
  }

  Future<void> retryInitialLoad() async {
    final id = agentId.value.trim();
    if (id.isEmpty) {
      return;
    }
    await _loadAgent(id, initialLoad: true);
  }

  Future<void> _loadAgent(String id, {required bool initialLoad}) async {
    if (initialLoad) {
      isLoadingAgent.value = true;
    }
    loadFailed.value = false;
    try {
      final loaded = await agentService.getAgent(id);
      if (loaded == null) {
        loadFailed.value = true;
        return;
      }
      _applyAgent(loaded);
    } finally {
      if (initialLoad) {
        isLoadingAgent.value = false;
      }
    }
  }

  void _applyAgent(AgentModel value) {
    agent.value = value;
    if (value.id.trim().isNotEmpty) {
      agentId.value = value.id.trim();
    }
    if (value.apiEndpoint.trim().isNotEmpty || apiEndpoint.value.isEmpty) {
      apiEndpoint.value = value.apiEndpoint.trim();
    }
    if (value.apiKey.trim().isNotEmpty) {
      apiKey.value = value.apiKey.trim();
    }
    if (value.apiKeyHint.trim().isNotEmpty || apiKeyHint.value.isEmpty) {
      apiKeyHint.value = value.apiKeyHint.trim();
    }
    final clientType = _normalizeGuideType(value.agentClientType);
    if (clientType.isNotEmpty) {
      _preferredInstallGuideType = clientType;
    }
  }

  Future<void> loadInstallGuides() async {
    if (isLoadingGuides.value) {
      return;
    }
    isLoadingGuides.value = true;
    try {
      final catalog = await agentService.getAgentApiInstallGuides();
      if (catalog == null) {
        return;
      }
      installGuides.assignAll(catalog.list);
      _defaultInstallGuideType = _normalizeGuideType(catalog.defaultType);
      _syncSelectedInstallGuide();
    } finally {
      isLoadingGuides.value = false;
    }
  }

  void selectInstallGuide(String? rawType) {
    final type = _normalizeGuideType(rawType ?? '');
    if (type.isEmpty || !_hasInstallGuide(type)) {
      return;
    }
    selectedInstallGuideType.value = type;
  }

  void _syncSelectedInstallGuide() {
    if (installGuides.isEmpty) {
      selectedInstallGuideType.value = '';
      return;
    }
    final candidates = <String>[
      selectedInstallGuideType.value,
      _preferredInstallGuideType,
      _defaultInstallGuideType,
      installGuides.first.type,
    ];
    for (final candidate in candidates) {
      final normalized = _normalizeGuideType(candidate);
      if (_hasInstallGuide(normalized)) {
        selectedInstallGuideType.value = normalized;
        return;
      }
    }
  }

  bool _hasInstallGuide(String normalizedType) {
    if (normalizedType.isEmpty) {
      return false;
    }
    return installGuides.any(
      (guide) => _normalizeGuideType(guide.type) == normalizedType,
    );
  }

  String _normalizeGuideType(String value) => value.trim().toLowerCase();

  Future<void> copyInstallTask() async {
    final task = installTask;
    if (task.isEmpty) {
      return;
    }
    await copyText(task);
  }

  Future<void> copyText(String value) async {
    final normalized = value.trim();
    if (normalized.isEmpty) {
      return;
    }
    await _writeClipboard(normalized);
    _showToast('ai_agent_api_copied'.tr, isError: false);
  }

  Future<void> primaryAction() async {
    if (isApiAgent && !isOnline) {
      await refreshStatus();
      return;
    }
    await startChat();
  }

  Future<void> startChat() async {
    final value = currentAgent;
    final id = agentId.value.trim();
    if (value == null || id.isEmpty || isNavigating.value) {
      return;
    }
    isNavigating.value = true;
    try {
      final sessionId = await _openChat(
        peerId: id,
        peerType: 2,
        fallbackTitle: value.agentName,
      );
      if (sessionId == null || sessionId.trim().isEmpty) {
        _showToast('ai_agent_setup_chat_failed'.tr, isError: true);
      }
    } finally {
      isNavigating.value = false;
    }
  }

  Future<void> continueConfiguration() async {
    final value = currentAgent;
    if (value == null || isNavigating.value) {
      return;
    }
    isNavigating.value = true;
    try {
      await _openRoute(
        AppRoutes.agentEdit,
        arguments: <String, dynamic>{'agent': value, 'agent_id': value.id},
      );
    } finally {
      isNavigating.value = false;
    }
  }

  void finish() => _closePage(result: currentAgent);

  void finishLater() => _closePage(result: currentAgent);

  Map<String, dynamic> _readArguments() {
    if (_initialArguments != null) {
      return _initialArguments;
    }
    final rawArguments = Get.arguments;
    if (rawArguments is AgentModel) {
      return <String, dynamic>{'agent': rawArguments};
    }
    if (rawArguments is Map) {
      return rawArguments.map((key, value) => MapEntry(key.toString(), value));
    }
    return const <String, dynamic>{};
  }

  static Future<void> _defaultWriteClipboard(String text) {
    return Clipboard.setData(ClipboardData(text: text));
  }

  static Future<String?> _defaultOpenChat({
    required String peerId,
    required int peerType,
    required String fallbackTitle,
  }) {
    return ChatRouteNavigator.createAndOpenPrivateChat(
      peerId: peerId,
      peerType: peerType,
      fallbackTitle: fallbackTitle,
    );
  }

  static Future<dynamic> _defaultOpenRoute(String route, {Object? arguments}) {
    return Get.toNamed<dynamic>(route, arguments: arguments) ??
        Future<dynamic>.value();
  }

  static void _defaultClosePage({Object? result}) {
    Get.back(result: result);
  }
}
