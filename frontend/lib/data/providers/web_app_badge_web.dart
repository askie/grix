import 'dart:js_interop';

import 'package:web/web.dart' as web;

Future<bool> syncWebAppBadge(int unreadCount) async {
  final normalized = unreadCount < 0 ? 0 : unreadCount;
  try {
    if (normalized > 0) {
      await web.window.navigator.setAppBadge(normalized).toDart;
    } else {
      await web.window.navigator.clearAppBadge().toDart;
    }
    return true;
  } catch (_) {
    return false;
  }
}
