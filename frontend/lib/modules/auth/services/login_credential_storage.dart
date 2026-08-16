import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../../shared/utils/app_region_config.dart';

class LoginCredentialState {
  final String account;

  const LoginCredentialState({this.account = ''});
}

/// 登录页"记住账号"的本地存储。
///
/// 安全要求：绝不持久化密码，只保存账号用于登录页回填。
/// 历史版本曾把密码明文写入 `login_saved_password_<region>`，
/// 读写路径都会顺带清理该 legacy key。
class LoginCredentialStorage {
  static String _accountKey(AppRegion region) =>
      'login_saved_account_${region.name}';
  static String _legacyPasswordKey(AppRegion region) =>
      'login_saved_password_${region.name}';

  Future<LoginCredentialState> load(AppRegion region) async {
    try {
      final prefs = await SharedPreferences.getInstance();
      await prefs.remove(_legacyPasswordKey(region));
      return LoginCredentialState(
        account: prefs.getString(_accountKey(region))?.trim() ?? '',
      );
    } catch (error) {
      debugPrint('Load login credentials failed: $error');
      return const LoginCredentialState();
    }
  }

  Future<void> save(LoginCredentialState state, AppRegion region) async {
    try {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString(_accountKey(region), state.account.trim());
      await prefs.remove(_legacyPasswordKey(region));
    } catch (error) {
      debugPrint('Save login credentials failed: $error');
    }
  }
}
