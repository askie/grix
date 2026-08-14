import 'package:get/get.dart';

import 'push_settings_controller.dart';

class PushSettingsBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<PushSettingsController>(() => PushSettingsController());
  }
}
