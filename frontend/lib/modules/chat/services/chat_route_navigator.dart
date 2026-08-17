import 'dart:async';

import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../app/routes/app_routes.dart';
import '../../../data/providers/im_service.dart';
import '../../../data/providers/session_service.dart';
import '../../../shared/models/session_avatar_member.dart';
import '../bindings/chat_binding.dart';
import '../chat_view.dart';
import '../controllers/chat_controller.dart';
import '../private_chat_creating_route.dart';
import '../private_chat_creating_view.dart';
import 'private_chat_open_perf_logger.dart';

class ChatRouteNavigator {
  const ChatRouteNavigator._();

  /// Test/debug only: hold on the creating shell before `createSession` returns
  /// so the status ellipsis can be observed. Production must stay [Duration.zero].
  @visibleForTesting
  static Duration debugHoldBeforeCreateSession = Duration.zero;

  /// The creating shell has already provided the route transition feedback.
  /// Replace it atomically so a stalled second animation cannot leave the
  /// creating shell and the real chat page visible side by side.
  @visibleForTesting
  static GetPageRoute<T> buildCreatingToChatReplacementRoute<T>({
    required Map<String, dynamic> arguments,
    required Map<String, String> parameters,
  }) {
    final routeName = Uri(
      path: AppRoutes.chat,
      queryParameters: parameters,
    ).toString();
    return GetPageRoute<T>(
      page: () => ChatView(controllerTag: ChatBinding.currentControllerTag()),
      parameter: parameters,
      settings: RouteSettings(name: routeName, arguments: arguments),
      binding: ChatBinding(),
      transition: Transition.noTransition,
      transitionDuration: Duration.zero,
      showCupertinoParallax: false,
      opaque: false,
    );
  }

