import 'package:get/get.dart';

import '../../shared/widgets/confirm_dialog.dart';
import 'feature_gate_models.dart';
import 'feature_gate_service.dart';

class FeatureGatesController extends GetxController {
  final RxList<FeatureGateInfo> gates = <FeatureGateInfo>[].obs;
  final RxList<AvailableFeature> available = <AvailableFeature>[].obs;
  final RxBool loading = false.obs;
  final RxnString error = RxnString();

  @override
  void onInit() {
    super.onInit();
    load();
  }

  Future<void> load() async {
    loading.value = true;
    error.value = null;
    try {
      final result = await FeatureGateService.list();
      gates.assignAll(result.gates);
      available.assignAll(result.available);
    } catch (e) {
      error.value = e.toString();
    } finally {
      loading.value = false;
    }
  }

  Future<void> create(String key) async {
    try {
      await FeatureGateService.create(key);
      Toast.success('功能开关已创建');
      await load();
    } catch (e) {
      Toast.error(e.toString());
    }
  }

  Future<void> updateStatus(String key, String status) async {
    try {
      await FeatureGateService.updateStatus(key, status);
      Toast.success('状态已更新');
      await load();
    } catch (e) {
      Toast.error(e.toString());
    }
  }

  Future<void> addUsers(String key, String userIds) async {
    try {
      await FeatureGateService.modifyUsers(key, 'add', userIds);
      Toast.success('用户已添加');
      await load();
    } catch (e) {
      Toast.error(e.toString());
    }
  }

  Future<void> removeUsers(String key, String userIds) async {
    try {
      await FeatureGateService.modifyUsers(key, 'remove', userIds);
      Toast.success('用户已移除');
      await load();
    } catch (e) {
      Toast.error(e.toString());
    }
  }
}
