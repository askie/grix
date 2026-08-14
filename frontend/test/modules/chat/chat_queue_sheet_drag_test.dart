import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/chat/chat_view.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SharedPreferences.setMockInitialValues(<String, Object>{});
  });

  EventLifecycleQueueItem buildItem(
    String eventId, {
    String sessionId = 'sess-q',
    String state = 'queued',
    int position = 0,
    bool held = false,
    String heldReason = '',
  }) {
    return EventLifecycleQueueItem(
      eventId: eventId,
      sessionId: sessionId,
      messageId: '',
      clientMsgId: '',
      contentPreview: 'msg-$eventId',
      state: state,
      queuePosition: position,
      actions: state == 'running'
          ? const <String>['stop']
          : const <String>['cancel'],
      updatedAt: 1000,
      held: held,
      heldReason: heldReason,
    );
  }

  Future<ImService> pumpQueueSheet(
    WidgetTester tester, {
    ImService? service,
    List<EventLifecycleQueueItem>? items,
  }) async {
    final imService = service ?? ImService();
    imService.eventLifecycleQueues['sess-q'] =
        items ??
        <EventLifecycleQueueItem>[
          buildItem('r0', state: 'running', position: 0),
          buildItem('e1', position: 1),
          buildItem('e2', position: 2),
          buildItem('e3', position: 3),
        ];
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (context) => Center(
              child: ElevatedButton(
                onPressed: () => showChatQueueSheet(
                  context,
                  imService: imService,
                  sessionId: 'sess-q',
                ),
                child: const Text('open'),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
    return imService;
  }

  List<String> realOrder(ImService imService) {
    final items =
        imService
            .queueItemsForSession('sess-q')
            .where((e) => e.queuePosition > 0)
            .toList()
          ..sort((a, b) => a.queuePosition.compareTo(b.queuePosition));
    return items.map((e) => e.eventId).toList();
  }

  testWidgets('排队项整行可长按拖动，running 项不可拖且不再显示拖动手柄', (tester) async {
    await pumpQueueSheet(tester);
    expect(find.byIcon(Icons.reorder), findsNothing);
    expect(
      find.byType(LongPressDraggable<EventLifecycleQueueItem>),
      findsNWidgets(3),
    );
    // 展示倒序：最新 e3 在最上，running 沉底
    final e3Y = tester.getCenter(find.text('msg-e3')).dy;
    final r0Y = tester.getCenter(find.text('msg-r0')).dy;
    expect(e3Y, lessThan(r0Y));
  });

  testWidgets('长按整行显示完整行反馈并改变队列顺序', (tester) async {
    final imService = await pumpQueueSheet(tester);
    expect(realOrder(imService), <String>['e1', 'e2', 'e3']);

    // 长按展示最上面的 e3 行，拖到 e2 下沿 → 展示序 [e2,e3,e1]
    // → 真实序 [e1,e3,e2]：e3 从队尾挪到中间
    final e2Bottom = tester.getRect(find.byKey(const ValueKey('e2'))).bottom;
    final from = tester.getCenter(find.text('msg-e3'));
    final gesture = await tester.startGesture(from);
    await tester.pump(kLongPressTimeout + const Duration(milliseconds: 50));
    final feedback = find.byKey(const ValueKey('queue_drag_feedback_e3'));
    expect(feedback, findsOneWidget);
    final feedbackRect = tester.getRect(feedback);
    expect(feedbackRect.width, greaterThan(300));
    expect((feedbackRect.center.dx - from.dx).abs(), lessThan(1));
    expect((feedbackRect.center.dy - from.dy).abs(), lessThan(8));
    await gesture.moveBy(Offset(0, e2Bottom - 4 - from.dy));
    await tester.pump();
    await gesture.up();
    await tester.pumpAndSettle();

    expect(realOrder(imService), <String>['e1', 'e3', 'e2']);
  });

  testWidgets('长按整行拖到另一行下沿进行排序', (tester) async {
    final imService = await pumpQueueSheet(tester);
    expect(realOrder(imService), <String>['e1', 'e2', 'e3']);

    // 展示序从上到下 e3, e2, e1, r0：长按 e3 行，落到 e2 行下沿
    // （行上/下 1/4 边带＝排序插入区，中心带＝合并区）→ e3 插到 e2
    // 之后 → 展示序 [e2,e3,e1] → 真实序 [e1,e3,e2]。
    final from = tester.getCenter(find.text('msg-e3'));
    final e2Bottom = tester.getRect(find.byKey(const ValueKey('e2'))).bottom;
    final gesture = await tester.startGesture(from);
    await tester.pump(kLongPressTimeout + const Duration(milliseconds: 50));
    await gesture.moveBy(Offset(0, e2Bottom - 4 - from.dy));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 100));

    // e2 下沿属于排序区：只亮间隙插入线，任务本身不能同时高亮。
    final indicator = find.byKey(const ValueKey('queue_sort_indicator_2'));
    expect(tester.getRect(indicator).height, 3);
    final target = tester.widget<AnimatedContainer>(
      find.byKey(const ValueKey('queue_merge_target_e2')),
    );
    final decoration = target.decoration! as BoxDecoration;
    expect(decoration.color, isNull);
    expect(decoration.border, isNull);

    await gesture.up();
    await tester.pumpAndSettle();

    expect(realOrder(imService), <String>['e1', 'e3', 'e2']);
  });

  testWidgets('任务行之间保留可命中的排序落点并显示插入线', (tester) async {
    final imService = await pumpQueueSheet(tester);
    expect(realOrder(imService), <String>['e1', 'e2', 'e3']);

    final from = tester.getCenter(find.text('msg-e3'));
    final sortGap = find.byKey(const ValueKey('queue_sort_gap_2'));
    expect(sortGap, findsOneWidget);
    expect(tester.getRect(sortGap).height, greaterThanOrEqualTo(14));

    // 展示序 e3,e2,e1,r0；把 e3 放到 e1 前面的固定间隙。
    final to = tester.getCenter(sortGap);
    final gesture = await tester.startGesture(from);
    await tester.pump(kLongPressTimeout + const Duration(milliseconds: 50));
    await gesture.moveBy(to - from);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 100));

    final indicator = find.byKey(const ValueKey('queue_sort_indicator_2'));
    expect(tester.getRect(indicator).height, 3);

    await gesture.up();
    await tester.pumpAndSettle();
    expect(find.byType(AlertDialog), findsNothing);
    expect(realOrder(imService), <String>['e1', 'e3', 'e2']);
  });

  testWidgets('长按整行拖到列表下方空白处移到队尾', (tester) async {
    final imService = await pumpQueueSheet(tester);
    expect(realOrder(imService), <String>['e1', 'e2', 'e3']);

    // 落到最后一行（running）之下的空白区：插入间隙=末尾 → e3 挪到
    // queued 段尾 → 展示序 [e2,e1,e3] → 真实序 [e3,e1,e2]。
    final from = tester.getCenter(find.text('msg-e3'));
    final r0Bottom = tester.getRect(find.byKey(const ValueKey('r0'))).bottom;
    final gesture = await tester.startGesture(from);
    await tester.pump(kLongPressTimeout + const Duration(milliseconds: 50));
    await gesture.moveBy(Offset(0, r0Bottom + 20 - from.dy));
    await tester.pump();
    await gesture.up();
    await tester.pumpAndSettle();

    expect(realOrder(imService), <String>['e3', 'e1', 'e2']);
  });

  testWidgets('连续拖过目标行边缘再进入中心会触发合并', (tester) async {
    final imService = await pumpQueueSheet(tester);

    // 模拟真机连续移动：先从 e2 上边缘进入，再移动到中心后松手。
    final from = tester.getCenter(find.text('msg-e3'));
    final to = tester.getCenter(find.text('msg-e2'));
    final targetTop = tester.getRect(find.byKey(const ValueKey('e2'))).top;
    final edge = Offset(to.dx, targetTop + 2);
    final gesture = await tester.startGesture(from);
    await tester.pump(kLongPressTimeout + const Duration(milliseconds: 50));
    await gesture.moveBy(edge - from);
    await tester.pump();
    await gesture.moveBy(to - edge);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 100));
    final target = tester.widget<AnimatedContainer>(
      find.byKey(const ValueKey('queue_merge_target_e2')),
    );
    final decoration = target.decoration! as BoxDecoration;
    expect(decoration.color, isNotNull);
    expect(decoration.border, isNotNull);
    await gesture.up();
    await tester.pumpAndSettle();

    // 弹出合并确认框（测试环境未加载翻译，.tr 回退为 key 本身）
    expect(find.byType(AlertDialog), findsOneWidget);
    expect(find.text('chat_queue_merge_title'), findsOneWidget);

    // 取消合并：不删任务、不改顺序
    await tester.tap(find.text('common_cancel'));
    await tester.pumpAndSettle();
    expect(realOrder(imService), <String>['e1', 'e2', 'e3']);
    expect(imService.queueItemsForSession('sess-q').length, 4);
  });

  testWidgets('长按整行拖到 running 行上不触发合并', (tester) async {
    final imService = await pumpQueueSheet(tester);

    final from = tester.getCenter(find.text('msg-e3'));
    final to = tester.getCenter(find.text('msg-r0'));
    final gesture = await tester.startGesture(from);
    await tester.pump(kLongPressTimeout + const Duration(milliseconds: 50));
    await gesture.moveBy(to - from);
    await tester.pump();
    await gesture.up();
    await tester.pumpAndSettle();

    expect(find.byType(AlertDialog), findsNothing);
    expect(realOrder(imService), <String>['e1', 'e2', 'e3']);
  });

  testWidgets('长按 running 行拖动不改变顺序', (tester) async {
    final imService = await pumpQueueSheet(tester);

    final gesture = await tester.startGesture(
      tester.getCenter(find.text('msg-r0')),
    );
    await tester.pump(kLongPressTimeout + const Duration(milliseconds: 50));
    await gesture.moveBy(const Offset(0, -150));
    await tester.pump();
    await gesture.up();
    await tester.pumpAndSettle();

    expect(realOrder(imService), <String>['e1', 'e2', 'e3']);
  });

  /// 长按展示最上面一条（e3）整行，连续经过目标边缘后在中心松手。
  Future<void> dragFirstRowOnto(WidgetTester tester, String targetText) async {
    final from = tester.getCenter(find.text('msg-e3'));
    final to = tester.getCenter(find.text(targetText));
    final targetTop = tester
        .getRect(
          find.ancestor(
            of: find.text(targetText),
            matching: find.byType(DragTarget<EventLifecycleQueueItem>),
          ),
        )
        .top;
    final edge = Offset(to.dx, targetTop + 2);
    final gesture = await tester.startGesture(from);
    await tester.pump(kLongPressTimeout + const Duration(milliseconds: 50));
    await gesture.moveBy(edge - from);
    await tester.pump();
    await gesture.moveBy(to - edge);
    await tester.pump();
    await gesture.up();
    await tester.pumpAndSettle();
  }

  testWidgets('确认合并：目标全文变为 被拖全文+换行+原全文，随后移除被拖任务', (tester) async {
    final service = _RecordingImService();
    await pumpQueueSheet(tester, service: service);

    await dragFirstRowOnto(tester, 'msg-e2');
    // 确认框的确认按钮（测试环境未加载翻译，显示 key 本身）
    await tester.tap(find.text('chat_queue_merge'));
    await tester.pumpAndSettle();

    expect(service.queueEdits, hasLength(1));
    expect(service.queueEdits.single.eventId, 'e2');
    expect(service.queueEdits.single.content, 'msg-e3\nmsg-e2');
    expect(service.cancels, <String>['e3']);
    // 目标非 held 时不多发 hold
    expect(service.holds, isEmpty);
  });

  testWidgets('合并编辑失败时不移除被拖任务', (tester) async {
    final service = _RecordingImService()..editOk = false;
    await pumpQueueSheet(tester, service: service);

    await dragFirstRowOnto(tester, 'msg-e2');
    await tester.tap(find.text('chat_queue_merge'));
    await tester.pumpAndSettle();

    expect(service.queueEdits, hasLength(1));
    expect(service.cancels, isEmpty);
  });

  testWidgets('目标处于暂停时合并后补 hold 还原暂停', (tester) async {
    final service = _RecordingImService();
    await pumpQueueSheet(
      tester,
      service: service,
      items: <EventLifecycleQueueItem>[
        buildItem('r0', state: 'running', position: 0),
        buildItem('e1', position: 1),
        buildItem('e2', position: 2, held: true, heldReason: 'manual'),
        buildItem('e3', position: 3),
      ],
    );

    await dragFirstRowOnto(tester, 'msg-e2');
    await tester.tap(find.text('chat_queue_merge'));
    await tester.pumpAndSettle();

    expect(service.queueEdits.single.eventId, 'e2');
    expect(service.cancels, <String>['e3']);
    // queue_edit 会解除目标 hold，合并后应补发 hold 还原暂停
    expect(service.holds, hasLength(1));
    expect(service.holds.single.eventId, 'e2');
    expect(service.holds.single.hold, isTrue);
    expect(service.holds.single.reason, 'manual');
  });
}

