import 'dart:async';

import 'package:get/get.dart';

import '../../../app/routes/app_routes.dart';
import 'home_tab_url_sync_base.dart';

HomeTabUrlSync createHomeTabUrlSync() => _StubHomeTabUrlSync();

class _StubHomeTabUrlSync implements HomeTabUrlSync {
  @override
  String get currentPath {
    final currentRoute = Get.currentRoute;
    if (currentRoute.trim().isEmpty) {
      return AppRoutes.home;
    }
    return AppRoutes.pathOf(currentRoute);
  }

  @override
  Stream<String> get onPathChanged => const Stream<String>.empty();

  @override
  void pushPath(String path) {}

  @override
  void replacePath(String path) {}

  @override
  void dispose() {}
}
