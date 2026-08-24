import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

const _channel = MethodChannel('pub.dhf.grix/sentry_event_dedup');

/// Installs the Android native beforeSend wrapper immediately after the native
/// Sentry SDK has initialized. iOS installs its processor in AppDelegate before
/// native cached crash reports are processed.
Future<void> installNativeSentryEventDedup() async {
  if (kIsWeb || defaultTargetPlatform != TargetPlatform.android) return;
  try {
    await _channel.invokeMethod<void>('installNative');
  } on MissingPluginException {
    // Unit tests and unsupported embedders have no native channel.
  } on PlatformException {
    // Observability setup must not prevent the app from starting.
  }
}
