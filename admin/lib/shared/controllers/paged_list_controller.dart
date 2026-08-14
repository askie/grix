import 'package:get/get.dart';

import '../../core/network/page_result.dart';
import '../widgets/confirm_dialog.dart';

/// 列表+无限滚动页面的控制器基类。
///
/// 收敛所有「列表 / 加载态 / 错误态 / 无限滚动 / 行操作刷新」的通用逻辑，
/// 各模块控制器继承后只需实现 [fetchPage]，并维护自己的筛选条件。
abstract class PagedListController<T> extends GetxController {
  final RxList<T> items = <T>[].obs;
  final RxBool loading = false.obs;
  final RxnString error = RxnString();

  final RxInt page = 1.obs;
  final RxInt pageSize = 20.obs;
  final RxInt total = 0.obs;

  /// 是否还有更多数据可加载。
  final RxBool hasMore = true.obs;

  /// 正在加载更多（非首次加载）。
  final RxBool loadingMore = false.obs;

  bool get isEmpty => items.isEmpty;

  /// 子类实现：按当前筛选条件与 [page]/[pageSize] 请求一页数据。
  Future<PageResult<T>> fetchPage();

  @override
  void onInit() {
    super.onInit();
    reload();
  }

  /// 重新加载第一页（用于首次加载、切换筛选/搜索后）。
  Future<void> reload() async {
    loading.value = true;
    error.value = null;
    try {
      final result = await fetchPage();
      items.assignAll(result.items);
      total.value = result.total;
      page.value = result.page;
      pageSize.value = result.pageSize;
      hasMore.value = result.items.length >= result.pageSize;
    } catch (e) {
      error.value = e.toString();
    } finally {
      loading.value = false;
    }
  }

  /// 重置到第一页并加载（用于切换筛选/搜索后）。
  Future<void> reloadFromFirstPage() {
    page.value = 1;
    return reload();
  }

  /// 滚动到底部时追加加载下一页。
  Future<void> loadMore() async {
    if (!hasMore.value || loading.value || loadingMore.value) return;
    loadingMore.value = true;
    try {
      page.value++;
      final result = await fetchPage();
      items.addAll(result.items);
      total.value = result.total;
      pageSize.value = result.pageSize;
      hasMore.value = result.items.length >= result.pageSize;
    } catch (e) {
      // 加载更多失败时回退 page，不覆盖 error（不影响当前列表）
      page.value--;
    } finally {
      loadingMore.value = false;
    }
  }

  /// 执行一个行操作：成功后提示并刷新，失败弹错误提示。
  Future<void> runAction(
    Future<void> Function() action,
    String successMessage,
  ) async {
    try {
      await action();
      Toast.success(successMessage);
      await reloadFromFirstPage();
    } catch (e) {
      Toast.error(e.toString());
    }
  }
}
