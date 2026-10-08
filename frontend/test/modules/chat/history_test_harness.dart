import 'dart:convert';
import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/data/providers/oss_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/modules/chat/bindings/chat_binding.dart';
import 'package:grix/modules/chat/chat_view.dart';
import 'package:grix/modules/chat/controllers/chat_controller.dart';
import 'package:grix/shared/widgets/message_bubble.dart';
import 'package:shared_preferences/shared_preferences.dart';

class OfflineAuth extends AuthService {
  @override
  bool get isLoggedIn => true;
  @override
  String? get userId => 'recheck-user';
  @override
  String? get token => null;
}

class OfflineAgent extends AgentService {
  @override
  Future<void> loadAgents({String? categoryId}) async {}
}

class OfflineSession extends SessionService {
  bool remoteHasMore = true;
  int historyCalls = 0;
  @override
  bool get isInitialized => true;
  @override
  Future<SessionMessageHistoryResult> fetchMessageHistoryResult({
    required String sessionId,
    String? beforeMsgId,
    int limit = 20,
  }) async {
    historyCalls++;
    return SessionMessageHistoryResult(
      code: 0,
      messages: const [],
      hasMore: remoteHasMore,
    );
  }

  @override
  Future<Map<String, dynamic>?> fetchSessionDetail(String id) async => {
    'session_type': 1,
    'members': [],
  };
  @override
  Future<SessionDetailResult> fetchSessionDetailResult(String id) async =>
      const SessionDetailResult(data: {'session_type': 1, 'members': []});
}

void evidence(String kind, Map<String, Object?> values) =>
    debugPrint('HISTORY $kind ${jsonEncode(values)}');
Map<String, dynamic> row(String sid, int id, {String? content}) => {
  'msg_id': 'probe-$id',
  'session_id': sid,
  'sender_id': 'offline-peer',
  'sender_type': 1,
  'msg_type': 1,
  'content': content ?? 'message $id',
  'created_at': 1700000000000 + id * 1000,
  'status': 'sent',
  'state_version': '1',
};
Future<void> waitReal(bool Function() check) async {
  for (int i = 0; i < 500; i++) {
    if (check()) return;
    await Future<void>.delayed(const Duration(milliseconds: 10));
  }
  throw StateError('recheck wait timed out');
}

Future<void> flushDb(WidgetTester tester, bool Function() check) async {
  for (int i = 0; i < 500; i++) {
    await tester.pump();
    if (check()) return;
    await tester.runAsync(() async {
      await Future<void>.delayed(const Duration(milliseconds: 10));
    });
  }
  throw StateError('widget/real DB wait timed out');
}

Future<T> runDb<T>(WidgetTester tester, Future<T> Function() action) async {
  bool done = false;
  T? value;
  Object? error;
  // SQLite futures must belong to the real event loop so later widget tests
  // do not inherit a DB queue scheduled on this test's expired fake clock.
  await tester.runAsync(() async {
    action().then(
      (v) {
        value = v;
        done = true;
      },
      onError: (Object e) {
        error = e;
        done = true;
      },
    );
  });
  await flushDb(tester, () => done);
  if (error != null) throw error!;
  return value as T;
}

Future<ImService> initOffline(WidgetTester tester, String label) async {
  Get.testMode = true;
  Get.reset();
  SharedPreferences.setMockInitialValues({});
  resetChatInitialMessageRenderWarmupSchedulerForTest();
  resetChatViewDebugBuildCounterForTest();
  MessageBubble.resetFinalRenderCacheForTest();
  Get.put<AuthService>(OfflineAuth());
  Get.put<AgentService>(OfflineAgent());
  Get.put<SessionService>(OfflineSession());
  Get.put<OssService>(OssService());
  await tester.runAsync(() async {
    await LocalDb.initDatabaseFactory();
    final directory = await Directory.systemTemp.createTemp(
      'chat-history-test-',
    );
    await LocalDb.databaseFactory.setDatabasesPath(directory.path);
    addTearDown(() async {
      await directory.delete(recursive: true);
    });
  });
  await runDb(
    tester,
    () => LocalDb.setActiveUser(
      '$label-${DateTime.now().microsecondsSinceEpoch}',
    ),
  );
  final im = Get.put<ImService>(ImService());
  im.setActiveSyncModeForTest('v1');
  return im; // exact production class; no init(), endpoints or credentials.
}

class BuildWitness extends StatelessWidget {
  const BuildWitness({super.key, required this.child, required this.label});
  final Widget child;
  final String label;
  @override
  Widget build(BuildContext context) {
    final route = ModalRoute.of(context)!;
    evidence('BUILD', {
      'label': label,
      'offstage': route.offstage,
      'status': route.animation?.status.name,
      'value': route.animation?.value,
      'registered': Get.isRegistered<ChatController>(
        tag: ChatBinding.currentControllerTag(),
      ),
    });
    return child; // does not find or subclass controller; ChatView triggers production init.
  }
}

