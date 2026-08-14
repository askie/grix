import 'package:shared_preferences/shared_preferences.dart';

Future<String?> regionPrefGetString(String key) async {
  final prefs = await SharedPreferences.getInstance();
  return prefs.getString(key);
}

Future<void> regionPrefSetString(String key, String value) async {
  final prefs = await SharedPreferences.getInstance();
  await prefs.setString(key, value);
}

Future<void> regionPrefRemove(String key) async {
  final prefs = await SharedPreferences.getInstance();
  await prefs.remove(key);
}
