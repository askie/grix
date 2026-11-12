/// Programmatic controls for a Flutter-embedded Grix widget view.
class GrixWidgetController {
  void Function(bool visible)? _setVisibleHandler;
  void Function()? _closeHandler;

  /// Informs the hosted page whether the widget is currently visible.
  ///
  /// Calling with `true` clears the unread badge inside the page.
  void setVisible(bool visible) => _setVisibleHandler?.call(visible);

  /// Triggers the same close action as the header close button. This emits the
  /// `onClose` callback through the normal bridge event flow.
  void close() => _closeHandler?.call();

  void attach({
    required void Function(bool visible) setVisible,
    required void Function() close,
  }) {
    _setVisibleHandler = setVisible;
    _closeHandler = close;
  }

  void detach() {
    _setVisibleHandler = null;
    _closeHandler = null;
  }
}
