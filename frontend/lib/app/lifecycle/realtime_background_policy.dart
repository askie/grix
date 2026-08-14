import 'package:flutter/foundation.dart';

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
