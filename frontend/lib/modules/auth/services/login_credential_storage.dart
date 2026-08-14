import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../../shared/utils/app_region_config.dart';

class LoginCredentialState {
  final String account;
  final String password;

  const LoginCredentialState({
    this.account = '',
    this.password = '',
  });
}

class LoginCredentialStorage {
  static String _accountKey(AppRegion region) =>
      'login_saved_account_${region.name}';
  static String _passwordKey(AppRegion region) =>
      'login_saved_password_${region.name}';

  Future<LoginCredentialState> load(AppRegion region) async {
    try {
      final prefs = await SharedPreferences.getInstance();
      return LoginCredentialState(
        account: prefs.getString(_accountKey(region))?.trim() ?? '',
        password: prefs.getString(_passwordKey(region)) ?? '',
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
      await prefs.setString(_passwordKey(region), state.password);
    } catch (error) {
      debugPrint('Save login credentials failed: $error');
    }
  }
}
