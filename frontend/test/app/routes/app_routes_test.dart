import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/modules/ai/agent_connection_setup_view.dart';
import 'package:grix/modules/ai/agent_create_view.dart';
import 'package:grix/modules/ai/agent_create_wizard_view.dart';

void main() {
  group('AppRoutes', () {
    GetPage<dynamic>? routeByName(String name) {
      for (final route in AppRoutes.routes) {
        if (route.name == name) {
          return route;
        }
      }
      return null;
    }

    test('disables pop gesture for root flow routes', () {
      final splashRoute = routeByName(AppRoutes.splash);
      final loginRoute = routeByName(AppRoutes.login);
      final homeRoute = routeByName(AppRoutes.home);
      final appAgreementRoute = routeByName(AppRoutes.appAgreement);
      final friendRequestsRoute = routeByName(AppRoutes.friendRequests);

      expect(splashRoute, isNotNull);
      expect(loginRoute, isNotNull);
      expect(homeRoute, isNotNull);
      expect(appAgreementRoute, isNotNull);
      expect(friendRequestsRoute, isNotNull);
      expect(splashRoute!.popGesture, isFalse);
      expect(loginRoute!.popGesture, isFalse);
      expect(homeRoute!.popGesture, isFalse);
    });

    test('registers device management settings route', () {
      final route = routeByName(AppRoutes.deviceManagement);

      expect(route, isNotNull);
      expect(route!.transition, Transition.rightToLeft);
    });

    test('registers dedicated home tab routes', () {
      expect(routeByName(AppRoutes.home), isNotNull);
      expect(routeByName(AppRoutes.homeAgents), isNotNull);
      expect(routeByName(AppRoutes.homeContacts), isNotNull);
      expect(routeByName(AppRoutes.homeSettings), isNotNull);
      expect(
        AppRoutes.homeTabForPath(AppRoutes.homeSettings),
        HomeTab.settings,
      );
    });

    test('registers agent scope settings route', () {
      final route = routeByName(AppRoutes.agentScopes);

      expect(route, isNotNull);
      expect(route!.transition, Transition.rightToLeft);
    });

    test('separates Agent create, setup, and edit routes', () {
      final createRoute = routeByName(AppRoutes.agentCreate);
      final setupRoute = routeByName(AppRoutes.agentSetup);
      final editRoute = routeByName(AppRoutes.agentEdit);

      expect(createRoute, isNotNull);
      expect(setupRoute, isNotNull);
      expect(editRoute, isNotNull);
      expect(createRoute!.page(), isA<AgentCreateWizardView>());
      expect(setupRoute!.page(), isA<AgentConnectionSetupView>());
      expect(editRoute!.page(), isA<AgentCreateView>());
    });

    test('registers report route', () {
      final route = routeByName(AppRoutes.report);

      expect(route, isNotNull);
      expect(route!.transition, Transition.rightToLeft);
    });

    test('keeps chat route non-opaque so the lower list stays painted', () {
      // opaque=false 让聊天页打开后不遮挡剔除下层会话列表,
      // 右滑返回时下层始终保持已绘制,消除重新光栅化导致的白屏。
      final chatRoute = routeByName(AppRoutes.chat);

      expect(chatRoute, isNotNull);
      expect(chatRoute!.opaque, isFalse);
      expect(chatRoute.transition, Transition.rightToLeft);
    });
  });
}
