import 'package:get/get.dart';

import 'visitor_bans_controller.dart';

class VisitorBansBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<VisitorBansController>(VisitorBansController.new);
  }
}
