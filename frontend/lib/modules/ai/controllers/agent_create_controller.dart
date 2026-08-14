import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../data/providers/agent_service.dart';
import '../../../data/providers/feature_flag_service.dart';
import '../../../data/providers/friend_service.dart';
import '../../../data/providers/im_service.dart';
import '../../../data/providers/local_db.dart';
import '../../../shared/utils/hardware_facade.dart';
import '../../../shared/utils/toast_util.dart';
import '../../../shared/utils/user_image_cache_manager.dart';
import '../../../shared/widgets/app_dialog_style.dart';
import '../../call/call_controller.dart';
import '../../profile/services/avatar_cropper_service.dart';
import '../models/agent_editor_result.dart';
import '../models/agent_profile_draft.dart';
import '../utils/agent_introduction_mention.dart';
import '../widgets/contact_agent_pick_result.dart';

typedef ToastShower = void Function(String message, {bool isError});
typedef PageCloser = void Function({Object? result});

enum AgentSaveButtonState { idle, saving, saved }

class AgentCreateController extends GetxController {
  AgentCreateController({ToastShower? showToast, PageCloser? closePage})
    : _showToast = showToast ?? CustomToast.show,
      _closePage = closePage ?? _defaultClosePage;

  final AgentService agentService = Get.find<AgentService>();
  final AvatarCropperService avatarCropperService =
      Get.find<AvatarCropperService>();
  final ToastShower _showToast;
  final PageCloser _closePage;
  final formKey = GlobalKey<FormState>();

  final profileDraft = AgentProfileDraft();
  final providerController = TextEditingController();
  final promptController = TextEditingController();
  final endpointController = TextEditingController(
    text: 'http://localhost:11434',
  );
  final modelNameController = TextEditingController(text: 'gemma3:4b');

  // 语音大模型 BYOK（provider_type=4）
  // provider/model/endpoint 不再由用户手填，统一从塘主维护的语音模型清单中选择。
  // 新建场景下必须保持空值——_reconcileVoiceSelection 用 provider+model 是否非空来区分
  // 新建/编辑：非空走"按清单匹配"分支，命不中会合成占位项。塞默认值会让新建用户也走匹配
  // 分支并意外触发占位项。
  final voiceProvider = ''.obs;
  final voiceModelController = TextEditingController();
  final voiceEndpointController = TextEditingController();
  // 语音模型清单（塘主后台维护）+ 当前选中项 id。
  final voiceModelOptions = <VoiceModelOption>[].obs;
  final selectedVoiceModelId = ''.obs;
  final voiceModelsLoading = false.obs;
  // 合成的"当前配置"占位项 id（编辑旧 agent 且其配置已不在清单时使用，避免丢数据）。
  static const String _currentVoiceOptionId = '__current__';
  final voiceIdController = TextEditingController();
  final availableVoicePresets = <VoicePresetOption>[].obs;
  final voiceApiKeyController = TextEditingController();
  final voiceApiKeyHint = ''.obs;
  final voiceMaxCallSecondsController = TextEditingController(text: '0');
  final voiceDailyCallLimitController = TextEditingController(text: '0');
  final voiceAllowVisitor = false.obs;
  // 语音开场白：按语言存文案，通话建立后主动播报；缺省不打招呼。
  final voiceWelcomeI18n = <String, String>{}.obs;

  final providerType = 3.obs; // 1=remote, 2=local, 3=agent API, 4=voice
  final isLoading = false.obs;
  final saveButtonState = AgentSaveButtonState.idle.obs;
  final apiAgentId = ''.obs;
  final apiEndpoint = ''.obs;
  final apiKey = ''.obs;
  final apiKeyHint = ''.obs;
  final apiInstallGuides = <AgentApiInstallGuide>[].obs;
  final apiInstallGuidesLoading = false.obs;
  final selectedApiInstallGuideType = ''.obs;
  final categoryId = '0'.obs;

  bool isEditMode = false;
  String? editAgentId;
  Timer? _saveButtonResetTimer;
  String _preferredApiInstallGuideType = '';
  final List<AgentIntroductionMention> _pendingIntroductionMentions =
      <AgentIntroductionMention>[];
  String? _rawIntroductionFromServer;

