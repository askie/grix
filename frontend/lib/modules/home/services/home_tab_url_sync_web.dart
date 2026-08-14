import 'dart:async';
import 'dart:js_interop';

import 'package:web/web.dart' as web;

import 'home_tab_url_sync_base.dart';

HomeTabUrlSync createHomeTabUrlSync() => _WebHomeTabUrlSync();

class _WebHomeTabUrlSync implements HomeTabUrlSync {
  _WebHomeTabUrlSync() {
    _popStateListener = ((web.Event _) {
      _pathChangedController.add(currentPath);
    }).toJS;
    web.window.addEventListener('popstate', _popStateListener);
  }

  final StreamController<String> _pathChangedController =
      StreamController<String>.broadcast();
  late final JSFunction _popStateListener;

  @override
  String get currentPath => web.window.location.pathname;

  @override
  Stream<String> get onPathChanged => _pathChangedController.stream;

  @override
  void pushPath(String path) {
    _setPath(path, replace: false);
  }

  @override
  void replacePath(String path) {
    _setPath(path, replace: true);
  }

  void _setPath(String path, {required bool replace}) {
    final currentUri = Uri.base;
    final nextUri = currentUri.replace(path: path);
    if (nextUri.path == currentUri.path) {
      return;
    }
    if (replace) {
      web.window.history.replaceState(null, '', nextUri.toString());
    } else {
      web.window.history.pushState(null, '', nextUri.toString());
    }
    _pathChangedController.add(nextUri.path);
  }

  @override
  void dispose() {
    web.window.removeEventListener('popstate', _popStateListener);
    _pathChangedController.close();
  }
}
