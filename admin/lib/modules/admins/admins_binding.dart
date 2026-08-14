import 'package:get/get.dart';

import '../roles/roles_controller.dart';
import 'admins_controller.dart';

class AdminsBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<AdminsController>(() => AdminsController());
    Get.lazyPut<RolesController>(() => RolesController());
  }
}
