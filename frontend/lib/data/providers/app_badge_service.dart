import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import 'web_app_badge_stub.dart'
    if (dart.library.js_interop) 'web_app_badge_web.dart' as web_app_badge;

class AppBadgeService {
  AppBadgeService._();

  static const MethodChannel _channel = MethodChannel(
    'pub.dhf.grix/app_badge',
  );
  static int _lastSyncedBadge = -1;
  static bool _channelAvailable = true;

  static Future<void> syncUnreadBadge(
    int unreadCount, {
    bool force = false,
  }) async {
    if (!_supportsBadgeSync()) {
      return;
    }

    final normalized = unreadCount < 0 ? 0 : unreadCount;
    if (_channelAvailable == false) {
      return;
    }
    if (!force && _lastSyncedBadge == normalized) {
      return;
    }

    if (kIsWeb) {
      final synced = await web_app_badge.syncWebAppBadge(normalized);
      if (synced) {
        _lastSyncedBadge = normalized;
      }
      return;
    }

    try {
      await _channel.invokeMethod<void>('setAppBadge', {
        'count': normalized,
      });
      _lastSyncedBadge = normalized;
    } on MissingPluginException {
      _channelAvailable = false;
    } on PlatformException catch (e) {
      debugPrint(
        'sync unread badge failed code=${e.code} message=${e.message}',
      );
    } catch (e) {
      debugPrint('sync unread badge failed: $e');
    }
  }

  static bool _supportsBadgeSync() {
    if (kIsWeb) return true;
    return defaultTargetPlatform == TargetPlatform.iOS;
  }

  @visibleForTesting
  static void resetForTest() {
    _lastSyncedBadge = -1;
    _channelAvailable = true;
  }
}