  TextEditingController get nameController => profileDraft.nameController;
  TextEditingController get introductionController =>
      profileDraft.introductionController;
  RxString get avatarUrl => profileDraft.avatarUrl;
  Rxn<Uint8List> get pendingAvatarBytes => profileDraft.pendingAvatarBytes;

  /// 把联系人 / agent 以艾特形式插入介绍输入框：编辑态显示昵称，保存时归一成 id。
  void insertContactIntoIntroduction(ContactAgentPickResult pick) {
    final id = pick.id.trim();
    final displayName = pick.displayName.trim().isNotEmpty
        ? pick.displayName.trim()
        : id;
    if (id.isEmpty || displayName.isEmpty) {
      return;
    }

    final controller = introductionController;
    final text = controller.text;
    final selection = controller.selection;
    final start = selection.isValid ? selection.start : text.length;
    final end = selection.isValid ? selection.end : text.length;
    final prefix = text.substring(0, start);
    final suffix = text.substring(end);
    final insertText = buildIntroductionMentionInsertText(
      prefix: prefix,
      suffix: suffix,
      displayName: displayName,
    );
    controller.value = TextEditingValue(
      text: '$prefix$insertText$suffix',
      selection: TextSelection.collapsed(
        offset: prefix.length + insertText.length,
      ),
    );
    _upsertPendingIntroductionMention(id: id, displayName: displayName);
  }

  /// 兼容旧调用：仅有 id 时按 id 展示并保存。
  void insertIdIntoIntroduction(String id) {
    final trimmedId = id.trim();
    if (trimmedId.isEmpty) {
      return;
    }
    insertContactIntoIntroduction(
      ContactAgentPickResult(id: trimmedId, displayName: trimmedId),
    );
  }

  /// 保存用介绍：把编辑态 `@昵称` 归一成 `@id`。
  String get introductionForSave {
    _syncPendingIntroductionMentions(introductionController.text);
    return normalizeIntroductionMentions(
      profileDraft.introduction,
      _pendingIntroductionMentions,
    );
  }

  void _upsertPendingIntroductionMention({
    required String id,
    required String displayName,
  }) {
    final existingIndex = _pendingIntroductionMentions.indexWhere(
      (mention) =>
          mention.id == id && mention.displayName == displayName,
    );
    if (existingIndex != -1) {
      return;
    }
    _pendingIntroductionMentions.add(
      AgentIntroductionMention(id: id, displayName: displayName),
    );
  }

  void _syncPendingIntroductionMentions(String content) {
    if (_pendingIntroductionMentions.isEmpty) {
      return;
    }
    _pendingIntroductionMentions.removeWhere(
      (mention) =>
          !containsIntroductionMentionToken(content, mention.displayName),
    );
  }

  Map<String, String> _buildIntroductionIdDisplayMap() {
    final map = <String, String>{};
    if (Get.isRegistered<FriendService>()) {
      final friends = Get.find<FriendService>().friendList;
      for (final friend in friends) {
        final id = friend.userId.trim();
        if (id.isEmpty) {
          continue;
        }
        final remark = friend.remarkName.trim();
        final nickname = friend.nickname.trim();
        final username = friend.username.trim();
        final display = remark.isNotEmpty
            ? remark
            : (nickname.isNotEmpty ? nickname : username);
        if (display.isNotEmpty) {
          map[id] = display;
        }
      }
    }
    if (Get.isRegistered<AgentService>()) {
      for (final agent in agentService.allAccessibleAgents) {
        final id = agent.id.trim();
        final name = agent.agentName.trim();
        if (id.isEmpty || name.isEmpty) {
          continue;
        }
        map[id] = name;
      }
    }
    return map;
  }

  void _hydrateIntroductionMentions(String rawIntroduction) {
    final hydrated = hydrateIntroductionMentions(
      rawIntroduction,
      _buildIntroductionIdDisplayMap(),
    );
    _pendingIntroductionMentions
      ..clear()
      ..addAll(hydrated.mentions);
    introductionController.value = TextEditingValue(
      text: hydrated.text,
      selection: TextSelection.collapsed(offset: hydrated.text.length),
    );
  }