/// 记录合并相关调用的 ImService 测试子类：不进编辑模式、不依赖网络，
/// 只记录 sendQueueEdit / sendEventCancel / sendEventHold 的入参。
class _RecordingImService extends ImService {
  final List<({String eventId, String content})> queueEdits =
      <({String eventId, String content})>[];
  final List<String> cancels = <String>[];
  final List<({String eventId, bool hold, String reason})> holds =
      <({String eventId, bool hold, String reason})>[];
  bool editOk = true;

  @override
  Future<EventLifecycleCmdResult> sendQueueEdit({
    required String sessionId,
    required String eventId,
    required String content,
  }) async {
    queueEdits.add((eventId: eventId, content: content));
    return EventLifecycleCmdResult(
      ok: editOk,
      error: editOk ? '' : 'not_found',
    );
  }

  @override
  void sendEventCancel({
    required String sessionId,
    required EventLifecycleQueueItem item,
  }) {
    cancels.add(item.eventId);
  }

  @override
  Future<EventLifecycleCmdResult> sendEventHold({
    required String sessionId,
    required String eventId,
    required bool hold,
    String reason = 'manual',
    int? ttlMs,
  }) async {
    holds.add((eventId: eventId, hold: hold, reason: reason));
    return const EventLifecycleCmdResult(ok: true, held: true);
  }
}
