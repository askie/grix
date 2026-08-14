import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/locale/locale_service.dart';
import '../../app/translations/app_translations.dart';
import '../../app/settings/chat_background_service.dart';
import '../../app/settings/chat_font_size_service.dart';
import '../../app/settings/agent_toolbar_visibility_service.dart';
import '../../app/settings/chat_skill_usage_service.dart';
import '../../app/settings/chat_thinking_collapse_service.dart';
import '../../app/settings/theme_preference_service.dart';
import '../../data/providers/agent_category_service.dart';
import '../../data/providers/agent_service.dart';
import '../../data/providers/app_update_service.dart';
import '../../data/providers/auth_service.dart';
import '../../data/providers/desktop_auto_updater.dart';
import '../../data/providers/deep_link_service.dart';
import '../../data/providers/egg_market_service.dart';
import '../../data/providers/feature_flag_service.dart';
import '../../data/providers/friend_service.dart';
import '../../data/providers/friend_qr_service.dart';
import '../../data/services/link_safety_service.dart';
import '../../data/providers/group_qr_service.dart';
import '../../data/providers/im_service.dart';
import '../../data/providers/local_db.dart';
import '../../data/providers/network_reconnect_service.dart';
import '../../data/providers/oss_service.dart';
import '../../data/providers/push_registration_service.dart';
import '../../data/providers/push_tap_handler.dart';
import '../../data/providers/qr_login_service.dart';
import '../../data/providers/session_service.dart';
import '../../data/providers/notification_prefs_service.dart';
import '../../data/providers/user_settings_service.dart';
import '../../modules/profile/services/avatar_cropper_service.dart';
import '../../platform/desktop/desktop_module.dart';
import '../../shared/services/in_app_notification_service.dart';
import '../routes/app_routes.dart';
import '../../modules/call/call_controller.dart';
import '../../modules/call/call_dialogs.dart';
import '../../platform/call_kit_bridge.dart';

class AppBootstrapData {
  const AppBootstrapData({
    required this.initialLocale,
    required this.initialRoute,
    required this.translations,
  });

  final Locale? initialLocale;
  final String? initialRoute;
  final AppTranslations translations;

  String resolveInitialRoute({required bool isLoggedIn}) {
    final route = initialRoute?.trim();
    if (route != null && route.isNotEmpty) {
      return route;
    }
    return isLoggedIn ? AppRoutes.home : AppRoutes.login;
  }
}

class AppInitializer {
  AppInitializer._();

  static const Duration _serviceInitTimeout = Duration(seconds: 12);

  static Future<AppBootstrapData> bootstrap() async {
    debugPrint('🚀 App bootstrap started');
    final savedLocaleFuture = LocaleService.loadSavedLocale();
    final translationsFuture = AppTranslations.load();

    await _runInitStep<void>(
      'LocalDb.initDatabaseFactory',
      LocalDb.initDatabaseFactory,
    );
    await _initCriticalServices();
    await DesktopModule.initialize();
    final authService = Get.find<AuthService>();
    final initialRoute = authService.isLoggedIn
        ? AppRoutes.home
        : AppRoutes.login;

    debugPrint('✅ App bootstrap completed');
    return AppBootstrapData(
      initialLocale: await savedLocaleFuture,
      initialRoute: initialRoute,
      translations: await translationsFuture,
    );
  }

  static Future<void> _initCriticalServices() async {
    await _runInitStep<AuthService>('AuthService', _putAuthService);

    await _waitAll([
      _runInitStep<ImService>('ImService', _putImService),
      _runInitStep<FriendService>('FriendService', _putFriendService),
      _runInitStep<AgentService>('AgentService', _putAgentService),
      _runInitStep<AgentCategoryService>(
        'AgentCategoryService',
        _putAgentCategoryService,
      ),
      _runInitStep<SessionService>('SessionService', _putSessionService),
      _runInitStep<FeatureFlagService>(
        'FeatureFlagService',
        _putFeatureFlagService,
      ),
      _runInitStep<ThemePreferenceService>(
        'ThemePreferenceService',
        _putThemePreferenceService,
      ),
      _runInitStep<ChatFontSizeService>(
        'ChatFontSizeService',
        _putChatFontSizeService,
      ),
      _runInitStep<ChatBackgroundService>(
        'ChatBackgroundService',
        _putChatBackgroundService,
      ),
      _runInitStep<ChatThinkingCollapseService>(
        'ChatThinkingCollapseService',
        _putChatThinkingCollapseService,
      ),
      _runInitStep<AgentToolbarVisibilityService>(
        'AgentToolbarVisibilityService',
        _putAgentToolbarVisibilityService,
      ),
      _runInitStep<ChatSkillUsageService>(
        'ChatSkillUsageService',
        _putChatSkillUsageService,
      ),
    ]);

    // 全局通话浮层(GrixApp builder)在首帧就要订阅通话状态，因此 CallController
    // 必须在首页显示前注册好；放到延迟初始化里会让首帧拿不到它、浮层永远订阅不上
    // 通话状态，导致"拨号已接通但通话弹窗不出现"。构造很轻(无网络)，可走关键路径。
    // 依赖 FeatureFlagService(用于 CallKit 门控)，故置于上面的 _waitAll 之后。
    _putCallController();
  }

