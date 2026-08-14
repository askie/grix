import 'dart:io' show File, Platform;

import 'package:flutter/foundation.dart';
import 'package:pasteboard/pasteboard.dart';
import 'package:path/path.dart' as p;

import 'chat_file_interceptor_base.dart';
import 'chat_image_codec.dart';

ChatFileInterceptor createChatFileInterceptor() {
  if (!kIsWeb && (Platform.isMacOS || Platform.isWindows || Platform.isLinux)) {
    return _DesktopFileInterceptor();
  }
  return _MobileStubFileInterceptor();
}

/// 移动端空实现（iOS/Android 不需要快捷键粘贴图片）
class _MobileStubFileInterceptor implements ChatFileInterceptor {
  @override
  bool get isDragOver => false;

  @override
  void setDragOverCallback(void Function(bool isOver) callback) {}

  @override
  void register(FileInterceptorCallback onFiles) {}

  @override
  void unregister() {}

  @override
  Future<bool> handlePasteIntent() async => false;
}

/// 桌面端实现：通过 pasteboard 包读取剪贴板里的图片或文件
class _DesktopFileInterceptor implements ChatFileInterceptor {
  FileInterceptorCallback? _onFiles;

  @override
  bool get isDragOver => false;

  @override
  void setDragOverCallback(void Function(bool isOver) callback) {}

  @override
  void register(FileInterceptorCallback onFiles) {
    _onFiles = onFiles;
  }

  @override
  void unregister() {
    _onFiles = null;
  }

  @override
  Future<bool> handlePasteIntent() async {
    final cb = _onFiles;
    if (cb == null) return false;

    // 这里不吞异常：粘贴失败要让调用方提示用户，不能静默什么都不发生。
    final Uint8List? imageBytes = await Pasteboard.image;
    if (imageBytes != null && imageBytes.isNotEmpty) {
      // Windows 剪贴板给回来的是未压缩的 BMP 位图，统一转成 PNG 再进上传链路，
      // 否则会得到一个「挂着 .png 名字、内容却是 BMP」的附件。
      final pngBytes = await ChatImageCodec.toPng(imageBytes);
      final fileName = 'clipboard_${DateTime.now().millisecondsSinceEpoch}.png';
      await cb(pngBytes, fileName, 'image/png');
      return true;
    }

    // 剪贴板里没有位图时，再看有没有从文件管理器复制来的文件（与 Web 端行为对齐）。
    final paths = await Pasteboard.files();
    for (final path in paths) {
      final file = File(path);
      if (!file.existsSync()) continue;
      final bytes = await file.readAsBytes();
      if (bytes.isEmpty) continue;
      // 落成图片 / 视频 / 文件由下游 stageFileFromBytes 按文件名统一分流。
      await cb(bytes, p.basename(path), 'application/octet-stream');
      return true;
    }

    return false;
  }
}
