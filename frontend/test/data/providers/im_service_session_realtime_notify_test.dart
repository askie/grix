import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/session_activity_model.dart';
import 'package:grix/data/providers/im_service.dart';

void main() {
  testWidgets(
    'a realtime event of one session only rebuilds readers of that session',
    (tester) async {
      final service = ImService();
      final builds = <String, int>{'a': 0, 'b': 0};
      final live = <String, bool>{};

      Widget reader(String sid) => Obx(() {
        builds[sid] = builds[sid]! + 1;
        live[sid] = service.hasSessionLiveActivity(sid);
        return const SizedBox.shrink();
      });

      await tester.pumpWidget(
        Directionality(
          textDirection: TextDirection.ltr,
          child: Column(children: [reader('a'), reader('b')]),
        ),
      );
      expect(builds, {'a': 1, 'b': 1});

      service.agentOutputStates['b'] = <String, dynamic>{'state': 'streaming'};
      await tester.pump();
      expect(builds, {'a': 1, 'b': 2});
      expect(live, {'a': false, 'b': true});

      service.sessionActivities['a'] = <SessionActivityModel>[
        SessionActivityModel.fromJson(<String, dynamic>{
          'session_id': 'a',
          'kind': 'composing',
          'active': true,
          'actor_type': 'agent',
          'actor_id': '42',
          'expires_at': DateTime.now()
              .add(const Duration(minutes: 1))
              .millisecondsSinceEpoch,
        }),
      ];
      await tester.pump();
      expect(builds, {'a': 2, 'b': 2});
      expect(live['a'], isTrue);

      service.agentOutputStates.remove('b');
      await tester.pump();
      expect(builds, {'a': 2, 'b': 3});
      expect(live['b'], isFalse);
    },
  );
}
