import 'package:flutter/foundation.dart';
import 'package:get/get.dart';

import '../../../app/routes/root_route_navigator.dart';
import '../../../data/providers/auth_service.dart';
import '../../../data/providers/im_service.dart';
import '../../../shared/utils/app_region_config.dart';
import '../../../shared/utils/app_storage_service.dart';

class SplashController extends GetxController {
  final AuthService authService = Get.find<AuthService>();
  final ImService imService = Get.find<ImService>();

  @override
  void onReady() {
    super.onReady();
    _bootstrapApp();
  }

  Future<void> _bootstrapApp() async {
    debugPrint(
      '🔐 Splash bootstrap: '
      'route=${Get.currentRoute} logged_in=${authService.isLoggedIn} '
      'user_id=${authService.userId ?? '-'} im_connected=${imService.isConnected}',
    );
    if (authService.isLoggedIn) {
      // API 端点：从持久化存储恢复，确保打到正确区域。
      // WS 端点：ImService.init() 已在启动时从存储预载，applyAuthPayload() 在
      // 登录/注册时同步更新；此处只调 ensureConnected()，不再重复读存储。
      // 必须用 updateBaseUrl（同步所有已注册 dio），不能只设 AuthService._dio：
      // 业务请求（profile/sessions/agents/friends 等）走各 service 自己的 dio，
      // 它们默认停在编译期 CN 端点，只有 updateBaseUrl 能一次性同步过来。
      // 端点存储为空（老账号 / 后端 region 字段为空导致登录时未写端点）时，
      // 按用户手选区域推导兜底——否则全球区冷启动业务 dio 仍打 CN 触发 401 跳登录。
      if (!kIsWeb) {
        final savedApiEndpoint = await AppStorageService.loadApiEndpoint();
        if (savedApiEndpoint != null && savedApiEndpoint.isNotEmpty) {
          authService.updateBaseUrl(savedApiEndpoint);
        } else {
          final region = await resolveInitialRegion();
          authService.updateBaseUrl(resolveRegionApiBaseUrl(region));
        }
      }

      if (!imService.isConnected) {
        debugPrint('🔌 Splash bootstrap triggering IM connect');
        imService.ensureConnected();
      }
      debugPrint('🧭 Splash bootstrap navigating to home');
      RootRouteNavigator.toHome();
      return;
    }

    // 未登录：按"记住的选择 / 系统语言"解析初始区域端点，供注册/登录/找回密码页使用。
    final region = await resolveInitialRegion();
    authService.updateBaseUrl(resolveRegionApiBaseUrl(region));

    debugPrint(
      '🧭 Splash bootstrap navigating to login (region: ${region.name})',
    );
    RootRouteNavigator.toLogin();
  }
}
