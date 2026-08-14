import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:get/get.dart';

import 'app_routes.dart';

class RootRouteNavigator {
  const RootRouteNavigator._();

  static void toHome() {
    resetTo(AppRoutes.home);
  }

  static void toLogin() {
    resetTo(AppRoutes.login);
  }

  static void resetTo(
    String routeName, {
    dynamic arguments,
    Map<String, String>? parameters,
  }) {
    final navigatorState = Get.key.currentState;
    final canPop = navigatorState?.canPop() ?? false;
    final currentRoute = Get.currentRoute.trim();
    debugPrint(
      '🧭 RootRouteNavigator.resetTo requested: '
      'target=$routeName current=${currentRoute.isEmpty ? '(empty)' : currentRoute} '
      'can_pop=$canPop is_web=$kIsWeb has_params=${parameters?.isNotEmpty == true}',
    );
    if (!canPop && _isCurrentRouteTarget(routeName, parameters: parameters)) {
      debugPrint(
        '🧭 RootRouteNavigator.resetTo skipped: '
        'target already active target=$routeName current=$currentRoute',
      );
      return;
    }

    void performReset() {
      try {
        debugPrint(
          '🧭 RootRouteNavigator.resetTo dispatch: '
          'target=$routeName current_before=${Get.currentRoute}',
        );
        Get.offAllNamed<void>(
          routeName,
          arguments: arguments,
          parameters: parameters,
        );
        Future<void>.microtask(() {
          debugPrint(
            '🧭 RootRouteNavigator.resetTo dispatched: '
            'target=$routeName current_after=${Get.currentRoute}',
          );
        });
      } catch (e, st) {
        debugPrint(
          '❌ RootRouteNavigator.resetTo failed: '
          'target=$routeName current=${Get.currentRoute} err=$e\n$st',
        );
        rethrow;
      }
    }

    final context = Get.key.currentContext;
    final hasRouterAncestor =
        context != null && Router.maybeOf<Object?>(context) != null;
    if (kIsWeb && context != null && hasRouterAncestor) {
      debugPrint(
        '🧭 RootRouteNavigator.resetTo using Router.neglect target=$routeName',
      );
      Router.neglect(context, performReset);
      return;
    }
    if (kIsWeb && context != null && !hasRouterAncestor) {
      debugPrint(
        '🧭 RootRouteNavigator.resetTo fallback without Router.neglect '
        'target=$routeName reason=no_router_ancestor',
      );
    }

    performReset();
  }

  static bool _isCurrentRouteTarget(
    String routeName, {
    Map<String, String>? parameters,
  }) {
    if (parameters != null && parameters.isNotEmpty) {
      return false;
    }
    final currentRoute = Get.currentRoute.trim();
    if (currentRoute.isEmpty) {
      return false;
    }
    return AppRoutes.pathOf(currentRoute) == AppRoutes.pathOf(routeName);
  }
}