  static bool _deferredInitStarted = false;

  static Future<void> runDeferredInit() async {
    if (_deferredInitStarted) return;
    _deferredInitStarted = true;
    debugPrint('🔄 Deferred init started');
    await _waitAll([
      _runInitStep<LinkSafetyService>(
        'LinkSafetyService',
        _putLinkSafetyService,
      ),
      _runInitStep<PushRegistrationService>(
        'PushRegistrationService',
        _putPushRegistrationService,
      ),
      _runInitStep<NetworkReconnectService>(
        'NetworkReconnectService',
        _putNetworkReconnectService,
      ),
      _runInitStep<FriendQrService>('FriendQrService', _putFriendQrService),
      _runInitStep<GroupQrService>('GroupQrService', _putGroupQrService),
      _runInitStep<QrLoginService>('QrLoginService', _putQrLoginService),
      _runInitStep<EggMarketService>('EggMarketService', _putEggMarketService),
      _runInitStep<UserSettingsService>(
        'UserSettingsService',
        _putUserSettingsService,
      ),
      _runInitStep<NotificationPrefsService>(
        'NotificationPrefsService',
        _putNotificationPrefsService,
      ),
      _runInitStep<AvatarCropperService>(
        'AvatarCropperService',
        _putAvatarCropperService,
      ),
      _runInitStep<InAppNotificationService>(
        'InAppNotificationService',
        _putInAppNotificationService,
      ),
      _runInitStep<AppUpdateService>(
        'AppUpdateService',
        _putAppUpdateService,
      ),
      _runInitStep<DesktopAutoUpdaterService>(
        'DesktopAutoUpdaterService',
        _putDesktopAutoUpdaterService,
      ),
    ]);

    await _runInitStep<DeepLinkService>('DeepLinkService', _putDeepLinkService);
    await _runInitStep<PushTapHandler>('PushTapHandler', _putPushTapHandler);
    await _runInitStep<OssService>('OssService', () async => _putOssService());
    debugPrint('✅ Deferred init completed');
  }

  /// Like Future.wait but tolerates individual failures.
  /// Each future is wrapped so errors are logged but never propagate.
  static Future<void> _waitAll(List<Future<void>> futures) {
    return Future.wait(
      futures.map((f) => f.catchError((error) {
        // Already logged inside _runInitStep; swallow here so the
        // whole batch completes even if one step throws.
      })),
    );
  }

  static Future<T> _runInitStep<T>(
    String name,
    Future<T> Function() step,
  ) async {
    final watch = Stopwatch()..start();
    try {
      final result = await step().timeout(_serviceInitTimeout);
      debugPrint('✅ Init step ready: $name (${watch.elapsedMilliseconds}ms)');
      return result;
    } on TimeoutException {
      final seconds = _serviceInitTimeout.inSeconds;
      final message = 'Initialization timeout in $name after ${seconds}s';
      debugPrint('⏱️ $message');
      throw TimeoutException(message, _serviceInitTimeout);
    } catch (error) {
      debugPrint('❌ Init step failed: $name, error: $error');
      rethrow;
    }
  }

  static Future<AuthService> _putAuthService() {
    if (Get.isRegistered<AuthService>()) {
      return Future.value(Get.find<AuthService>());
    }
    return Get.putAsync<AuthService>(() => AuthService().init());
  }

