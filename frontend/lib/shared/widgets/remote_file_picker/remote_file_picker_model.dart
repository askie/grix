import 'dart:io' show Platform;

import 'package:flutter/foundation.dart';

class RemoteFileNode {
  const RemoteFileNode({
    required this.id,
    required this.name,
    required this.isDirectory,
    this.size,
    this.modifiedAt,
    this.mimeType,
  });

  final String id;
  final String name;
  final bool isDirectory;
  final int? size;
  final DateTime? modifiedAt;
  final String? mimeType;

  @override
  bool operator ==(Object other) =>
      identical(this, other) || other is RemoteFileNode && id == other.id;

  @override
  int get hashCode => id.hashCode;
}

class RemoteFilePickerResult {
  const RemoteFilePickerResult({required this.selectedFiles});

  final List<RemoteFileNode> selectedFiles;
}

enum RemoteFileSelectionMode { single, multiple }

enum RemoteFilePickTarget { files, directories, both }

/// 列表排序模式。目录始终分组在文件之前，仅组内顺序随模式变化。
/// nameAsc 为默认（保持原有行为）；按钮在 nameAsc（字母升序）↔ timeDesc（时间降序）间切换。
enum RemoteFileSortMode { nameAsc, timeDesc }

typedef RemoteFileListProvider =
    Future<RemoteFileListResult> Function(
      String? parentId,
      RemoteFileListQuery query,
    );

typedef RemoteCreateFolderProvider =
    Future<RemoteFileNode> Function(String? parentId, String name);

class RemoteFileListResult {
  const RemoteFileListResult({
    required this.files,
    this.currentPath,
    this.machineName,
  });

  final List<RemoteFileNode> files;
  final String? currentPath;

  /// 当前列表所在机器的名字（由 connector 返回）。收藏时记录到收藏项上，
  /// 收藏夹据此分组并默认只展示当前机器的收藏。
  final String? machineName;
}

class RemoteFileListQuery {
  const RemoteFileListQuery({required this.showHidden, this.allowedExtensions});

  final bool showHidden;
  final List<String>? allowedExtensions;
}

bool get isDesktopPlatform {
  if (kIsWeb) return true;
  return Platform.isMacOS || Platform.isWindows || Platform.isLinux;
}
