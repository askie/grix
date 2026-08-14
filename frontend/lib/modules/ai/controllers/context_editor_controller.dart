import 'package:flutter/material.dart';
import 'package:get/get.dart';
import '../../../data/providers/agent_service.dart';
import '../../../shared/utils/toast_util.dart';

class ContextEditorController extends GetxController {
  final AgentService agentService = Get.find<AgentService>();
  final textController = TextEditingController();

  final isLoading = false.obs;
  final isSaving = false.obs;
  late String agentId;

  @override
  void onInit() {
    super.onInit();
    final args = Get.arguments as Map<String, dynamic>? ?? {};
    agentId = args['agent_id']?.toString() ?? '';
    _loadContext();
  }

  Future<void> _loadContext() async {
    isLoading.value = true;
    final agent = await agentService.getAgent(agentId);
    if (agent != null) {
      textController.value = TextEditingValue(
        text: agent.contextFile,
        selection: TextSelection.collapsed(offset: agent.contextFile.length),
      );
    }
    isLoading.value = false;
  }

  Future<void> save() async {
    isSaving.value = true;
    final success = await agentService.updateContext(
      agentId,
      textController.text,
    );
    isSaving.value = false;
    if (success) {
      CustomToast.show('common_save_success'.tr, isError: false);
      Get.back();
    } else {
      CustomToast.show('common_error'.tr, isError: true);
    }
  }

  @override
  void onClose() {
    textController.dispose();
    super.onClose();
  }
}
