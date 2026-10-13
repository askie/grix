import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/themes/app_theme.dart';
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

  testWidgets('emphasized badge uses a solid warning capsule', (tester) async {
    ChatDraftIndex.update(sessionId: 's1', hasDraft: true);

    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: SessionDraftBadge(sessionIds: ['s1'], emphasized: true),
        ),
      ),
    );
    await tester.pump();

    final text = tester.widget<Text>(find.text('conversations_draft_badge'));
    expect(text.style?.color, AppTheme.lightTextPrimary);
    expect(text.style?.fontSize, 12);
    expect(text.style?.fontWeight, FontWeight.w700);

    final container = tester.widget<Container>(
      find
          .ancestor(
            of: find.text('conversations_draft_badge'),
            matching: find.byType(Container),
          )
          .first,
    );
    final decoration = container.decoration! as BoxDecoration;
    expect(decoration.color, AppTheme.warningColor);
  });
}
