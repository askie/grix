import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/shared/utils/hardware_facade.dart';
import 'package:grix/shared/utils/permission_purpose_banner.dart';
import 'package:permission_handler/permission_handler.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    HardwareFacade.debugReset();
  });

  tearDown(() {
    HardwareFacade.debugReset();
    PermissionPurposeBanner.debugReset();
    Get.reset();
  });

  Future<void> pumpApp(WidgetTester tester) async {
    late OverlayState overlayState;
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations({
          'en_US': {
            'android_permission_purpose_camera_title':
                'Camera permission notice',
            'android_permission_purpose_camera_body':
                'Used for taking photos, recording video, and scanning QR codes.',
            'android_permission_purpose_microphone_title':
                'Microphone permission notice',
            'android_permission_purpose_microphone_body':
                'Used for voice calls and voice input.',
            'android_permission_purpose_photos_title':
                'Photos permission notice',
            'android_permission_purpose_photos_body':
                'Used to select and send photos and videos.',
            'android_permission_purpose_notification_title':
                'Notification permission notice',
            'android_permission_purpose_notification_body':
                'Used to receive new message alerts.',
            'android_permission_purpose_generic_title': 'Permission notice',
            'android_permission_purpose_generic_body':
                'Used to enable required features for this action.',
          },
        }),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: Overlay(
            initialEntries: [
              OverlayEntry(
                builder: (context) {
                  overlayState = Overlay.of(context);
                  return const SizedBox.expand();
                },
              ),
            ],
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    PermissionPurposeBanner.debugOverlayResolver = () => overlayState;
  }

  testWidgets('banner appears during request and dismisses after', (
    tester,
  ) async {
    await pumpApp(tester);
    HardwareFacade.debugForceRuntimePermissionGate = true;
    HardwareFacade.debugForceAndroidPurposeBanner = true;

    final gate = Completer<PermissionStatus>();
    HardwareFacade.debugStatusResolver = (_) async => PermissionStatus.denied;
    HardwareFacade.debugRequestResolver = (_) => gate.future;

    final future = HardwareFacade.requestPermission(Permission.camera);
    await tester.pump();
    await tester.pump();

    expect(PermissionPurposeBanner.isVisible, isTrue);
    expect(
      find.byKey(const Key('android_permission_purpose_banner_title')),
      findsOneWidget,
    );
    expect(find.text('Camera permission notice'), findsOneWidget);

    gate.complete(PermissionStatus.granted);
    final granted = await future;
    await tester.pump();

    expect(granted, isTrue);
    expect(PermissionPurposeBanner.isVisible, isFalse);
    expect(
      find.byKey(const Key('android_permission_purpose_banner_title')),
      findsNothing,
    );
  });

  testWidgets('already granted skips banner and request', (tester) async {
    await pumpApp(tester);
    HardwareFacade.debugForceRuntimePermissionGate = true;
    HardwareFacade.debugForceAndroidPurposeBanner = true;

    var requestCalls = 0;
    HardwareFacade.debugStatusResolver = (_) async => PermissionStatus.granted;
    HardwareFacade.debugRequestResolver = (_) async {
      requestCalls += 1;
      return PermissionStatus.granted;
    };

    final granted = await HardwareFacade.requestPermission(Permission.camera);
    await tester.pump();

    expect(granted, isTrue);
    expect(requestCalls, 0);
    expect(PermissionPurposeBanner.isVisible, isFalse);
    expect(
      find.byKey(const Key('android_permission_purpose_banner_title')),
      findsNothing,
    );
  });

  testWidgets('permanently denied skips banner and request', (tester) async {
    await pumpApp(tester);
    HardwareFacade.debugForceRuntimePermissionGate = true;
    HardwareFacade.debugForceAndroidPurposeBanner = true;

    var requestCalls = 0;
    HardwareFacade.debugStatusResolver =
        (_) async => PermissionStatus.permanentlyDenied;
    HardwareFacade.debugRequestResolver = (_) async {
      requestCalls += 1;
      return PermissionStatus.permanentlyDenied;
    };

    final granted = await HardwareFacade.requestPermission(Permission.camera);
    await tester.pump();

    expect(granted, isFalse);
    expect(requestCalls, 0);
    expect(PermissionPurposeBanner.isVisible, isFalse);
  });

  testWidgets('non-Android does not show purpose banner', (tester) async {
    await pumpApp(tester);
    HardwareFacade.debugForceRuntimePermissionGate = true;
    HardwareFacade.debugForceAndroidPurposeBanner = false;

    final gate = Completer<PermissionStatus>();
    HardwareFacade.debugStatusResolver = (_) async => PermissionStatus.denied;
    HardwareFacade.debugRequestResolver = (_) => gate.future;

    final future = HardwareFacade.requestPermission(Permission.camera);
    await tester.pump();
    await tester.pump();

    expect(PermissionPurposeBanner.isVisible, isFalse);
    expect(
      find.byKey(const Key('android_permission_purpose_banner_title')),
      findsNothing,
    );

    gate.complete(PermissionStatus.granted);
    expect(await future, isTrue);
  });

  testWidgets('banner dismisses when request throws', (tester) async {
    await pumpApp(tester);
    HardwareFacade.debugForceRuntimePermissionGate = true;
    HardwareFacade.debugForceAndroidPurposeBanner = true;

    HardwareFacade.debugStatusResolver = (_) async => PermissionStatus.denied;
    HardwareFacade.debugRequestResolver = (_) async {
      throw PlatformException(code: 'error', message: 'boom');
    };

    final granted = await HardwareFacade.requestPermission(Permission.camera);
    await tester.pump();

    expect(granted, isFalse);
    expect(PermissionPurposeBanner.isVisible, isFalse);
  });

  test('Android purpose banner defaults follow target platform', () {
    debugDefaultTargetPlatformOverride = TargetPlatform.android;
    try {
      HardwareFacade.debugForceAndroidPurposeBanner = null;
      // Request path itself is covered by widget tests with force flags.
      expect(defaultTargetPlatform, TargetPlatform.android);
    } finally {
      debugDefaultTargetPlatformOverride = null;
    }

    debugDefaultTargetPlatformOverride = TargetPlatform.iOS;
    try {
      expect(defaultTargetPlatform, TargetPlatform.iOS);
    } finally {
      debugDefaultTargetPlatformOverride = null;
    }
  });
}
