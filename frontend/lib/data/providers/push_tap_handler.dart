import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';
import 'package:sentry_flutter/sentry_flutter.dart';

import '../../app/routes/app_routes.dart';
import '../../modules/chat/services/chat_route_navigator.dart';
import '../models/session_model.dart';
import 'auth_service.dart';
import 'im_service.dart';
import 'session_service.dart';

class PushTapHandler extends GetxService {
  PushTapHandler({
    AuthService? authService,
    ImService? imService,
    SessionService? sessionService,
  }) : _authService = authService ?? Get.find<AuthService>(),
       _imService = imService ?? Get.find<ImService>(),
       _sessionService = sessionService ?? Get.find<SessionService>();

  final AuthService _authService;
  final ImService _imService;
  final SessionService _sessionService;

  static const _channel = MethodChannel('pub.dhf.grix/push_tap');

  Worker? _loginStateWorker;
  Map<String, String>? _pendingTapPayload;
  Map<String, String>? _queuedTapPayload;
  bool _navigating = false;

  Future<PushTapHandler> init() async {
    _channel.setMethodCallHandler(_onMethodCall);
    _loginStateWorker = ever<bool>(_authService.isLoggedInRx, (isLoggedIn) {
      if (!isLoggedIn) return;
      _consumePending();
    });

    if (kIsWeb) {
      _checkWebUrlParameter();
    }

    return this;
  }

  @override
  void onClose() {
    _loginStateWorker?.dispose();
    _loginStateWorker = null;
    super.onClose();
  }

  Future<void> _onMethodCall(MethodCall call) async {
    if (call.method == 'onPushTapped') {
      final args = call.arguments as Map<dynamic, dynamic>?;
      final sessionId = args?['session_id']?.toString().trim() ?? '';
      final messageId = args?['message_id']?.toString().trim() ?? '';
      final recipientId = args?['recipient_id']?.toString().trim() ?? '';
      _bc('tap payload received', {
        'session_id': sessionId,
        'message_id': messageId,
        'recipient_id': recipientId,
      });
      if (sessionId.isNotEmpty) {
        await _navigateToSession(
          sessionId,
          messageId: messageId,
          recipientId: recipientId,
        );
      }
    }
  }

  void _bc(String message, [Map<String, dynamic>? data]) {
    Sentry.addBreadcrumb(
      Breadcrumb(
        category: 'push_tap',
        message: message,
        data: data,
        level: SentryLevel.info,
      ),
    );
    debugPrint('[PushTap] $message ${data ?? ''}');
  }

