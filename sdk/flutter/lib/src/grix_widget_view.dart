import 'dart:async';

import 'package:flutter/material.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:permission_handler/permission_handler.dart';
import 'package:url_launcher/url_launcher.dart';
import 'package:webview_flutter/webview_flutter.dart';

import 'grix_bridge_event.dart';
import 'grix_config.dart';
import 'grix_url_builder.dart';
import 'grix_visitor_store.dart';
import 'grix_widget.dart';
import 'grix_widget_controller.dart';

/// Embeddable WebView that hosts the Grix mobile widget page.
///
/// The hosted page owns all chat UI and transport logic. This widget owns the
/// `GrixFlutter` JavaScript bridge, visitor-key persistence, visibility
/// notifications, microphone permission delegation, and external-link opening.
class GrixWidgetView extends StatefulWidget {
  const GrixWidgetView({
    super.key,
    required this.config,
    this.controller,
    this.autoPopOnClose = false,
    this.useSafeArea = false,
  });

  final GrixConfig config;
  final GrixWidgetController? controller;
  final bool autoPopOnClose;
  final bool useSafeArea;

  @override
  State<GrixWidgetView> createState() => _GrixWidgetViewState();
}

class _GrixWidgetViewState extends State<GrixWidgetView>
    with WidgetsBindingObserver, RouteAware {
  late final WebViewController _webViewController;
  final GrixVisitorStore _visitorStore = GrixWidget.visitorStore;

  PageRoute<dynamic>? _subscribedRoute;
  bool _routeVisible = true;
  bool _appVisible = true;
  bool _pageLoaded = false;
  String _lastOpenedUrl = '';
  DateTime? _lastOpenedAt;

  bool get _isVisible => _routeVisible && _appVisible;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    widget.controller?.attach(setVisible: _notifyWebVisibility, close: _close);
    _webViewController = WebViewController(
      onPermissionRequest: _handlePermissionRequest,
    );
    unawaited(_initialize());
  }

  Future<void> _initialize() async {
    await _webViewController.setJavaScriptMode(JavaScriptMode.unrestricted);
    await _webViewController.setBackgroundColor(Colors.white);
    await _webViewController.addJavaScriptChannel(
      'GrixFlutter',
      onMessageReceived: (message) {
        unawaited(_handleRawBridgeMessage(message.message));
      },
    );
    await _webViewController.setNavigationDelegate(
      NavigationDelegate(
        onNavigationRequest: _onNavigationRequest,
        onPageFinished: (_) {
          _pageLoaded = true;
          _pushVisibilityToPage();
        },
      ),
    );

    String hostPackageName = '';
    try {
      final info = await PackageInfo.fromPlatform();
      hostPackageName = info.packageName;
    } catch (_) {
      GrixWidget.onError?.call(
        'package_info_failed',
        'Unable to read the host bundle id / package name.',
      );
    }

    String? visitorKey;
    try {
      visitorKey = await _visitorStore.read(widget.config.normalizedSiteKey);
    } catch (_) {
      visitorKey = null;
    }

    final url = GrixUrlBuilder.buildAppUrl(
      config: widget.config,
      hostPackageName: hostPackageName,
      visitorKey: visitorKey,
    );
    await _webViewController.loadRequest(Uri.parse(url));
  }

  Future<void> _handleRawBridgeMessage(String raw) async {
    final event = GrixBridgeEvent.parse(raw);
    if (event == null) return;

    if (event is GrixReadyEvent) {
      if (event.visitorKey.isNotEmpty) {
        try {
          await _visitorStore.write(
            widget.config.normalizedSiteKey,
            event.visitorKey,
          );
        } catch (_) {
          // Persistence failure must not break the chat.
        }
      }
      _pushVisibilityToPage();
      return;
    }

    if (event is GrixCloseEvent) {
      GrixWidget.onClose?.call();
      if (widget.autoPopOnClose && mounted) {
        await Navigator.of(context).maybePop();
      }
      return;
    }

    if (event is GrixSessionClosedEvent) {
      GrixWidget.onSessionClosed?.call(event.reason);
      return;
    }

    if (event is GrixUnreadEvent) {
      GrixWidget.updateUnread(event.count);
      return;
    }

    if (event is GrixOpenUrlEvent) {
      await _openExternal(event.url);
      return;
    }

    if (event is GrixErrorEvent) {
      GrixWidget.onError?.call(event.code, event.message);
    }
  }

  NavigationDecision _onNavigationRequest(NavigationRequest request) {
    final target = Uri.tryParse(request.url);
    if (target == null) return NavigationDecision.navigate;
    final scheme = target.scheme.toLowerCase();
    if (scheme != 'http' && scheme != 'https') {
      return NavigationDecision.navigate;
    }
    final apiBase = Uri.tryParse(widget.config.normalizedApiBase);
    final isApiOrigin = apiBase != null &&
        target.host == apiBase.host &&
        target.port == apiBase.port;
    if (isApiOrigin) return NavigationDecision.navigate;
    unawaited(_openExternal(request.url));
    return NavigationDecision.prevent;
  }

  Future<void> _handlePermissionRequest(
    WebViewPermissionRequest request,
  ) async {
    // webview_flutter's unified WebViewPermissionRequest does not expose the
    // requesting origin. Origin isolation is enforced by the navigation
    // delegate above, which only keeps apiBase-origin pages in this WebView.
    final wantsMicrophone = request.types.contains(
      WebViewPermissionResourceType.microphone,
    );
    if (wantsMicrophone == false) {
      await request.deny();
      return;
    }
    try {
      final status = await Permission.microphone.request();
      if (status.isGranted) {
        await request.grant();
      } else {
        await request.deny();
      }
    } catch (_) {
      await request.deny();
    }
  }

  Future<void> _openExternal(String rawUrl) async {
    final uri = Uri.tryParse(rawUrl.trim());
    if (uri == null) return;
    final scheme = uri.scheme.toLowerCase();
    if (scheme != 'http' && scheme != 'https') return;

    final now = DateTime.now();
    final lastOpenedAt = _lastOpenedAt;
    if (rawUrl == _lastOpenedUrl &&
        lastOpenedAt != null &&
        now.difference(lastOpenedAt) < const Duration(seconds: 1)) {
      return;
    }
    _lastOpenedUrl = rawUrl;
    _lastOpenedAt = now;
    try {
      await launchUrl(uri, mode: LaunchMode.externalApplication);
    } catch (_) {
      // No browser available; the chat page remains untouched.
    }
  }

  void _pushVisibilityToPage() {
    if (_pageLoaded == false) return;
    final visibleLiteral = _isVisible ? 'true' : 'false';
    unawaited(
      _webViewController
          .runJavaScript(
            "window.grixWidget && typeof window.grixWidget.setVisible === 'function' && window.grixWidget.setVisible($visibleLiteral);",
          )
          .catchError((_) {}),
    );
  }

  void _notifyWebVisibility(bool visible) {
    if (visible) {
      _routeVisible = true;
      _appVisible = true;
    } else {
      _routeVisible = false;
    }
    _pushVisibilityToPage();
  }

  void _close() {
    unawaited(
      _webViewController
          .runJavaScript(
            "window.grixWidget && typeof window.grixWidget.close === 'function' && window.grixWidget.close();",
          )
          .catchError((_) {}),
    );
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final route = ModalRoute.of(context);
    if (route is PageRoute<dynamic> && route != _subscribedRoute) {
      if (_subscribedRoute != null) {
        GrixWidget.routeObserver.unsubscribe(this);
      }
      _subscribedRoute = route;
      try {
        GrixWidget.routeObserver.subscribe(this, route);
      } catch (_) {
        // The host did not register GrixWidget.routeObserver. Lifecycle
        // visibility still works, and the route remains usable.
      }
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _appVisible = state == AppLifecycleState.resumed;
    _pushVisibilityToPage();
  }

  @override
  void didPush() {
    _routeVisible = true;
    _pushVisibilityToPage();
  }

  @override
  void didPopNext() {
    _routeVisible = true;
    _pushVisibilityToPage();
  }

  @override
  void didPushNext() {
    _routeVisible = false;
    _pushVisibilityToPage();
  }

  @override
  void didPop() {
    _routeVisible = false;
    _pushVisibilityToPage();
  }

  @override
  void dispose() {
    _appVisible = false;
    _routeVisible = false;
    _pushVisibilityToPage();
    WidgetsBinding.instance.removeObserver(this);
    if (_subscribedRoute != null) {
      GrixWidget.routeObserver.unsubscribe(this);
    }
    widget.controller?.detach();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final webView = WebViewWidget(controller: _webViewController);
    return widget.useSafeArea ? SafeArea(child: webView) : webView;
  }
}
