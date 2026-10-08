import 'package:flutter/widgets.dart';

/// Chat message list scroll controller.
///
/// [ScrollController.jumpTo] always drops the current scroll activity: a
/// finger drag is cancelled (the list stops following the finger until it is
/// lifted and pressed again) and a fling loses its velocity. Viewport
/// compensation after history paging runs while the user is still scrolling,
/// so it goes through [shiftViewportTo] instead, which keeps the gesture.
class ChatScrollController extends ScrollController {
  ChatScrollController({super.initialScrollOffset, super.keepScrollOffset});

  bool anchorsHistoryInLayout = false;
  bool? historyContentFitsViewport;
  void Function(Set<String>)? prepareForWindowChange;
  (String, String)? Function()? readingRange;
  double? _layoutPixels;
  double get unlaidScrollDelta => hasClients && _layoutPixels != null
      ? position.pixels - _layoutPixels!
      : 0;

  void recordLayoutPixels() {
    if (hasClients) _layoutPixels = position.pixels;
  }

  /// Changes the coordinate origin during layout, retaining drag/fling
  /// activity. New dimensions restart ballistic simulation from this origin.
  void rebaseViewportTo(double pixels) {
    final position = this.position;
    if (position is _ChatScrollPosition) {
      position.rebaseViewportTo(pixels);
    } else {
      position.correctBy(pixels - position.pixels);
    }
  }

  @override
  ScrollPosition createScrollPosition(
    ScrollPhysics physics,
    ScrollContext context,
    ScrollPosition? oldPosition,
  ) {
    return _ChatScrollPosition(
      physics: physics,
      context: context,
      initialPixels: initialScrollOffset,
      keepScrollOffset: keepScrollOffset,
      oldPosition: oldPosition,
      debugLabel: debugLabel,
    );
  }

  /// Moves the viewport to [target] without cancelling an in-progress user
  /// drag, hold or fling. Falls back to [jumpTo] when no gesture is active.
  void shiftViewportTo(double target) {
    final position = this.position;
    if (position is _ChatScrollPosition) {
      position.shiftPreservingActivity(target);
      return;
    }
    jumpTo(target);
  }
}

class _ChatScrollPosition extends ScrollPositionWithSingleContext {
  _ChatScrollPosition({
    required super.physics,
    required super.context,
    super.initialPixels,
    super.keepScrollOffset,
    super.oldPosition,
    super.debugLabel,
  });

  void rebaseViewportTo(double value) {
    // DrivenScrollActivity owns an absolute tween and deliberately ignores
    // new dimensions. Cancel any such positioning animation (top, bottom or
    // ensureVisible) before changing origins; its old coordinates cannot be
    // resumed safely. Relative gestures and ballistic dimension handling stay
    // intact. There is no deferred restart to overtake a later user gesture.
    if (activity is DrivenScrollActivity) goIdle();
    correctBy(value - pixels);
  }

  void shiftPreservingActivity(double value) {
    final current = activity;
    if (current is DragScrollActivity || current is HoldScrollActivity) {
      // Drags apply relative finger deltas, so moving the base offset keeps
      // the content under the finger without breaking the gesture.
      if (pixels != value) {
        forcePixels(value);
      }
      return;
    }
    if (current is BallisticScrollActivity) {
      // A fling simulates absolute positions; restart it from the shifted
      // offset with the same velocity, as applyNewDimensions does.
      final velocity = current.velocity;
      if (pixels != value) {
        forcePixels(value);
      }
      goBallistic(velocity);
      return;
    }
    jumpTo(value);
  }
}
