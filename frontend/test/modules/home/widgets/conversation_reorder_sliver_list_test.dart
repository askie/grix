import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/modules/home/controllers/conversations_controller.dart';
import 'package:grix/modules/home/widgets/conversation_reorder_sliver_list.dart';

ConversationListItem _buildItem(
  String sessionId, {
  required int activityAt,
  String lastMessage = '',
}) {
  final session = SessionModel(
    sessionId: sessionId,
    title: sessionId,
    type: 'group',
    updatedAt: activityAt,
    lastMessage: lastMessage,
    lastMessageTime: activityAt,
  );
  return ConversationListItem(
    groupKey: 'session:$sessionId',
    latestSession: session,
    sessions: <SessionModel>[session],
    unreadCount: 0,
    isPinned: false,
    pinnedAt: 0,
  );
}

Widget _buildTestApp(List<ConversationListItem> sessions) {
  return MaterialApp(
    home: CustomScrollView(
      slivers: [
        ConversationReorderSliverList(
          sessions: sessions,
          itemBuilder: (context, item) {
            return SizedBox(
              height: 82,
              child: ColoredBox(
                color: Colors.white,
                child: Text(item.groupKey),
              ),
            );
          },
        ),
      ],
    ),
  );
}

double _moveDy(WidgetTester tester, String groupKey) {
  final transform = tester.widget<Transform>(
    find.byKey(ConversationReorderSliverList.moveKey(groupKey)),
  );
  return transform.transform.getTranslation().y;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('conversation moving to top still animates from below', (
    WidgetTester tester,
  ) async {
    final olderTop = _buildItem('a', activityAt: 100);
    final rising = _buildItem('b', activityAt: 90);

    await tester.pumpWidget(
      _buildTestApp(<ConversationListItem>[olderTop, rising]),
    );
    await tester.pump();

    final promoted = _buildItem('b', activityAt: 110, lastMessage: 'new');
    await tester.pumpWidget(
      _buildTestApp(<ConversationListItem>[promoted, olderTop]),
    );
    await tester.pump();

    final topOffset = _moveDy(tester, promoted.groupKey);
    expect(topOffset, greaterThan(0));
  });

  testWidgets(
    'top conversation stops move animation on repeated new messages',
    (WidgetTester tester) async {
      final olderTop = _buildItem('a', activityAt: 100);
      final rising = _buildItem('b', activityAt: 90);

      await tester.pumpWidget(
        _buildTestApp(<ConversationListItem>[olderTop, rising]),
      );
      await tester.pump();

      final firstPromotion = _buildItem('b', activityAt: 110, lastMessage: '1');
      await tester.pumpWidget(
        _buildTestApp(<ConversationListItem>[firstPromotion, olderTop]),
      );
      await tester.pump();

      final movingOffset = _moveDy(tester, firstPromotion.groupKey);
      expect(movingOffset, greaterThan(0));

      final repeatedUpdate = _buildItem('b', activityAt: 120, lastMessage: '2');
      await tester.pumpWidget(
        _buildTestApp(<ConversationListItem>[repeatedUpdate, olderTop]),
      );
      await tester.pump();

      final settledOffset = _moveDy(tester, repeatedUpdate.groupKey);
      expect(settledOffset, closeTo(0, 0.01));
    },
  );
}
