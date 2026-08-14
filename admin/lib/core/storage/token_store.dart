import 'package:shared_preferences/shared_preferences.dart';

/// 管理员登录 token 的本地持久化。
///
/// token 即后端登录返回的 sessionID，作为 Bearer Token 使用。
class TokenStore {
  TokenStore._();

  static const String _tokenKey = 'grix_admin_token';

  static String? _cached;

  /// 内存中的 token（同步读取，供拦截器快速注入）。
  static String? get current => _cached;

  /// 启动时从本地加载 token 到内存。
  static Future<void> load() async {
    final prefs = await SharedPreferences.getInstance();
    _cached = prefs.getString(_tokenKey);
  }

  /// 保存 token。
  static Future<void> save(String token) async {
    _cached = token;
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_tokenKey, token);
  }

  /// 清除 token（登出或失效时）。
  static Future<void> clear() async {
    _cached = null;
    final prefs = await SharedPreferences.getInstance();
    await prefs.remove(_tokenKey);
  }

  static bool get hasToken => (_cached ?? '').isNotEmpty;
}
