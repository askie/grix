import 'package:get/get.dart';
import 'controller.dart';

class AppReleasesBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<AppReleasesController>(() => AppReleasesController());
  }
}
