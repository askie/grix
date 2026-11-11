import 'dart:async';

import 'package:flutter/material.dart';

import 'grix_config.dart';
import 'grix_url_builder.dart';
import 'grix_visitor_store.dart';
import 'grix_widget_controller.dart';
import 'grix_widget_view.dart';

/// Unified entry point for the Grix Website Widget Flutter SDK.
class GrixWidget {
  GrixWidget._();

  static GrixConfig? _config;

  /// Optional storage override for tests or hosts that manage identity.
  static GrixVisitorStore visitorStore = SharedPreferencesGrixVisitorStore();

  /// Fired when the visitor taps the page close button or the host calls
  /// [GrixWidgetController.close].
  static void Function()? onClose;

  /// Fired whenever the hosted page reports a new unread count.
  static void Function(int count)? onUnreadChange;

  /// Fired when the server closes or bans the visitor session.
  static void Function(String reason)? onSessionClosed;

  /// Fired for initialization or runtime errors reported by the hosted page.
  static void Function(String code, String message)? onError;

  /// Optional route observer used to keep WebView visibility in sync.
  ///
  /// Add this to `MaterialApp.navigatorObservers` for the most accurate
  /// visible/hidden notifications when routes are pushed or popped.
  static final RouteObserver<PageRoute<dynamic>> routeObserver =
      RouteObserver<PageRoute<dynamic>>();

  static final ValueNotifier<int> _unread = ValueNotifier<int>(0);
  static OverlayEntry? _launcherEntry;

  static GrixConfig? get config => _config;

  /// Configures the SDK. Call this once before [present] or [makeView].
  static void configure({
    required String siteKey,
    String apiBase = GrixConfig.defaultApiBase,
    String? locale,
    String? themeColor,
    String? title,
    GrixUser? user,
    String? appIdOverride,
  }) {
    _config = GrixConfig(
      siteKey: siteKey,
      apiBase: apiBase,
      locale: locale,
      themeColor: themeColor,
      title: title,
      user: user,
      appIdOverride: appIdOverride,
    );
    if (_launcherEntry != null) {
      _launcherEntry?.markNeedsBuild();
    }
  }

  /// Returns an embeddable widget that can live inside any host layout.
  static Widget makeView({Key? key, GrixWidgetController? controller}) {
    final cfg = _config;
    if (cfg == null) {
      const message =
          'GrixWidget.configure(...) must be called before makeView().';
      onError?.call('not_configured', message);
      throw StateError(message);
    }
    return GrixWidgetView(
      key: key,
      config: cfg,
      controller: controller,
      autoPopOnClose: false,
    );
  }

  /// Pushes a full-screen route containing the hosted widget.
  static Future<void> present(
    BuildContext context, {
    GrixWidgetController? controller,
  }) async {
    final cfg = _config;
    if (cfg == null) {
      const message =
          'GrixWidget.configure(...) must be called before present().';
      onError?.call('not_configured', message);
      return;
    }
    await Navigator.of(context).push<void>(
      MaterialPageRoute<void>(
        builder: (_) => GrixWidgetView(
          config: cfg,
          controller: controller,
          autoPopOnClose: true,
          useSafeArea: true,
        ),
      ),
    );
  }

  /// Shows or hides a floating chat launcher in the current navigator overlay.
  ///
  /// The badge is fed by the hosted page's `unread` events and the button color
  /// follows the configured `themeColor`.
  static void setLauncherVisible(BuildContext context, bool visible) {
    final cfg = _config;
    if (visible == false || cfg == null) {
      _launcherEntry?.remove();
      _launcherEntry = null;
      if (visible && cfg == null) {
        onError?.call(
          'not_configured',
          'GrixWidget.configure(...) must be called before setLauncherVisible().',
        );
      }
      return;
    }
    if (_launcherEntry != null) return;

    final overlay = Overlay.of(context, rootOverlay: true);
    final entry = OverlayEntry(
      builder: (_) => Positioned(
        right: 20,
        bottom: 20,
        child: SafeArea(
          child: ValueListenableBuilder<int>(
            valueListenable: _unread,
            builder: (_, count, __) {
              final currentConfig = _config;
              final color =
                  currentConfig?.resolvedThemeColor ?? const Color(0xFF0F766E);
              return Material(
                color: color,
                elevation: 6,
                shape: const CircleBorder(),
                child: InkWell(
                  customBorder: const CircleBorder(),
                  onTap: () => unawaited(present(context)),
                  child: SizedBox(
                    width: 56,
                    height: 56,
                    child: Stack(
                      clipBehavior: Clip.none,
                      children: [
                        const Center(
                          child: Icon(
                            Icons.chat_bubble_rounded,
                            color: Colors.white,
                            size: 26,
                          ),
                        ),
                        if (count > 0)
                          Positioned(
                            top: -4,
                            right: -4,
                            child: Container(
                              constraints: const BoxConstraints(
                                minWidth: 20,
                                minHeight: 20,
                              ),
                              padding: const EdgeInsets.symmetric(
                                horizontal: 5,
                              ),
                              decoration: const BoxDecoration(
                                color: Color(0xFFEF4444),
                                shape: BoxShape.circle,
                              ),
                              alignment: Alignment.center,
                              child: Text(
                                count > 99 ? '99+' : '$count',
                                style: const TextStyle(
                                  color: Colors.white,
                                  fontSize: 11,
                                  fontWeight: FontWeight.bold,
                                ),
                              ),
                            ),
                          ),
                      ],
                    ),
                  ),
                ),
              );
            },
          ),
        ),
      ),
    );
    _launcherEntry = entry;
    overlay.insert(entry);
  }

  /// Delivers unread counts reported by `GrixWidgetView`.
  static void updateUnread(int count) {
    final normalized = count < 0 ? 0 : count;
    _unread.value = normalized;
    onUnreadChange?.call(normalized);
  }

  /// Builds the current app URL. Exposed for diagnostics and tests.
  static String buildUrlForTesting({
    required String appId,
    String? visitorKey,
  }) {
    final cfg = _config;
    if (cfg == null) {
      throw StateError('GrixWidget.configure(...) must be called first.');
    }
    return GrixUrlBuilder.buildAppUrl(
      config: cfg,
      hostPackageName: appId,
      visitorKey: visitorKey,
    );
  }
}
