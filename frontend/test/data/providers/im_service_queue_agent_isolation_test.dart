import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SharedPreferences.setMockInitialValues(<String, Object>{});
  });

  String packet(String cmd, Map<String, dynamic> payload) {
    return jsonEncode(<String, dynamic>{'cmd': cmd, 'payload': payload});
  }

  SessionModel groupSession(String sid) {
    return SessionModel(
      sessionId: sid,
      title: 'Group Session',
      type: 'group',
      peerType: 0,
      updatedAt: 0,
      lastMessageTime: 0,
    );
  }

  group('group chat queue isolation by agent', () {
    test('two agents in one session keep separate queues', () async {
      final service = ImService();
      const sid = 'sess-group-queue';
      const agentA = '9001';
      const agentB = '9002';

      await service.handleDownstreamForTest(
        packet('queue_snapshot', <String, dynamic>{
          'session_id': sid,
          'agent_id': agentA,
          'running': <String>[],
          'queued': <Map<String, dynamic>>[
            <String, dynamic>{
              'event_id': 'evt-a1',
              'position': 1,
              'content_preview': 'A task',
              'actions': <String>['cancel'],
            },
          ],
        }),
      );
      await service.handleDownstreamForTest(
        packet('queue_snapshot', <String, dynamic>{
          'session_id': sid,
          'agent_id': agentB,
          'running': <String>['evt-b-run'],
          'queued': <Map<String, dynamic>>[
            <String, dynamic>{
              'event_id': 'evt-b1',
              'position': 1,
              'content_preview': 'B task',
              'actions': <String>['cancel'],
            },
          ],
        }),
      );

      final itemsA = service.queueItemsForSession(sid, agentId: agentA);
      final itemsB = service.queueItemsForSession(sid, agentId: agentB);
      expect(itemsA.map((e) => e.eventId), ['evt-a1']);
      expect(itemsB.map((e) => e.eventId).toSet(), {'evt-b-run', 'evt-b1'});
      expect(service.queueCountForSession(sid, agentId: agentA), 1);
      expect(service.queueCountForSession(sid, agentId: agentB), 2);
    });

    test('empty snapshot for agent A does not clear agent B queue', () async {
      final service = ImService();
      const sid = 'sess-group-empty-a';
      const agentA = '9101';
      const agentB = '9102';

      await service.handleDownstreamForTest(
        packet('queue_snapshot', <String, dynamic>{
          'session_id': sid,
          'agent_id': agentA,
          'running': <String>[],
          'queued': <Map<String, dynamic>>[
            <String, dynamic>{
              'event_id': 'evt-a',
              'position': 1,
              'content_preview': 'A',
              'actions': <String>['cancel'],
            },
          ],
        }),
      );
      await service.handleDownstreamForTest(
        packet('queue_snapshot', <String, dynamic>{
          'session_id': sid,
          'agent_id': agentB,
          'running': <String>[],
          'queued': <Map<String, dynamic>>[
            <String, dynamic>{
              'event_id': 'evt-b',
              'position': 1,
              'content_preview': 'B',
              'actions': <String>['cancel'],
            },
          ],
        }),
      );

      await service.handleDownstreamForTest(
        packet('queue_snapshot', <String, dynamic>{
          'session_id': sid,
          'agent_id': agentA,
          'running': <String>[],
          'queued': <Map<String, dynamic>>[],
        }),
      );

      expect(service.queueItemsForSession(sid, agentId: agentA), isEmpty);
      expect(
        service.queueItemsForSession(sid, agentId: agentB).single.eventId,
        'evt-b',
      );
      expect(service.queueCountForSession(sid, agentId: agentB), 1);
    });

    test('toolbar target selects which agent queue is shown', () async {
      final service = ImService();
      const sid = 'sess-group-toolbar-target';
      const agentA = '9201';
      const agentB = '9202';
      service.sessions.add(groupSession(sid));

      await service.handleDownstreamForTest(
        packet('queue_snapshot', <String, dynamic>{
          'session_id': sid,
          'agent_id': agentA,
          'running': <String>[],
          'queued': <Map<String, dynamic>>[
            <String, dynamic>{
              'event_id': 'evt-a',
              'position': 1,
              'content_preview': 'A',
              'actions': <String>['cancel'],
            },
          ],
        }),
      );
      await service.handleDownstreamForTest(
        packet('queue_snapshot', <String, dynamic>{
          'session_id': sid,
          'agent_id': agentB,
          'running': <String>[],
          'queued': <Map<String, dynamic>>[
            <String, dynamic>{
              'event_id': 'evt-b',
              'position': 1,
              'content_preview': 'B',
              'actions': <String>['cancel'],
            },
          ],
        }),
      );

      service.setGroupToolbarTargetAgent(sid, agentId: agentA);
      expect(service.queueItemsForSession(sid).single.eventId, 'evt-a');
      service.setGroupToolbarTargetAgent(sid, agentId: agentB);
      expect(service.queueItemsForSession(sid).single.eventId, 'evt-b');
    });
  });
}
