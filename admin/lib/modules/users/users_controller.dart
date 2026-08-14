import 'package:flutter/widgets.dart';
import 'package:get/get.dart';

import '../../core/network/page_result.dart';
import '../../shared/controllers/paged_list_controller.dart';
import '../../shared/widgets/confirm_dialog.dart';
import 'admin_user_item.dart';
import 'user_service.dart';

/// 用户管理页控制器：在通用列表基类之上，补充搜索、状态筛选与各项操作。
class UsersController extends PagedListController<AdminUserItem> {
  UsersController({this.onlineOnly = false});

  final bool onlineOnly;

  /// 状态筛选：'' 全部 / 'active' 正常 / 'banned' 封禁
  final RxString statusFilter = ''.obs;
  final RxString keyword = ''.obs;

  final TextEditingController searchCtrl = TextEditingController();

  @override
  Future<PageResult<AdminUserItem>> fetchPage() {
    return UserService.list(
      query: keyword.value,
      status: statusFilter.value,
      onlineOnly: onlineOnly,
      page: page.value,
      pageSize: pageSize.value,
    );
  }

  void applySearch(String value) {
    keyword.value = value.trim();
    reloadFromFirstPage();
  }

  void changeStatus(String status) {
    if (statusFilter.value == status) return;
    statusFilter.value = status;
    reloadFromFirstPage();
  }

  void resetFilters() {
    statusFilter.value = '';
    searchCtrl.clear();
    keyword.value = '';
    reloadFromFirstPage();
  }

  // --- 行操作 ---

  Future<void> ban(AdminUserItem user) async {
    final reason = await ConfirmDialog.showWithReason(
      title: '封禁用户 ${user.displayName}',
      hint: '请输入封禁原因（可选）',
      confirmText: '封禁',
    );
    if (reason == null) return;
    await runAction(() => UserService.ban(user.id, reason), '用户已封禁');
  }

  Future<void> unban(AdminUserItem user) async {
    final ok = await ConfirmDialog.show(
      title: '解封用户',
      message: '确定要解封 ${user.displayName} 吗？',
      confirmText: '解封',
    );
    if (!ok) return;
    await runAction(() => UserService.unban(user.id), '用户已恢复');
  }

  Future<void> unmuteModeration(AdminUserItem user) async {
    final ok = await ConfirmDialog.show(
      title: '解除审核禁言',
      message: '确定解除 ${user.displayName} 的内容审查禁言吗？',
      confirmText: '解除',
    );
    if (!ok) return;
    await runAction(() => UserService.unmuteModeration(user.id), '审查禁言已解除');
  }

  Future<void> unlockLogin(AdminUserItem user) async {
    final ok = await ConfirmDialog.show(
      title: '解除登录锁定',
      message: '确定解除 ${user.displayName} 的登录锁定吗？',
      confirmText: '解除',
    );
    if (!ok) return;
    await runAction(() => UserService.unlockLogin(user.id), '登录锁定已解除');
  }

  Future<void> unbindPhone(AdminUserItem user) async {
    final ok = await ConfirmDialog.show(
      title: '解绑手机号',
      message:
          '确定解绑 ${user.displayName} 的手机号 ${user.phoneE164} 吗？\n'
          '该号码将释放，用户可重新用同号注册或绑定到其他账户。',
      confirmText: '解绑',
      danger: true,
    );
    if (!ok) return;
    await runAction(() => UserService.unbindPhone(user.id), '手机号已解绑');
  }

  @override
  void onClose() {
    searchCtrl.dispose();
    super.onClose();
  }
}
