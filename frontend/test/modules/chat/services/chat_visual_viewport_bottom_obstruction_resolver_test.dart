import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/services/chat_visual_viewport_bottom_obstruction_resolver.dart';

void main() {
  group('ChatVisualViewportBottomObstructionResolver', () {
    test('returns keyboard obstruction from layout and visual viewport delta',
        () {
      final obstruction = ChatVisualViewportBottomObstructionResolver.resolve(
        layoutViewportHeight: 800,
        visualViewportHeight: 540,
        visualViewportOffsetTop: 0,
      );

      expect(obstruction, 260);
    });

    test('subtracts top offset introduced by mobile browser viewport shifts',
        () {
      final obstruction = ChatVisualViewportBottomObstructionResolver.resolve(
        layoutViewportHeight: 800,
        visualViewportHeight: 560,
        visualViewportOffsetTop: 20,
      );

      expect(obstruction, 220);
    });

    test('clamps negative obstruction to zero', () {
      final obstruction = ChatVisualViewportBottomObstructionResolver.resolve(
        layoutViewportHeight: 800,
        visualViewportHeight: 820,
        visualViewportOffsetTop: 0,
      );

      expect(obstruction, 0);
    });
  });
}