  /// 统一的"新建私聊会话并进入"入口。
  ///
  /// 后端 `createSession(peerId, peerType)` 返回的就是当前用户与该对端的私聊会话，
  /// 是权威结果，直接据此跳转即可，不在跳转前再做核对 / 全量会话刷新（那会让进入
  /// 对话页阻塞数秒）。会话列表的对账放到跳转之后后台增量完成。
  ///
  /// 跳转场景会先进入轻量的创建中页面，再后台请求真实 sessionId，避免网络 RTT
  /// 阻塞首次路由反馈。返回真实 sessionId；建会话失败返回 null（由调用方提示）。
  ///
  /// [openChat] 为 false 时只建会话、不跳转，供只需要 sessionId 的调用方使用。
  ///
  /// [replaceCurrentRoute] 用于从 bottom sheet 发起创建：先用创建中页面替换 sheet，
  /// 成功后再原位替换成聊天页，返回时不会重新露出旧 sheet。
  static Future<String?> createAndOpenPrivateChat({
    required String peerId,
    required int peerType,
    String fallbackTitle = '',
    bool openChat = true,
    bool replaceCurrentRoute = false,
  }) async {
    final pid = peerId.trim();
    if (pid.isEmpty || peerType <= 0) {
      return null;
    }
    if (!Get.isRegistered<SessionService>() || !Get.isRegistered<ImService>()) {
      return null;
    }
    final sessionService = Get.find<SessionService>();
    final imService = Get.find<ImService>();
    final title = fallbackTitle.trim();
    final openPerfTrace = PrivateChatOpenPerfLogger.start(
      peerId: pid,
      peerType: peerType,
    );
    Future<void>? creatingRouteReady;
    PrivateChatCreationDraft? creationDraft;

    if (openChat && Get.key.currentState != null) {
      creationDraft = PrivateChatCreationDraft();
      final arguments = <String, dynamic>{
        'title': title,
        'creation_draft': creationDraft,
        PrivateChatOpenPerfLogger.argumentKey: openPerfTrace,
      };
      PrivateChatOpenPerfLogger.mark(
        openPerfTrace,
        'creating_route_push_start',
      );
      // Use a dedicated PageRoute with allowSnapshotting:false. GetX GetPageRoute
      // cannot turn that off; named noTransition alone still left a frozen
      // quarter-arc on device (SnapshotWidget / muted overlay tickers).
      final navigator = Get.key.currentState!;
      final route = PrivateChatCreatingRoute(arguments: arguments);
      final routeFuture = replaceCurrentRoute
          ? navigator.pushReplacement<void, void>(route)
          : navigator.push<void>(route);
      unawaited(routeFuture);
      // Zero-duration route: yield one frame so createSession completion cannot
      // race the first paint of the creating shell.
      creatingRouteReady = Future<void>.delayed(Duration.zero).then((_) {
        PrivateChatOpenPerfLogger.mark(openPerfTrace, 'creating_route_ready');
      });
    }

    if (debugHoldBeforeCreateSession > Duration.zero) {
      await Future<void>.delayed(debugHoldBeforeCreateSession);
    }

    PrivateChatOpenPerfLogger.mark(
      openPerfTrace,
      'create_session_request_start',
    );
    final createdSessionId = await sessionService.createSession(pid, peerType);
    final sid = createdSessionId?.trim() ?? '';
    openPerfTrace['session_id'] = sid;
    PrivateChatOpenPerfLogger.mark(
      openPerfTrace,
      'create_session_request_done',
      data: {'success': sid.isNotEmpty},
    );
    if (sid.isEmpty) {
      await creatingRouteReady;
      if (openChat &&
          AppRoutes.pathOf(Get.currentRoute) == AppRoutes.privateChatCreating) {
        PrivateChatOpenPerfLogger.mark(
          openPerfTrace,
          'creating_route_pop_failed',
        );
        Get.back<void>();
      }
      return null;
    }

    await creatingRouteReady;
    if (openChat) {
      final routeTitle = imService.resolveSessionDisplayTitleById(
        sid,
        fallbackTitle: title,
        type: 'private',
      );
      if (AppRoutes.pathOf(Get.currentRoute) == AppRoutes.privateChatCreating) {
        // 创建中页面已经响应了用户点击；拿到真实 sid 后原位换成聊天页，不再播放
        // 第二次 push，也不会把创建中页留在返回栈里。
        unawaited(
          toChat(
            sessionId: sid,
            title: routeTitle,
            type: 'private',
            replaceCurrentRoute: true,
            initialDraft: creationDraft?.text ?? '',
            openPerfTrace: openPerfTrace,
          ),
        );
      }
    }

    // 本地标题绑定与会话列表对账放到后台，避免在路由首帧前触发资料页历史列表重建。
    if (title.isNotEmpty && !imService.hasSessionDisplayTitleById(sid)) {
      unawaited(
        imService.bindSessionDisplayTitle(
          sid,
          title,
          type: 'private',
          peerId: pid,
          peerType: peerType,
        ),
      );
    }
    PrivateChatOpenPerfLogger.mark(
      openPerfTrace,
      'sessions_window_refresh_queued',
    );
    unawaited(imService.refreshSessionsWindowNow());
    return sid;
  }

