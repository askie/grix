import 'package:get/get.dart';

import '../../core/network/page_result.dart';
import '../../shared/controllers/paged_list_controller.dart';
import 'reach_models.dart';
import 'reach_service.dart';

class ReachTasksController extends PagedListController<ReachTask> {
  final RxString statusFilter = ''.obs;
  final RxString kindFilter = ''.obs;

  @override
  Future<PageResult<ReachTask>> fetchPage() {
    return ReachService.listTasks(
      status: statusFilter.value,
      kind: kindFilter.value,
      page: page.value,
      pageSize: pageSize.value,
    );
  }

  void applyStatusFilter(String value) {
    statusFilter.value = value;
    reloadFromFirstPage();
  }

  void applyKindFilter(String value) {
    kindFilter.value = value;
    reloadFromFirstPage();
  }

  Future<void> pauseTask(String id) {
    return runAction(() => ReachService.pauseTask(id), '已暂停');
  }

  Future<void> cancelTask(String id) {
    return runAction(() => ReachService.cancelTask(id), '已取消');
  }

  Future<void> resumeTask(String id) {
    return runAction(() => ReachService.resumeTask(id), '已恢复');
  }
}
