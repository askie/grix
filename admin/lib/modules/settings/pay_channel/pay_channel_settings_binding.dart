import 'package:get/get.dart';

import 'pay_channel_settings_controller.dart';

class PayChannelSettingsBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<PayChannelSettingsController>(() => PayChannelSettingsController());
  }
}
