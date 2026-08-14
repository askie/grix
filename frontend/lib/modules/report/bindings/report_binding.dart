import 'package:get/get.dart';

import '../../../data/providers/report_service.dart';
import '../controllers/report_controller.dart';

class ReportBinding extends Bindings {
  @override
  void dependencies() {
    if (!Get.isRegistered<ReportService>()) {
      Get.lazyPut<ReportService>(() => ReportService(), fenix: true);
    }
    Get.lazyPut<ReportController>(() => ReportController());
  }
}