  static Future<LinkSafetyService> _putLinkSafetyService() {
    if (Get.isRegistered<LinkSafetyService>()) {
      return Future.value(Get.find<LinkSafetyService>());
    }
    return Get.putAsync<LinkSafetyService>(() => LinkSafetyService().init());
  }

  static Future<PushRegistrationService> _putPushRegistrationService() {
    if (Get.isRegistered<PushRegistrationService>()) {
      return Future.value(Get.find<PushRegistrationService>());
    }
    return Get.putAsync<PushRegistrationService>(
      () => PushRegistrationService().init(),
    );
  }

  static Future<NetworkReconnectService> _putNetworkReconnectService() {
    if (Get.isRegistered<NetworkReconnectService>()) {
      return Future.value(Get.find<NetworkReconnectService>());
    }
    return Get.putAsync<NetworkReconnectService>(
      () => NetworkReconnectService().init(),
    );
  }

  static Future<FriendService> _putFriendService() {
    if (Get.isRegistered<FriendService>()) {
      return Future.value(Get.find<FriendService>());
    }
    return Get.putAsync<FriendService>(() => FriendService().init());
  }

  static Future<FriendQrService> _putFriendQrService() {
    if (Get.isRegistered<FriendQrService>()) {
      return Future.value(Get.find<FriendQrService>());
    }
    return Get.putAsync<FriendQrService>(() => FriendQrService().init());
  }

  static Future<GroupQrService> _putGroupQrService() {
    if (Get.isRegistered<GroupQrService>()) {
      return Future.value(Get.find<GroupQrService>());
    }
    return Get.putAsync<GroupQrService>(() => GroupQrService().init());
  }

  static Future<QrLoginService> _putQrLoginService() {
    if (Get.isRegistered<QrLoginService>()) {
      return Future.value(Get.find<QrLoginService>());
    }
    return Get.putAsync<QrLoginService>(() => QrLoginService().init());
  }

  static Future<ImService> _putImService() {
    if (Get.isRegistered<ImService>()) {
      return Future.value(Get.find<ImService>());
    }
    return Get.putAsync<ImService>(() => ImService().init());
  }

  static void _putCallController() {
    if (!Get.isRegistered<CallController>()) {
      Get.put<CallController>(CallController(), permanent: true);
    }
    // 注入来电弹窗构建器（活跃通话已改为 GrixApp builder overlay，不再需要注入）
    CallController.incomingDialogBuilder = (_) => const IncomingCallDialog();
    // iOS CallKit/PushKit 来电唤醒：仅当语音通话功能开关开启时才需要。
    // Feature gate 控制是否启用语音通话，关闭时不会注册 VoIP token。
    if (Get.find<FeatureFlagService>().isEnabled('voice_call') && !kIsWeb && GetPlatform.isIOS) {
      CallKitBridge();
    }
  }

  static Future<AgentService> _putAgentService() {
    if (Get.isRegistered<AgentService>()) {
      return Future.value(Get.find<AgentService>());
    }
    return Get.putAsync<AgentService>(() => AgentService().init());
  }

  static Future<AgentCategoryService> _putAgentCategoryService() {
    if (Get.isRegistered<AgentCategoryService>()) {
      return Future.value(Get.find<AgentCategoryService>());
    }
    return Get.putAsync<AgentCategoryService>(
      () => AgentCategoryService().init(),
    );
  }

  static Future<FeatureFlagService> _putFeatureFlagService() {
    if (Get.isRegistered<FeatureFlagService>()) {
      return Future.value(Get.find<FeatureFlagService>());
    }
    return Get.putAsync<FeatureFlagService>(
      () => FeatureFlagService().init(),
    );
  }

  static Future<EggMarketService> _putEggMarketService() {
    if (Get.isRegistered<EggMarketService>()) {
      return Future.value(Get.find<EggMarketService>());
    }
    return Get.putAsync<EggMarketService>(() => EggMarketService().init());
  }

  static Future<SessionService> _putSessionService() {
    if (Get.isRegistered<SessionService>()) {
      return Future.value(Get.find<SessionService>());
    }
    return Get.putAsync<SessionService>(() => SessionService().init());
  }

  static Future<UserSettingsService> _putUserSettingsService() {
    if (Get.isRegistered<UserSettingsService>()) {
      return Future.value(Get.find<UserSettingsService>());
    }
    return Get.putAsync<UserSettingsService>(
      () => UserSettingsService().init(),
    );
  }

