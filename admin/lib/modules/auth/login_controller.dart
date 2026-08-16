import 'package:flutter/widgets.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../app/routes/app_routes.dart';
import '../../core/config/admin_region.dart';
import '../../core/config/app_config.dart';
import '../../core/network/api_client.dart';
import 'auth_service.dart';

/// 各区域独立存储账号，避免混用。
///
/// 安全要求：只持久化账号用于登录页回填，管理员密码绝不落盘。
/// `admin_saved_password_<region>` 是历史版本明文存密码的 legacy key，
/// 现在仅用于一次性清理。
String _usernameKey(AdminRegion r) =>
    'admin_saved_username_${r == AdminRegion.global ? 'global' : 'cn'}';
String _legacyPasswordKey(AdminRegion r) =>
    'admin_saved_password_${r == AdminRegion.global ? 'global' : 'cn'}';

/// 登录页控制器。
class LoginController extends GetxController {
  final TextEditingController usernameCtrl = TextEditingController();
  final TextEditingController passwordCtrl = TextEditingController();

  final RxBool loading = false.obs;
  final RxnString error = RxnString();
  final RxBool rememberCredentials = true.obs;

  /// 当前选定的区域，初始值从 AdminRegionStore 读取。
  late final Rx<AdminRegion> selectedRegion =
      AdminRegionStore.current.obs;

  @override
  void onInit() {
    super.onInit();
    _restoreCredentials(AdminRegionStore.current);
  }

  /// 切换区域：持久化选择、更新 ApiClient baseUrl、载入对应区域凭据。
  ///
  /// 未显式选择过区域时，即便点选的值和当前展示值相同（比如 Web 端按当前域名
  /// 推断展示的默认区域），也要落成一次真实的显式选择，否则用户点不动、也没法
  /// 再点回来。
  Future<void> changeRegion(AdminRegion region) async {
    if (AdminRegionStore.hasExplicitChoice && region == selectedRegion.value) {
      return;
    }
    selectedRegion.value = region;
    await AdminRegionStore.save(region);
    ApiClient.instance.updateBaseUrl(AppConfig.apiRoot);
    error.value = null;
    await _restoreCredentials(region);
  }

  /// 从本地读取并填充指定区域保存的账号（密码不做持久化，顺带清理历史明文）。
  Future<void> _restoreCredentials(AdminRegion region) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.remove(_legacyPasswordKey(region));
    usernameCtrl.text = prefs.getString(_usernameKey(region)) ?? '';
    passwordCtrl.text = '';
  }

  Future<void> submit() async {
    if (loading.value) return;
    final region = selectedRegion.value;
    final username = usernameCtrl.text.trim();
    final password = passwordCtrl.text;
    if (username.isEmpty || password.isEmpty) {
      error.value = '请输入账号和密码';
      return;
    }
    error.value = null;
    loading.value = true;
    try {
      await AuthService.to.login(username, password);
      final prefs = await SharedPreferences.getInstance();
      if (rememberCredentials.value) {
        await prefs.setString(_usernameKey(region), username);
      } else {
        await prefs.remove(_usernameKey(region));
      }
      await prefs.remove(_legacyPasswordKey(region));
      Get.offAllNamed(AppRoutes.home);
    } catch (e) {
      error.value = e.toString();
    } finally {
      loading.value = false;
    }
  }

  @override
  void onClose() {
    usernameCtrl.dispose();
    passwordCtrl.dispose();
    super.onClose();
  }
}
