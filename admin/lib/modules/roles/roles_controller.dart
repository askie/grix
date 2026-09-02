import 'package:get/get.dart';

import 'role_item.dart';
import 'role_service.dart';

class RolesController extends GetxController {
  final isLoading = true.obs;
  final roles = <RoleItem>[].obs;
  final error = ''.obs;

  @override
  void onInit() {
    super.onInit();
    load();
  }

  Future<void> load() async {
    isLoading.value = true;
    error.value = '';
    try {
      roles.value = await RoleService.list();
    } catch (e) {
      error.value = e.toString();
    } finally {
      isLoading.value = false;
    }
  }

  Future<bool> create(
    String name,
    String description,
    List<String> perms,
  ) async {
    try {
      await RoleService.create(
        name: name,
        description: description,
        permissions: perms,
      );
      await load();
      return true;
    } catch (e) {
      Get.snackbar('创建失败', e.toString());
      return false;
    }
  }

  Future<bool> updateRole(
    String id,
    String name,
    String description,
    List<String> perms,
  ) async {
    try {
      await RoleService.update(
        id,
        name: name,
        description: description,
        permissions: perms,
      );
      await load();
      return true;
    } catch (e) {
      Get.snackbar('更新失败', e.toString());
      return false;
    }
  }

  Future<bool> remove(String id) async {
    try {
      await RoleService.remove(id);
      await load();
      return true;
    } catch (e) {
      Get.snackbar('删除失败', e.toString());
      return false;
    }
  }
}
