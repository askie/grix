import 'dart:async';
import 'dart:collection';
import 'dart:convert';

import 'package:flutter/widgets.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'user_image_cache_manager.dart';

/// Remembers the intrinsic size of chat markdown images so rebuilt bubbles can
/// reserve the final layout height before the image bytes finish decoding.
/// Without this, lazily rebuilt list items collapse to the placeholder height
/// and jump once the image arrives, which shifts the whole chat viewport.
///
/// Entries are keyed by [UserImageCacheManager.cacheKeyForImageUrl] so signed
/// URL variants (rotating OSS/COS signatures) share one record, and the map is
/// persisted to [SharedPreferences] so history sessions reserve the correct
/// box on the first frame after a cold start.
class ChatImageDimensionCache {
  ChatImageDimensionCache._();

  static const int _maxEntries = 512;
  static const String _prefsKey = 'chat_image_dims_v1';

  static final LinkedHashMap<String, Size> _sizes =
      LinkedHashMap<String, Size>();
  static const int _minPersistIntervalMs = 5000;

  static Future<void>? _loading;
  static bool _loaded = false;
  static bool _persistScheduled = false;
  static bool _dirty = false;
  static int _lastPersistMs = 0;

  // lookup 在每个图片组件的每次 build 里都会调用，而 cacheKeyForImageUrl
  // 要做 Uri 解析 + 查询参数排序重组；备忘录化后滚动热路径上只剩一次
  // map 命中。容量与 _maxEntries 同级即可。
  static final LinkedHashMap<String, String> _keyMemo =
      LinkedHashMap<String, String>();

  static String _keyFor(String url) {
    final memoized = _keyMemo.remove(url);
    if (memoized != null) {
      _keyMemo[url] = memoized;
      return memoized;
    }
    final stable = UserImageCacheManager.cacheKeyForImageUrl(url);
    final key = stable.isEmpty ? url : stable;
    _keyMemo[url] = key;
    while (_keyMemo.length > _maxEntries) {
      _keyMemo.remove(_keyMemo.keys.first);
    }
    return key;
  }

  /// Starts the disk load early (e.g. during app bootstrap) so the first
  /// chat page build can already reserve image boxes from persisted sizes.
  static void warmUp() {
    _ensureLoaded();
  }

  static Size? lookup(String url) {
    _ensureLoaded();
    final key = _keyFor(url);
    final size = _sizes.remove(key);
    if (size == null) {
      return null;
    }
    _sizes[key] = size;
    return size;
  }

  static void store(String url, Size size) {
    if (url.isEmpty || size.width <= 0 || size.height <= 0) {
      return;
    }
    final key = _keyFor(url);
    final previous = _sizes.remove(key);
    _sizes[key] = size;
    while (_sizes.length > _maxEntries) {
      _sizes.remove(_sizes.keys.first);
    }
    if (previous != size) {
      _schedulePersist();
    }
  }

  static void _ensureLoaded() {
    if (_loaded || _loading != null) {
      return;
    }
    _loading = _loadFromDisk();
  }

  static Future<void> _loadFromDisk() async {
    try {
      final prefs = await SharedPreferences.getInstance();
      final raw = prefs.getString(_prefsKey);
      if (raw == null || raw.isEmpty) {
        return;
      }
      final decoded = jsonDecode(raw);
      if (decoded is! Map) {
        return;
      }
      decoded.forEach((key, value) {
        if (key is! String || value is! String) {
          return;
        }
        final parts = value.split(',');
        if (parts.length != 2) {
          return;
        }
        final width = double.tryParse(parts[0]) ?? 0;
        final height = double.tryParse(parts[1]) ?? 0;
        if (width <= 0 || height <= 0) {
          return;
        }
        // In-memory entries recorded before the disk load finished win.
        _sizes.putIfAbsent(key, () => Size(width, height));
      });
      while (_sizes.length > _maxEntries) {
        _sizes.remove(_sizes.keys.first);
      }
    } catch (_) {
      // Persistence is best-effort: tests and platforms without the plugin
      // fall back to the in-memory session cache.
    } finally {
      _loaded = true;
      _loading = null;
    }
  }

  // 落盘限频：滚动浏览大量新图时每张解码完成都会 store 一次，若每次都
  // jsonEncode 全表 + 走平台通道，会在滚动热路径上制造零散卡顿。冷却窗内
  // 只标脏不落盘，靠冷却后的下一次 store 或 App 退后台的 flushIfDirty 补写。
  // 不用 Timer 防抖，避免在 widget 测试收尾时留下挂起定时器
  // （!timersPending 断言）；微任务只合并同一事件轮内的多次 store。
  static void _schedulePersist() {
    _dirty = true;
    if (_persistScheduled) {
      return;
    }
    final nowMs = DateTime.now().millisecondsSinceEpoch;
    if (nowMs - _lastPersistMs < _minPersistIntervalMs) {
      return;
    }
    _persistScheduled = true;
    scheduleMicrotask(() {
      _persistScheduled = false;
      if (!_dirty) {
        return;
      }
      _dirty = false;
      _lastPersistMs = DateTime.now().millisecondsSinceEpoch;
      unawaited(_persistToDisk());
    });
  }

  /// App 退后台等时机补写冷却窗内被跳过的落盘。
  static void flushIfDirty() {
    if (!_dirty) {
      return;
    }
    _dirty = false;
    _lastPersistMs = DateTime.now().millisecondsSinceEpoch;
    unawaited(_persistToDisk());
  }

  static Future<void> _persistToDisk() async {
    try {
      final prefs = await SharedPreferences.getInstance();
      final payload = <String, String>{
        for (final entry in _sizes.entries)
          entry.key:
              '${entry.value.width.toStringAsFixed(0)},'
                  '${entry.value.height.toStringAsFixed(0)}',
      };
      await prefs.setString(_prefsKey, jsonEncode(payload));
    } catch (_) {
      // Best-effort; the in-memory cache still covers this session.
    }
  }

  @visibleForTesting
  static Future<void> flushForTest() async {
    _dirty = false;
    await _persistToDisk();
  }

  @visibleForTesting
  static Future<void> ensureLoadedForTest() async {
    _ensureLoaded();
    await _loading;
  }

  @visibleForTesting
  static void resetForTest() {
    _persistScheduled = false;
    _dirty = false;
    _lastPersistMs = 0;
    _loading = null;
    _loaded = false;
    _sizes.clear();
    _keyMemo.clear();
  }
}
