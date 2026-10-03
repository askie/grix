import 'package:get/get.dart';

import 'system_controls_controller.dart';

class SystemControlsBinding extends Bindings {
  @override
  void dependencies() =>
      Get.lazyPut<SystemControlsController>(() => SystemControlsController());
}