Future<ChatController> openRoute(
  WidgetTester tester,
  String sid, {
  bool witness = false,
  bool zero = false,
}) async {
  final page = AppRoutes.routes.singleWhere((p) => p.name == AppRoutes.chat);
  await tester.pumpWidget(
    GetMaterialApp(
      key: UniqueKey(),
      translations: AppTranslations(),
      locale: const Locale('en', 'US'),
      initialRoute: '/',
      getPages: [
        GetPage(
          name: '/',
          page: () => const Scaffold(body: Text('offline home')),
        ),
        if (witness || zero)
          GetPage(
            name: page.name,
            page: () => BuildWitness(label: sid, child: page.page()),
            binding: page.binding,
            transition: page.transition,
            transitionDuration: zero ? Duration.zero : page.transitionDuration,
            opaque: page.opaque,
          )
        else
          page,
      ],
    ),
  );
  Get.toNamed(
    AppRoutes.chat,
    arguments: {'session_id': sid, 'title': sid, 'type': 'private'},
  );
  await tester.pump();
  return Get.find<ChatController>(
    tag: ChatBinding.controllerTagForSession(sid),
  );
}

Future<void> settleInitial(WidgetTester tester, ImService im) async {
  evidence('SETTLE-start', {'ready': im.initialHistoryReady.value});
  await tester.pump(const Duration(milliseconds: 350));
  await flushDb(tester, () => im.initialHistoryReady.value);
  evidence('SETTLE-DB', {'ready': im.initialHistoryReady.value});
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 350));
  await tester.pumpAndSettle();
  evidence('SETTLE-end', {});
}

Finder chatList(ChatController c) => find.byWidgetPredicate(
  (w) => w is ListView && identical(w.controller, c.scrollController),
);
Map<String, Object?> metrics(ChatController c, {String? msgId}) {
  final p = c.scrollController.position;
  final key = msgId == null
      ? null
      : c.peekMessageViewportItemGlobalKey('m:$msgId');
  final box = key?.currentContext?.findRenderObject();
  return {
    'pixels': p.pixels,
    'max': p.maxScrollExtent,
    'viewport': p.viewportDimension,
    'msg': msgId,
    'y': box is RenderBox && box.attached
        ? box.localToGlobal(Offset.zero).dy
        : null,
    'h': box is RenderBox && box.attached ? box.size.height : null,
    'loadingOlder': c.isLoadingOlderHistory,
    'hasOlder': c.hasOlderHistory,
    'activity': (p as ScrollPositionWithSingleContext).activity.runtimeType
        .toString(),
    'bottomButton': c.scrollToBottomButtonVisible.value,
  };
}

String? leadingId(ChatController c, ImService im) {
  final viewportY =
      c.scrollController.position.context.notificationContext!
              .findRenderObject()
          as RenderBox;
  final top = viewportY.localToGlobal(Offset.zero).dy;
  final bottom = top + c.scrollController.position.viewportDimension;
  String? best;
  double bestY = double.infinity;
  for (final m in im.currentMessages) {
    final box = c
        .peekMessageViewportItemGlobalKey('m:${m.msgId}')
        ?.currentContext
        ?.findRenderObject();
    if (box is! RenderBox || !box.attached) continue;
    final y = box.localToGlobal(Offset.zero).dy;
    if (y + box.size.height > top && y < bottom && y < bestY) {
      best = m.msgId;
      bestY = y;
    }
  }
  return best;
}

Future<void> settleHistory(WidgetTester tester) async {
  // A real SQLite load must get real event-loop time while fling and spinner
  // frames advance. pumpAndSettle alone only advances the fake frame clock.
  for (var i = 0; i < 250; i++) {
    await tester.pump(const Duration(milliseconds: 16));
    await tester.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 5)),
    );
    if (!tester.binding.hasScheduledFrame) return;
  }
  throw StateError('History viewport did not settle');
}

Future<void> closeOffline(WidgetTester tester) async {
  await tester.pumpWidget(const SizedBox.shrink());
  await tester.pump();
  if (Get.isRegistered<ChatController>()) {
    Get.delete<ChatController>(force: true);
  }
  if (Get.isRegistered<ImService>()) {
    Get.find<ImService>().leaveSession();
    Get.find<ImService>().onClose();
  }
  await tester.pump(const Duration(milliseconds: 120));
  Get.reset();
  resetChatInitialMessageRenderWarmupSchedulerForTest();
  MessageBubble.resetFinalRenderCacheForTest();
  for (int i = 0; i < 12; i++) {
    await tester.runAsync(() async {
      await Future<void>.delayed(const Duration(milliseconds: 10));
    });
    await tester.pump();
  }
  await runDb(tester, () => LocalDb.setActiveUser(null));
}
