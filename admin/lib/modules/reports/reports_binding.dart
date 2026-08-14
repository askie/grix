import 'package:get/get.dart';

import 'report_detail_controller.dart';
import 'reports_controller.dart';

class ReportsBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<ReportsController>(() => ReportsController());
  }
}

class ReportDetailBinding extends Bindings {
  @override
  void dependencies() {
    // 详情 ID 通过路由参数传入。
    final id = Get.parameters['id'] ?? Get.arguments?.toString() ?? '';
    Get.lazyPut<ReportDetailController>(() => ReportDetailController(id));
  }
}
