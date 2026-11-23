import 'package:flutter/foundation.dart';

bool shouldReconnectRealtimeOnForeground({
  required Duration backgroundDuration,
  bool isWeb = kIsWeb,
  TargetPlatform? targetPlatform,
}) {
  if (isWeb) {
    return false;
  }
  switch (targetPlatform ?? defaultTargetPlatform) {
    case TargetPlatform.android:
      // Android may freeze the background suspend timer and leave a writable
      // but dead socket. A new connection is more reliable than probing it.
      return backgroundDuration >= const Duration(seconds: 5);
    case TargetPlatform.iOS:
      // iOS suspends the process seconds after backgrounding, so a socket that
      // is still "connected" after this long outlived the suspend timer only
      // because Dart was frozen: no heartbeat ran, and the server or a carrier
      // NAT may already have dropped it. Reusing it means waiting for a close
      // frame plus reconnect backoff before unread state can sync. Short
      // app switches keep the live socket and resume on it.
      return backgroundDuration >= const Duration(seconds: 30);
    case TargetPlatform.fuchsia:
    case TargetPlatform.linux:
    case TargetPlatform.macOS:
    case TargetPlatform.windows:
      return false;
  }
}

Duration realtimeBackgroundSuspendDelay({
  bool isWeb = kIsWeb,
  TargetPlatform? targetPlatform,
}) {
  if (isWeb) {
    return Duration.zero;
  }

  final resolvedTargetPlatform = targetPlatform ?? defaultTargetPlatform;

  switch (resolvedTargetPlatform) {
    case TargetPlatform.android:
      return const Duration(seconds: 25);
    case TargetPlatform.iOS:
      return const Duration(seconds: 8);
    case TargetPlatform.fuchsia:
    case TargetPlatform.linux:
    case TargetPlatform.macOS:
    case TargetPlatform.windows:
      return Duration.zero;
  }
}

bool shouldSuspendRealtimeForBackground({
  bool isWeb = kIsWeb,
  TargetPlatform? targetPlatform,
}) {
  return realtimeBackgroundSuspendDelay(
        isWeb: isWeb,
        targetPlatform: targetPlatform,
      ) >
      Duration.zero;
}
