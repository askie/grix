import 'package:get/get.dart';

import '../controllers/group_invite_controller.dart';

class GroupInviteBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<GroupInviteController>(() => GroupInviteController());
  }
}
