import 'package:get/get.dart';

import '../../shared/widgets/confirm_dialog.dart';
import 'dashboard_models.dart';
import 'dashboard_service.dart';

class DashboardController extends GetxController {
  final Rxn<DashboardStats> stats = Rxn<DashboardStats>();
  final RxBool loading = false.obs;

  @override
  void onInit() {
    super.onInit();
    loadStats();
  }

  Future<void> loadStats() async {
    if (loading.value) return;
    loading.value = true;
    try {
      stats.value = await DashboardService.stats();
    } catch (e) {
      Toast.error(e.toString());
    } finally {
      loading.value = false;
    }
  }
}
