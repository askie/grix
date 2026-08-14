import 'auth_session_browser_storage_stub.dart'
    if (dart.library.js_interop) 'auth_session_browser_storage_web.dart'
    as impl;

Future<String?> readBrowserLocalStorage(String key) {
  return impl.readBrowserLocalStorage(key);
}

Future<String?> readBrowserSessionStorage(String key) {
  return impl.readBrowserSessionStorage(key);
}

Future<void> clearBrowserLocalStorage(Iterable<String> keys) {
  return impl.clearBrowserLocalStorage(keys);
}

Future<void> clearBrowserSessionStorage(Iterable<String> keys) {
  return impl.clearBrowserSessionStorage(keys);
}
