import 'package:get/get.dart';

import '../controllers/group_info_controller.dart';

class GroupInfoBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<GroupInfoController>(() => GroupInfoController());
  }
}
