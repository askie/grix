import 'package:flutter/foundation.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../../shared/utils/app_region_config.dart';
import '../../../shared/utils/test_env.dart';

class LoginCredentialState {
  final String account;
  final String password;

  const LoginCredentialState({
    this.account = '',
    this.password = '',
  });
}

/// 登录页"记住账号和密码"的本地存储。
///
/// 账号非敏感，存 SharedPreferences；密码是高敏感凭证，存系统安全存储
/// （iOS Keychain / Android Keystore 加密），不再明文落盘。
/// 历史版本曾把密码明文写在 SharedPreferences 同名 key，读写路径会将其
/// 一次性迁入安全存储并删除明文副本。
/// 测试环境（FLUTTER_TEST）没有 flutter_secure_storage 平台通道，
/// 密码回落 SharedPreferences mock，避免 widget 测试挂死。
class LoginCredentialStorage {
  static bool get _isTest => isFlutterTestEnv;
  static const FlutterSecureStorage _secure = FlutterSecureStorage();

  static String _accountKey(AppRegion region) =>
      'login_saved_account_${region.name}';
  static String _passwordKey(AppRegion region) =>
      'login_saved_password_${region.name}';

  Future<LoginCredentialState> load(AppRegion region) async {
    try {
      final prefs = await SharedPreferences.getInstance();
      return LoginCredentialState(
        account: prefs.getString(_accountKey(region))?.trim() ?? '',
        password: await _loadPassword(prefs, region),
      );
    } catch (error) {
      debugPrint('Load login credentials failed: $error');
      return const LoginCredentialState();
    }
  }

  Future<String> _loadPassword(
    SharedPreferences prefs,
    AppRegion region,
  ) async {
    final key = _passwordKey(region);
    if (_isTest) {
      return prefs.getString(key) ?? '';
    }
    try {
      final secured = await _secure.read(key: key);
      if (secured != null) return secured;
      // 历史明文一次性迁入安全存储并删除明文副本。
      final legacy = prefs.getString(key);
      if (legacy != null) {
        await _secure.write(key: key, value: legacy);
        await prefs.remove(key);
        return legacy;
      }
      return '';
    } catch (error) {
      debugPrint('Load saved password failed: $error');
      return '';
    }
  }

  Future<void> save(LoginCredentialState state, AppRegion region) async {
    try {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString(_accountKey(region), state.account.trim());
      final key = _passwordKey(region);
      if (_isTest) {
        await prefs.setString(key, state.password);
        return;
      }
      try {
        if (state.password.isEmpty) {
          await _secure.delete(key: key);
        } else {
          await _secure.write(key: key, value: state.password);
        }
        // 无论成败都顺带清理历史明文副本，避免双写残留。
        await prefs.remove(key);
      } catch (error) {
        debugPrint('Save password to secure storage failed: $error');
      }
    } catch (error) {
      debugPrint('Save login credentials failed: $error');
    }
  }
}
