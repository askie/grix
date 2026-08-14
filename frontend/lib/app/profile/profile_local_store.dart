import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart' show debugPrint;
import 'package:path/path.dart' as p;

import 'profile_paths.dart';

/// 桌面端 profile 内的账号态 KV 存储（仅 io 平台使用）。
///
/// 数据落在 `profiles/<name>/auth_session.json`，替代全局单份的
/// SharedPreferences，使多实例登录不同账号时凭证互不覆盖。
/// 写入为原子替换：先写临时文件再 rename，避免中途崩溃留下半个 json。
class ProfileLocalStore {
  ProfileLocalStore._(this._file, this._data);

  static const String fileName = 'auth_session.json';

  static ProfileLocalStore? _cached;
  static Future<ProfileLocalStore>? _creating;

  /// 当前进程 profile 的存储单例。
  static Future<ProfileLocalStore> instance() {
    if (_cached != null) return Future.value(_cached);
    return _creating ??= () async {
      final dir = await ProfilePaths.currentDir();
      final store = await open(File(p.join(dir.path, fileName)));
      _cached = store;
      return store;
    }();
  }

  /// 打开任意 json 文件为存储（实例列表读取其他 profile、单测均复用此入口）。
  static Future<ProfileLocalStore> open(File file) async {
    var data = <String, Object?>{};
    try {
      if (await file.exists()) {
        final raw = await file.readAsString();
        if (raw.trim().isNotEmpty) {
          final decoded = jsonDecode(raw);
          if (decoded is Map<String, dynamic>) {
            data = Map<String, Object?>.from(decoded);
          }
        }
      }
    } catch (e) {
      // 文件损坏按空库处理（用户最多重新登录一次），不阻塞启动。
      debugPrint('⚠️ ProfileLocalStore load failed, start empty: $e');
      data = <String, Object?>{};
    }
    return ProfileLocalStore._(file, data);
  }

  final File _file;
  final Map<String, Object?> _data;
  Future<void> _flushChain = Future.value();

  Object? get(String key) => _data[key];

  String? getString(String key) {
    final value = _data[key];
    return value is String ? value : null;
  }

  int? getInt(String key) {
    final value = _data[key];
    if (value is int) return value;
    if (value is String) return int.tryParse(value.trim());
    return null;
  }

  bool? getBool(String key) {
    final value = _data[key];
    return value is bool ? value : null;
  }

  bool containsKey(String key) => _data.containsKey(key);

  Future<void> set(String key, Object? value) {
    if (value == null) {
      _data.remove(key);
    } else {
      _data[key] = value;
    }
    return _scheduleFlush();
  }

  Future<void> remove(String key) => set(key, null);

  /// 串行化落盘：内存态先行更新，写文件排队执行，后写覆盖先写。
  Future<void> _scheduleFlush() {
    final snapshot = jsonEncode(_data);
    _flushChain = _flushChain.then((_) => _writeAtomic(snapshot));
    return _flushChain;
  }

  Future<void> _writeAtomic(String content) async {
    try {
      final tmp = File('${_file.path}.tmp');
      await tmp.writeAsString(content, flush: true);
      try {
        await tmp.rename(_file.path);
      } on FileSystemException {
        // Windows 上 rename 覆盖已存在目标可能失败：删除旧文件后重试。
        if (await _file.exists()) {
          await _file.delete();
        }
        await tmp.rename(_file.path);
      }
    } catch (e) {
      debugPrint('⚠️ ProfileLocalStore flush failed: $e');
    }
  }

  /// 仅测试用：清空单例缓存。
  static void resetForTest() {
    _cached = null;
    _creating = null;
  }
}
