import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';

import 'app/routes/app_routes.dart';
import 'app/theme/app_theme.dart';
import 'core/config/admin_region.dart';
import 'core/config/app_config.dart';
import 'core/network/api_client.dart';
import 'core/storage/token_store.dart';
import 'modules/auth/auth_service.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();

  // 加载区域选择与 token（区域必须先加载，ApiClient 首次请求前要用到）。
  await Future.wait([
    AdminRegionStore.load(),
    TokenStore.load(),
    _preloadChineseUiFont(),
  ]);
  ApiClient.instance.updateBaseUrl(AppConfig.apiRoot);

  // 全局认证服务常驻。
  final auth = Get.put(AuthService(), permanent: true);

  // 401 时清理并跳转登录。
  ApiClient.instance.onUnauthorized = () async {
    await TokenStore.clear();
    auth.profile.value = null;
    if (Get.currentRoute != AppRoutes.login) {
      Get.offAllNamed(AppRoutes.login);
    }
  };

  // 已登录则后台拉取管理员信息（失败由 401 流程兜底）。
  if (TokenStore.hasToken) {
    auth.fetchProfile().catchError((_) {});
  }

  runApp(const GrixAdminApp());
}

Future<void> _preloadChineseUiFont() async {
  try {
    final loader = FontLoader('GrixUiZh');
    loader.addFont(rootBundle.load('assets/fonts/grix_ui_zh_subset.ttf'));
    await loader.load();
  } catch (e) {
    debugPrint('Chinese font preload skipped (non-fatal): $e');
  }
}

class GrixAdminApp extends StatelessWidget {
  const GrixAdminApp({super.key});

  @override
  Widget build(BuildContext context) {
    return GetMaterialApp(
      title: '塘主',
      debugShowCheckedModeBanner: false,
      theme: AppTheme.light,
      // 禁用页面切换动画，避免宽屏侧边栏在路由跳转时出现滑入闪动
      defaultTransition: Transition.noTransition,
      transitionDuration: Duration.zero,
      initialRoute:
          TokenStore.hasToken ? AppRoutes.home : AppRoutes.login,
      getPages: AppPages.pages,
    );
  }
}
