import 'package:shared_preferences/shared_preferences.dart';

/// Persists the visitor key scoped by site key.
abstract class GrixVisitorStore {
  Future<String?> read(String siteKey);
  Future<void> write(String siteKey, String visitorKey);
}

/// Default persistence backed by `shared_preferences`.
class SharedPreferencesGrixVisitorStore implements GrixVisitorStore {
  SharedPreferencesGrixVisitorStore({String prefsKeyPrefix = defaultPrefix})
    : _prefix = prefsKeyPrefix;

  static const String defaultPrefix = 'im.grix.widget.visitor_key.';
  final String _prefix;

  String _keyFor(String siteKey) => '$_prefix${siteKey.trim()}';

  @override
  Future<String?> read(String siteKey) async {
    final key = siteKey.trim();
    if (key.isEmpty) return null;
    final prefs = await SharedPreferences.getInstance();
    final value = prefs.getString(_keyFor(key))?.trim() ?? '';
    return value.isEmpty ? null : value;
  }

  @override
  Future<void> write(String siteKey, String visitorKey) async {
    final key = siteKey.trim();
    final value = visitorKey.trim();
    if (key.isEmpty || value.isEmpty) return;
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_keyFor(key), value);
  }
}

/// In-memory store for tests and hosts that manage identity themselves.
class InMemoryGrixVisitorStore implements GrixVisitorStore {
  final Map<String, String> _values = <String, String>{};

  @override
  Future<String?> read(String siteKey) async {
    final value = _values[siteKey.trim()]?.trim() ?? '';
    return value.isEmpty ? null : value;
  }

  @override
  Future<void> write(String siteKey, String visitorKey) async {
    final key = siteKey.trim();
    final value = visitorKey.trim();
    if (key.isNotEmpty && value.isNotEmpty) _values[key] = value;
  }
}
