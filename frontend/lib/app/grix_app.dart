import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:get/get.dart';

import 'application_switcher_title.dart';
import 'bootstrap/app_initializer.dart';
import 'locale/app_material_localizations.dart';
import 'locale/locale_service.dart';
import 'routes/app_routes.dart';
import 'routes/app_route_observer.dart';
import 'scroll/app_scroll_behavior.dart';
import 'settings/theme_preference_service.dart';
import 'themes/app_theme.dart';
import 'translations/app_translations.dart';
import 'lifecycle/realtime_background_policy.dart';
import '../modules/call/call_controller.dart';
import '../modules/call/call_dialogs.dart';
import '../modules/text_document/services/text_document_open_service.dart';
import '../modules/share_ingest/services/share_ingest_service.dart';
import '../data/providers/auth_service.dart';
import '../data/providers/im_service.dart';
import '../data/providers/push_registration_service.dart';
import '../data/providers/user_settings_service.dart';
import '../shared/services/in_app_notification_service.dart';
import '../shared/widgets/in_app_notification_banner.dart';
import '../platform/desktop/desktop_tray_service.dart';

class GrixApp extends StatefulWidget {
  const GrixApp({
    super.key,
    this.initialLocale,
    required this.initialRoute,
    required this.translations,
  });

  final Locale? initialLocale;
  final String initialRoute;
  final AppTranslations translations;

  @override
  State<GrixApp> createState() => _GrixAppState();
}

