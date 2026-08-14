import 'package:flutter/foundation.dart';
import 'package:get/get.dart';

import '../../../app/routes/app_routes.dart';
import '../../../data/providers/auth_service.dart';
import '../../../data/providers/saved_account_store.dart';
import '../../../shared/utils/toast_util.dart';

/// "切换账号"页：展示本机已登录账号列表，支持切换 / 移除 / 添加。
class AccountSwitchController extends GetxController {
  AccountSwitchController({AuthService? authService})
    : authService = authService ?? Get.find<AuthService>();

  final AuthService authService;

  final accounts = <SavedAccount>[].obs;
  final isLoaded = false.obs;

  /// 切换/移除进行中，页面显示遮罩并禁止其他操作。
  final isBusy = false.obs;

  String? get currentUserId => authService.userId;

  bool get isLoggedIn => authService.isLoggedIn;

  bool isCurrent(SavedAccount account) =>
      isLoggedIn && account.userId == currentUserId;

  @override
  void onInit() {
    super.onInit();
    reload();
  }

  Future<void> reload() async {
    accounts.value = await authService.listSavedAccounts();
    isLoaded.value = true;
  }

  Future<void> switchTo(SavedAccount account) async {
    if (isBusy.value || isCurrent(account)) return;
    if (account.needsRelogin) {
      CustomToast.show('account_switch_need_relogin'.tr);
      await _goToLoginForAdd();
      return;
    }
    isBusy.value = true;
    try {
      final outcome = await authService.switchToSavedAccount(account.userId);
      switch (outcome) {
        case AccountSwitchOutcome.success:
          Get.offAllNamed(AppRoutes.home);
          return;
        case AccountSwitchOutcome.needLogin:
          CustomToast.show('account_switch_need_relogin'.tr);
          await reload();
          await _goToLoginForAdd(suspend: false);
          return;
        case AccountSwitchOutcome.failed:
          CustomToast.show('account_switch_failed'.tr);
          await reload();
          return;
      }
    } catch (e) {
      debugPrint('Switch account error: $e');
      CustomToast.show('account_switch_failed'.tr);
      await reload();
    } finally {
      isBusy.value = false;
    }
  }

  Future<void> removeAccount(SavedAccount account) async {
    if (isBusy.value) return;
    final wasCurrent = isCurrent(account);
    isBusy.value = true;
    try {
      await authService.removeSavedAccount(account.userId);
      if (wasCurrent) {
        Get.offAllNamed(AppRoutes.login);
        return;
      }
      await reload();
    } catch (e) {
      debugPrint('Remove account error: $e');
      await reload();
    } finally {
      isBusy.value = false;
    }
  }

  Future<void> addAccount() async {
    if (isBusy.value) return;
    await _goToLoginForAdd();
  }

  /// 未登录状态下（添加账号后取消返回）按返回键：栈内旧页面的数据服务
  /// 已被重置，不能回退，直接落到登录页。
  void handleBackWhenLoggedOut() {
    Get.offAllNamed(AppRoutes.login);
  }

  /// 进登录页添加/重登账号。当前有登录会话时先本地挂起（凭证已在列表里，
  /// 随时可切回），让登录页保持在"未登录"状态下运行。
  Future<void> _goToLoginForAdd({bool suspend = true}) async {
    if (isBusy.value && suspend) return;
    isBusy.value = true;
    try {
      if (suspend && authService.isLoggedIn) {
        await authService.suspendCurrentSessionLocally();
      }
      await reload();
    } finally {
      isBusy.value = false;
    }
    await Get.toNamed(AppRoutes.login);
    // 从登录页返回（取消登录）：刷新列表状态，页面继续可用。
    await reload();
  }
}
