import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/models/login_device_session_model.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/device_management_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/profile/device_management_view.dart';

class _FakeAuthService extends AuthService {
  bool logoutCalled = false;

  @override
  Future<void> logout({bool notifyServer = true}) async {
    logoutCalled = true;
  }
}

class _FakeImService extends ImService {
  bool disconnectCalled = false;

  @override
  void disconnect({ImConnectionStage stage = ImConnectionStage.disconnected}) {
    disconnectCalled = true;
  }
}

class _FakeDeviceManagementService extends DeviceManagementService {
  _FakeDeviceManagementService(this.sessions) : super(dio: Dio());

  final List<LoginDeviceSessionModel> sessions;

  @override
  Future<List<LoginDeviceSessionModel>> fetchSessions() async {
    return sessions;
  }
}

class _FailingDeviceManagementService extends DeviceManagementService {
  _FailingDeviceManagementService() : super(dio: Dio());

  @override
  Future<List<LoginDeviceSessionModel>> fetchSessions() async {
    throw Exception('DioException [connection timeout]');
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
  });

  tearDown(() {
    Get.reset();
  });

  testWidgets('shows logout for current device and remove for others', (
    WidgetTester tester,
  ) async {
    Get.put<AuthService>(_FakeAuthService());
    Get.put<ImService>(_FakeImService());

    final service = _FakeDeviceManagementService([
      LoginDeviceSessionModel(
        sessionId: 'current-session',
        deviceId: 'mac-device',
        platform: 'macos',
        online: true,
        current: true,
        lastSeenAt: DateTime.parse('2026-03-15T15:39:00Z'),
        createdAt: DateTime.parse('2026-03-15T13:02:00Z'),
      ),
      LoginDeviceSessionModel(
        sessionId: 'other-session',
        deviceId: 'ios-device',
        platform: 'ios',
        online: false,
        current: false,
        lastSeenAt: DateTime.parse('2026-03-14T15:39:00Z'),
        createdAt: DateTime.parse('2026-03-14T13:02:00Z'),
      ),
    ]);

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        initialRoute: AppRoutes.deviceManagement,
        getPages: [
          GetPage(
            name: AppRoutes.deviceManagement,
            page: () => DeviceManagementView(service: service),
          ),
          GetPage(name: AppRoutes.login, page: () => const SizedBox.shrink()),
        ],
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Current device'), findsOneWidget);
    expect(find.widgetWithText(OutlinedButton, 'Log Out'), findsOneWidget);
    expect(find.widgetWithText(OutlinedButton, 'Remove'), findsOneWidget);
  });

  testWidgets('logout action waits for confirmation before leaving session', (
    WidgetTester tester,
  ) async {
    final authService = _FakeAuthService();
    final imService = _FakeImService();
    Get.put<AuthService>(authService);
    Get.put<ImService>(imService);

    final service = _FakeDeviceManagementService([
      LoginDeviceSessionModel(
        sessionId: 'current-session',
        deviceId: 'mac-device',
        platform: 'macos',
        online: true,
        current: true,
        lastSeenAt: DateTime.parse('2026-03-15T15:39:00Z'),
        createdAt: DateTime.parse('2026-03-15T13:02:00Z'),
      ),
    ]);

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        initialRoute: AppRoutes.deviceManagement,
        getPages: [
          GetPage(
            name: AppRoutes.deviceManagement,
            page: () => DeviceManagementView(service: service),
          ),
          GetPage(
            name: AppRoutes.login,
            page: () => const Scaffold(body: Text('login')),
          ),
        ],
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.widgetWithText(OutlinedButton, 'Log Out'));
    await tester.pumpAndSettle();

    expect(find.byType(AlertDialog), findsOneWidget);
    expect(find.text('Are you sure you want to log out?'), findsOneWidget);
    expect(authService.logoutCalled, isFalse);
    expect(imService.disconnectCalled, isFalse);

    await tester.tap(
      find.descendant(
        of: find.byType(AlertDialog),
        matching: find.widgetWithText(ElevatedButton, 'Log Out'),
      ),
    );
    await tester.pumpAndSettle();

    expect(authService.logoutCalled, isTrue);
    expect(imService.disconnectCalled, isTrue);
    expect(Get.currentRoute, AppRoutes.login);
    expect(find.text('login'), findsOneWidget);
  });

  testWidgets('load failure shows localized toast instead of exception text', (
    WidgetTester tester,
  ) async {
    Get.put<AuthService>(_FakeAuthService());
    Get.put<ImService>(_FakeImService());

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: DeviceManagementView(
          service: _FailingDeviceManagementService(),
        ),
      ),
    );
    await tester.pump();
    await tester.pump();

    expect(find.text('Failed to load devices'), findsOneWidget);
    expect(find.textContaining('DioException'), findsNothing);
    await tester.pump(const Duration(seconds: 3));
  });
}
