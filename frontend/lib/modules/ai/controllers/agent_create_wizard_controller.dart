import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../app/routes/app_routes.dart';
import '../../../data/providers/agent_service.dart';
import '../../../shared/utils/toast_util.dart';

typedef AgentSetupOpener = Future<void> Function(AgentModel agent);
typedef AgentCreateToast = void Function(String message, {bool isError});

class AgentCreateWizardController extends GetxController {
  AgentCreateWizardController({
    AgentService? agentService,
    AgentSetupOpener? openSetup,
    AgentCreateToast? showToast,
    int? presetProviderType,
  }) : agentService = agentService ?? Get.find<AgentService>(),
       _openSetup = openSetup ?? _defaultOpenSetup,
       _showToast = showToast ?? CustomToast.show,
       _presetProviderType = presetProviderType;

  final AgentService agentService;
  final AgentSetupOpener _openSetup;
  final AgentCreateToast _showToast;
  final int? _presetProviderType;

  final formKey = GlobalKey<FormState>();
  final nameController = TextEditingController();
  final introductionController = TextEditingController();
  final modelProviderController = TextEditingController();
  final promptController = TextEditingController();
  final localEndpointController = TextEditingController(
    text: 'http://localhost:11434',
  );
  final localModelController = TextEditingController(text: 'gemma3:4b');
  final voiceApiKeyController = TextEditingController();

  final step = 0.obs;
  final providerType = 0.obs;
  final showOptionalProfile = false.obs;
  final isSubmitting = false.obs;
  final voiceModelsLoading = false.obs;
  final voiceModels = <VoiceModelOption>[].obs;
  final selectedVoiceModelId = ''.obs;

  bool get hasSelectedType =>
      providerType.value >= 1 && providerType.value <= 4;

  VoiceModelOption? get selectedVoiceModel {
    final selectedId = selectedVoiceModelId.value.trim();
    for (final option in voiceModels) {
      if (option.id == selectedId) {
        return option;
      }
    }
    return null;
  }

  @override
  void onInit() {
    super.onInit();
    final arguments = Get.arguments;
    final rawPreset = arguments is Map
        ? arguments['preset_provider_type']?.toString()
        : null;
    final preset = _presetProviderType ?? int.tryParse(rawPreset ?? '');
    if (preset != null && preset >= 1 && preset <= 4) {
      selectProviderType(preset);
    }
  }

  void selectProviderType(int type) {
    if (type < 1 || type > 4) {
      return;
    }
    providerType.value = type;
    step.value = 1;
    if (type == 4) {
      loadVoiceModels();
    }
  }

  void chooseAnotherType() {
    FocusManager.instance.primaryFocus?.unfocus();
    step.value = 0;
  }

  Future<void> loadVoiceModels() async {
    if (voiceModelsLoading.value || voiceModels.isNotEmpty) {
      return;
    }
    voiceModelsLoading.value = true;
    try {
      final result = await agentService.getVoiceModels();
      if (result == null) {
        return;
      }
      voiceModels.assignAll(result);
      if (voiceModels.isNotEmpty && selectedVoiceModelId.value.isEmpty) {
        selectedVoiceModelId.value = voiceModels.first.id;
      }
    } finally {
      voiceModelsLoading.value = false;
    }
  }

  void selectVoiceModel(String? id) {
    if (id == null || id.trim().isEmpty) {
      return;
    }
    selectedVoiceModelId.value = id.trim();
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
      if (rune < 0x20 || rune == 0x7f) {
        return 'ai_agent_name_invalid_control_chars'.tr;
      }
    }
    return null;
  }

  String? validateLocalEndpoint(String? raw) {
    if (providerType.value == 2 && (raw == null || raw.trim().isEmpty)) {
      return 'ai_agent_endpoint_required'.tr;
    }
    return null;
  }

  String? validateVoiceModel(String? raw) {
    if (providerType.value == 4 && (raw == null || raw.trim().isEmpty)) {
      return 'ai_voice_model_required'.tr;
    }
    return null;
  }

  String? validateVoiceApiKey(String? raw) {
    if (providerType.value == 4 && (raw == null || raw.trim().isEmpty)) {
      return 'ai_voice_api_key_required'.tr;
    }
    return null;
  }

  Future<void> submit() async {
    FocusManager.instance.primaryFocus?.unfocus();
    if (!hasSelectedType || isSubmitting.value) {
      return;
    }
    if (!(formKey.currentState?.validate() ?? false)) {
      return;
    }

    final payload = _buildPayload();
    if (payload == null) {
      _showToast('ai_voice_model_required'.tr, isError: true);
      return;
    }

    isSubmitting.value = true;
    try {
      final created = await agentService.createAgent(payload);
      if (created == null) {
        final message = agentService.lastOperationError.trim();
        _showToast(
          message.isEmpty ? 'ai_agents_create_failed'.tr : message,
          isError: true,
        );
        return;
      }
      await _openSetup(created);
    } finally {
      isSubmitting.value = false;
    }
  }

  Map<String, dynamic>? _buildPayload() {
    final type = providerType.value;
    final payload = <String, dynamic>{
      'agent_name': nameController.text.trim(),
      'introduction': introductionController.text
          .replaceAll('\r\n', '\n')
          .replaceAll('\r', '\n')
          .trim(),
      'provider_type': type,
      'category_id': '0',
    };

    switch (type) {
      case 1:
        payload['model_provider'] = modelProviderController.text.trim();
        payload['system_prompt'] = promptController.text.trim();
        break;
      case 2:
        payload['local_endpoint'] = localEndpointController.text.trim();
        payload['local_model_name'] = localModelController.text.trim();
        break;
      case 3:
        break;
      case 4:
        final option = selectedVoiceModel;
        if (option == null) {
          return null;
        }
        payload['voice_provider'] = option.provider;
        payload['voice_model'] = option.model;
        payload['voice_endpoint'] = option.endpoint;
        payload['voice_id'] = option.voices.isEmpty
            ? ''
            : option.voices.first.id;
        payload['voice_api_key'] = voiceApiKeyController.text.trim();
        break;
      default:
        return null;
    }
    return payload;
  }

  static Future<void> _defaultOpenSetup(AgentModel agent) async {
    await Get.offNamed<void>(AppRoutes.agentSetup, arguments: {'agent': agent});
  }

  @override
  void onClose() {
    nameController.dispose();
    introductionController.dispose();
    modelProviderController.dispose();
    promptController.dispose();
    localEndpointController.dispose();
    localModelController.dispose();
    voiceApiKeyController.dispose();
    super.onClose();
  }
}
