import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

class PushFilterService {
  PushFilterService._();

  static const _channel = MethodChannel(
    'pub.dhf.grix/push_filter',
  );
  static bool _channelAvailable = true;

  /// Notify native iOS of the session the user is currently viewing so that
  /// foreground APNs notifications for that session are suppressed.
  /// Pass [null] or an empty string when leaving the session.
  static Future<void> setActiveSessionID(String? sessionId) async {
    if (!_supportsPushFilter()) return;
    if (!_channelAvailable) return;
    try {
      await _channel.invokeMethod<void>('setActiveSessionID', {
        'sessionId': sessionId ?? '',
      });
    } on MissingPluginException {
      _channelAvailable = false;
    } catch (e) {
      debugPrint('PushFilterService.setActiveSessionID failed: $e');
    }
  }

  static bool _supportsPushFilter() {
    if (kIsWeb) return false;
    return defaultTargetPlatform == TargetPlatform.iOS;
  }

  @visibleForTesting
  static void resetForTest() {
    _channelAvailable = true;
  }
}
