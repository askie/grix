import 'package:get/get.dart';
import 'feature_gates_controller.dart';

class FeatureGatesBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<FeatureGatesController>(() => FeatureGatesController());
  }
}