  Future<void> _ensureMentionSourcesLoaded() async {
    final futures = <Future<void>>[];
    if (Get.isRegistered<FriendService>()) {
      futures.add(Get.find<FriendService>().loadFriendList());
    }
    futures.add(agentService.loadAgents());
    if (futures.isNotEmpty) {
      await Future.wait(futures);
    }
  }

  Future<void> _hydrateIntroductionWhenReady() async {
    final raw = _rawIntroductionFromServer;
    if (raw == null) {
      return;
    }
    await _ensureMentionSourcesLoaded();
    // 仍以服务端原文为准；用户已改动则不再覆盖编辑态文本。
    if (_rawIntroductionFromServer != raw) {
      return;
    }
    if (introductionController.text != raw) {
      return;
    }
    _hydrateIntroductionMentions(raw);
  }

  static void _defaultClosePage({Object? result}) {
    Get.back(result: result);
  }

  @override
  void onInit() {
    super.onInit();
    final args = Get.arguments as Map<String, dynamic>? ?? {};
    final preloadedAgent = args['agent'];
    if (preloadedAgent is AgentModel) {
      _applyAgent(preloadedAgent);
    }
    final agentId =
        args['agent_id']?.toString().trim() ??
        (preloadedAgent is AgentModel ? preloadedAgent.id.trim() : '');
    if (agentId.isNotEmpty) {
      isEditMode = true;
      editAgentId = agentId;
      apiAgentId.value = agentId;
      _loadAgent(agentId);
    }
    unawaited(loadAgentApiInstallGuides());
    unawaited(loadVoiceModels());
    if (isEditMode) {
      unawaited(_hydrateIntroductionWhenReady());
    }
  }

  /// 拉取塘主维护的语音模型清单，并对齐当前选中项。
  Future<void> loadVoiceModels() async {
    if (voiceModelsLoading.value) {
      return;
    }
    voiceModelsLoading.value = true;
    try {
      final list = await agentService.getVoiceModels();
      if (list == null) {
        return;
      }
      voiceModelOptions.assignAll(list);
      _reconcileVoiceSelection();
    } finally {
      voiceModelsLoading.value = false;
    }
  }

  VoiceModelOption? _voiceOptionById(String id) {
    for (final o in voiceModelOptions) {
      if (o.id == id) {
        return o;
      }
    }
    return null;
  }

  /// 选中清单中的某项：把 provider/model/endpoint 一并落到提交字段，同时更新可选音色列表。
  void selectVoiceModel(String id) {
    final option = _voiceOptionById(id);
    if (option == null) {
      return;
    }
    selectedVoiceModelId.value = option.id;
    voiceProvider.value = option.provider;
    voiceModelController.text = option.model;
    voiceEndpointController.text = option.endpoint;
    availableVoicePresets.assignAll(option.voices);
  }

  /// 对齐选中项：
  /// - 新建：默认选清单第一项；
  /// - 编辑：按已存 provider+model 命中清单项；命不中则合成"当前配置"占位项，避免保存时丢数据。
  void _reconcileVoiceSelection() {
    if (voiceModelOptions.isEmpty) {
      return;
    }
    final currentProvider = voiceProvider.value.trim();
    final currentModel = voiceModelController.text.trim();
    if (currentProvider.isNotEmpty && currentModel.isNotEmpty) {
      VoiceModelOption? matched;
      for (final o in voiceModelOptions) {
        if (o.provider == currentProvider && o.model == currentModel) {
          matched = o;
          break;
        }
      }
      if (matched != null) {
        selectVoiceModel(matched.id);
        return;
      }
      // 旧配置已不在清单中：合成占位项并保留原 endpoint。
      voiceModelOptions.removeWhere((o) => o.id == _currentVoiceOptionId);
      voiceModelOptions.insert(
        0,
        VoiceModelOption(
          id: _currentVoiceOptionId,
          label: '$currentProvider · $currentModel',
          provider: currentProvider,
          model: currentModel,
          endpoint: voiceEndpointController.text.trim(),
        ),
      );
      selectedVoiceModelId.value = _currentVoiceOptionId;
      return;
    }
    // 新建场景：默认选第一项。
    selectVoiceModel(voiceModelOptions.first.id);
  }

