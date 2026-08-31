import 'dart:collection';

import 'package:flutter/widgets.dart';

/// Remembers the intrinsic size of chat markdown images within the session so
/// rebuilt bubbles can reserve the final layout height before the image bytes
/// finish decoding. Without this, lazily rebuilt list items collapse to the
/// placeholder height and jump once the image arrives, which shifts the whole
/// chat viewport.
class ChatImageDimensionCache {
  ChatImageDimensionCache._();

  static const int _maxEntries = 512;

  static final LinkedHashMap<String, Size> _sizes =
      LinkedHashMap<String, Size>();

  static Size? lookup(String url) {
    final size = _sizes.remove(url);
    if (size == null) {
      return null;
    }
    _sizes[url] = size;
    return size;
  }

  static void store(String url, Size size) {
    if (url.isEmpty || size.width <= 0 || size.height <= 0) {
      return;
    }
    _sizes.remove(url);
    _sizes[url] = size;
    while (_sizes.length > _maxEntries) {
      _sizes.remove(_sizes.keys.first);
    }
  }

  @visibleForTesting
  static void resetForTest() {
    _sizes.clear();
  }
}
