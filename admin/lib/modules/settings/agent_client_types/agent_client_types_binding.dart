import 'package:get/get.dart';

import 'agent_client_types_controller.dart';

class AgentClientTypesSettingsBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<AgentClientTypesSettingsController>(
      () => AgentClientTypesSettingsController(),
    );
  }
}
