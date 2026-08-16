import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_math_fork/flutter_math.dart';
import 'package:get/get.dart';

import 'chat_markdown_captured_preview_dialog.dart';
import '../utils/capture_export_pixel_ratio.dart';
import '../utils/mermaid_image_exporter.dart';
import '../utils/toast_util.dart';
import 'chat_markdown_latex_render_normalizer.dart';
import 'chat_markdown_style_sheet.dart';

class ChatMarkdownMathBlockView extends StatefulWidget {
  const ChatMarkdownMathBlockView({
    super.key,
    required this.tex,
    required this.styleSheet,
  });

  final String tex;
  final ChatMarkdownStyleSheet styleSheet;

  @override
  State<ChatMarkdownMathBlockView> createState() =>
      _ChatMarkdownMathBlockViewState();
}

class _ChatMarkdownMathBlockViewState extends State<ChatMarkdownMathBlockView> {
  final GlobalKey _exportBoundaryKey = GlobalKey();
  bool _isPreparingPreview = false;

  @override
  Widget build(BuildContext context) {
    final styleSheet = widget.styleSheet;
    final renderTex =
        ChatMarkdownLatexRenderNormalizer.normalizeForMathRenderer(widget.tex);
    final controlsIconColor =
        (styleSheet.preTextStyle.color ?? const Color(0xFF2A2214))
            .withValues(alpha: 0.86);

    return Container(
      decoration: styleSheet.preDecoration,
      margin: styleSheet.preMargin,
      padding: styleSheet.prePadding,
      width: double.infinity,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text('Math', style: styleSheet.preLabelStyle),
              ),
              _MathExportButton(
                tooltip: 'chat_export_download_math'.tr,
                iconColor: controlsIconColor,
                onPressed: _isPreparingPreview ? null : _exportFormulaAsImage,
              ),
            ],
          ),
          const SizedBox(height: 8),
          RepaintBoundary(
            key: _exportBoundaryKey,
            child: Container(
              width: double.infinity,
              padding: const EdgeInsets.symmetric(vertical: 8),
              child: Center(
                child: SingleChildScrollView(
                  scrollDirection: Axis.horizontal,
                  child: Math.tex(
                    renderTex,
                    textStyle: styleSheet.blockMathStyle,
                    mathStyle: MathStyle.display,
                    onErrorFallback: (error) => Text(
                      '\$\$$renderTex\$\$',
                      style: styleSheet.preTextStyle,
                    ),
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Future<void> _exportFormulaAsImage() async {
    if (_isPreparingPreview) {
      return;
    }
    setState(() {
      _isPreparingPreview = true;
    });
    try {
      await showChatMarkdownCapturedPreview(
        context: context,
        bytesFuture: _captureFormulaAsPngBytes(),
        onSave: _saveFormulaImage,
        backgroundColor: widget.styleSheet.preBackgroundColor,
        errorText: 'chat_export_preview_failed'.trParams({
          'kind': 'chat_export_kind_math'.tr,
        }),
      );
    } catch (_) {
      if (mounted) {
        CustomToast.show(
          'chat_export_download_failed'.trParams({
            'kind': 'chat_export_kind_math'.tr,
          }),
        );
      }
    } finally {
      if (mounted) {
        setState(() {
          _isPreparingPreview = false;
        });
      }
    }
  }

  Future<Uint8List?> _captureFormulaAsPngBytes() async {
    final buildContext = _exportBoundaryKey.currentContext;
    final renderObject = buildContext?.findRenderObject();
    if (renderObject is! RenderRepaintBoundary) {
      return null;
    }
    final pixelRatio = resolveCaptureExportPixelRatio(
      logicalSize: renderObject.size,
      devicePixelRatio: MediaQuery.devicePixelRatioOf(context),
    );

    await WidgetsBinding.instance.endOfFrame;
    if (!mounted) {
      return null;
    }
    ui.Image? image;
    try {
      image = await renderObject.toImage(pixelRatio: pixelRatio);
      final byteData = await image.toByteData(format: ui.ImageByteFormat.png);
      if (byteData == null) {
        throw StateError('Exported image bytes are empty');
      }
      return byteData.buffer.asUint8List();
    } finally {
      image?.dispose();
    }
  }

  Future<bool> _saveFormulaImage(Uint8List imageBytes) async {
    try {
      final now = DateTime.now().millisecondsSinceEpoch;
      final fileName = 'math_formula_$now.png';
      final result = await exportMermaidPng(
        imageBytes,
        fileName: fileName,
      );
      if (!mounted) {
        return false;
      }
      CustomToast.show(
        localizedExportResultMessage(
          isDownload: result.isDownload,
          isGallery: result.isGallery,
          location: result.location,
          kindKey: 'chat_export_kind_math',
        ),
        isError: false,
      );
      return true;
    } catch (error) {
      debugPrint('Failed to save math formula image: $error');
      if (mounted) {
        CustomToast.show(
          'chat_export_save_failed'.trParams({
            'kind': 'chat_export_kind_math'.tr,
          }),
        );
      }
      return false;
    }
  }
}

class _MathExportButton extends StatelessWidget {
  const _MathExportButton({
    required this.tooltip,
    required this.iconColor,
    this.onPressed,
  });

  static const double _buttonExtent = 24;
  static const double _iconSize = 14;

  final String tooltip;
  final Color iconColor;
  final VoidCallback? onPressed;

  @override
  Widget build(BuildContext context) {
    return IconButton(
      tooltip: tooltip,
      onPressed: onPressed,
      icon: const Icon(Icons.download_rounded),
      iconSize: _iconSize,
      color: iconColor,
      padding: EdgeInsets.zero,
      visualDensity: VisualDensity.compact,
      constraints: const BoxConstraints.tightFor(
        width: _buttonExtent,
        height: _buttonExtent,
      ),
      splashRadius: 12,
    );
  }
}
