import 'package:get/get.dart';

import 'local_search_controller.dart';

class LocalSearchBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<LocalSearchController>(() => LocalSearchController());
  }
}
