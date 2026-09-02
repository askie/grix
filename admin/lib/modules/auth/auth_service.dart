import 'package:get/get.dart';

import '../../core/network/api_client.dart';
import '../../core/storage/token_store.dart';
import 'admin_profile.dart';

/// 全局认证服务：负责登录、登出、加载当前管理员，并持有会话状态。
class AuthService extends GetxService {
  static AuthService get to => Get.find<AuthService>();

  final Rxn<AdminProfile> profile = Rxn<AdminProfile>();

  bool get isLoggedIn => TokenStore.hasToken;

  /// 账号密码登录，成功后持久化 token 并加载管理员信息。
  Future<void> login(String username, String password) async {
    final data = await ApiClient.instance.post(
      '/login',
      data: {'username': username, 'password': password},
    );
    final map = (data as Map).cast<String, dynamic>();
    final token = (map['token'] ?? '').toString();
    if (token.isEmpty) {
      throw Exception('登录返回为空');
    }
    await TokenStore.save(token);
    profile.value = _parseProfile(map);
  }

  /// 拉取当前管理员信息（用于启动时校验 token 有效性）。
  Future<void> fetchProfile() async {
    final data = await ApiClient.instance.get('/me');
    final map = (data as Map).cast<String, dynamic>();
    profile.value = _parseProfile(map);
  }

  /// 修改当前管理员密码。成功后后端会撤销所有会话，需重新登录。
  Future<void> changePassword(
    String currentPassword,
    String newPassword,
  ) async {
    await ApiClient.instance.post(
      '/settings/password',
      data: {'current_password': currentPassword, 'new_password': newPassword},
    );
    // 改密成功后清除本地状态，跳转登录页
    await TokenStore.clear();
    profile.value = null;
  }

  /// 登出：通知后端撤销会话并清除本地 token。
  Future<void> logout() async {
    try {
      await ApiClient.instance.post('/logout');
    } catch (_) {}
    await TokenStore.clear();
    profile.value = null;
  }

  AdminProfile? _parseProfile(Map<String, dynamic> map) {
    final adminJson = (map['admin'] as Map?)?.cast<String, dynamic>();
    if (adminJson == null) return null;
    final perms =
        (map['permissions'] as List?)?.map((e) => e.toString()).toList() ??
        const [];
    return AdminProfile.fromJson(adminJson, permissions: perms);
  }
}
