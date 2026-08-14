import 'dart:async';
import 'dart:js_interop';
import 'dart:typed_data';

import 'package:web/web.dart' as web;

import 'chat_file_interceptor_base.dart';

ChatFileInterceptor createChatFileInterceptor() => _WebFileInterceptor();

class _WebFileInterceptor implements ChatFileInterceptor {
  JSFunction? _pasteListener;
  JSFunction? _dragOverListener;
  JSFunction? _dragLeaveListener;
  JSFunction? _dropListener;
  JSFunction? _dragEndListener;

  FileInterceptorCallback? _onFiles;
  void Function(bool isOver)? _onDragOver;
  bool _dragActive = false;

  @override
  bool get isDragOver => _dragActive;

  @override
  void setDragOverCallback(void Function(bool isOver) callback) {
    _onDragOver = callback;
  }

  @override
  void register(FileInterceptorCallback onFiles) {
    _onFiles = onFiles;
    _pasteListener = ((web.Event event) {
      _handlePaste(event);
    }).toJS;
    web.document.addEventListener('paste', _pasteListener);

    // 拖拽覆盖层（"松开以添加为附件"）的显隐由这里统一驱动：只有拖进来的
    // 真是文件才点亮，拖链接/选中文字一律不弹；并且 dragleave/drop/dragend
    // 任一发生都强制复位，避免文字/链接拖拽后覆盖层卡住不消失。
    _dragOverListener = ((web.Event event) {
      _handleDragOver(event);
    }).toJS;
    _dragLeaveListener = ((web.Event event) {
      _handleDragLeave(event);
    }).toJS;
    _dropListener = ((web.Event event) {
      _setDragOver(false);
    }).toJS;
    _dragEndListener = ((web.Event event) {
      _setDragOver(false);
    }).toJS;
    web.window.addEventListener('dragover', _dragOverListener);
    web.window.addEventListener('dragleave', _dragLeaveListener);
    web.window.addEventListener('drop', _dropListener);
    web.window.addEventListener('dragend', _dragEndListener);
  }

  @override
  void unregister() {
    if (_pasteListener != null) {
      web.document.removeEventListener('paste', _pasteListener!);
    }
    if (_dragOverListener != null) {
      web.window.removeEventListener('dragover', _dragOverListener!);
    }
    if (_dragLeaveListener != null) {
      web.window.removeEventListener('dragleave', _dragLeaveListener!);
    }
    if (_dropListener != null) {
      web.window.removeEventListener('drop', _dropListener!);
    }
    if (_dragEndListener != null) {
      web.window.removeEventListener('dragend', _dragEndListener!);
    }
    _pasteListener = null;
    _dragOverListener = null;
    _dragLeaveListener = null;
    _dropListener = null;
    _dragEndListener = null;
    _onFiles = null;
    _setDragOver(false);
    _onDragOver = null;
  }

  void _handleDragOver(web.Event event) {
    final dt = (event as web.DragEvent).dataTransfer;
    if (dt == null) return;
    if (_dragHasFiles(dt)) {
      // 必须 preventDefault 浏览器才会把本次拖拽视为可放下（否则 drop 不触发），
      // 同时点亮覆盖层。
      event.preventDefault();
      _setDragOver(true);
    }
  }

  void _handleDragLeave(web.Event event) {
    // 拖出窗口边界时 relatedTarget 为 null，此时复位覆盖层。
    final related = (event as web.DragEvent).relatedTarget;
    if (related == null) {
      _setDragOver(false);
    }
  }

  bool _dragHasFiles(web.DataTransfer dataTransfer) {
    final types = dataTransfer.types.toDart;
    for (final t in types) {
      if (t.toDart == 'Files') return true;
    }
    return false;
  }

  void _setDragOver(bool isOver) {
    if (_dragActive == isOver) return;
    _dragActive = isOver;
    _onDragOver?.call(isOver);
  }

  void _handlePaste(web.Event event) {
    final cb = _onFiles;
    if (cb == null) return;

    final clipboardData = (event as web.ClipboardEvent).clipboardData;
    if (clipboardData == null) return;

    // 粘贴任意类型的文件：图片、视频、文档都收。具体落成哪种附件由下游
    // stageFileFromBytes 按文件名/MIME 统一分流，这里不再只放行 image/*。
    final items = clipboardData.items;
    for (var i = 0; i < items.length; i++) {
      final item = items[i];
      if (item.kind != 'file') continue;
      final file = item.getAsFile();
      if (file == null) continue;
      event.preventDefault();
      // 从文件管理器复制来的真实文件自带文件名；剪贴板里的图片/截图通常
      // 没有文件名，按 MIME 兜底生成一个。
      final fileName =
          file.name.isNotEmpty ? file.name : _fallbackFileName(item.type);
      _processBlob(file, fileName, item.type, cb);
      return;
    }
  }

  Future<void> _processBlob(
    web.Blob blob,
    String fileName,
    String contentType,
    FileInterceptorCallback callback,
  ) async {
    final reader = web.FileReader();
    final completer = Completer<Uint8List?>();

    reader.onload = ((web.Event _) {
      final result = reader.result;
      if (result is JSArrayBuffer) {
        completer.complete(result.toDart.asUint8List());
      } else {
        completer.complete(null);
      }
    }).toJS;

    reader.onerror = ((web.Event _) {
      completer.complete(null);
    }).toJS;

    reader.readAsArrayBuffer(blob);

    final bytes = await completer.future;
    if (bytes != null && bytes.isNotEmpty) {
      callback(bytes, fileName, contentType);
    }
  }

  @override
  Future<bool> handlePasteIntent() async {
    // Web 端通过 DOM paste 事件处理，不需要此方法
    return false;
  }

  // 仅在剪贴板项没有文件名时兜底用：按 MIME 猜一个扩展名，下游再据此分流。
  String _fallbackFileName(String mimeType) {
    final lower = mimeType.toLowerCase();
    final ext = switch (lower) {
      'image/png' => 'png',
      'image/jpeg' => 'jpg',
      'image/webp' => 'webp',
      'image/gif' => 'gif',
      'image/bmp' => 'bmp',
      'image/svg+xml' => 'svg',
      'image/heic' => 'heic',
      'video/mp4' => 'mp4',
      'video/quicktime' => 'mov',
      'video/webm' => 'webm',
      'application/pdf' => 'pdf',
      'text/plain' => 'txt',
      'application/zip' => 'zip',
      _ => _extFromMimeSubtype(lower),
    };
    return 'clipboard_${DateTime.now().millisecondsSinceEpoch}.$ext';
  }

  // 从 MIME 的子类型粗取一个扩展名（如 image/x-foo -> 兜底 bin）。
  String _extFromMimeSubtype(String mime) {
    final slash = mime.indexOf('/');
    if (slash < 0 || slash == mime.length - 1) return 'bin';
    final sub = mime.substring(slash + 1);
    if (sub.isEmpty || sub.contains('+') || sub.contains('.')) return 'bin';
    return sub;
  }
}
