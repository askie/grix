import 'package:get/get.dart';

import 'sms_settings_controller.dart';

class SmsSettingsBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<SmsSettingsController>(() => SmsSettingsController());
  }
}
