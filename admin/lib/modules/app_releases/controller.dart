import 'package:get/get.dart';

import '../../core/network/page_result.dart';
import '../../shared/controllers/paged_list_controller.dart';
import '../../shared/widgets/confirm_dialog.dart';
import 'models.dart';
import 'service.dart';

class AppReleasesController extends PagedListController<AppRelease> {
  final RxString platform = ''.obs;
  final RxString channel = ''.obs;

  @override
  Future<PageResult<AppRelease>> fetchPage() async {
    final r = await AppReleaseService.list(
      platform: platform.value,
      channel: channel.value,
      page: page.value,
      pageSize: pageSize.value,
    );
    return PageResult(
      items: r.releases,
      total: r.total,
      page: page.value,
      pageSize: pageSize.value,
    );
  }

  void changePlatform(String v) {
    if (platform.value == v) return;
    platform.value = v;
    reloadFromFirstPage();
  }

  void changeChannel(String v) {
    if (channel.value == v) return;
    channel.value = v;
    reloadFromFirstPage();
  }

  int get activeFilterCount {
    int count = 0;
    if (platform.value.isNotEmpty) count++;
    if (channel.value.isNotEmpty) count++;
    return count;
  }

  void resetFilters() {
    platform.value = '';
    channel.value = '';
    reloadFromFirstPage();
  }

  Future<void> create(Map<String, dynamic> body) async {
    await AppReleaseService.create(body);
    Toast.success('版本已创建');
    await reloadFromFirstPage();
  }

  Future<void> publish(AppRelease r) => _act(() => AppReleaseService.publish(r.id), '已发布');
  Future<void> pause(AppRelease r) => _act(() => AppReleaseService.pause(r.id), '已暂停');
  Future<void> resume(AppRelease r) => _act(() => AppReleaseService.resume(r.id), '已恢复');
  Future<void> revoke(AppRelease r) => _act(() => AppReleaseService.revoke(r.id), '已撤回');

  Future<void> delete(AppRelease r) async {
    final ok = await ConfirmDialog.show(title: '删除版本', message: '确定删除 ${r.version} (${r.platform}) 吗？', confirmText: '删除', danger: true);
    if (!ok) return;
    await _act(() => AppReleaseService.delete(r.id), '已删除');
  }

  Future<void> _act(Future<void> Function() fn, String msg) async {
    try { await fn(); Toast.success(msg); await reloadFromFirstPage(); }
    catch (e) { Toast.error(e.toString()); }
  }
}
