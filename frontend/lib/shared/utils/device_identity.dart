import 'package:flutter/foundation.dart';
import 'package:uuid/uuid.dart';

import 'device_identity_store.dart';

class DeviceIdentity {
  DeviceIdentity._();

  static const Uuid _uuid = Uuid();

  static String platformLabel({TargetPlatform? targetPlatform}) {
    if (kIsWeb) {
      return 'web';
    }

    switch (targetPlatform ?? defaultTargetPlatform) {
      case TargetPlatform.android:
        return 'android';
      case TargetPlatform.iOS:
        return 'ios';
      case TargetPlatform.macOS:
        return 'macOS';
      case TargetPlatform.windows:
        return 'windows';
      case TargetPlatform.linux:
        return 'linux';
      case TargetPlatform.fuchsia:
        return 'fuchsia';
    }
  }

  static Future<String> resolveDeviceId() async {
    final store = await DeviceIdentityStore.create();
    final existing = (await store.getDeviceId())?.trim() ?? '';
    if (existing.isNotEmpty) {
      return existing;
    }

    final generated = '${platformLabel()}_${_uuid.v4()}';
    await store.setDeviceId(generated);
    return generated;
  }
}