class _GrixAppState extends State<GrixApp> with WidgetsBindingObserver {
  bool _realtimeSuspendedForBackground = false;
  Timer? _backgroundSuspendTimer;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _ensureRealtimeConnected();
      unawaited(AppInitializer.runDeferredInit());
      unawaited(TextDocumentOpenService.initialize());
      // 桌面托盘菜单在 bootstrap 期间构建，早于翻译注册进 GetX，
      // 首帧后翻译已就绪，重建一次菜单让 label 取到真实译文。
      unawaited(DesktopTrayService.refreshContextMenuIfRegistered());
    });
  }

  @override
  void dispose() {
    _backgroundSuspendTimer?.cancel();
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  @override
  void didChangeLocales(List<Locale>? locales) {
    unawaited(_followSystemLocalesIfNeeded(locales));
  }

  /// When the user has not manually picked a language, keep the UI in sync with
  /// the OS locale list. Never persist the system choice. If logged in, push the
  /// new UI language to the server so push/email copy stay aligned.
  Future<void> _followSystemLocalesIfNeeded(List<Locale>? locales) async {
    if (await LocaleService.hasSavedLocale()) {
      return;
    }
    final resolved = LocaleService.resolveFromSystemLocales(
      locales ?? PlatformDispatcher.instance.locales,
    );
    final current = Get.locale;
    if (current != null && LocaleService.isSameLocale(current, resolved)) {
      return;
    }
    Get.updateLocale(resolved);
    _pushFollowSystemLanguageToServer(resolved);
  }

  void _pushFollowSystemLanguageToServer(Locale locale) {
    if (!Get.isRegistered<AuthService>() ||
        !Get.isRegistered<UserSettingsService>()) {
      return;
    }
    final userId = Get.find<AuthService>().userId?.trim() ?? '';
    if (userId.isEmpty) {
      return;
    }
    unawaited(
      Get.find<UserSettingsService>().updatePreferredLanguage(
        locale.languageCode,
      ),
    );
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    final imService = Get.isRegistered<ImService>()
        ? Get.find<ImService>()
        : null;
    imService?.suppressConnectionBannerTemporarily();

    switch (state) {
      case AppLifecycleState.resumed:
        _cancelBackgroundSuspend();
        _realtimeSuspendedForBackground = false;
        imService?.setRealtimeAppState('foreground');
        imService?.deferSystemUnreadBadgeSyncUntilAuthoritativeRefresh();
        _ensureRealtimeConnected();
        // WS 若在后台期间保持连接，上面的 ensureConnected 是 no-op，不会有
        // 重连→pull_sync 链路来完成权威刷新；补一次对账，否则被 defer 的
        // 图标角标同步整个前台期都不会执行，陈旧角标无法清除。
        imService?.reconcileUnreadBadgeOnResume();
        // On web, immediately sync badge from current session data.
        // This corrects stale badges caused by missed/throttled pushes.
        if (kIsWeb) {
          imService?.syncSystemUnreadBadgeNow(force: true);
        }
        if (Get.isRegistered<ShareIngestService>()) {
          unawaited(Get.find<ShareIngestService>().consumePendingOnLaunch());
        }
        return;
      case AppLifecycleState.hidden:
      case AppLifecycleState.paused:
        imService?.setRealtimeAppState('background');
        if (_isVoiceCallActive()) {
          return;
        }
        if (!shouldSuspendRealtimeForBackground()) {
          return;
        }
        if (_realtimeSuspendedForBackground) {
          return;
        }
        _scheduleBackgroundSuspend(imService);
        return;
      case AppLifecycleState.detached:
        imService?.setRealtimeAppState('background');
        if (_isVoiceCallActive()) {
          return;
        }
        if (!shouldSuspendRealtimeForBackground()) {
          return;
        }
        _cancelBackgroundSuspend();
        if (_realtimeSuspendedForBackground) {
          return;
        }
        _realtimeSuspendedForBackground = true;
        imService?.suspendForAppBackground();
        return;
      case AppLifecycleState.inactive:
        return;
    }
  }

  bool _isVoiceCallActive() {
    if (!Get.isRegistered<CallController>()) {
      return false;
    }
    return Get.find<CallController>().isInCall;
  }

  void _ensureRealtimeConnected() {
    unawaited(_ensureRealtimeConnectedAsync());
  }

  Future<void> _ensureRealtimeConnectedAsync() async {
    if (!Get.isRegistered<AuthService>() || !Get.isRegistered<ImService>()) {
      return;
    }
    final authService = Get.find<AuthService>();
    if (!authService.isLoggedIn) return;
    final imService = Get.find<ImService>();
    if (!imService.isConnected) {
      final token = authService.token?.trim() ?? '';
      if (token.isEmpty) {
        final tokenOk = await authService.ensureTokenFresh();
        if (!tokenOk) {
          // 唤醒瞬间网络可能尚未就绪：刷新失败也不能吞掉本次恢复触发
          // （resume 是唤醒后最及时的触发源，丢了就要等 90 秒心跳兜底），
          // 把重连交给 IM 自己的退避循环，它每轮建连前会重新刷 token。
          imService.syncNow();
          return;
        }
      }
      imService.ensureConnected();
      if (Get.isRegistered<PushRegistrationService>()) {
        unawaited(Get.find<PushRegistrationService>().refreshBindingIfNeeded());
      }
      return;
    }
    final tokenOk = await authService.ensureTokenFresh();
    if (!tokenOk) {
      // 连接还挂着但 token 刷不动（多为唤醒后网络半就绪）：syncNow 会在
      // 连接健康时做一次拉取对账、在半死/未鉴权时强制走重连循环，两种
      // 情况都比直接放弃触发要快得多。
      imService.syncNow();
      return;
    }

    if (Get.isRegistered<PushRegistrationService>()) {
      unawaited(Get.find<PushRegistrationService>().refreshBindingIfNeeded());
    }
    if (imService.isAuthenticated) {
      imService.reAuthWithLatestToken();
    }
    imService.syncNow();
  }

  void _scheduleBackgroundSuspend(ImService? imService) {
    final delay = realtimeBackgroundSuspendDelay();
    if (delay <= Duration.zero || _backgroundSuspendTimer?.isActive == true) {
      return;
    }
    _backgroundSuspendTimer = Timer(delay, () {
      _backgroundSuspendTimer = null;
      if (_realtimeSuspendedForBackground) {
        return;
      }
      _realtimeSuspendedForBackground = true;
      imService?.suspendForAppBackground();
    });
  }

  void _cancelBackgroundSuspend() {
    _backgroundSuspendTimer?.cancel();
    _backgroundSuspendTimer = null;
  }

  @override
  Widget build(BuildContext context) {
    final themePreferenceService = Get.find<ThemePreferenceService>();

    return Obx(
      () => GetMaterialApp(
        debugShowCheckedModeBanner: false,
        title: resolveApplicationSwitcherTitle(),
        theme: AppTheme.lightTheme,
        darkTheme: AppTheme.darkTheme,
        themeMode: themePreferenceService.themeMode,
        translations: widget.translations,
        locale: widget.initialLocale ?? LocaleService.fallbackLocale,
        fallbackLocale: LocaleService.fallbackLocale,
        localizationsDelegates: AppMaterialLocalizations.delegates,
        supportedLocales: AppMaterialLocalizations.supportedLocales,
        scrollBehavior: const AppScrollBehavior(),
        initialRoute: widget.initialRoute,
        getPages: AppRoutes.routes,
        navigatorObservers: [appRouteObserver],
        builder: (context, child) {
          final page = child ?? const SizedBox.shrink();
          if (!Get.isRegistered<AuthService>() ||
              !Get.isRegistered<ImService>()) {
            return page;
          }
          final authService = Get.find<AuthService>();
          final imService = Get.find<ImService>();

          return Obx(() {
            // CallController 在 Obx 内解析：保证每次重建都重新取，避免在响应式作用域外
            // 捕获到一个 null(冷启动首帧尚未注册)后再也订阅不上通话状态。
            final callCtrl = Get.isRegistered<CallController>()
                ? Get.find<CallController>()
                : null;
            // 通话 overlay 状态
            final showCallOverlay =
                callCtrl?.isActiveCallOverlayVisible ?? false;
            final isMinimized = callCtrl?.isMinimized ?? false;

            // 通话进行中：手机端全屏独占；电脑/网页宽屏改为居中的悬浮通话窗，
            // 避免把手机版全屏深色页当遮罩铺满整个窗口（表现为"整个后台灰屏"）。
            if (showCallOverlay && !isMinimized) {
              final screen = MediaQuery.of(context).size;
              final isWide = screen.width >= 720;
              if (!isWide) {
                return Stack(
                  children: [
                    page,
                    const Positioned.fill(child: ActiveCallOverlay()),
                  ],
                );
              }
              const windowWidth = 420.0;
              final maxWindowHeight = screen.height * 0.9;
              final windowHeight = maxWindowHeight < 640.0
                  ? maxWindowHeight
                  : 640.0;
              // 通话布局固有高度约 560；矮窗(高<560)时给内容保底展开高度并可滚动，
              // 避免定高盒子把底部挂断键裁掉导致挂不了断。
              final contentHeight = windowHeight < 560.0 ? 560.0 : windowHeight;
              return Stack(
                children: [
                  page,
                  Center(
                    child: Container(
                      decoration: BoxDecoration(
                        borderRadius: BorderRadius.circular(20),
                        boxShadow: [
                          BoxShadow(
                            color: Colors.black.withValues(alpha: 0.4),
                            blurRadius: 24,
                            offset: const Offset(0, 8),
                          ),
                        ],
                      ),
                      child: ClipRRect(
                        borderRadius: BorderRadius.circular(20),
                        child: SizedBox(
                          width: windowWidth,
                          height: windowHeight,
                          child: SingleChildScrollView(
                            child: SizedBox(
                              height: contentHeight,
                              child: const ActiveCallOverlay(),
                            ),
                          ),
                        ),
                      ),
                    ),
                  ),
                ],
              );
            }

            // 非全屏状态：检查是否需要显示其他横幅
            final showBanner =
                authService.isLoggedIn && imService.shouldShowConnectionBanner;
            final showNotification =
                authService.isLoggedIn &&
                Get.isRegistered<InAppNotificationService>() &&
                Get.find<InAppNotificationService>()
                        .currentNotification
                        .value !=
                    null;

            if (!showBanner && !showNotification && !showCallOverlay) {
              return page;
            }

            // 构建 overlay 层，从上到下排列各横幅
            final safeTop = MediaQuery.of(context).padding.top + 8;
            // 通话横幅高度 48 + 间距 8
            final callBannerExtra = showCallOverlay ? 56.0 : 0.0;
            // 连接横幅高度约 44 + 间距 8
            final connBannerExtra = showBanner ? 52.0 : 0.0;

            return Stack(
              children: [
                page,
                // 折叠通话横幅（最顶层）
                if (showCallOverlay)
                  Positioned(
                    top: safeTop,
                    left: 12,
                    right: 12,
                    child: const CollapsedCallBanner(),
                  ),
                // 连接状态横幅
                if (showBanner)
                  Positioned(
                    top: safeTop + callBannerExtra,
                    left: 12,
                    right: 12,
                    child: Material(
                      color: Colors.transparent,
                      child: Container(
                        padding: const EdgeInsets.symmetric(
                          horizontal: 12,
                          vertical: 8,
                        ),
                        decoration: BoxDecoration(
                          color: const Color(0xFF7A4A00),
                          borderRadius: BorderRadius.circular(10),
                          boxShadow: [
                            BoxShadow(
                              color: Colors.black.withValues(alpha: 0.18),
                              blurRadius: 10,
                              offset: const Offset(0, 4),
                            ),
                          ],
                        ),
                        child: Row(
                          children: [
                            const Icon(
                              Icons.wifi_tethering_error_rounded,
                              size: 16,
                              color: Colors.white,
                            ),
                            const SizedBox(width: 8),
                            Expanded(
                              child: Text(
                                imService.connectionBannerTextKey.tr,
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: const TextStyle(
                                  color: Colors.white,
                                  fontSize: 12,
                                  fontWeight: FontWeight.w500,
                                ),
                              ),
                            ),
                            const SizedBox(width: 8),
                            GestureDetector(
                              onTap: imService.syncNow,
                              child: Text(
                                'common_retry'.tr,
                                style: const TextStyle(
                                  color: Colors.white,
                                  fontSize: 12,
                                  fontWeight: FontWeight.w700,
                                ),
                              ),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                // 通知横幅
                if (showNotification)
                  Positioned(
                    top: safeTop + callBannerExtra + connBannerExtra,
                    left: 12,
                    right: 12,
                    child: const InAppNotificationBanner(),
                  ),
              ],
            );
          });
        },
      ),
    );
  }
}
