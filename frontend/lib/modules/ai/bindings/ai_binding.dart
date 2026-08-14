import 'package:get/get.dart';
import '../controllers/agents_controller.dart';
import '../controllers/agent_create_controller.dart';
import '../controllers/agent_create_wizard_controller.dart';
import '../controllers/agent_connection_setup_controller.dart';
import '../controllers/agent_quick_onboard_controller.dart';
import '../controllers/agent_conn_security_controller.dart';
import '../controllers/agent_scope_controller.dart';
import '../controllers/context_editor_controller.dart';
import '../../profile/services/avatar_cropper_service.dart';

class AiBinding extends Bindings {
  @override
  void dependencies() {
    if (!Get.isRegistered<AvatarCropperService>()) {
      Get.put<AvatarCropperService>(AvatarCropperService(), permanent: true);
    }
    Get.lazyPut<AgentsController>(() => AgentsController());
    Get.lazyPut<AgentCreateController>(
      () => AgentCreateController(),
      fenix: true,
    );
    Get.lazyPut<AgentCreateWizardController>(
      () => AgentCreateWizardController(),
      fenix: true,
    );
    Get.lazyPut<AgentConnectionSetupController>(
      () => AgentConnectionSetupController(),
      fenix: true,
    );
    Get.lazyPut<AgentQuickOnboardController>(
      () => AgentQuickOnboardController(),
      fenix: true,
    );
    Get.lazyPut<AgentScopeController>(
      () => AgentScopeController(),
      fenix: true,
    );
    Get.lazyPut<AgentConnSecurityController>(
      () => AgentConnSecurityController(),
      fenix: true,
    );
    Get.lazyPut<ContextEditorController>(
      () => ContextEditorController(),
      fenix: true,
    );
  }
}
