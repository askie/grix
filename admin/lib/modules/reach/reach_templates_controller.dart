import '../../core/network/page_result.dart';
import '../../shared/controllers/paged_list_controller.dart';
import 'reach_models.dart';
import 'reach_service.dart';

class ReachTemplatesController extends PagedListController<ReachTemplate> {
  @override
  Future<PageResult<ReachTemplate>> fetchPage() {
    return ReachService.listTemplates(
      page: page.value,
      pageSize: pageSize.value,
    );
  }

  Future<void> deleteTemplate(String id) {
    return runAction(() => ReachService.deleteTemplate(id), '已删除');
  }
}
