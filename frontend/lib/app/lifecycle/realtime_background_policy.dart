import 'package:flutter/foundation.dart';

bool shouldReconnectRealtimeOnForeground({
  required Duration backgroundDuration,
  bool isWeb = kIsWeb,
  TargetPlatform? targetPlatform,
}) {
  // Android may freeze the background suspend timer and leave a writable but
  // dead socket. A new connection is more reliable than probing that socket.
  return !isWeb &&
      (targetPlatform ?? defaultTargetPlatform) == TargetPlatform.android &&
      backgroundDuration >= const Duration(seconds: 5);
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
