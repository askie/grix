import 'dart:async';

abstract class HomeTabUrlSync {
  String get currentPath;

  Stream<String> get onPathChanged;

  void pushPath(String path);

  void replacePath(String path);

  void dispose();
}