  Future<void> _loadAgent(String agentId) async {
    final agent = await agentService.getAgent(agentId);
    if (agent == null) {
      return;
    }
    _applyAgent(agent);
  }

  void _applyAgent(AgentModel agent) {
    profileDraft.applyAgent(agent);
    _rawIntroductionFromServer = agent.introduction;
    providerController.value = TextEditingValue(
      text: agent.modelProvider,
      selection: TextSelection.collapsed(offset: agent.modelProvider.length),
    );
    promptController.value = TextEditingValue(
      text: agent.systemPrompt,
      selection: TextSelection.collapsed(offset: agent.systemPrompt.length),
    );
    endpointController.value = TextEditingValue(
      text: agent.localEndpoint,
      selection: TextSelection.collapsed(offset: agent.localEndpoint.length),
    );
    modelNameController.value = TextEditingValue(
      text: agent.localModelName,
      selection: TextSelection.collapsed(offset: agent.localModelName.length),
    );
    providerType.value = agent.providerType;
    apiAgentId.value = agent.id;
    apiEndpoint.value = agent.apiEndpoint;
    apiKey.value = agent.apiKey;
    apiKeyHint.value = agent.apiKeyHint;
    categoryId.value = agent.categoryId;
    // 语音大模型字段（api key 永不回明文，仅 hint）
    if (agent.voiceProvider.isNotEmpty) {
      voiceProvider.value = agent.voiceProvider;
    }
    if (agent.voiceModel.isNotEmpty) {
      voiceModelController.text = agent.voiceModel;
    }
    voiceEndpointController.text = agent.voiceEndpoint;
    voiceIdController.text = agent.voiceId;
    voiceApiKeyHint.value = agent.voiceApiKeyHint;
    voiceMaxCallSecondsController.text = agent.voiceMaxCallSeconds.toString();
    voiceDailyCallLimitController.text = agent.voiceDailyCallLimit.toString();
    voiceAllowVisitor.value = agent.voiceAllowVisitor;
    voiceWelcomeI18n.value = Map.of(agent.voiceWelcomeI18n);
    // 清单可能已先加载完成，这里对齐选中项（命中或合成占位项）。
    _reconcileVoiceSelection();
    _syncApiInstallGuideType(agent.agentClientType);
    unawaited(_hydrateIntroductionWhenReady());
  }

  @override
  void onClose() {
    _saveButtonResetTimer?.cancel();
    profileDraft.dispose();
    providerController.dispose();
    promptController.dispose();
    endpointController.dispose();
    modelNameController.dispose();
    voiceModelController.dispose();
    voiceEndpointController.dispose();
    voiceIdController.dispose();
    voiceApiKeyController.dispose();
    voiceMaxCallSecondsController.dispose();
    voiceDailyCallLimitController.dispose();
    super.onClose();
  }

  Future<void> save() async {
    FocusScope.of(Get.context!).unfocus();
    if (!formKey.currentState!.validate()) {
      return;
    }
    if (isLoading.value) {
      return;
    }
    // 语音类型必须从清单里选中一项（provider/model 才会落定）。
    if (providerType.value == 4 &&
        (selectedVoiceModelId.value.trim().isEmpty ||
            voiceModelController.text.trim().isEmpty)) {
      _showToast('ai_voice_model_required'.tr, isError: true);
      return;
    }

    _setSaveButtonState(AgentSaveButtonState.saving);
    isLoading.value = true;
    try {
      final payload = _buildAgentPayload();
      AgentModel? result;
      if (isEditMode && editAgentId != null) {
        result = await agentService.updateAgent(editAgentId!, payload);
      } else {
        result = await agentService.createAgent(payload);
      }
      if (result == null) {
        _setSaveButtonState(AgentSaveButtonState.idle);
        _showToast(_resolveSaveErrorMessage(), isError: true);
        return;
      }

      isEditMode = true;
      editAgentId = result.id;
      apiAgentId.value = result.id;

      final avatarSynced = await _uploadPendingAvatarIfNeeded(result);
      if (avatarSynced == null) {
        return;
      }

      _syncAgentSecrets(avatarSynced, fallback: result);
      // 语音大模型：保存后回填 hint 并清空已输入的明文 key
      if (providerType.value == 4) {
        voiceApiKeyHint.value = result.voiceApiKeyHint;
        voiceApiKeyController.clear();
      }
      _setSaveButtonState(
        AgentSaveButtonState.saved,
        autoResetAfter: const Duration(seconds: 2),
      );
      if (providerType.value == 3 || providerType.value == 4) {
        _showSaveSuccessToast();
        return;
      }

      _closePage(result: AgentEditorResult.saved);
    } finally {
      isLoading.value = false;
    }
  }

