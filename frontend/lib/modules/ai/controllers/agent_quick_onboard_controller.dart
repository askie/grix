import 'dart:async';

import 'package:flutter/services.dart';
import 'package:get/get.dart';

import '../../../data/providers/agent_service.dart';
import '../../../data/providers/im_service.dart';
import '../../../data/providers/session_service.dart';
import '../../../shared/utils/toast_util.dart';
import '../../chat/services/chat_route_navigator.dart';
import '../models/agent_install_task.dart';

typedef QuickOnboardClipboardWriter = Future<void> Function(String text);

/// Creates (or reuses) the private session with the agent and sends the probe
/// message without navigating. Returns the sessionId, or null on failure.
typedef QuickOnboardProbeSender =
    Future<String?> Function({
      required String agentId,
      required String agentName,
      required String probeText,
    });

typedef QuickOnboardChatOpener =
    Future<String?> Function({
      required String peerId,
      required int peerType,
      required String fallbackTitle,
    });
typedef QuickOnboardToastShower = void Function(String message, {bool isError});

enum QuickOnboardStep { selectType, install, online }

/// One-question onboarding: pick the CLI you use, everything else is automatic.
///
/// The agent is created the moment a type is picked — its name is the type
/// label — and the page then shows the backend-authored install task for that
/// type. Switching type later NEVER creates another agent: the credentials
/// (agent_id / api_key / endpoint) stay, only the task template swaps, and the
/// new client type is synced to the backend in the background.
///
/// The one-time api_key only exists in memory right after creation, so the
/// whole journey (create → copy → detect → chat) must survive on this page
/// without a reload.
class AgentQuickOnboardController extends GetxController {
  AgentQuickOnboardController({
    AgentService? agentService,
    bool autoPolling = true,
    QuickOnboardClipboardWriter? writeClipboard,
    QuickOnboardProbeSender? sendProbe,
    QuickOnboardChatOpener? openChat,
    QuickOnboardToastShower? showToast,
  }) : agentService = agentService ?? Get.find<AgentService>(),
       _autoPolling = autoPolling,
       _writeClipboard = writeClipboard ?? _defaultWriteClipboard,
       _sendProbe = sendProbe ?? _defaultSendProbe,
       _openChat = openChat ?? _defaultOpenChat,
       _showToast = showToast ?? CustomToast.show;

  /// Poll fast right after the user copies the task — the connector usually
  /// comes online within a minute — then back off, and give up after ten
  /// minutes so a forgotten page does not poll forever.
  static const Duration fastPollInterval = Duration(seconds: 4);
  static const Duration slowPollInterval = Duration(seconds: 10);
  static const Duration fastPollWindow = Duration(minutes: 2);
  static const Duration pollTimeoutAfter = Duration(minutes: 10);

  final AgentService agentService;
  final bool _autoPolling;
  final QuickOnboardClipboardWriter _writeClipboard;
  final QuickOnboardProbeSender _sendProbe;
  final QuickOnboardChatOpener _openChat;
  final QuickOnboardToastShower _showToast;

  final step = QuickOnboardStep.selectType.obs;
  final installGuides = <AgentApiInstallGuide>[].obs;
  final selectedType = ''.obs;
  final agent = Rxn<AgentModel>();
  final agentId = ''.obs;
  final apiEndpoint = ''.obs;
  final apiKey = ''.obs;

  final isLoadingGuides = false.obs;
  final isCreating = false.obs;
  final isRefreshing = false.obs;
  final isNavigating = false.obs;
  final pollTimedOut = false.obs;

  /// Whether the automatic first message went out. Reactive because it flips
  /// AFTER the step already switched to online — the online card must rebuild
  /// to show the "we already said hi" hint.
  final probeDelivered = false.obs;

  String _defaultGuideType = '';

  /// The name this page itself assigned (type label, possibly deduped). As long
  /// as the agent still carries it, a type switch may rename the agent to the
  /// new label; once the owner renamed it elsewhere we keep hands off.
  String _autoAssignedName = '';
  bool _probeSent = false;
  Timer? _pollTimer;
  Duration _polledFor = Duration.zero;

  AgentModel? get currentAgent => agent.value;
  bool get isOnline => currentAgent?.online == true;

