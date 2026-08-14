import 'package:web/web.dart' as web;

Future<String?> readBrowserLocalStorage(String key) async {
  return web.window.localStorage.getItem(key);
}

Future<String?> readBrowserSessionStorage(String key) async {
  return web.window.sessionStorage.getItem(key);
}

Future<void> clearBrowserLocalStorage(Iterable<String> keys) async {
  for (final key in keys) {
    web.window.localStorage.removeItem(key);
  }
}

Future<void> clearBrowserSessionStorage(Iterable<String> keys) async {
  for (final key in keys) {
    web.window.sessionStorage.removeItem(key);
  }
}
