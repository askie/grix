import 'package:flutter/foundation.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/widgets/chat_selection_area.dart';

Offset _textOffsetToPosition(RenderParagraph paragraph, int offset) {
  const caretPrototype = Rect.fromLTWH(0, 0, 2, 20);
  final localOffset =
      paragraph.getOffsetForCaret(TextPosition(offset: offset), caretPrototype) +
      Offset(0, paragraph.preferredLineHeight);
  return paragraph.localToGlobal(localOffset) + const Offset(0, -2);
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('ChatSelectionArea restores toolbar for multi-line selection', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: SizedBox(
            width: 220,
            child: ChatSelectionArea(
              child: Text('first line\nsecond line\nthird line'),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    final selectionAreaState = tester.state<SelectionAreaState>(
      find.byType(SelectionArea),
    );
    final localizations = MaterialLocalizations.of(
      tester.element(find.byType(SelectionArea)),
    );
    final copyLabel = localizations.copyButtonLabel;
    selectionAreaState.selectableRegion.selectAll(
      SelectionChangedCause.toolbar,
    );
    await tester.pump();

    expect(find.text(copyLabel), findsOneWidget);

    selectionAreaState.selectableRegion.hideToolbar(false);
    await tester.pump();
    expect(find.text(copyLabel), findsNothing);

    await tester.pump(const Duration(milliseconds: 140));
    await tester.pump();

    expect(find.text(copyLabel), findsOneWidget);
  });

  testWidgets(
    'ChatSelectionArea is disabled on iOS when not explicitly enabled',
    (WidgetTester tester) async {
      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(
            body: SizedBox(
              width: 220,
              child: ChatSelectionArea(
                enabled: false,
                child: Text('first line\nsecond line\nthird line'),
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.byType(SelectionArea), findsNothing);
      expect(find.text('first line\nsecond line\nthird line'), findsOneWidget);
    },
    variant: TargetPlatformVariant.only(TargetPlatform.iOS),
    skip: kIsWeb,
  );

  testWidgets(
    'ChatSelectionArea preserves multi-line mouse selection on macOS secondary tap',
    (WidgetTester tester) async {
      const text = 'first line\nsecond line\nthird line';

      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(
            body: SizedBox(
              width: 220,
              child: ChatSelectionArea(child: Text(text)),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      final paragraph = tester.renderObject<RenderParagraph>(
        find.descendant(
          of: find.text(text),
          matching: find.byType(RichText),
        ),
      );
      final selectionAreaFinder = find.byType(SelectionArea);
      final localizations = MaterialLocalizations.of(
        tester.element(selectionAreaFinder),
      );
      final copyLabel = localizations.copyButtonLabel;

      final primaryGesture = await tester.createGesture(
        kind: PointerDeviceKind.mouse,
      );
      addTearDown(primaryGesture.removePointer);

      await primaryGesture.down(_textOffsetToPosition(paragraph, 2));
      await tester.pump();
      await primaryGesture.moveTo(_textOffsetToPosition(paragraph, 22));
      await tester.pump();
      await primaryGesture.up();
      await tester.pumpAndSettle();

      final initialSelection = paragraph.selections.single;
      expect(initialSelection.baseOffset, 2);
      expect(initialSelection.isCollapsed, isFalse);
      expect(find.text(copyLabel), findsNothing);

      await tester.tapAt(
        _textOffsetToPosition(paragraph, 15),
        buttons: kSecondaryMouseButton,
        kind: PointerDeviceKind.mouse,
      );
      await tester.pumpAndSettle();

      expect(paragraph.selections.single, initialSelection);
      expect(find.text(copyLabel), findsOneWidget);
    },
    variant: TargetPlatformVariant.only(TargetPlatform.macOS),
    skip: kIsWeb,
  );
}
