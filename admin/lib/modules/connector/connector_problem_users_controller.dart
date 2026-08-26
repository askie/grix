import 'package:flutter/widgets.dart';
import 'package:get/get.dart';

import '../../core/network/page_result.dart';
import '../../shared/controllers/paged_list_controller.dart';
import '../../shared/widgets/confirm_dialog.dart';
import 'connector_service.dart';

/// 「问题用户」页控制器：按版本查出升级仍失败的用户，勾选后手动发升级失败告知。
///
/// 版本为空时不请求接口（后端要求必填 version），避免进页面就报错。
class ConnectorProblemUsersController extends PagedListController<ConnectorProblemUser> {
  final RxString version = ''.obs;
  final RxString typeFilter = ''.obs;
  final RxString statusFilter = ''.obs;
  final RxBool includeUnsupported = false.obs;
  final RxSet<String> selected = <String>{}.obs;
  final RxBool sending = false.obs;
  final TextEditingController versionCtrl = TextEditingController();

  @override
  Future<PageResult<ConnectorProblemUser>> fetchPage() async {
    final v = version.value.trim();
    if (v.isEmpty) {
      return PageResult(items: [], total: 0, page: 1, pageSize: pageSize.value);
    }
    final r = await ConnectorService.listProblemUsers(
      version: v,
      clientType: typeFilter.value.isEmpty ? null : typeFilter.value,
      statuses: statusFilter.value.isEmpty ? null : statusFilter.value,
      includeUnsupported: includeUnsupported.value,
      page: page.value,
      pageSize: pageSize.value,
    );
    return PageResult(items: r.users, total: r.total, page: page.value, pageSize: pageSize.value);
  }

  void applyVersion(String value) {
    version.value = value.trim();
    selected.clear();
    reloadFromFirstPage();
  }

  void changeType(String value) {
    if (typeFilter.value == value) return;
    typeFilter.value = value;
    selected.clear();
    reloadFromFirstPage();
  }

  void changeStatus(String value) {
    if (statusFilter.value == value) return;
    statusFilter.value = value;
    selected.clear();
    reloadFromFirstPage();
  }

  void toggleUnsupported(bool value) {
    if (includeUnsupported.value == value) return;
    includeUnsupported.value = value;
    selected.clear();
    reloadFromFirstPage();
  }

  void toggleSelected(String userId, bool value) {
    if (value) {
      selected.add(userId);
    } else {
      selected.remove(userId);
    }
  }

  void selectAllLoaded(bool value) {
    if (!value) {
      selected.clear();
      return;
    }
    selected.addAll(items.map((e) => e.userId));
  }

  int get activeFilterCount =>
      (statusFilter.value.isNotEmpty ? 1 : 0) + (includeUnsupported.value ? 1 : 0);

  void resetFilters() {
    statusFilter.value = '';
    includeUnsupported.value = false;
    selected.clear();
    reloadFromFirstPage();
  }

  Future<ConnectorNotifyPreview?> preview(String title, String body) async {
    try {
      return await ConnectorService.previewNotify(
        title: title,
        body: body,
        sampleUserId: selected.isEmpty ? null : selected.first,
      );
    } catch (e) {
      Toast.error(e.toString());
      return null;
    }
  }

  /// 发送并返回逐人结果；调用方负责展示。发送失败时返回 null。
  Future<List<ConnectorNotifyResult>?> notify({
    required String channel,
    required String title,
    required String body,
  }) async {
    if (selected.isEmpty || sending.value) return null;
    sending.value = true;
    try {
      final results = await ConnectorService.notifyProblemUsers(
        version: version.value.trim(),
        userIds: selected.toList(),
        channel: channel,
        title: title,
        body: body,
      );
      selected.clear();
      return results;
    } catch (e) {
      Toast.error(e.toString());
      return null;
    } finally {
      sending.value = false;
    }
  }

  @override
  void onClose() {
    versionCtrl.dispose();
    super.onClose();
  }
}
