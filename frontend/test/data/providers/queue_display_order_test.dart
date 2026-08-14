import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/im_service.dart';

EventLifecycleQueueItem _item({
  required String eventId,
  required String state,
  required int queuePosition,
  int updatedAt = 0,
}) {
  return EventLifecycleQueueItem(
    eventId: eventId,
    sessionId: 's1',
    messageId: '',
    clientMsgId: '',
    contentPreview: eventId,
    state: state,
    queuePosition: queuePosition,
    actions: const <String>[],
    updatedAt: updatedAt,
  );
}

void main() {
  group('orderQueueItemsForDisplay', () {
    test('最新入队的消息置顶，running 沉到队尾', () {
      // 存储态顺序：queued#1(最老等待) → queued#2 → queued#3 → running
      final input = [
        _item(eventId: 'q1', state: 'queued', queuePosition: 1),
        _item(eventId: 'q2', state: 'queued', queuePosition: 2),
        _item(eventId: 'q3', state: 'queued', queuePosition: 3),
        _item(eventId: 'run', state: 'running', queuePosition: 0),
      ];

      final ordered = orderQueueItemsForDisplay(input);

      expect(
        ordered.map((e) => e.eventId).toList(),
        ['q3', 'q2', 'q1', 'run'],
      );
    });

    test('多个 running 时按 updatedAt 倒序，且都在队尾', () {
      final input = [
        _item(eventId: 'run_old', state: 'running', queuePosition: 0, updatedAt: 100),
        _item(eventId: 'q1', state: 'queued', queuePosition: 1),
        _item(eventId: 'run_new', state: 'running', queuePosition: 0, updatedAt: 200),
      ];

      final ordered = orderQueueItemsForDisplay(input);

      expect(ordered.map((e) => e.eventId).toList(), ['q1', 'run_new', 'run_old']);
    });

    test('不修改传入的原列表', () {
      final input = [
        _item(eventId: 'q1', state: 'queued', queuePosition: 1),
        _item(eventId: 'q2', state: 'queued', queuePosition: 2),
      ];

      orderQueueItemsForDisplay(input);

      expect(input.map((e) => e.eventId).toList(), ['q1', 'q2']);
    });
  });
}
