import 'package:get/get.dart';

import 'link_blocklist_controller.dart';
import 'link_blocklist_settings_controller.dart';

class LinkBlocklistBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<LinkBlocklistController>(() => LinkBlocklistController());
  }
}

class LinkBlocklistSettingsBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<LinkBlocklistSettingsController>(
        () => LinkBlocklistSettingsController());
  }
}
