import 'package:flutter/foundation.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'test_env.dart';

/// 管理员登录 token 的本地持久化。
///
/// token 即后端登录返回的 sessionID，作为 Bearer Token 使用。
/// 安全要求：token 落系统安全存储（iOS Keychain / Android Keystore 加密），
/// 不再明文写 SharedPreferences。历史版本明文存储的 `grix_admin_token`
/// 在 load 时一次性迁移并删除明文副本。
///
/// 测试环境（FLUTTER_TEST）没有 flutter_secure_storage 平台通道，
/// 回落 SharedPreferences mock，避免 widget 测试挂死。
class TokenStore {
  TokenStore._();

  static const String _tokenKey = 'grix_admin_token';

  static bool get _isTest => isFlutterTestEnv;
  static const FlutterSecureStorage _secure = FlutterSecureStorage();

  static String? _cached;

  /// 内存中的 token（同步读取，供拦截器快速注入）。
  static String? get current => _cached;

  /// 启动时从本地加载 token 到内存。
  static Future<void> load() async {
    if (_isTest) {
      final prefs = await SharedPreferences.getInstance();
      _cached = prefs.getString(_tokenKey);
      return;
    }
    try {
      _cached = await _secure.read(key: _tokenKey);
      // 旧版本明文存在 SharedPreferences：迁入安全存储并删除明文副本。
      final prefs = await SharedPreferences.getInstance();
      final legacy = prefs.getString(_tokenKey);
      if (legacy != null) {
        if ((_cached ?? '').isEmpty) {
          _cached = legacy;
          await _secure.write(key: _tokenKey, value: legacy);
        }
        await prefs.remove(_tokenKey);
      }
    } catch (e) {
      debugPrint('TokenStore load failed: $e');
      _cached = null;
    }
  }

  /// 保存 token。
  static Future<void> save(String token) async {
    _cached = token;
    if (_isTest) {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString(_tokenKey, token);
      return;
    }
    try {
      await _secure.write(key: _tokenKey, value: token);
      // 兜底清理历史明文副本。
      final prefs = await SharedPreferences.getInstance();
      await prefs.remove(_tokenKey);
    } catch (e) {
      debugPrint('TokenStore save failed: $e');
    }
  }

  /// 清除 token（登出或失效时）。
  static Future<void> clear() async {
    _cached = null;
    if (_isTest) {
      final prefs = await SharedPreferences.getInstance();
      await prefs.remove(_tokenKey);
      return;
    }
    try {
      await _secure.delete(key: _tokenKey);
      final prefs = await SharedPreferences.getInstance();
      await prefs.remove(_tokenKey);
    } catch (e) {
      debugPrint('TokenStore clear failed: $e');
    }
  }

  static bool get hasToken => (_cached ?? '').isNotEmpty;
}
