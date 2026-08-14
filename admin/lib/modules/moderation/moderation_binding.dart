import 'package:get/get.dart';

import 'moderation_controller.dart';
import 'moderation_settings_controller.dart';

class ModerationBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<ModerationController>(() => ModerationController());
  }
}

class ModerationSettingsBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<ModerationSettingsController>(
        () => ModerationSettingsController());
  }
}
