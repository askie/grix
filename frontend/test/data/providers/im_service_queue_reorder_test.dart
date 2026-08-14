import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/im_service.dart';
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
    );
  }

  String packet(String cmd, Map<String, dynamic> payload) {
    return jsonEncode(<String, dynamic>{'cmd': cmd, 'payload': payload});
  }

  group('queueOrderFromDisplay 展示序还原真实序', () {
    test('倒序展示列表反转即队列真实顺序（队头在前）', () {
      // 展示序：position 降序（最新在上）→ 真实序应为 position 升序
      final display = <EventLifecycleQueueItem>[
        buildItem('e3', position: 3),
        buildItem('e2', position: 2),
        buildItem('e1', position: 1),
      ];
      expect(queueOrderFromDisplay(display), <String>['e1', 'e2', 'e3']);
    });

    test('与 orderQueueItemsForDisplay 互逆', () {
      final items = <EventLifecycleQueueItem>[
        buildItem('e1', position: 1),
        buildItem('e3', position: 3),
        buildItem('e2', position: 2),
      ];
      final display = orderQueueItemsForDisplay(items)
          .where((e) => e.queuePosition > 0)
          .toList();
      expect(queueOrderFromDisplay(display), <String>['e1', 'e2', 'e3']);
    });

    test('拖动后的展示序转换：把展示区第 3 条拖到顶部', () {
      // 展示序 [e3,e2,e1]（e1 是队头）。把最底部的 e1 拖到展示顶部
      // → 展示序 [e1,e3,e2] → 真实序 [e2,e3,e1]：e1 变成队尾。
      final display = <EventLifecycleQueueItem>[
        buildItem('e1', position: 1),
        buildItem('e3', position: 3),
        buildItem('e2', position: 2),
      ];
      expect(queueOrderFromDisplay(display), <String>['e2', 'e3', 'e1']);
    });
  });

  group('computeReorderedQueueIds 拖动索引换算', () {
    // 展示序：[e3(#3), e2(#2), e1(#1), running]
    List<EventLifecycleQueueItem> displayItems() => <EventLifecycleQueueItem>[
      buildItem('e3', position: 3),
      buildItem('e2', position: 2),
      buildItem('e1', position: 1),
      buildItem('r0', state: 'running', position: 0),
    ];

    test('把展示最上（最新 e3）拖到展示最下 → e3 变队头', () {
      // Flutter 语义：向下拖 newIndex 指向目标位置的后一格
      final ids = computeReorderedQueueIds(
        displayItems: displayItems(),
        oldIndex: 0,
        newIndex: 3,
      );
      // 展示序变 [e2,e1,e3] → 真实序 [e3,e1,e2]
      expect(ids, <String>['e3', 'e1', 'e2']);
    });

    test('把队头 e1 向上拖到展示顶部 → e1 变队尾', () {
      final ids = computeReorderedQueueIds(
        displayItems: displayItems(),
        oldIndex: 2,
        newIndex: 0,
      );
      // 展示序变 [e1,e3,e2] → 真实序 [e2,e3,e1]
      expect(ids, <String>['e2', 'e3', 'e1']);
    });

    test('拖到 running 区被 clamp 回排队段末尾', () {
      final ids = computeReorderedQueueIds(
        displayItems: displayItems(),
        oldIndex: 0,
        newIndex: 4,
      );
      expect(ids, <String>['e3', 'e1', 'e2']);
    });

    test('拖 running 项无效', () {
      final ids = computeReorderedQueueIds(
        displayItems: displayItems(),
        oldIndex: 3,
        newIndex: 0,
      );
      expect(ids, isNull);
    });

    test('位置未变返回 null（含 newIndex=oldIndex+1 的原地放回）', () {
      expect(
        computeReorderedQueueIds(
          displayItems: displayItems(),
          oldIndex: 1,
          newIndex: 1,
        ),
        isNull,
      );
      expect(
        computeReorderedQueueIds(
          displayItems: displayItems(),
          oldIndex: 1,
          newIndex: 2,
        ),
        isNull,
      );
    });
  });

  group('sendQueueReorder 本地乐观重排', () {
    test('按新顺序重赋 position，running 项不动', () {
      final service = ImService();
      const sid = 'sess-q';
      service.eventLifecycleQueues[sid] = <EventLifecycleQueueItem>[
        buildItem('r0', state: 'running', position: 0),
        buildItem('e1', position: 1),
        buildItem('e2', position: 2),
        buildItem('e3', position: 3),
      ];

      service.sendQueueReorder(
        sessionId: sid,
        orderedEventIds: const <String>['e3', 'e1', 'e2'],
      );

      final items = service.queueItemsForSession(sid);
      final positions = <String, int>{
        for (final e in items) e.eventId: e.queuePosition,
      };
      expect(positions['e3'], 1);
      expect(positions['e1'], 2);
      expect(positions['e2'], 3);
      expect(positions['r0'], 0);
      expect(
        items.firstWhere((e) => e.eventId == 'r0').state,
        'running',
      );
    });

    test('清单外的排队项按原相对顺序排尾（愿望清单语义）', () {
      final service = ImService();
      const sid = 'sess-q';
      service.eventLifecycleQueues[sid] = <EventLifecycleQueueItem>[
        buildItem('e1', position: 1),
        buildItem('e2', position: 2),
        buildItem('e3', position: 3),
      ];

      service.sendQueueReorder(
        sessionId: sid,
        orderedEventIds: const <String>['e2', 'e1'],
      );

      final items = service.queueItemsForSession(sid);
      final positions = <String, int>{
        for (final e in items) e.eventId: e.queuePosition,
      };
      expect(positions['e2'], 1);
      expect(positions['e1'], 2);
      expect(positions['e3'], 3);
    });

    test('清单里未知 id 忽略，不影响其余排序', () {
      final service = ImService();
      const sid = 'sess-q';
      service.eventLifecycleQueues[sid] = <EventLifecycleQueueItem>[
        buildItem('e1', position: 1),
        buildItem('e2', position: 2),
      ];

      service.sendQueueReorder(
        sessionId: sid,
        orderedEventIds: const <String>['ghost', 'e2', 'e1'],
      );

      final items = service.queueItemsForSession(sid);
      final positions = <String, int>{
        for (final e in items) e.eventId: e.queuePosition,
      };
      // ghost 占据序号 1 但不存在于队列；e2/e1 取序号 2/3 后经
      // _sortQueueItems 收敛为相对顺序 e2 在前
      expect(items.length, 2);
      expect(
        positions['e2']! < positions['e1']!,
        isTrue,
        reason: 'e2 应排在 e1 前',
      );
    });
  });

  group('queue_reorder_result 下行处理', () {
    test('applied_event_ids 收敛本地顺序', () async {
      final service = ImService();
      const sid = 'sess-q';
      service.eventLifecycleQueues[sid] = <EventLifecycleQueueItem>[
        buildItem('e1', position: 1),
        buildItem('e2', position: 2),
        buildItem('e3', position: 3),
      ];

      await service.handleDownstreamForTest(
        packet('queue_reorder_result', <String, dynamic>{
          'session_id': sid,
          'applied_event_ids': <String>['e2', 'e3', 'e1'],
        }),
      );

      final items = service.queueItemsForSession(sid);
      final ordered = (items.toList()
            ..sort((a, b) => a.queuePosition.compareTo(b.queuePosition)))
          .map((e) => e.eventId)
          .toList();
      expect(ordered, <String>['e2', 'e3', 'e1']);
    });

    test('失败结果不改变本地顺序', () async {
      final service = ImService();
      const sid = 'sess-q';
      service.eventLifecycleQueues[sid] = <EventLifecycleQueueItem>[
        buildItem('e1', position: 1),
        buildItem('e2', position: 2),
      ];

      await service.handleDownstreamForTest(
        packet('queue_reorder_result', <String, dynamic>{
          'session_id': sid,
          'success': false,
          'msg': '',
        }),
      );

      final items = service.queueItemsForSession(sid);
      final positions = <String, int>{
        for (final e in items) e.eventId: e.queuePosition,
      };
      expect(positions['e1'], 1);
      expect(positions['e2'], 2);
    });
  });
}
