import 'package:flutter/widgets.dart';
import 'package:get/get.dart';

import '../../app/routes/app_routes.dart';
import '../../core/storage/token_store.dart';

/// 路由鉴权守卫：未登录访问受保护页面时重定向到登录页。
///
/// 主要防止 Web 端直接输入 URL 或刷新进入受保护路由。
class AuthGuard extends GetMiddleware {
  @override
  RouteSettings? redirect(String? route) {
    if (TokenStore.hasToken) return null;
    return const RouteSettings(name: AppRoutes.login);
  }
}
