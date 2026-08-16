import 'package:flutter/material.dart';
import 'package:get/get.dart';

import 'chat_markdown_zoomable_image_viewport.dart';

/// 统一的全屏可缩放查看脚手架：图片、流程图、表格、公式等"下载预览"共用。
/// 提供捏合/双击/滚轮/拖动缩放、顶部 +−/复位（位于下载与关闭按钮之间）、
/// 右上角关闭、未放大时下滑关闭，以及可选的左上角保存按钮（保存中显示进度）。
class ChatMarkdownImageViewerScaffold extends StatefulWidget {
  const ChatMarkdownImageViewerScaffold({
    super.key,
    required this.child,
    this.onSave,
    this.saveTooltip,
    this.backgroundColor = Colors.black,
    this.onZoomStateChanged,
  });

  final Widget child;
  final Future<void> Function()? onSave;
  final String? saveTooltip;
  final Color backgroundColor;

  /// 缩放状态变化时回调（true=处于原始比例，false=已放大）。
  /// 供外层滑动查看器据此临时锁住横滑切页，避免与拖动放大后的图片手势打架。
  final ValueChanged<bool>? onZoomStateChanged;

  @override
  State<ChatMarkdownImageViewerScaffold> createState() =>
      _ChatMarkdownImageViewerScaffoldState();
}

class _ChatMarkdownImageViewerScaffoldState
    extends State<ChatMarkdownImageViewerScaffold> {
  final TransformationController _transformationController =
      TransformationController();
  final ChatMarkdownImageZoomController _zoomController =
      ChatMarkdownImageZoomController();
  bool _isSaving = false;

  @override
  void initState() {
    super.initState();
    _zoomController.addListener(_handleZoomControllerChanged);
  }

  void _handleZoomControllerChanged() {
    final onZoomStateChanged = widget.onZoomStateChanged;
    if (onZoomStateChanged == null || !_zoomController.isBound) {
      return;
    }
    onZoomStateChanged(_zoomController.isAtBaseScale);
  }

  @override
  void dispose() {
    _zoomController.removeListener(_handleZoomControllerChanged);
    _zoomController.dispose();
    _transformationController.dispose();
    super.dispose();
  }

  Future<void> _handleSave() async {
    final onSave = widget.onSave;
    if (onSave == null || _isSaving) {
      return;
    }
    setState(() => _isSaving = true);
    try {
      await onSave();
    } finally {
      if (mounted) {
        setState(() => _isSaving = false);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return SizedBox.expand(
      key: const ValueKey('markdown_image_preview_dialog'),
      child: Material(
        color: widget.backgroundColor,
        child: SafeArea(
          child: Stack(
            children: [
              Positioned.fill(
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(12, 60, 12, 16),
                  child: ChatMarkdownZoomableImageViewport(
                    transformationController: _transformationController,
                    controller: _zoomController,
                    onDismiss: () => Navigator.of(context).pop(),
                    child: Center(child: widget.child),
                  ),
                ),
              ),
              if (widget.onSave != null)
                Positioned(
                  top: 8,
                  left: 12,
                  child: _buildActionButton(
                    key: const ValueKey(
                      'markdown_image_preview_download_button',
                    ),
                    tooltip:
                        widget.saveTooltip ?? 'chat_export_download_image'.tr,
                    onPressed: _isSaving ? null : _handleSave,
                    child: _isSaving
                        ? const SizedBox(
                            width: 18,
                            height: 18,
                            child: CircularProgressIndicator(
                              strokeWidth: 2,
                              valueColor:
                                  AlwaysStoppedAnimation<Color>(Colors.white),
                            ),
                          )
                        : const Icon(Icons.download_rounded),
                  ),
                ),
              Positioned(
                top: 8,
                left: 0,
                right: 0,
                child: Center(child: _buildZoomBar()),
              ),
              Positioned(
                top: 8,
                right: 12,
                child: _buildActionButton(
                  key: const ValueKey('markdown_image_preview_close_button'),
                  tooltip: 'chat_export_close_image'.tr,
                  onPressed: () => Navigator.of(context).pop(),
                  child: const Icon(Icons.close_rounded),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildZoomBar() {
    return AnimatedBuilder(
      animation: _zoomController,
      builder: (context, _) {
        return Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            _buildActionButton(
              key: const ValueKey('markdown_image_preview_zoom_out'),
              tooltip: 'chat_zoom_out'.tr,
              onPressed:
                  _zoomController.canZoomOut ? _zoomController.zoomOut : null,
              child: const Icon(Icons.remove_rounded),
            ),
            const SizedBox(width: 8),
            _buildActionButton(
              key: const ValueKey('markdown_image_preview_zoom_reset'),
              tooltip: 'chat_zoom_reset'.tr,
              onPressed:
                  _zoomController.isAtBaseScale ? null : _zoomController.reset,
              child: const Icon(Icons.fit_screen_rounded),
            ),
            const SizedBox(width: 8),
            _buildActionButton(
              key: const ValueKey('markdown_image_preview_zoom_in'),
              tooltip: 'chat_zoom_in'.tr,
              onPressed:
                  _zoomController.canZoomIn ? _zoomController.zoomIn : null,
              child: const Icon(Icons.add_rounded),
            ),
          ],
        );
      },
    );
  }

  Widget _buildActionButton({
    required Key key,
    required String tooltip,
    required VoidCallback? onPressed,
    required Widget child,
  }) {
    return Material(
      color: Colors.black.withValues(alpha: 0.4),
      shape: const CircleBorder(),
      child: IconButton(
        key: key,
        tooltip: tooltip,
        onPressed: onPressed,
        color: Colors.white,
        icon: child,
        splashRadius: 24,
      ),
    );
  }
}