  String? validateAgentName(String? raw) {
    final value = (raw ?? '').trim();
    if (value.isEmpty) {
      return 'ai_agent_name_required'.tr;
    }
    if (value.runes.length > 100) {
      return 'ai_agent_name_too_long'.tr;
    }
    for (final rune in value.runes) {
      if (rune < 0x20 || rune == 0x7F) {
        return 'ai_agent_name_invalid_control_chars'.tr;
      }
    }
    return null;
  }

  /// 是否可对当前语音大模型 agent 测试拨打（仅 Web/桌面、已保存的 type=4）。
  bool get canTestCall =>
      providerType.value == 4 &&
      Get.find<FeatureFlagService>().isEnabled('voice_call') &&
      (editAgentId?.trim().isNotEmpty ?? false);

  /// 对自建语音大模型 agent 发起测试拨打。
  void testCall() {
    if (!canTestCall) {
      return;
    }
    final im = Get.find<ImService>();
    Get.find<CallController>().directCallAgent(
      editAgentId!,
      profileDraft.name,
      im.sendCallPacket,
    );
  }

  Future<void> rotateApiKey() async {
    if (editAgentId == null || providerType.value != 3 || isLoading.value) {
      return;
    }
    isLoading.value = true;
    try {
      final result = await agentService.rotateAgentApiKey(editAgentId!);
      if (result == null) {
        return;
      }
      _syncAgentSecrets(result);
    } finally {
      isLoading.value = false;
    }
  }

  Future<void> deleteCurrentAgent() async {
    if (editAgentId == null || isLoading.value) {
      return;
    }
    isLoading.value = true;
    try {
      final ok = await agentService.deleteAgent(editAgentId!);
      if (!ok) {
        _showToast('ai_agents_delete_failed'.tr);
        return;
      }
      _closePage(result: AgentEditorResult.deleted);
    } finally {
      isLoading.value = false;
    }
  }

  Future<void> showAvatarEditSheet() async {
    if (isLoading.value) {
      return;
    }

    await showAppActionSheet(
      context: Get.context!,
      items: [
        AppActionSheetItem(
          label: 'profile_avatar_pick_gallery'.tr,
          icon: Icons.photo_library_outlined,
          onTap: () => pickAvatar(fromCamera: false),
        ),
        AppActionSheetItem(
          label: 'profile_avatar_pick_camera'.tr,
          icon: Icons.photo_camera_outlined,
          onTap: () => pickAvatar(fromCamera: true),
        ),
      ],
    );
  }

  Future<void> pickAvatar({required bool fromCamera}) async {
    final picked = await HardwareFacade.pickImage(fromCamera: fromCamera);
    if (picked == null) {
      return;
    }

    try {
      final cropResult = await avatarCropperService.cropSquareAvatar(
        sourcePath: picked.path,
        webContext: Get.context,
      );
      if (cropResult == null) {
        return;
      }
      profileDraft.setPendingAvatar(
        bytes: cropResult.bytes,
        filename: picked.name.trim().isEmpty ? 'agent_avatar.jpg' : picked.name,
      );
    } catch (_) {
      _showToast('profile_avatar_crop_failed'.tr);
    }
  }

