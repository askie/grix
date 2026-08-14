import 'dart:typed_data';

typedef FileInterceptorCallback = Future<void> Function(
  Uint8List bytes,
  String fileName,
  String contentType,
);

abstract class ChatFileInterceptor {
  bool get isDragOver;

  void setDragOverCallback(void Function(bool isOver) callback);
  void register(FileInterceptorCallback onFiles);
  void unregister();

  /// 处理粘贴快捷键（Cmd+V / Ctrl+V）。
  /// 返回 true 表示剪贴板中有图片已被处理，调用方应阻止默认粘贴行为；
  /// 返回 false 表示剪贴板中没有图片，调用方应放行让 TextField 正常粘贴文本。
  Future<bool> handlePasteIntent();
}