  Future<void> _navigateToSession(
    String sessionId, {
    String? messageId,
    String? recipientId,
  }) async {
    final normalizedMessageId = messageId?.trim() ?? '';
    final normalizedRecipientId = recipientId?.trim() ?? '';
    if (_navigating) {
      _queuedTapPayload = {
        'session_id': sessionId,
        'message_id': normalizedMessageId,
        'recipient_id': normalizedRecipientId,
      };
      _bc('navigate queued: already navigating', {
        'session_id': sessionId,
        'message_id': normalizedMessageId,
      });
      return;
    }

    if (!_authService.isLoggedIn) {
      _pendingTapPayload = {
        'session_id': sessionId,
        'message_id': normalizedMessageId,
        'recipient_id': normalizedRecipientId,
      };
      _bc('queued session (not logged in)', {
        'session_id': sessionId,
        'message_id': normalizedMessageId,
      });
      return;
    }

    // 账号守卫：推送携带目标账号时，若与当前登录账号不一致，直接忽略。
    // 典型场景：A 账号离线消息在切换到 B 账号后才送达，点击不应打开 B 无权访问的会话。
    final currentUserId = _authService.userId?.trim() ?? '';
    if (normalizedRecipientId.isNotEmpty &&
        currentUserId.isNotEmpty &&
        normalizedRecipientId != currentUserId) {
      _bc('ignore tap: recipient mismatch', {
        'session_id': sessionId,
        'recipient_id': normalizedRecipientId,
        'current_user_id': currentUserId,
      });
      return;
    }

    _navigating = true;
    _bc('start navigate', {'session_id': sessionId, 'message_id': normalizedMessageId});
    try {
      // 冷启动时导航器可能尚未初始化，等待其就绪（最多 3s）。
      if (Get.key.currentState == null) {
        _bc('navigator not ready, waiting');
        for (var i = 0; i < 30; i++) {
          await Future<void>.delayed(const Duration(milliseconds: 100));
          if (Get.key.currentState != null) break;
        }
        if (Get.key.currentState == null) {
          _bc('navigator still not ready after timeout, aborting');
          return;
        }
        _bc('navigator ready');
      }

      // 快速路径：已在目标会话页，直接确保消息加载即可。
      if (_isOnTargetSession(sessionId)) {
        _bc('already on target session, skip navigation');
        await _ensureMessageLoadedAfterNavigation(
          sessionId: sessionId,
          messageId: normalizedMessageId,
        );
        return;
      }

      // 后台触发 WS 连接，但不串行等待。
      // 推送跳转只需 HTTP 获取 session 详情 + 本地 DB 加载消息即可。
      _ensureImServiceConnectingInBackground();

      // 冷启动：等 splash 路由消失（最多 1.5s）。
      final current = Get.currentRoute;
      if (current == AppRoutes.splash || current == '/') {
        _bc('waiting for splash to resolve');
        for (var i = 0; i < 15; i++) {
          if (Get.currentRoute.startsWith(AppRoutes.home)) break;
          await Future<void>.delayed(const Duration(milliseconds: 100));
        }
        _bc('splash wait done', {'route': Get.currentRoute});
      }

      // 等待后再检查——用户可能已手动打开目标会话。
      if (_isOnTargetSession(sessionId)) {
        _bc('already on target session after waits, skip navigation');
        await _ensureMessageLoadedAfterNavigation(
          sessionId: sessionId,
          messageId: normalizedMessageId,
        );
        return;
      }

      // 如果在非 home/chat 路由（如设置页），跳回 home。
      final routeAfterWaits = Get.currentRoute;
      final onChatRoute = routeAfterWaits.startsWith(AppRoutes.chat);
      final onHomeRoute = routeAfterWaits.startsWith(AppRoutes.home);
      if (!onHomeRoute && !onChatRoute) {
        _bc('forcing offAllNamed home', {'from_route': routeAfterWaits});
        Get.offAllNamed(AppRoutes.home);
        // 只让出一帧让路由生效，不等固定 300ms。
        await Future<void>.delayed(const Duration(milliseconds: 50));
        _bc('home settled', {'route': Get.currentRoute});
      }

      // 查找 session：先本地 → 找不到直接单会话 API 获取。
      var session = _imService.findSessionById(sessionId);
      if (session == null) {
        _bc('session not in local cache, fetching via API');
        final result = await _sessionService.fetchSessionDetailResult(
          sessionId,
        );
        _bc('API result', {
          'code': result.code,
          'has_data': result.data != null,
        });
        if (result.data != null && result.code == 0) {
          final title = result.data!['title']?.toString() ?? '';
          final type = result.data!['type']?.toString() ?? 'private';
          await _navigateToChatAndEnsureMessages(
            sessionId: sessionId,
            title: title,
            type: type,
            messageId: normalizedMessageId,
          );
          return;
        }
        // API 也拿不到，记日志。
        _bc('session not found via API');
        Sentry.captureMessage(
          'PushTap: session not found via API, sid=$sessionId code=${result.code}',
          level: SentryLevel.warning,
        );
        return;
      }

      // 本地已有 session，直接导航。
      _bc('navigating to chat (local)', {
        'session_id': session.sessionId,
        'type': session.type,
      });
      await _navigateToChatAndEnsureMessages(
        sessionId: session.sessionId,
        title: _resolveTitle(session),
        type: session.type,
        messageId: normalizedMessageId,
      );
    } catch (e, st) {
      _bc('navigate error', {'error': '$e'});
      Sentry.captureException(e, stackTrace: st);
    } finally {
      _navigating = false;
      _bc('navigate done', {'route': Get.currentRoute});
      _consumeQueuedTapIfAny();
    }
  }

  /// 导航到聊天页并确保消息已加载的统一入口。
  Future<void> _navigateToChatAndEnsureMessages({
    required String sessionId,
    required String title,
    required String type,
    required String messageId,
  }) async {
    await ChatRouteNavigator.toChat(
      sessionId: sessionId,
      title: title,
      type: type,
    );
    await _ensureMessageLoadedAfterNavigation(
      sessionId: sessionId,
      messageId: messageId,
    );
    await _ensureNonEmptyWindowAfterNavigation(
      sessionId: sessionId,
      messageId: messageId,
    );
    _bc('chat navigation complete', {'route': Get.currentRoute});
  }

  /// 当前是否已在目标会话页。
  bool _isOnTargetSession(String sessionId) {
    return Get.currentRoute.startsWith(AppRoutes.chat) &&
        _imService.currentSessionId == sessionId;
  }

  /// 后台触发 WS 连接（不阻塞当前流程）。
  void _ensureImServiceConnectingInBackground() {
    if (_imService.isConnected) return;
    _bc('triggering WS connect in background');
    _imService.ensureConnected();
  }

  String _resolveTitle(SessionModel session) {
    if (session.title.isNotEmpty) return session.title;
    if (session.peerNickname.isNotEmpty) return session.peerNickname;
    if (session.peerUsername.isNotEmpty) return session.peerUsername;
    return '';
  }

  void _consumePending() {
    final payload = _pendingTapPayload;
    if (payload == null) return;
    _pendingTapPayload = null;
    final sid = payload['session_id']?.trim() ?? '';
    if (sid.isEmpty) return;
    final messageId = payload['message_id']?.trim();
    final recipientId = payload['recipient_id']?.trim();
    unawaited(
      _navigateToSession(sid, messageId: messageId, recipientId: recipientId),
    );
  }

