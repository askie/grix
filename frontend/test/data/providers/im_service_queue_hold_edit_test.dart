import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/models/session_activity_model.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SharedPreferences.setMockInitialValues(<String, Object>{});
  });

  EventLifecycleQueueItem buildItem(
    String eventId, {
    String sessionId = 'sess-h',
    String state = 'queued',
    int position = 0,
    bool held = false,
    String heldReason = '',
    String content = '',
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
      content: content,
    );
  }

  String packet(String cmd, Map<String, dynamic> payload) {
    return jsonEncode(<String, dynamic>{'cmd': cmd, 'payload': payload});
  }

  group('_parseQueueItem 新字段解析（event_state 通道）', () {
    test('content/held/held_reason 正常解析', () async {
      final service = ImService();
      await service.handleDownstreamForTest(
        packet('event_state', <String, dynamic>{
          'session_id': 'sess-h',
          'event_id': 'e1',
          'state': 'queued',
          'queue_position': 1,
          'content_preview': '预览截断…',
          'content': '任务全文（超过 64 字预览截断的完整内容）',
          'held': true,
          'held_reason': 'manual',
        }),
      );

      final items = service.queueItemsForSession('sess-h');
      expect(items, hasLength(1));
      final item = items.single;
      expect(item.content, '任务全文（超过 64 字预览截断的完整内容）');
      expect(item.fullContent, item.content);
      expect(item.held, isTrue);
      expect(item.heldReason, 'manual');
    });

    test('老服务端缺失新字段：content 回退 contentPreview，held 为 false', () async {
      final service = ImService();
      await service.handleDownstreamForTest(
        packet('event_state', <String, dynamic>{
          'session_id': 'sess-h',
          'event_id': 'e1',
          'state': 'queued',
          'queue_position': 1,
          'content_preview': '仅有预览',
        }),
      );

      final item = service.queueItemsForSession('sess-h').single;
      expect(item.content, '仅有预览');
      expect(item.fullContent, '仅有预览');
      expect(item.held, isFalse);
      expect(item.heldReason, '');
    });
  });

  group('queue_snapshot queued 项新字段解析', () {
    test('快照 queued 携带 content/held/held_reason', () async {
      final service = ImService();
      await service.handleDownstreamForTest(
        packet('queue_snapshot', <String, dynamic>{
          'session_id': 'sess-h',
          'running': <String>['r0'],
          'queued': <Map<String, dynamic>>[
            <String, dynamic>{
              'event_id': 'e1',
              'position': 1,
              'content_preview': '预览1',
              'content': '全文1',
              'held': true,
              'held_reason': 'editing',
            },
            <String, dynamic>{
              'event_id': 'e2',
              'position': 2,
              'content_preview': '预览2',
            },
          ],
        }),
      );

      final items = service.queueItemsForSession('sess-h');
      final e1 = items.firstWhere((e) => e.eventId == 'e1');
      final e2 = items.firstWhere((e) => e.eventId == 'e2');
      expect(e1.content, '全文1');
      expect(e1.held, isTrue);
      expect(e1.heldReason, 'editing');
      expect(e2.content, '预览2', reason: '缺失 content 时回退预览');
      expect(e2.held, isFalse);
    });

    test('running 项优先取 running_items 的 actions，缺失时回退 stop', () async {
      final service = ImService();
      await service.handleDownstreamForTest(
        packet('queue_snapshot', <String, dynamic>{
          'session_id': 'sess-h',
          'running': <String>['r0', 'selfdrive_bg'],
          'running_items': <Map<String, dynamic>>[
            <String, dynamic>{
              'event_id': 'r0',
              'content_preview': '真实任务',
              'actions': <Map<String, dynamic>>[
                <String, dynamic>{'type': 'stop'},
              ],
            },
            <String, dynamic>{
              'event_id': 'selfdrive_bg',
              'content_preview': '后台任务',
              'actions': <Map<String, dynamic>>[],
            },
          ],
          'queued': <Map<String, dynamic>>[],
        }),
      );

      final items = service.queueItemsForSession('sess-h');
      final real = items.firstWhere((e) => e.eventId == 'r0');
      final bg = items.firstWhere((e) => e.eventId == 'selfdrive_bg');
      expect(real.actions, contains('stop'));
      expect(bg.actions, isEmpty, reason: 'selfdrive 虚拟任务不可停止');
    });

    test('权威空快照清理残留 agent output/composing，但保留人类 composing', () async {
      final service = ImService();
      const sid = 'sess-drained';
      service.agentOutputStates[sid] = <String, dynamic>{
        'run_id': 'evt-old',
        'session_id': sid,
        'agent_id': '9001',
        'state': 'received',
        'can_stop': true,
        'updated_at': DateTime.now().millisecondsSinceEpoch,
      };
      service.sessionActivities[sid] = <SessionActivityModel>[
        SessionActivityModel(
          sessionId: sid,
          kind: 'composing',
          active: true,
          actorId: '9001',
          actorType: 'agent',
          executorId: '9001',
          executorType: 'agent',
          source: 'agent_api',
          refMsgId: '',
          refEventId: 'evt-old',
          statusText: '',
          updatedAt: DateTime.now().millisecondsSinceEpoch,
          expiresAt: 0,
        ),
        SessionActivityModel(
          sessionId: sid,
          kind: 'composing',
          active: true,
          actorId: 'human-1',
          actorType: 'human',
          executorId: '',
          executorType: '',
          source: 'client',
          refMsgId: '',
          refEventId: '',
          statusText: '',
          updatedAt: DateTime.now().millisecondsSinceEpoch,
          expiresAt: 0,
        ),
      ];

      await service.handleDownstreamForTest(
        packet('queue_snapshot', <String, dynamic>{
          'session_id': sid,
          'running': <String>[],
          'queued': <Map<String, dynamic>>[],
        }),
      );

      expect(service.queueCountForSession(sid), 0);
      expect(service.agentOutputStates[sid], isNull);
      final activities = service.sessionActivities[sid];
      expect(activities, hasLength(1));
      expect(activities!.single.actorType, 'human');
    });

    test('event_state 终态清空队列时也清 agent composing', () async {
      final service = ImService();
      const sid = 'sess-event-state-drain';
      service.eventLifecycleQueues[sid] = <EventLifecycleQueueItem>[
        buildItem('evt-run', sessionId: sid, state: 'running'),
      ];
      service.agentOutputStates[sid] = <String, dynamic>{
        'run_id': 'evt-run',
        'session_id': sid,
        'agent_id': '9001',
        'state': 'received',
        'can_stop': true,
        'updated_at': DateTime.now().millisecondsSinceEpoch,
      };
      service.sessionActivities[sid] = <SessionActivityModel>[
        SessionActivityModel(
          sessionId: sid,
          kind: 'composing',
          active: true,
          actorId: '9001',
          actorType: 'agent',
          executorId: '9001',
          executorType: 'agent',
          source: 'agent_api',
          refMsgId: '',
          refEventId: 'evt-run',
          statusText: '',
          updatedAt: DateTime.now().millisecondsSinceEpoch,
          expiresAt: DateTime.now().millisecondsSinceEpoch + 60000,
        ),
      ];

      await service.handleDownstreamForTest(
        packet('event_state', <String, dynamic>{
          'session_id': sid,
          'event_id': 'evt-run',
          'state': 'completed',
        }),
      );

      expect(service.queueCountForSession(sid), 0);
      expect(service.agentOutputStates[sid], isNull);
      expect(service.sessionActivities[sid], isNull);
    });

    test('queue_clear_result 清空队列时也清 agent composing', () async {
      final service = ImService();
      const sid = 'sess-queue-clear-drain';
      service.eventLifecycleQueues[sid] = <EventLifecycleQueueItem>[
        buildItem('evt-q', sessionId: sid, state: 'queued', position: 1),
      ];
      service.sessionActivities[sid] = <SessionActivityModel>[
        SessionActivityModel(
          sessionId: sid,
          kind: 'composing',
          active: true,
          actorId: '9001',
          actorType: 'agent',
          executorId: '9001',
          executorType: 'agent',
          source: 'agent_api',
          refMsgId: '',
          refEventId: 'evt-q',
          statusText: '',
          updatedAt: DateTime.now().millisecondsSinceEpoch,
          expiresAt: DateTime.now().millisecondsSinceEpoch + 60000,
        ),
      ];

      await service.handleDownstreamForTest(
        packet('queue_clear_result', <String, dynamic>{
          'session_id': sid,
          'ok': true,
          'canceled_event_ids': <String>['evt-q'],
        }),
      );

      expect(service.queueCountForSession(sid), 0);
      expect(service.sessionActivities[sid], isNull);
    });
  });

  group('sendEventHold 回执往返', () {
    test('发送即本地乐观翻转 held；ok 回执完成 future', () async {
      final service = ImService();
      const sid = 'sess-h';
      service.eventLifecycleQueues[sid] = <EventLifecycleQueueItem>[
        buildItem('e1', position: 1),
      ];

      final future = service.sendEventHold(
        sessionId: sid,
        eventId: 'e1',
        hold: true,
        reason: 'manual',
      );
      // 乐观翻转立即生效
      expect(service.queueItemsForSession(sid).single.held, isTrue);
      expect(service.queueItemsForSession(sid).single.heldReason, 'manual');

      await service.handleDownstreamForTest(
        packet('event_hold_result', <String, dynamic>{
          'session_id': sid,
          'event_id': 'e1',
          'ok': true,
          'held': true,
        }),
      );
      final result = await future;
      expect(result.ok, isTrue);
      expect(result.held, isTrue);
      expect(result.timedOut, isFalse);
    });

    test('失败回执带 error；本地按回执不再改 held（快照兜底收敛）', () async {
      final service = ImService();
      const sid = 'sess-h';
      service.eventLifecycleQueues[sid] = <EventLifecycleQueueItem>[
        buildItem('e1', position: 1),
      ];

      final future = service.sendEventHold(
        sessionId: sid,
        eventId: 'e1',
        hold: true,
      );
      await service.handleDownstreamForTest(
        packet('event_hold_result', <String, dynamic>{
          'session_id': sid,
          'event_id': 'e1',
          'ok': false,
          'held': false,
          'error': 'not_found',
        }),
      );
      final result = await future;
      expect(result.ok, isFalse);
      expect(result.error, 'not_found');
    });

    test('同一任务重复发起（续期语义）：旧等待被 timedOut 收口，不泄漏', () async {
      final service = ImService();
      const sid = 'sess-h';
      service.eventLifecycleQueues[sid] = <EventLifecycleQueueItem>[
        buildItem('e1', position: 1),
      ];

      final first = service.sendEventHold(
        sessionId: sid,
        eventId: 'e1',
        hold: true,
      );
      final second = service.sendEventHold(
        sessionId: sid,
        eventId: 'e1',
        hold: true,
      );
      final firstResult = await first;
      expect(firstResult.ok, isFalse);
      expect(firstResult.timedOut, isTrue);

      await service.handleDownstreamForTest(
        packet('event_hold_result', <String, dynamic>{
          'session_id': sid,
          'event_id': 'e1',
          'ok': true,
          'held': true,
        }),
      );
      final secondResult = await second;
      expect(secondResult.ok, isTrue);
    });

    test('空参数直接 bad_request，不发包', () async {
      final service = ImService();
      final result = await service.sendEventHold(
        sessionId: '',
        eventId: 'e1',
        hold: true,
      );
      expect(result.ok, isFalse);
      expect(result.error, 'bad_request');
    });
  });

  group('sendQueueEdit 回执往返', () {
    test('ok 回执完成 future', () async {
      final service = ImService();
      const sid = 'sess-h';
      final future = service.sendQueueEdit(
        sessionId: sid,
        eventId: 'e1',
        content: '改写后的全文',
      );
      await service.handleDownstreamForTest(
        packet('queue_edit_result', <String, dynamic>{
          'session_id': sid,
          'event_id': 'e1',
          'ok': true,
        }),
      );
      final result = await future;
      expect(result.ok, isTrue);
    });

    test('not_found 失败回执透传 error', () async {
      final service = ImService();
      const sid = 'sess-h';
      final future = service.sendQueueEdit(
        sessionId: sid,
        eventId: 'e-gone',
        content: '改写后的全文',
      );
      await service.handleDownstreamForTest(
        packet('queue_edit_result', <String, dynamic>{
          'session_id': sid,
          'event_id': 'e-gone',
          'ok': false,
          'error': 'not_found',
        }),
      );
      final result = await future;
      expect(result.ok, isFalse);
      expect(result.error, 'not_found');
    });

    test('空内容本地直接 empty_content', () async {
      final service = ImService();
      final result = await service.sendQueueEdit(
        sessionId: 'sess-h',
        eventId: 'e1',
        content: '   ',
      );
      expect(result.ok, isFalse);
      expect(result.error, 'empty_content');
    });
  });

  group('copyWith 保留 held/content（重排/重编号不丢新字段）', () {
    test('本地乐观重排后 held/content 不丢', () {
      final service = ImService();
      const sid = 'sess-h';
      service.eventLifecycleQueues[sid] = <EventLifecycleQueueItem>[
        buildItem(
          'e1',
          position: 1,
          held: true,
          heldReason: 'manual',
          content: '全文1',
        ),
        buildItem('e2', position: 2),
      ];

      service.sendQueueReorder(
        sessionId: sid,
        orderedEventIds: const <String>['e2', 'e1'],
      );

      final e1 = service
          .queueItemsForSession(sid)
          .firstWhere((e) => e.eventId == 'e1');
      expect(e1.queuePosition, 2);
      expect(e1.held, isTrue);
      expect(e1.heldReason, 'manual');
      expect(e1.content, '全文1');
    });
  });
}
