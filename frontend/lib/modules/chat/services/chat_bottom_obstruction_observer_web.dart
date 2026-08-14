import 'dart:async';
import 'dart:js_interop';

import 'package:web/web.dart' as web;

import 'chat_bottom_obstruction_observer_base.dart';
import 'chat_visual_viewport_bottom_obstruction_resolver.dart';

ChatBottomObstructionObserver createChatBottomObstructionObserver() {
  return _WebChatBottomObstructionObserver();
}

class _WebChatBottomObstructionObserver
    implements ChatBottomObstructionObserver {
  _WebChatBottomObstructionObserver() {
    _windowResizeListener = ((web.Event _) {
      _emitLatest();
    }).toJS;
    web.window.addEventListener('resize', _windowResizeListener);

    final viewport = web.window.visualViewport;
    if (viewport != null) {
      _viewportResizeListener = ((web.Event _) {
        _emitLatest();
      }).toJS;
      _viewportScrollListener = ((web.Event _) {
        _emitLatest();
      }).toJS;
      viewport.addEventListener('resize', _viewportResizeListener);
      viewport.addEventListener('scroll', _viewportScrollListener);
    }

    _currentBottomObstruction = _computeBottomObstruction();
  }

  final StreamController<double> _changedController =
      StreamController<double>.broadcast();
  late final JSFunction _windowResizeListener;
  JSFunction? _viewportResizeListener;
  JSFunction? _viewportScrollListener;
  double _currentBottomObstruction = 0;

  @override
  double get currentBottomObstruction => _currentBottomObstruction;

  @override
  Stream<double> get onChanged => _changedController.stream;

  @override
  void dispose() {
    web.window.removeEventListener('resize', _windowResizeListener);

    final viewport = web.window.visualViewport;
    if (viewport != null) {
      final viewportResizeListener = _viewportResizeListener;
      if (viewportResizeListener != null) {
        viewport.removeEventListener('resize', viewportResizeListener);
      }
      final viewportScrollListener = _viewportScrollListener;
      if (viewportScrollListener != null) {
        viewport.removeEventListener('scroll', viewportScrollListener);
      }
    }

    _changedController.close();
  }

  void _emitLatest() {
    final nextBottomObstruction = _computeBottomObstruction();
    if ((nextBottomObstruction - _currentBottomObstruction).abs() < 0.5) {
      return;
    }
    _currentBottomObstruction = nextBottomObstruction;
    if (_changedController.isClosed) {
      return;
    }
    _changedController.add(nextBottomObstruction);
  }

  double _computeBottomObstruction() {
    final viewport = web.window.visualViewport;
    if (viewport == null) {
      return 0;
    }

    return ChatVisualViewportBottomObstructionResolver.resolve(
      layoutViewportHeight: web.window.innerHeight.toDouble(),
      visualViewportHeight: viewport.height,
      visualViewportOffsetTop: viewport.offsetTop,
    );
  }
}
