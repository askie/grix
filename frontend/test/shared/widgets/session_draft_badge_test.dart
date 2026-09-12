import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/utils/chat_draft_index.dart';
import 'package:grix/shared/widgets/session_draft_badge.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    ChatDraftIndex.resetForTest();
  });

  tearDown(() {
    ChatDraftIndex.resetForTest();
  });

  Future<void> pumpBadge(WidgetTester tester, List<String> sessionIds) {
    return tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Row(children: [SessionDraftBadge(sessionIds: sessionIds)]),
        ),
      ),
    );
  }

  testWidgets('shows nothing when no session has a draft', (tester) async {
    await pumpBadge(tester, const ['s1', 's2']);
    await tester.pump();

    expect(find.text('conversations_draft_badge'), findsNothing);
  });

  testWidgets('shows badge when any listed session has a draft', (
    tester,
  ) async {
    ChatDraftIndex.update(sessionId: 's2', hasDraft: true);

    await pumpBadge(tester, const ['s1', 's2']);
    await tester.pump();

    expect(find.text('conversations_draft_badge'), findsOneWidget);
  });

  testWidgets('reacts to draft index changes', (tester) async {
    await pumpBadge(tester, const ['s1']);
    await tester.pump();
    expect(find.text('conversations_draft_badge'), findsNothing);

    ChatDraftIndex.update(sessionId: 's1', hasDraft: true);
    await tester.pump();
    expect(find.text('conversations_draft_badge'), findsOneWidget);

    ChatDraftIndex.update(sessionId: 's1', hasDraft: false);
    await tester.pump();
    expect(find.text('conversations_draft_badge'), findsNothing);
  });
}
