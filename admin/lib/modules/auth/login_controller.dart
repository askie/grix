import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../app/routes/app_routes.dart';
import '../../core/config/admin_region.dart';
import '../../core/config/app_config.dart';
import '../../core/network/api_client.dart';
import '../../core/storage/test_env.dart';
import 'auth_service.dart';

/// 各区域独立存储账号密码，避免混用。
///
/// 账号非敏感，存 SharedPreferences；密码是高敏感凭证，存系统安全存储
/// （iOS Keychain / Android Keystore 加密），不再明文落盘。
/// `admin_saved_password_<region>` 曾明文写在 SharedPreferences，读写路径
/// 会将其一次性迁入安全存储并删除明文副本。
/// 测试环境（FLUTTER_TEST）没有 flutter_secure_storage 平台通道，
/// 密码回落 SharedPreferences mock，避免 widget 测试挂死。
String _usernameKey(AdminRegion r) =>
    'admin_saved_username_${r == AdminRegion.global ? 'global' : 'cn'}';
String _passwordKey(AdminRegion r) =>
    'admin_saved_password_${r == AdminRegion.global ? 'global' : 'cn'}';

bool get _isTest => isFlutterTestEnv;
const FlutterSecureStorage _secure = FlutterSecureStorage();

/// 读取保存的密码：安全存储优先，legacy 明文一次性迁移。
Future<String> _loadPassword(SharedPreferences prefs, AdminRegion r) async {
  final key = _passwordKey(r);
  if (_isTest) {
    return prefs.getString(key) ?? '';
  }
  try {
    final secured = await _secure.read(key: key);
    if (secured != null) return secured;
    final legacy = prefs.getString(key);
    if (legacy != null) {
      await _secure.write(key: key, value: legacy);
      await prefs.remove(key);
      return legacy;
    }
    return '';
  } catch (e) {
    debugPrint('Load saved admin password failed: $e');
    return '';
  }
}

/// 保存/清除密码：写安全存储并删除 legacy 明文副本。
Future<void> _savePassword(
  SharedPreferences prefs,
  AdminRegion r,
  String password,
) async {
  final key = _passwordKey(r);
  if (_isTest) {
    if (password.isEmpty) {
      await prefs.remove(key);
    } else {
      await prefs.setString(key, password);
    }
    return;
  }
  try {
    if (password.isEmpty) {
      await _secure.delete(key: key);
    } else {
      await _secure.write(key: key, value: password);
    }
    await prefs.remove(key);
  } catch (e) {
    debugPrint('Save admin password to secure storage failed: $e');
  }
}

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

  /// 从本地读取并填充指定区域保存的账号密码（密码来自系统安全存储）。
  Future<void> _restoreCredentials(AdminRegion region) async {
    final prefs = await SharedPreferences.getInstance();
    usernameCtrl.text = prefs.getString(_usernameKey(region)) ?? '';
    passwordCtrl.text = await _loadPassword(prefs, region);
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
        await _savePassword(prefs, region, password);
      } else {
        await prefs.remove(_usernameKey(region));
        await _savePassword(prefs, region, '');
      }
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
