import 'dart:io' show Platform;

import 'package:flutter/foundation.dart' show debugPrint;
import 'package:flutter/services.dart' show MissingPluginException;
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

import '../../app/profile/profile_paths.dart';

Future<DatabaseFactory> initLocalDatabaseFactory() async {
  if (Platform.isLinux || Platform.isMacOS || Platform.isWindows) {
    sqfliteFfiInit();
    // FFI 默认的 databases 路径相对进程工作目录解析；从 Finder/桌面双击启动时
    // 工作目录是不可写的 “/”，会导致本地库整体打不开，必须锚定到应用数据目录。
    // 目录按实例 profile 隔离（default 保持历史位置，存量用户零迁移）。
    try {
      final dbDir = await ProfilePaths.currentDatabasesDir();
      await databaseFactoryFfi.setDatabasesPath(dbDir.path);
    } on MissingPluginException {
      // 纯 Dart VM（单元测试）没有 path_provider 宿主实现，保留 FFI 默认路径。
      debugPrint(
        '⚠️ path_provider unavailable, local db falls back to cwd-relative path',
      );
    }
    return databaseFactoryFfi;
  }
  return databaseFactory;
}
