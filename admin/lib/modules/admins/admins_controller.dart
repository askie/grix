import 'package:get/get.dart';

import '../../shared/widgets/confirm_dialog.dart';
import 'admin_item.dart';
import 'admin_service.dart';

/// 管理员管理控制器（列表不分页）。
class AdminsController extends GetxController {
  final RxList<AdminItem> items = <AdminItem>[].obs;
  final RxBool loading = false.obs;
  final RxnString error = RxnString();
  final RxString currentAdminId = ''.obs;

  bool get isEmpty => items.isEmpty;

  @override
  void onInit() {
    super.onInit();
    load();
  }

  Future<void> load() async {
    loading.value = true;
    error.value = null;
    try {
      final result = await AdminService.list();
      items.assignAll(result.items);
      currentAdminId.value = result.currentAdminId;
    } catch (e) {
      error.value = e.toString();
    } finally {
      loading.value = false;
    }
  }

  bool isSelf(AdminItem a) => a.id == currentAdminId.value;

  Future<void> create(String username, String nickname, String password,
      {int role = 1, String? roleId}) async {
    try {
      await AdminService.create(username, nickname, password,
          role: role, roleId: roleId);
      Toast.success('管理员已创建');
      await load();
    } catch (e) {
      Toast.error(e.toString());
      rethrow;
    }
  }

  Future<void> enable(AdminItem a) =>
      _run(() => AdminService.enable(a.id), '管理员已启用');

  Future<void> disable(AdminItem a) async {
    final ok = await ConfirmDialog.show(
      title: '禁用管理员',
      message: '确定禁用 ${a.displayName} 吗？该账号将无法登录。',
      confirmText: '禁用',
      danger: true,
    );
    if (!ok) return;
    await _run(() => AdminService.disable(a.id), '管理员已禁用');
  }

  Future<void> remove(AdminItem a) async {
    final ok = await ConfirmDialog.show(
      title: '删除管理员',
      message: '确定删除 ${a.displayName} 吗？此操作不可恢复。',
      confirmText: '删除',
      danger: true,
    );
    if (!ok) return;
    await _run(() => AdminService.remove(a.id), '管理员已删除');
  }

  Future<void> _run(Future<void> Function() action, String msg) async {
    try {
      await action();
      Toast.success(msg);
      await load();
    } catch (e) {
      Toast.error(e.toString());
    }
  }
}