  void _consumeQueuedTapIfAny() {
    final payload = _queuedTapPayload;
    if (payload == null) return;
    _queuedTapPayload = null;
    final sid = payload['session_id']?.trim() ?? '';
    if (sid.isEmpty) return;
    final messageId = payload['message_id']?.trim();
    final recipientId = payload['recipient_id']?.trim();
    unawaited(
      _navigateToSession(sid, messageId: messageId, recipientId: recipientId),
    );
  }

  void _checkWebUrlParameter() {
    try {
      final uri = Uri.base;
      final sessionId = uri.queryParameters['session_id']?.trim() ?? '';
      if (sessionId.isEmpty) return;
      final messageId = uri.queryParameters['message_id']?.trim();
      final recipientId = uri.queryParameters['recipient_id']?.trim();
      unawaited(
        _navigateToSession(
          sessionId,
          messageId: messageId,
          recipientId: recipientId,
        ),
      );
    } catch (e) {
      debugPrint('[PushTap] web url check error: $e');
    }
  }

  /// 确保目标消息已加载到当前窗口中。
  /// 不再固定等 320ms，改为短暂 yield + 条件检查。
  Future<void> _ensureMessageLoadedAfterNavigation({
    required String sessionId,
    required String messageId,
  }) async {
    if (messageId.isEmpty) return;

    final sid = sessionId.trim();
    if (sid.isEmpty) return;

    // 聊天页 enterSession 会等转场动画(约 330ms)后才开始读 DB 加载消息。
    // 这里用轮询等待覆盖该初始加载延迟：命中即返回，避免在初始加载尚未发生时
    // 误判“消息缺失”而触发不必要的 forceReload，又能在加载完成后尽早返回。
    if (await _pollUntil(
      () =>
          _imService.currentSessionId == sid &&
          _imService.hasMessageInCurrentWindow(messageId),
    )) {
      _bc('target message already loaded', {
        'session_id': sid,
        'message_id': messageId,
      });
      return;
    }

    _bc('target message missing, force reload', {
      'session_id': sid,
      'message_id': messageId,
      'current_session_id': _imService.currentSessionId ?? '',
      'current_message_count': _imService.currentMessages.length,
      'ws_connected': _imService.isConnected,
      'ws_authenticated': _imService.isAuthenticated,
    });
    await _imService.forceReloadSessionWindow(sid, triggerPullSync: true);

    final loaded =
        _imService.currentSessionId == sid &&
        _imService.hasMessageInCurrentWindow(messageId);
    if (!loaded) {
      _bc('target message still missing after force reload', {
        'session_id': sid,
        'message_id': messageId,
        'current_session_id': _imService.currentSessionId ?? '',
        'current_message_count': _imService.currentMessages.length,
        'ws_connected': _imService.isConnected,
        'ws_authenticated': _imService.isAuthenticated,
      });
      Sentry.captureMessage(
        'PushTapFallback: still missing sid=$sid mid=$messageId '
        'current=${_imService.currentSessionId ?? ''} '
        'count=${_imService.currentMessages.length} '
        'ws=${_imService.isConnected}/${_imService.isAuthenticated}',
        level: SentryLevel.warning,
      );
    } else {
      _bc('target message loaded after force reload', {
        'session_id': sid,
        'message_id': messageId,
      });
    }
  }

  /// 确保打开聊天页后窗口不为空（当 messageId 为空时）。
  /// 同样用轮询覆盖聊天页的初始加载延迟，避免误判触发 forceReload。
  Future<void> _ensureNonEmptyWindowAfterNavigation({
    required String sessionId,
    required String messageId,
  }) async {
    if (messageId.isNotEmpty) return;
    final sid = sessionId.trim();
    if (sid.isEmpty) return;

    // 轮询等待初始加载产出消息；一旦窗口非空即返回。
    final hasMessages = await _pollUntil(
      () =>
          _imService.currentSessionId == sid &&
          _imService.currentMessages.isNotEmpty,
    );
    if (hasMessages) return;

    // 超时仍为空：可能本地确无数据，触发兜底重载。
    if (_imService.currentSessionId != sid) return;

    _bc('empty window without message_id, force reload', {
      'session_id': sid,
      'current_session_id': _imService.currentSessionId ?? '',
      'ws_connected': _imService.isConnected,
      'ws_authenticated': _imService.isAuthenticated,
    });
    await _imService.forceReloadSessionWindow(sid, triggerPullSync: true);
  }

  /// 在覆盖聊天页初始加载延迟(约 330ms)的时间窗内轮询条件。
  /// 命中返回 true；超时返回 false。最多 ~720ms(12 × 60ms)。
  Future<bool> _pollUntil(bool Function() condition) async {
    for (var i = 0; i < 12; i++) {
      if (condition()) return true;
      await Future<void>.delayed(const Duration(milliseconds: 60));
    }
    return condition();
  }
}
