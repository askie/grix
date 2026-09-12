part of 'chat_controller.dart';

/// Shared "scroll a message item into view, then flash-highlight it"
/// primitive used by both the edited-message notice pill and the pinned
/// message bar.
///
/// The message list is a plain lazily-built `ListView` with a small cache
/// extent ([ChatView._messageListCacheExtent]), so a target several screens
/// away is not mounted yet and its [GlobalKey] has no context. Rather than
/// estimate a pixel offset from variable-height bubbles (markdown, images,
/// cards), this walks the scroll position one viewport-sized step at a time
/// toward the target, letting the list build newly-revealed items, until the
/// target mounts or the scroll extent is exhausted.
class _ChatMessageJumpController {
  const _ChatMessageJumpController(this.owner);

  final ChatController owner;

  static const int _maxSteps = 60;
  static const double _stepFraction = 0.85;
  static const double _defaultViewportHeight = 600;

  /// Scrolls the message identified by [itemKey] into view and flashes it.
  /// Returns false when the message could not be located at all (removed
  /// from the loaded window) or the scrollable ran out of room before it
  /// could be reached.
  Future<bool> jumpToItem(String itemKey) async {
    final key = itemKey.trim();
    if (key.isEmpty) return false;
    if (!owner.imService.currentMessages.any(
      (m) => ChatMessageIdentity.selectionKey(m) == key,
    )) {
      return false;
    }
    if (!owner.scrollController.hasClients) return false;

    for (var attempt = 0; attempt < _maxSteps; attempt++) {
      final renderBox = owner._pageStateController._resolveMountedItemRenderBox(
        key,
      );
      if (renderBox != null) {
        await _alignAndHighlight(key, renderBox);
        return true;
      }
      if (!_stepToward(key)) {
        break;
      }
      await WidgetsBinding.instance.endOfFrame;
    }

    final finalRenderBox = owner._pageStateController
        ._resolveMountedItemRenderBox(key);
    if (finalRenderBox != null) {
      await _alignAndHighlight(key, finalRenderBox);
      return true;
    }
    return false;
  }

  /// Moves the scroll position one viewport-height step toward [itemKey].
  /// Returns false when direction can't be determined or the scrollable is
  /// already at the edge in that direction (no further progress possible).
  bool _stepToward(String itemKey) {
    if (!owner.scrollController.hasClients) return false;
    final position = owner.scrollController.position;
    final messages = owner.imService.currentMessages;
    final targetIndex = messages.indexWhere(
      (m) => ChatMessageIdentity.selectionKey(m) == itemKey,
    );
    if (targetIndex == -1) return false;

    final referenceIndex = _indexOfAnyVisibleMessage(messages);
    int direction;
    if (referenceIndex != null) {
      if (targetIndex == referenceIndex) return false;
      direction = targetIndex > referenceIndex ? 1 : -1;
    } else {
      // No mounted reference item (e.g. mid-jump between steps): fall back
      // to comparing against the scroll edges.
      direction = position.pixels <= position.minScrollExtent + 1 ? 1 : -1;
    }

    final viewportHeight =
        owner._pageStateController
            ._resolveScrollableViewportRenderBox()
            ?.size
            .height ??
        _defaultViewportHeight;
    final step = viewportHeight * _stepFraction * direction;
    final next = (position.pixels + step)
        .clamp(position.minScrollExtent, position.maxScrollExtent)
        .toDouble();
    if ((next - position.pixels).abs() < 1) {
      return false;
    }
    owner.scrollController.jumpTo(next);
    return true;
  }

  int? _indexOfAnyVisibleMessage(List<MessageModel> messages) {
    final viewport = owner._pageStateController
        ._resolveScrollableViewportRenderBox();
    if (viewport == null) return null;
    for (var i = 0; i < messages.length; i++) {
      final key = ChatMessageIdentity.selectionKey(messages[i]);
      final renderBox = owner._pageStateController._resolveMountedItemRenderBox(
        key,
      );
      if (renderBox == null) continue;
      final topLeft = renderBox.localToGlobal(Offset.zero, ancestor: viewport);
      final top = topLeft.dy;
      final bottom = top + renderBox.size.height;
      if (bottom > 0 && top < viewport.size.height) {
        return i;
      }
    }
    return null;
  }

  Future<void> _alignAndHighlight(String itemKey, RenderBox renderBox) async {
    final context = owner
        .peekMessageViewportItemGlobalKey(itemKey)
        ?.currentContext;
    if (context != null) {
      await Scrollable.ensureVisible(
        context,
        alignment: 0.25,
        duration: const Duration(milliseconds: 200),
        curve: Curves.easeOut,
      );
    }
    owner.triggerMessageHighlight(itemKey);
  }
}
