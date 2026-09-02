import 'package:get/get.dart';

import 'inactive_users_controller.dart';
import 'reach_task_detail_controller.dart';
import 'reach_tasks_controller.dart';
import 'reach_templates_controller.dart';

class ReachTasksBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<ReachTasksController>(() => ReachTasksController());
  }
}

class ReachTemplatesBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<ReachTemplatesController>(() => ReachTemplatesController());
  }
}

class ReachTaskDetailBinding extends Bindings {
  @override
  void dependencies() {
    final id = Get.parameters['id'] ?? '';
    Get.lazyPut<ReachTaskDetailController>(() => ReachTaskDetailController(id));
  }
}

class ReachInactiveUsersBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<InactiveUsersController>(() => InactiveUsersController());
  }
}
