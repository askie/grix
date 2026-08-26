import 'package:get/get.dart';

import '../../core/network/page_result.dart';
import '../../shared/controllers/paged_list_controller.dart';
import '../../shared/widgets/confirm_dialog.dart';
import 'inactive_users_service.dart';

/// 「沉默用户触达」页控制器：按天数/区域筛出近 N 天没有 agent 连接过的用户，
/// 勾选后逐人走 /reach/direct 发模板邮件。本期只走邮件，不做短信。
class InactiveUsersController extends PagedListController<InactiveAgentUser> {
  /// 默认 14 天与后端 no_agent_days 的默认值保持一致。
  static const int defaultNoAgentDays = 14;

  final RxInt noAgentDays = defaultNoAgentDays.obs;
  final RxString region = ''.obs;
  final RxSet<String> selected = <String>{}.obs;
  final RxBool sending = false.obs;

  /// 后端当前生效的阿里云模板 ID，用于预填「按次覆盖」输入框。
  final RxInt defaultTemplateId = 0.obs;

  /// 刷新会重建 items，勾选里残留的用户 ID 就找不到对应行了。
  /// 清空勾选，避免「刷新后再发送」静默漏掉那部分人。
  @override
  Future<void> reload() {
    selected.clear();
    return super.reload();
  }

  @override
  Future<PageResult<InactiveAgentUser>> fetchPage() async {
    final r = await InactiveUsersService.listInactiveUsers(
      noAgentDays: noAgentDays.value,
      region: region.value.isEmpty ? null : region.value,
      page: page.value,
      pageSize: pageSize.value,
    );
    defaultTemplateId.value = r.defaultTemplateId;
    return PageResult(items: r.users, total: r.total, page: page.value, pageSize: pageSize.value);
  }

  void applyDays(int days) {
    if (days <= 0 || noAgentDays.value == days) return;
    noAgentDays.value = days;
    selected.clear();
    reloadFromFirstPage();
  }

  void changeRegion(String value) {
    if (region.value == value) return;
    region.value = value;
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

  /// 全选只覆盖有邮箱的用户：没绑邮箱的发不出去，勾上只会得到一行失败。
  void selectAllLoaded(bool value) {
    if (!value) {
      selected.clear();
      return;
    }
    selected.addAll(items.where((e) => e.hasEmail).map((e) => e.userId));
  }

  int get selectableCount => items.where((e) => e.hasEmail).length;

  Future<ReachEmailPreview?> preview(String title, String body, int templateId) async {
    try {
      return await InactiveUsersService.previewEmail(
        title: title,
        body: body,
        emailTemplateId: templateId,
        sampleUserId: selected.isEmpty ? null : selected.first,
      );
    } catch (e) {
      Toast.error(e.toString());
      return null;
    }
  }

  /// 逐人发送并返回结果；调用方负责展示。发送中重复触发返回 null。
  Future<List<InactiveReachResult>?> send({
    required String title,
    required String body,
    required int templateId,
  }) async {
    if (selected.isEmpty || sending.value) return null;
    sending.value = true;
    try {
      final day = _todayStamp();
      final byId = {for (final u in items) u.userId: u};
      final results = <InactiveReachResult>[];
      for (final userId in selected.toList()) {
        final user = byId[userId];
        if (user == null) {
          // 兜底：列表在勾选后被改动过。宁可报一行失败，也不静默漏发。
          results.add(InactiveReachResult(
              userId: userId, channel: 'email', status: 'failed', error: '用户已不在当前列表，请刷新后重试'));
          continue;
        }
        try {
          results.add(await InactiveUsersService.sendOne(
            user: user,
            title: title,
            body: body,
            dedupKey: 'inactive_agent:$day:$userId:email',
            emailTemplateId: templateId,
          ));
        } catch (e) {
          // 单人失败不打断整批：记一行失败，继续发下一个。
          results.add(InactiveReachResult(userId: userId, channel: 'email', status: 'failed', error: e.toString()));
        }
      }
      selected.clear();
      return results;
    } finally {
      sending.value = false;
    }
  }

  /// 幂等键里的日期戳（yyyymmdd）：同一天对同一用户只发一次。
  static String _todayStamp() {
    final now = DateTime.now();
    return '${now.year.toString().padLeft(4, '0')}'
        '${now.month.toString().padLeft(2, '0')}'
        '${now.day.toString().padLeft(2, '0')}';
  }
}