  Future<void> loadAgentApiInstallGuides() async {
    if (apiInstallGuidesLoading.value) {
      return;
    }
    apiInstallGuidesLoading.value = true;
    try {
      final catalog = await agentService.getAgentApiInstallGuides();
      if (catalog == null) {
        return;
      }
      apiInstallGuides.assignAll(catalog.list);
      _syncSelectedApiInstallGuide(defaultType: catalog.defaultType);
    } finally {
      apiInstallGuidesLoading.value = false;
    }
  }

  Map<String, dynamic> _buildAgentPayload() {
    final data = <String, dynamic>{
      'agent_name': profileDraft.name,
      'introduction': introductionForSave,
      'provider_type': providerType.value,
      'category_id': categoryId.value,
    };
    if (providerType.value == 1) {
      data['model_provider'] = providerController.text.trim();
      data['system_prompt'] = promptController.text.trim();
    } else if (providerType.value == 2) {
      data['local_endpoint'] = endpointController.text.trim();
      data['local_model_name'] = modelNameController.text.trim();
    } else if (providerType.value == 4) {
      data['voice_provider'] = voiceProvider.value;
      data['voice_model'] = voiceModelController.text.trim();
      data['voice_endpoint'] = voiceEndpointController.text.trim();
      data['voice_id'] = voiceIdController.text.trim();
      data['system_prompt'] = promptController.text.trim();
      // api key 留空表示保持原值（编辑场景）
      final key = voiceApiKeyController.text.trim();
      if (key.isNotEmpty) {
        data['voice_api_key'] = key;
      }
      data['voice_max_call_seconds'] =
          int.tryParse(voiceMaxCallSecondsController.text.trim()) ?? 0;
      data['voice_daily_call_limit'] =
          int.tryParse(voiceDailyCallLimitController.text.trim()) ?? 0;
      data['voice_allow_visitor'] = voiceAllowVisitor.value;
      data['voice_welcome_i18n'] = voiceWelcomeI18n;
    }
    return data;
  }

  Future<AgentModel?> _uploadPendingAvatarIfNeeded(AgentModel baseAgent) async {
    if (!profileDraft.hasPendingAvatarUpload) {
      profileDraft.commitAvatarUrl(baseAgent.avatarUrl);
      return baseAgent;
    }

    final bytes = profileDraft.pendingAvatarBytes.value;
    final filename = profileDraft.pendingAvatarFilename.value.trim();
    if (bytes == null || filename.isEmpty) {
      return baseAgent;
    }

    final uploadResult = await agentService.uploadAgentAvatar(
      agentId: baseAgent.id,
      bytes: bytes,
      filename: filename,
    );
    if (!uploadResult.ok || uploadResult.data == null) {
      _setSaveButtonState(AgentSaveButtonState.idle);
      _showToast(
        uploadResult.message.isNotEmpty
            ? uploadResult.message
            : 'ai_agent_avatar_upload_failed'.tr,
      );
      return null;
    }

    final updatedAgent = uploadResult.data!;
    profileDraft.commitAvatarUrl(updatedAgent.avatarUrl);

    // Evict old avatar from the local image cache so the new one is displayed
    // immediately. The backend now uses versioned object keys, so the new URL
    // differs from the old one — but evicting the old URL ensures no stale
    // entry lingers in the cache.
    final oldAvatarUrl = baseAgent.avatarUrl.trim();
    final newAvatarUrl = updatedAgent.avatarUrl.trim();
    final userId = LocalDb.activeUserId?.trim() ?? '';
    if (userId.isNotEmpty) {
      final toEvict = <String>[
        if (oldAvatarUrl.isNotEmpty) oldAvatarUrl,
        if (newAvatarUrl.isNotEmpty && newAvatarUrl != oldAvatarUrl) newAvatarUrl,
      ];
      if (toEvict.isNotEmpty) {
        unawaited(
          UserImageCacheManager.evictUserImages(userId, toEvict).catchError(
            (Object _) {},
          ),
        );
      }
    }

    return updatedAgent;
  }