  static Future<NotificationPrefsService> _putNotificationPrefsService() {
    if (Get.isRegistered<NotificationPrefsService>()) {
      return Future.value(Get.find<NotificationPrefsService>());
    }
    return Get.putAsync<NotificationPrefsService>(
      () => NotificationPrefsService().init(),
    );
  }

  static Future<ChatFontSizeService> _putChatFontSizeService() {
    if (Get.isRegistered<ChatFontSizeService>()) {
      return Future.value(Get.find<ChatFontSizeService>());
    }
    return Get.putAsync<ChatFontSizeService>(
      () => ChatFontSizeService().init(),
    );
  }

  static Future<ChatThinkingCollapseService> _putChatThinkingCollapseService() {
    if (Get.isRegistered<ChatThinkingCollapseService>()) {
      return Future.value(Get.find<ChatThinkingCollapseService>());
    }
    return Get.putAsync<ChatThinkingCollapseService>(
      () => ChatThinkingCollapseService().init(),
    );
  }

  static Future<AgentToolbarVisibilityService> _putAgentToolbarVisibilityService() {
    if (Get.isRegistered<AgentToolbarVisibilityService>()) {
      return Future.value(Get.find<AgentToolbarVisibilityService>());
    }
    return Get.putAsync<AgentToolbarVisibilityService>(
      () => AgentToolbarVisibilityService().init(),
    );
  }

  static Future<ChatSkillUsageService> _putChatSkillUsageService() {
    if (Get.isRegistered<ChatSkillUsageService>()) {
      return Future.value(Get.find<ChatSkillUsageService>());
    }
    return Get.putAsync<ChatSkillUsageService>(
      () => ChatSkillUsageService().init(),
    );
  }

  static Future<ChatBackgroundService> _putChatBackgroundService() {
    if (Get.isRegistered<ChatBackgroundService>()) {
      return Future.value(Get.find<ChatBackgroundService>());
    }
    return Get.putAsync<ChatBackgroundService>(
      () => ChatBackgroundService().init(),
    );
  }

  static Future<ThemePreferenceService> _putThemePreferenceService() {
    if (Get.isRegistered<ThemePreferenceService>()) {
      return Future.value(Get.find<ThemePreferenceService>());
    }
    return Get.putAsync<ThemePreferenceService>(
      () => ThemePreferenceService().init(),
    );
  }

  static Future<AvatarCropperService> _putAvatarCropperService() {
    if (Get.isRegistered<AvatarCropperService>()) {
      return Future.value(Get.find<AvatarCropperService>());
    }
    return Future.value(
      Get.put<AvatarCropperService>(AvatarCropperService(), permanent: true),
    );
  }

  static Future<DeepLinkService> _putDeepLinkService() {
    if (Get.isRegistered<DeepLinkService>()) {
      return Future.value(Get.find<DeepLinkService>());
    }
    return Get.putAsync<DeepLinkService>(() => DeepLinkService().init());
  }

  static Future<PushTapHandler> _putPushTapHandler() {
    if (Get.isRegistered<PushTapHandler>()) {
      return Future.value(Get.find<PushTapHandler>());
    }
    return Get.putAsync<PushTapHandler>(() => PushTapHandler().init());
  }

  static OssService _putOssService() {
    if (Get.isRegistered<OssService>()) {
      return Get.find<OssService>();
    }
    return Get.put<OssService>(OssService());
  }

  static Future<InAppNotificationService> _putInAppNotificationService() {
    if (Get.isRegistered<InAppNotificationService>()) {
      return Future.value(Get.find<InAppNotificationService>());
    }
    return Get.putAsync<InAppNotificationService>(
      () => InAppNotificationService().init(),
    );
  }

  static Future<AppUpdateService> _putAppUpdateService() {
    if (Get.isRegistered<AppUpdateService>()) {
      return Future.value(Get.find<AppUpdateService>());
    }
    return Get.putAsync<AppUpdateService>(
      () => AppUpdateService().init(),
    );
  }

  static Future<DesktopAutoUpdaterService> _putDesktopAutoUpdaterService() {
    if (Get.isRegistered<DesktopAutoUpdaterService>()) {
      return Future.value(Get.find<DesktopAutoUpdaterService>());
    }
    return Get.putAsync<DesktopAutoUpdaterService>(
      () => DesktopAutoUpdaterService().init(),
    );
  }
}
