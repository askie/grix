import 'package:get/get.dart';

import '../../shared/widgets/confirm_dialog.dart';
import 'reach_models.dart';
import 'reach_service.dart';

class ReachTaskDetailController extends GetxController {
  ReachTaskDetailController(this.taskId);

  final String taskId;

  final Rxn<ReachTaskDetail> task = Rxn<ReachTaskDetail>();
  final Rxn<ReachTaskStats> stats = Rxn<ReachTaskStats>();
  final RxBool loading = false.obs;
  final RxnString error = RxnString();

  @override
  void onInit() {
    super.onInit();
    loadDetail();
  }

  Future<void> loadDetail() async {
    loading.value = true;
    error.value = null;
    try {
      final results = await Future.wait([
        ReachService.getTask(taskId),
        ReachService.getTaskStats(taskId),
      ]);
      task.value = results[0] as ReachTaskDetail;
      stats.value = results[1] as ReachTaskStats;
    } catch (e) {
      error.value = e.toString();
    } finally {
      loading.value = false;
    }
  }

  Future<void> pauseTask() async {
    try {
      await ReachService.pauseTask(taskId);
      Toast.success('已暂停');
      await loadDetail();
    } catch (e) {
      Toast.error(e.toString());
    }
  }

  Future<void> cancelTask() async {
    try {
      await ReachService.cancelTask(taskId);
      Toast.success('已取消');
      await loadDetail();
    } catch (e) {
      Toast.error(e.toString());
    }
  }

  Future<void> resumeTask() async {
    try {
      await ReachService.resumeTask(taskId);
      Toast.success('已恢复');
      await loadDetail();
    } catch (e) {
      Toast.error(e.toString());
    }
  }

  Future<void> sendTask() async {
    try {
      await ReachService.sendTask(taskId);
      Toast.success('已开始发送');
      await loadDetail();
    } catch (e) {
      Toast.error(e.toString());
    }
  }
}
