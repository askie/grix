import 'package:flutter/widgets.dart';
import 'package:get/get.dart';

import '../../core/network/page_result.dart';
import '../../shared/controllers/paged_list_controller.dart';
import '../../shared/widgets/confirm_dialog.dart';
import 'egg_service.dart';

class EggsController extends PagedListController<EggListItem> {
  final RxList<EggCategory> categories = <EggCategory>[].obs;
  final RxBool categoriesLoading = false.obs;
  final RxnString categoriesError = RxnString();
  final RxString statusFilter = ''.obs;
  final RxString categoryFilter = ''.obs;
  final RxString keyword = ''.obs;
  final TextEditingController searchCtrl = TextEditingController();

  @override
  void onInit() {
    super.onInit();
    loadCategories();
  }

  @override
  Future<PageResult<EggListItem>> fetchPage() async {
    final r = await EggService.listEggs(
      status: statusFilter.value,
      categoryId: categoryFilter.value,
      keyword: keyword.value,
      page: page.value,
      pageSize: pageSize.value,
    );
    return PageResult(
      items: r.list,
      total: r.total,
      page: page.value,
      pageSize: pageSize.value,
    );
  }

  Future<void> loadCategories() async {
    categoriesLoading.value = true;
    categoriesError.value = null;
    try {
      categories.assignAll(await EggService.listCategories());
    } catch (e) {
      categoriesError.value = e.toString();
    } finally {
      categoriesLoading.value = false;
    }
  }

  void applySearch(String v) {
    keyword.value = v.trim();
    reloadFromFirstPage();
  }

  void changeStatus(String v) {
    if (statusFilter.value == v) return;
    statusFilter.value = v;
    reloadFromFirstPage();
  }

  void changeCategory(String v) {
    if (categoryFilter.value == v) return;
    categoryFilter.value = v;
    reloadFromFirstPage();
  }

  int get activeFilterCount {
    int count = 0;
    if (statusFilter.value.isNotEmpty) count++;
    if (categoryFilter.value.isNotEmpty) count++;
    return count;
  }

  void resetFilters() {
    statusFilter.value = '';
    categoryFilter.value = '';
    searchCtrl.clear();
    keyword.value = '';
    reloadFromFirstPage();
  }

  Future<void> updateStatus(String eggId, String status) async {
    try {
      await EggService.updateEggStatus(eggId, status);
      Toast.success('状态已更新');
      await reloadFromFirstPage();
    } catch (e) {
      Toast.error(e.toString());
    }
  }

  Future<void> togglePinned(String eggId, bool pinned) async {
    try {
      await EggService.setPinned(eggId, pinned);
      Toast.success(pinned ? '已置顶' : '已取消置顶');
      await reloadFromFirstPage();
    } catch (e) {
      Toast.error(e.toString());
    }
  }

  @override
  void onClose() {
    searchCtrl.dispose();
    super.onClose();
  }
}

class EggsBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<EggsController>(() => EggsController());
  }
}