  static Future<T?> toChat<T>({
    required String sessionId,
    required String title,
    required String type,
    List<SessionAvatarMember> initialGroupAvatarMembers =
        const <SessionAvatarMember>[],
    bool replaceCurrentRoute = false,
    String initialDraft = '',
    Map<String, dynamic>? openPerfTrace,
  }) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return Future<T?>.value(null);
    }

    final replacingActiveChatRoute = Get.currentRoute.startsWith(
      AppRoutes.chat,
    );
    final activeTag = replacingActiveChatRoute
        ? ChatBinding.currentControllerTag()
        : null;

    // 已经停留在目标会话页：保持无反应，不跳转、不收起输入法、不动当前页面状态。
    if (replacingActiveChatRoute &&
        activeTag == ChatBinding.controllerTagForSession(sid)) {
      return Future<T?>.value(null);
    }

    // 只有当前页面本身是聊天页时，才处理当前聊天控制器。资料页/列表页等非聊天页
    // 可能携带 session_id 路由参数，不能把它误判成 active chat controller 后同步
    // persist/delete，否则会在点击“创建会话”时卡住按钮动画。
    if (replacingActiveChatRoute) {
      if (activeTag != null &&
          Get.isRegistered<ChatController>(tag: activeTag)) {
        final activeController = Get.find<ChatController>(tag: activeTag);
        activeController.persistDraftImmediately();
        activeController.deactivateVoiceCommandForRouteChange();
        activeController.dismissInputInteraction();
      } else if (Get.isRegistered<ChatController>()) {
        final activeController = Get.find<ChatController>();
        activeController.persistDraftImmediately();
        activeController.deactivateVoiceCommandForRouteChange();
        activeController.dismissInputInteraction();
      }
    }

    final normalizedType = type.trim();
    final normalizedInitialGroupAvatarMembers = initialGroupAvatarMembers
        .where((member) => member.memberId.trim().isNotEmpty)
        .take(9)
        .map((member) => member.toJson())
        .toList(growable: false);

    final arguments = <String, dynamic>{
      'session_id': sid,
      'title': title,
      'type': normalizedType.isEmpty ? 'private' : normalizedType,
    };
    if (normalizedInitialGroupAvatarMembers.isNotEmpty) {
      arguments['initial_group_avatar_members'] =
          normalizedInitialGroupAvatarMembers;
    }
    final normalizedInitialDraft = initialDraft.trim();
    if (normalizedInitialDraft.isNotEmpty) {
      arguments['initial_draft'] = initialDraft;
    }
    if (openPerfTrace != null && openPerfTrace.isNotEmpty) {
      arguments[PrivateChatOpenPerfLogger.argumentKey] =
          PrivateChatOpenPerfLogger.fork(openPerfTrace, sessionId: sid);
      PrivateChatOpenPerfLogger.mark(openPerfTrace, 'chat_route_push_start');
    }
    final routeParameters = <String, String>{
      'session_id': sid,
      'title': title,
      'type': normalizedType.isEmpty ? 'private' : normalizedType,
    };

    // Chat -> Chat 跳转到不同会话：用 push，保留上一个聊天页，
    // 返回时回到上一个聊天而不是首页。（同会话已在函数开头直接 no-op 返回。）
    //
    // ImService 的 currentMessages 是全局单例状态，B 的 enterSession 会覆盖 A 的
    // 消息。当 B pop 后，需要让 A 重新 enterSession 恢复消息窗口。
    final previousSessionId = replacingActiveChatRoute ? activeTag : null;
    final future = replaceCurrentRoute
        ? Get.key.currentState?.pushReplacement<T, void>(
                buildCreatingToChatReplacementRoute<T>(
                  arguments: arguments,
                  parameters: routeParameters,
                ),
              ) ??
              Future<T?>.value(null)
        : Get.toNamed<T>(
                AppRoutes.chat,
                arguments: arguments,
                parameters: routeParameters,
              ) ??
              Future<T?>.value(null);
    if (openPerfTrace != null && openPerfTrace.isNotEmpty) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        PrivateChatOpenPerfLogger.mark(openPerfTrace, 'chat_route_first_frame');
      });
    }

    if (previousSessionId != null && replacingActiveChatRoute) {
      future.then((_) {
        // B 已 pop，恢复 A 的会话消息窗口。
        final previousSid = previousSessionId.replaceFirst('chat:', '');
        if (previousSid.isEmpty) return;
        if (!Get.isRegistered<ChatController>(tag: previousSessionId)) return;
        final ctrl = Get.find<ChatController>(tag: previousSessionId);
        if (ctrl.isClosed) return;
        ctrl.imService.enterSession(previousSid);
      });
    }

    return future;
  }
}
