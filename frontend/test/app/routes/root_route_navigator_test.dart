import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/routes/root_route_navigator.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Future<void> pumpShell(
    WidgetTester tester, {
    required String initialRoute,
  }) async {
    await tester.pumpWidget(
      GetMaterialApp(
        initialRoute: initialRoute,
        getPages: [
          GetPage(
            name: AppRoutes.splash,
            page: () => const Scaffold(body: Text('splash')),
          ),
          GetPage(
            name: AppRoutes.login,
            page: () => const Scaffold(body: Text('login')),
          ),
          GetPage(
            name: AppRoutes.register,
            page: () => const Scaffold(body: Text('register')),
          ),
          GetPage(
            name: AppRoutes.resetPassword,
            page: () => const Scaffold(body: Text('reset')),
          ),
          GetPage(
            name: AppRoutes.home,
            page: () => const Scaffold(body: Text('home')),
          ),
        ],
      ),
    );
    await tester.pumpAndSettle();
  }

  setUp(() {
    Get.testMode = true;
    Get.reset();
  });

  tearDown(() {
    Get.reset();
  });

  testWidgets('replaces splash root with home without leaving back stack', (
    tester,
  ) async {
    await pumpShell(tester, initialRoute: AppRoutes.splash);

    RootRouteNavigator.toHome();
    await tester.pumpAndSettle();

    expect(Get.currentRoute, AppRoutes.home);
    expect(find.text('home'), findsOneWidget);
    expect(Get.key.currentState?.canPop(), isFalse);
  });

  testWidgets('collapses auth stack before replacing root with home', (
    tester,
  ) async {
    await pumpShell(tester, initialRoute: AppRoutes.login);

    Get.toNamed(AppRoutes.register);
    await tester.pumpAndSettle();
    expect(Get.currentRoute, AppRoutes.register);

    RootRouteNavigator.toHome();
    await tester.pumpAndSettle();

    expect(Get.currentRoute, AppRoutes.home);
    expect(find.text('home'), findsOneWidget);
    expect(Get.key.currentState?.canPop(), isFalse);
  });

  testWidgets('pops back to existing login root without duplicating it', (
    tester,
  ) async {
    await pumpShell(tester, initialRoute: AppRoutes.login);

    Get.toNamed(AppRoutes.resetPassword);
    await tester.pumpAndSettle();
    expect(Get.currentRoute, AppRoutes.resetPassword);

    RootRouteNavigator.toLogin();
    await tester.pumpAndSettle();

    expect(Get.currentRoute, AppRoutes.login);
    expect(find.text('login'), findsOneWidget);
    expect(Get.key.currentState?.canPop(), isFalse);
  });

  testWidgets('keeps home as a single root when already on home',
      (tester) async {
    await pumpShell(tester, initialRoute: AppRoutes.home);

    RootRouteNavigator.toHome();
    await tester.pumpAndSettle();

    expect(Get.currentRoute, AppRoutes.home);
    expect(find.text('home'), findsOneWidget);
    expect(Get.key.currentState?.canPop(), isFalse);
  });

  testWidgets('rebuilds target route as a clean root when stack is dirty', (
    tester,
  ) async {
    await pumpShell(tester, initialRoute: AppRoutes.home);

    Get.toNamed(AppRoutes.register);
    await tester.pumpAndSettle();
    expect(Get.currentRoute, AppRoutes.register);

    RootRouteNavigator.toHome();
    await tester.pumpAndSettle();

    expect(Get.currentRoute, AppRoutes.home);
    expect(find.text('home'), findsOneWidget);
    expect(Get.key.currentState?.canPop(), isFalse);
  });
}
