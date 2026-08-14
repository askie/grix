import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'controllers/context_editor_controller.dart';

class ContextEditorView extends GetView<ContextEditorController> {
  const ContextEditorView({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text('ai_agent_context_file'.tr),
        actions: [
          Obx(() => TextButton(
                onPressed: controller.isSaving.value ? null : controller.save,
                child: controller.isSaving.value
                    ? const SizedBox(
                        width: 16,
                        height: 16,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : Text('common_save'.tr),
              )),
        ],
      ),
      body: Obx(() {
        if (controller.isLoading.value) {
          return const Center(child: CircularProgressIndicator());
        }
        return Padding(
          padding: const EdgeInsets.all(16),
          child: TextField(
            controller: controller.textController,
            maxLines: null,
            expands: true,
            textAlignVertical: TextAlignVertical.top,
            decoration: InputDecoration(
              hintText: 'ai_agent_context_placeholder'.tr,
              border: OutlineInputBorder(
                borderRadius: BorderRadius.circular(8),
              ),
              contentPadding: const EdgeInsets.all(12),
            ),
            style: const TextStyle(
              fontSize: 14,
              fontFamily: 'monospace',
              height: 1.5,
            ),
          ),
        );
      }),
    );
  }
}