  void _syncAgentSecrets(AgentModel agent, {AgentModel? fallback}) {
    apiAgentId.value = _pickAgentValue(agent.id, fallback?.id);
    apiEndpoint.value = _pickAgentValue(
      agent.apiEndpoint,
      fallback?.apiEndpoint,
    );
    apiKeyHint.value = _pickAgentValue(agent.apiKeyHint, fallback?.apiKeyHint);
    profileDraft.commitAvatarUrl(agent.avatarUrl);
    _syncApiInstallGuideType(
      _pickAgentValue(agent.agentClientType, fallback?.agentClientType),
    );
    apiKey.value = _pickAgentValue(agent.apiKey, fallback?.apiKey);
  }

  String _pickAgentValue(String primary, String? fallback) {
    final normalizedPrimary = primary.trim();
    if (normalizedPrimary.isNotEmpty) {
      return normalizedPrimary;
    }
    return fallback?.trim() ?? '';
  }

  AgentApiInstallGuide? get selectedApiInstallGuide {
    final normalizedType = _normalizeApiInstallGuideType(
      selectedApiInstallGuideType.value,
    );
    if (normalizedType.isEmpty) {
      return null;
    }
    for (final guide in apiInstallGuides) {
      if (_normalizeApiInstallGuideType(guide.type) == normalizedType) {
        return guide;
      }
    }
    return null;
  }

  void selectApiInstallGuide(String rawType) {
    final normalized = _normalizeApiInstallGuideType(rawType);
    if (normalized.isEmpty || !_hasApiInstallGuide(normalized)) {
      return;
    }
    selectedApiInstallGuideType.value = normalized;
  }

  void _syncApiInstallGuideType(String rawClientType) {
    final normalized = _normalizeApiInstallGuideType(rawClientType);
    if (normalized.isEmpty) {
      return;
    }
    _preferredApiInstallGuideType = normalized;
    if (apiInstallGuides.isEmpty) {
      return;
    }
    selectApiInstallGuide(normalized);
  }

  void _syncSelectedApiInstallGuide({String defaultType = ''}) {
    if (apiInstallGuides.isEmpty) {
      selectedApiInstallGuideType.value = '';
      return;
    }

    final current = _normalizeApiInstallGuideType(
      selectedApiInstallGuideType.value,
    );
    if (_hasApiInstallGuide(current)) {
      selectedApiInstallGuideType.value = current;
      return;
    }

    final preferred = _normalizeApiInstallGuideType(
      _preferredApiInstallGuideType,
    );
    if (_hasApiInstallGuide(preferred)) {
      selectedApiInstallGuideType.value = preferred;
      return;
    }

    final normalizedDefault = _normalizeApiInstallGuideType(defaultType);
    if (_hasApiInstallGuide(normalizedDefault)) {
      selectedApiInstallGuideType.value = normalizedDefault;
      return;
    }

    selectedApiInstallGuideType.value = _normalizeApiInstallGuideType(
      apiInstallGuides.first.type,
    );
  }

  bool _hasApiInstallGuide(String normalizedType) {
    if (normalizedType.isEmpty) {
      return false;
    }
    for (final guide in apiInstallGuides) {
      if (_normalizeApiInstallGuideType(guide.type) == normalizedType) {
        return true;
      }
    }
    return false;
  }

  String _normalizeApiInstallGuideType(String rawType) {
    return rawType.trim().toLowerCase();
  }

  void _showSaveSuccessToast() {
    _showToast('common_save_success'.tr, isError: false);
  }

  void _setSaveButtonState(
    AgentSaveButtonState state, {
    Duration? autoResetAfter,
  }) {
    _saveButtonResetTimer?.cancel();
    _saveButtonResetTimer = null;
    saveButtonState.value = state;
    if (autoResetAfter == null) {
      return;
    }
    _saveButtonResetTimer = Timer(autoResetAfter, () {
      if (saveButtonState.value == state) {
        saveButtonState.value = AgentSaveButtonState.idle;
      }
      _saveButtonResetTimer = null;
    });
  }

  String _resolveSaveErrorMessage() {
    final serviceMessage = agentService.lastOperationError.trim();
    if (serviceMessage.isNotEmpty) {
      return serviceMessage;
    }
    return isEditMode
        ? 'ai_agents_update_failed'.tr
        : 'ai_agents_create_failed'.tr;
  }
}
