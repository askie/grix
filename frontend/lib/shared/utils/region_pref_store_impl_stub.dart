import 'package:shared_preferences/shared_preferences.dart';

import '../../app/profile/desktop_runtime.dart';
import '../../app/profile/profile_local_store.dart';

Future<ProfileLocalStore?> _desktopStore() async {
  // 测试环境（FLUTTER_TEST）必须回落 SharedPreferences：
  // widget 测试的 FakeAsync 区里 path_provider 平台通道永不返回会挂死。
  if (!isDesktopRuntime) return null;
  return ProfileLocalStore.instance();
}

Future<String?> regionPrefGetString(String key) async {
  final store = await _desktopStore();
  if (store != null) return store.getString(key);
  final prefs = await SharedPreferences.getInstance();
  return prefs.getString(key);
}

Future<void> regionPrefSetString(String key, String value) async {
  final store = await _desktopStore();
  if (store != null) {
    await store.set(key, value);
    return;
  }
  final prefs = await SharedPreferences.getInstance();
  await prefs.setString(key, value);
}

Future<void> regionPrefRemove(String key) async {
  final store = await _desktopStore();
  if (store != null) {
    await store.remove(key);
    return;
  }
  final prefs = await SharedPreferences.getInstance();
  await prefs.remove(key);
}