  AgentApiInstallGuide? get selectedGuide => _guideOf(selectedType.value);

  AgentApiInstallGuide? _guideOf(String type) {
    final normalized = _normalizeType(type);
    if (normalized.isEmpty) {
      return null;
    }
    for (final guide in installGuides) {
      if (_normalizeType(guide.type) == normalized) {
        return guide;
      }
    }
    return null;
  }

  String get installTask => resolveAgentInstallTask(
    template: selectedGuide?.copyTemplate ?? '',
    agentName: currentAgent?.agentName ?? '',
    agentId: agentId.value,
    apiKey: apiKey.value,
    apiEndpoint: apiEndpoint.value,
  );

  bool get hasInstallTask => installTask.isNotEmpty;

  @override
  void onInit() {
    super.onInit();
    unawaited(loadInstallGuides());
    // Only needed for name dedup; fire-and-forget so the question shows fast.
    unawaited(agentService.loadAgents());
  }

  @override
  void onClose() {
    _pollTimer?.cancel();
    super.onClose();
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
      _defaultGuideType = _normalizeType(catalog.defaultType);
    } finally {
      isLoadingGuides.value = false;
    }
  }

  String get defaultGuideType => _defaultGuideType;

  /// The one answer to the one question. Creates the agent named after the
  /// type immediately — no form, no second step.
  Future<void> selectTypeAndCreate(String rawType) async {
    if (isCreating.value || currentAgent != null) {
      return;
    }
    final guide = _guideOf(rawType);
    if (guide == null) {
      return;
    }
    final type = _normalizeType(guide.type);
    selectedType.value = type;
    isCreating.value = true;
    try {
      final name = _dedupName(_labelOf(guide));
      final created = await agentService.createAgent(<String, dynamic>{
        'agent_name': name,
        'introduction': '',
        'provider_type': 3,
        'category_id': '0',
        'agent_client_type': type,
      });
      if (created == null) {
        final message = agentService.lastOperationError.trim();
        _showToast(
          message.isEmpty ? 'ai_agents_create_failed'.tr : message,
          isError: true,
        );
        return;
      }
      _applyAgent(created);
      _autoAssignedName = created.agentName.trim();
      step.value = QuickOnboardStep.install;
      _restartPolling();
    } finally {
      isCreating.value = false;
    }
  }

  /// Switch which CLI the install task targets. Same agent, same credentials —
  /// only the task template changes; the backend record follows silently.
  Future<void> switchType(String rawType) async {
    if (step.value != QuickOnboardStep.install) {
      return;
    }
    final guide = _guideOf(rawType);
    final current = currentAgent;
    if (guide == null || current == null) {
      return;
    }
    final type = _normalizeType(guide.type);
    if (type == _normalizeType(selectedType.value)) {
      return;
    }
    selectedType.value = type;

    final updates = <String, dynamic>{'agent_client_type': type};
    final autoNamed =
        _autoAssignedName.isNotEmpty &&
        current.agentName.trim() == _autoAssignedName;
    if (autoNamed) {
      updates['agent_name'] = _dedupName(_labelOf(guide));
    }
    final updated = await agentService.updateAgent(current.id, updates);
    if (updated != null) {
      _applyAgent(updated);
      if (autoNamed) {
        _autoAssignedName = updated.agentName.trim();
      }
    }
    // On failure the swapped template keeps working — the client type on the
    // backend is cosmetic for the install task itself, so stay quiet.
  }

  Future<void> copyInstallTask() async {
    final task = installTask;
    if (task.isEmpty) {
      return;
    }
    await _writeClipboard(task);
    _showToast('ai_agent_api_copied'.tr, isError: false);
  }

  /// Manual "check now" — also the resume action after a poll timeout.
  Future<void> pollNow() async {
    pollTimedOut.value = false;
    await _refreshAgentOnce();
    if (step.value == QuickOnboardStep.install && _pollTimer == null) {
      _restartPolling();
    }
  }

  Future<void> _refreshAgentOnce() async {
    final id = agentId.value.trim();
    if (id.isEmpty || isRefreshing.value) {
      return;
    }
    isRefreshing.value = true;
    try {
      final refreshed = await agentService.getAgent(id);
      if (isClosed) {
        return;
      }
      if (refreshed != null) {
        _applyAgent(refreshed);
        if (refreshed.online && step.value == QuickOnboardStep.install) {
          await _handleOnline();
        }
      }
    } finally {
      isRefreshing.value = false;
    }
  }

  Future<void> _handleOnline() async {
    _stopPolling();
    step.value = QuickOnboardStep.online;
    if (_probeSent) {
      return;
    }
    _probeSent = true;
    try {
      final sessionId = await _sendProbe(
        agentId: agentId.value,
        agentName: currentAgent?.agentName ?? '',
        probeText: 'ai_agent_quick_probe_message'.tr,
      );
      probeDelivered.value = (sessionId?.trim() ?? '').isNotEmpty;
    } catch (_) {
      // The greeting is a nicety; the chat still opens without it.
      probeDelivered.value = false;
    }
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

  void _restartPolling() {
    _stopPolling();
    _polledFor = Duration.zero;
    pollTimedOut.value = false;
    if (_autoPolling) {
      _scheduleNextPoll();
    }
  }

  void _stopPolling() {
    _pollTimer?.cancel();
    _pollTimer = null;
  }

  void _scheduleNextPoll() {
    // isClosed: onClose can race an in-flight getAgent — its callback tail
    // would otherwise re-arm a timer on a destroyed controller.
    if (isClosed || step.value != QuickOnboardStep.install) {
      return;
    }
    if (_polledFor >= pollTimeoutAfter) {
      _pollTimer = null;
      pollTimedOut.value = true;
      return;
    }
    final interval = _polledFor >= fastPollWindow
        ? slowPollInterval
        : fastPollInterval;
    _pollTimer = Timer(interval, () async {
      _polledFor += interval;
      await _refreshAgentOnce();
      _scheduleNextPoll();
    });
  }

  String _labelOf(AgentApiInstallGuide guide) {
    final label = guide.label.trim();
    return label.isEmpty ? guide.type.trim() : label;
  }

  String _dedupName(String base) {
    final taken = <String>{
      for (final existing in agentService.agents)
        existing.agentName.trim().toLowerCase(),
      for (final existing in agentService.sharedAgents)
        existing.agentName.trim().toLowerCase(),
    };
    if (!taken.contains(base.toLowerCase())) {
      return base;
    }
    for (var i = 2; i <= 99; i++) {
      final candidate = '$base-$i';
      if (!taken.contains(candidate.toLowerCase())) {
        return candidate;
      }
    }
    return base;
  }

  void _applyAgent(AgentModel value) {
    agent.value = value;
    if (value.id.trim().isNotEmpty) {
      agentId.value = value.id.trim();
    }
    if (value.apiEndpoint.trim().isNotEmpty || apiEndpoint.value.isEmpty) {
      apiEndpoint.value = value.apiEndpoint.trim();
    }
    // The one-time secret arrives exactly once, on the create response. Any
    // later refresh carries only the hint — never let it erase the secret.
    if (value.apiKey.trim().isNotEmpty) {
      apiKey.value = value.apiKey.trim();
    }
  }

  String _normalizeType(String value) => value.trim().toLowerCase();

  static Future<void> _defaultWriteClipboard(String text) {
    return Clipboard.setData(ClipboardData(text: text));
  }

  static Future<String?> _defaultSendProbe({
    required String agentId,
    required String agentName,
    required String probeText,
  }) async {
    if (!Get.isRegistered<SessionService>() || !Get.isRegistered<ImService>()) {
      return null;
    }
    final sessionService = Get.find<SessionService>();
    final imService = Get.find<ImService>();
    final sid = (await sessionService.createSession(agentId, 2))?.trim() ?? '';
    if (sid.isEmpty) {
      return null;
    }
    final title = agentName.trim();
    if (title.isNotEmpty && !imService.hasSessionDisplayTitleById(sid)) {
      await imService.bindSessionDisplayTitle(
        sid,
        title,
        type: 'private',
        peerId: agentId,
        peerType: 2,
      );
    }
    await imService.sendMessage(probeText, sid, updateCurrentSessionUi: false);
    return sid;
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
}
