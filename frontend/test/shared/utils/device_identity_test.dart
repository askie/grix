import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/utils/device_identity.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../data/providers/support/auth_session_browser_storage.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  test('platformLabel maps target platforms deterministically', () {
    expect(
      DeviceIdentity.platformLabel(targetPlatform: TargetPlatform.android),
      'android',
    );
    expect(
      DeviceIdentity.platformLabel(targetPlatform: TargetPlatform.iOS),
      'ios',
    );
    expect(
      DeviceIdentity.platformLabel(targetPlatform: TargetPlatform.macOS),
      'macOS',
    );
    expect(
      DeviceIdentity.platformLabel(targetPlatform: TargetPlatform.windows),
      'windows',
    );
    expect(
      DeviceIdentity.platformLabel(targetPlatform: TargetPlatform.linux),
      'linux',
    );
    expect(
      DeviceIdentity.platformLabel(targetPlatform: TargetPlatform.fuchsia),
      'fuchsia',
    );
  });

  test('resolveDeviceId persists a stable per-install identity', () async {
    final first = await DeviceIdentity.resolveDeviceId();
    final second = await DeviceIdentity.resolveDeviceId();

    expect(first, isNotEmpty);
    expect(second, first);
  });

  test(
    'web resolveDeviceId persists to localStorage with stable key',
    () async {
      if (!kIsWeb) {
        return;
      }

      await clearBrowserLocalStorage(const ['flutter.device_identity_id']);

      final first = await DeviceIdentity.resolveDeviceId();
      final second = await DeviceIdentity.resolveDeviceId();

      expect(first, isNotEmpty);
      expect(second, first);
      expect(
        await readBrowserLocalStorage('flutter.device_identity_id'),
        first,
      );
      expect(
        await readBrowserSessionStorage('flutter.device_identity_id'),
        isNull,
      );
    },
  );
}
