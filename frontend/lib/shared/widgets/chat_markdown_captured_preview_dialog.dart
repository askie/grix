import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:get/get.dart';

import 'app_dialog_style.dart';
import 'chat_markdown_image_viewer_scaffold.dart';

/// 以全屏可缩放查看器打开一段已截取的 PNG 字节（流程图/表格/公式等）。
/// 与图片预览共用同一套缩放/关闭交互，并提供保存按钮。
Future<void> showChatMarkdownCapturedPreview({
  required BuildContext context,
  required Future<Uint8List?> bytesFuture,
  required Future<bool> Function(Uint8List bytes) onSave,
  Color backgroundColor = Colors.black,
  String? errorText,
  String? saveTooltip,
}) {
  return showAppDialog<void>(
    context: context,
    useSafeArea: false,
    barrierColor: Colors.black.withValues(alpha: 0.92),
    builder: (_) => ChatMarkdownCapturedPreviewDialog(
      bytesFuture: bytesFuture,
      onSave: onSave,
      backgroundColor: backgroundColor,
      errorText: errorText ?? 'chat_export_preview_failed_generic'.tr,
      saveTooltip:
          saveTooltip ??
          (kIsWeb
              ? 'chat_export_download_image'.tr
              : 'chat_export_save_image'.tr),
    ),
  );
}

class ChatMarkdownCapturedPreviewDialog extends StatefulWidget {
  const ChatMarkdownCapturedPreviewDialog({
    super.key,
    required this.bytesFuture,
    required this.onSave,
    this.backgroundColor = Colors.black,
    this.errorText,
    this.saveTooltip,
  });

  final Future<Uint8List?> bytesFuture;
  final Future<bool> Function(Uint8List bytes) onSave;
  final Color backgroundColor;
  final String? errorText;
  final String? saveTooltip;

  @override
  State<ChatMarkdownCapturedPreviewDialog> createState() =>
      _ChatMarkdownCapturedPreviewDialogState();
}

class _ChatMarkdownCapturedPreviewDialogState
    extends State<ChatMarkdownCapturedPreviewDialog> {
  Uint8List? _bytes;
  bool _failed = false;

  @override
  void initState() {
    super.initState();
    widget.bytesFuture
        .then((bytes) {
          if (!mounted) {
            return;
          }
          setState(() {
            _bytes = bytes;
            _failed = bytes == null;
          });
        })
        .catchError((_) {
          if (!mounted) {
            return;
          }
          setState(() => _failed = true);
        });
  }

  @override
  Widget build(BuildContext context) {
    final bytes = _bytes;
    return ChatMarkdownImageViewerScaffold(
      backgroundColor: widget.backgroundColor,
      saveTooltip:
          widget.saveTooltip ??
          (kIsWeb
              ? 'chat_export_download_image'.tr
              : 'chat_export_save_image'.tr),
      onSave: bytes == null ? null : () async => widget.onSave(bytes),
      child: _buildContent(bytes),
    );
  }

  Widget _buildContent(Uint8List? bytes) {
    if (bytes != null) {
      return Image.memory(bytes, fit: BoxFit.contain, gaplessPlayback: true);
    }
    final fg =
        ThemeData.estimateBrightnessForColor(widget.backgroundColor) ==
            Brightness.dark
        ? Colors.white70
        : Colors.black54;
    if (_failed) {
      return Text(
        widget.errorText ?? 'chat_export_preview_failed_generic'.tr,
        style: TextStyle(color: fg),
        textAlign: TextAlign.center,
      );
    }
    return SizedBox(
      width: 36,
      height: 36,
      child: CircularProgressIndicator(
        strokeWidth: 2.4,
        valueColor: AlwaysStoppedAnimation<Color>(fg),
      ),
    );
  }
}
