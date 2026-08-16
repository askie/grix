import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';

import 'chat_markdown_captured_preview_dialog.dart';
import '../mermaid/chat_mermaid_model.dart';
import '../mermaid/chat_mermaid_parser.dart';
import '../utils/capture_export_pixel_ratio.dart';
import '../utils/mermaid_image_exporter.dart';
import '../utils/toast_util.dart';
import 'chat_markdown_mermaid_block_view.dart';
import 'chat_markdown_mermaid_class_view.dart';
import 'chat_markdown_mermaid_er_view.dart';
import 'chat_markdown_mermaid_flowchart_view.dart';
import 'chat_markdown_mermaid_gantt_view.dart';
import 'chat_markdown_mermaid_gitgraph_view.dart';
import 'chat_markdown_mermaid_journey_view.dart';
import 'chat_markdown_mermaid_kanban_view.dart';
import 'chat_markdown_mermaid_mindmap_view.dart';
import 'chat_markdown_mermaid_packet_view.dart';
import 'chat_markdown_mermaid_pie_view.dart';
import 'chat_markdown_mermaid_quadrant_view.dart';
import 'chat_markdown_mermaid_radar_view.dart';
import 'chat_markdown_mermaid_requirement_view.dart';
import 'chat_markdown_mermaid_sankey_view.dart';
import 'chat_markdown_mermaid_sequence_view.dart';
import 'chat_markdown_mermaid_state_view.dart';
import 'chat_markdown_mermaid_timeline_view.dart';
import 'chat_markdown_mermaid_treemap_view.dart';
import 'chat_markdown_mermaid_xychart_view.dart';
import 'chat_markdown_mermaid_zoomable_viewport.dart';

class ChatMarkdownMermaidView extends StatefulWidget {
  const ChatMarkdownMermaidView({
    super.key,
    required this.source,
    required this.textStyle,
    required this.backgroundColor,
    required this.decoration,
    required this.padding,
    this.margin,
  });

  final String source;
  final TextStyle textStyle;
  final Color backgroundColor;
  final Decoration? decoration;
  final EdgeInsetsGeometry? padding;
  final EdgeInsetsGeometry? margin;
  static const ChatMermaidParser _parser = ChatMermaidParser();

  @override
  State<ChatMarkdownMermaidView> createState() =>
      _ChatMarkdownMermaidViewState();
}

class _ChatMarkdownMermaidViewState extends State<ChatMarkdownMermaidView> {
  final ChatMarkdownMermaidZoomController _zoomController =
      ChatMarkdownMermaidZoomController();
  final GlobalKey _diagramExportBoundaryKey = GlobalKey();
  bool _isPreparingDiagramPreview = false;

  @override
  void dispose() {
    _zoomController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final parseResult = ChatMarkdownMermaidView._parser.parse(widget.source);
    final labelStyle = textStyle.copyWith(
      fontSize: (textStyle.fontSize ?? 13) - 1,
      fontWeight: FontWeight.w600,
      color: textStyle.color?.withValues(alpha: 0.78),
      letterSpacing: 0.2,
    );
    final supportsExport = parseResult.isSupported;
    final canCopySource = widget.source.trim().isNotEmpty;
    final controlsIconColor = (textStyle.color ?? const Color(0xFF2A2214))
        .withValues(alpha: 0.86);

    return Container(
      decoration: widget.decoration,
      margin: widget.margin,
      padding: widget.padding,
      width: double.infinity,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.center,
            children: [
              Expanded(child: Text('Mermaid', style: labelStyle)),
              if (supportsExport)
                Padding(
                  padding: const EdgeInsets.only(left: 6),
                  child: _MermaidHeaderButton(
                    tooltip: 'chat_export_view_mermaid'.tr,
                    icon: Icons.visibility_rounded,
                    iconColor: controlsIconColor,
                    onPressed: _isPreparingDiagramPreview
                        ? null
                        : _exportDiagramAsImage,
                  ),
                ),
              if (canCopySource)
                Padding(
                  padding: const EdgeInsets.only(left: 6),
                  child: _MermaidHeaderButton(
                    tooltip: 'chat_export_copy_mermaid'.tr,
                    icon: Icons.copy_rounded,
                    iconColor: controlsIconColor,
                    onPressed: _copySource,
                  ),
                ),
            ],
          ),
          const SizedBox(height: 8),
          GestureDetector(
            onTap: supportsExport && !_isPreparingDiagramPreview
                ? _exportDiagramAsImage
                : null,
            child: _buildExportableDiagram(parseResult),
          ),
        ],
      ),
    );
  }

  String get source => widget.source;
  TextStyle get textStyle => widget.textStyle;
  Color get backgroundColor => widget.backgroundColor;

  /// 这些图形已在各自的全画布层自行挂载导出边界：流程图、脑图在视图内部，
  /// 时序图、状态图、类图、ER 图、Git 图通过可缩放视口，甘特图在内容容器上。
  /// 因此外层不能再重复包裹 RepaintBoundary，否则会与内部边界使用同一
  /// GlobalKey 造成冲突。本判断与是否显示缩放控件（[_supportsHeaderZoom]）
  /// 是两个独立的概念，分开维护，任一方变化互不影响。
  bool _managesOwnExportBoundary(ChatMermaidDiagram? diagram) {
    return diagram is ChatMermaidFlowchart ||
        diagram is ChatMermaidSequenceDiagram ||
        diagram is ChatMermaidStateDiagram ||
        diagram is ChatMermaidGanttDiagram ||
        diagram is ChatMermaidClassDiagram ||
        diagram is ChatMermaidErDiagram ||
        diagram is ChatMermaidMindmapDiagram ||
        diagram is ChatMermaidGitGraphDiagram;
  }

  Future<void> _exportDiagramAsImage() async {
    if (_isPreparingDiagramPreview) {
      return;
    }
    setState(() {
      _isPreparingDiagramPreview = true;
    });
    try {
      await showChatMarkdownCapturedPreview(
        context: context,
        bytesFuture: _captureDiagramAsPngBytes(),
        onSave: _saveDiagramImage,
        backgroundColor: backgroundColor,
        errorText: 'chat_export_preview_failed'.trParams({
          'kind': 'chat_export_kind_mermaid'.tr,
        }),
      );
    } catch (_) {
      if (mounted) {
        CustomToast.show(
          'chat_export_download_failed'.trParams({
            'kind': 'chat_export_kind_mermaid'.tr,
          }),
        );
      }
    } finally {
      if (mounted) {
        setState(() {
          _isPreparingDiagramPreview = false;
        });
      }
    }
  }

  Future<void> _copySource() async {
    final source = widget.source.trim();
    if (source.isEmpty) {
      return;
    }
    await Clipboard.setData(ClipboardData(text: source));
    CustomToast.show('chat_copy_success'.tr, isError: false);
  }

  Future<Uint8List?> _captureDiagramAsPngBytes() async {
    final buildContext = _diagramExportBoundaryKey.currentContext;
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

  Future<bool> _saveDiagramImage(Uint8List imageBytes) async {
    try {
      final now = DateTime.now().millisecondsSinceEpoch;
      final fileName = 'mermaid_diagram_$now.png';
      final result = await exportMermaidPng(imageBytes, fileName: fileName);
      if (!mounted) {
        return false;
      }
      CustomToast.show(
        localizedExportResultMessage(
          isDownload: result.isDownload,
          isGallery: result.isGallery,
          location: result.location,
          kindKey: 'chat_export_kind_mermaid',
        ),
        isError: false,
      );
      return true;
    } catch (error) {
      debugPrint('Failed to save mermaid diagram image: $error');
      if (mounted) {
        CustomToast.show(
          'chat_export_save_failed'.trParams({
            'kind': 'chat_export_kind_mermaid'.tr,
          }),
        );
      }
      return false;
    }
  }

  Widget _buildExportableDiagram(ChatMermaidParseResult parseResult) {
    final diagram = _buildDiagram(parseResult);
    if (!parseResult.isSupported) {
      return diagram;
    }
    // 自管导出边界的图形不再外层包裹，截图覆盖各自的完整画布。
    if (_managesOwnExportBoundary(parseResult.diagram)) {
      return diagram;
    }
    // 其余静态图（饼图、旅程图、XY 图）内容本身即完整尺寸，在此挂导出边界。
    return RepaintBoundary(key: _diagramExportBoundaryKey, child: diagram);
  }

  Widget _buildDiagram(ChatMermaidParseResult parseResult) {
    final diagram = parseResult.diagram;
    if (diagram case ChatMermaidFlowchart()) {
      return ChatMarkdownMermaidFlowchartView(
        diagram: diagram,
        textStyle: textStyle,
        backgroundColor: backgroundColor,
        zoomController: _zoomController,
        exportBoundaryKey: _diagramExportBoundaryKey,
      );
    }
    if (diagram case ChatMermaidSequenceDiagram()) {
      return ChatMarkdownMermaidSequenceView(
        diagram: diagram,
        textStyle: textStyle,
        backgroundColor: backgroundColor,
        zoomController: _zoomController,
        exportBoundaryKey: _diagramExportBoundaryKey,
      );
    }
    if (diagram case ChatMermaidStateDiagram()) {
      return ChatMarkdownMermaidStateView(
        diagram: diagram,
        textStyle: textStyle,
        backgroundColor: backgroundColor,
        zoomController: _zoomController,
        exportBoundaryKey: _diagramExportBoundaryKey,
      );
    }
    if (diagram case ChatMermaidGanttDiagram()) {
      return ChatMarkdownMermaidGanttView(
        diagram: diagram,
        textStyle: textStyle,
        backgroundColor: backgroundColor,
        zoomController: _zoomController,
        exportBoundaryKey: _diagramExportBoundaryKey,
      );
    }
    if (diagram case ChatMermaidClassDiagram()) {
      return ChatMarkdownMermaidClassView(
        diagram: diagram,
        textStyle: textStyle,
        backgroundColor: backgroundColor,
        zoomController: _zoomController,
        exportBoundaryKey: _diagramExportBoundaryKey,
      );
    }
    if (diagram case ChatMermaidErDiagram()) {
      return ChatMarkdownMermaidErView(
        diagram: diagram,
        textStyle: textStyle,
        backgroundColor: backgroundColor,
        zoomController: _zoomController,
        exportBoundaryKey: _diagramExportBoundaryKey,
      );
    }
    if (diagram case ChatMermaidPieDiagram()) {
      return ChatMarkdownMermaidPieView(
        diagram: diagram,
        textStyle: textStyle,
        backgroundColor: backgroundColor,
      );
    }
    if (diagram case ChatMermaidMindmapDiagram()) {
      return ChatMarkdownMermaidMindmapView(
        diagram: diagram,
        textStyle: textStyle,
        backgroundColor: backgroundColor,
        zoomController: _zoomController,
        exportBoundaryKey: _diagramExportBoundaryKey,
      );
    }
    if (diagram case ChatMermaidJourneyDiagram()) {
      return ChatMarkdownMermaidJourneyView(
        diagram: diagram,
        textStyle: textStyle,
        backgroundColor: backgroundColor,
      );
    }
    if (diagram case ChatMermaidGitGraphDiagram()) {
      return ChatMarkdownMermaidGitGraphView(
        diagram: diagram,
        textStyle: textStyle,
        backgroundColor: backgroundColor,
        zoomController: _zoomController,
        exportBoundaryKey: _diagramExportBoundaryKey,
      );
    }
    if (diagram case ChatMermaidXyChartDiagram()) {
      return ChatMarkdownMermaidXyChartView(
        diagram: diagram,
        textStyle: textStyle,
        backgroundColor: backgroundColor,
      );
    }
    if (diagram case ChatMermaidTimelineDiagram()) {
      return ChatMarkdownMermaidTimelineView(
        diagram: diagram,
        textStyle: textStyle,
        backgroundColor: backgroundColor,
      );
    }
    if (diagram case ChatMermaidQuadrantDiagram()) {
      return ChatMarkdownMermaidQuadrantView(
        diagram: diagram,
        textStyle: textStyle,
        backgroundColor: backgroundColor,
      );
    }
    if (diagram case ChatMermaidSankeyDiagram()) {
      return ChatMarkdownMermaidSankeyView(
        diagram: diagram,
        textStyle: textStyle,
        backgroundColor: backgroundColor,
      );
    }
    if (diagram case ChatMermaidRadarDiagram()) {
      return ChatMarkdownMermaidRadarView(
        diagram: diagram,
        textStyle: textStyle,
        backgroundColor: backgroundColor,
      );
    }
    if (diagram case ChatMermaidKanbanDiagram()) {
      return ChatMarkdownMermaidKanbanView(
        diagram: diagram,
        textStyle: textStyle,
        backgroundColor: backgroundColor,
      );
    }
    if (diagram case ChatMermaidTreemapDiagram()) {
      return ChatMarkdownMermaidTreemapView(
        diagram: diagram,
        textStyle: textStyle,
        backgroundColor: backgroundColor,
      );
    }
    if (diagram case ChatMermaidBlockDiagram()) {
      return ChatMarkdownMermaidBlockView(
        diagram: diagram,
        textStyle: textStyle,
        backgroundColor: backgroundColor,
      );
    }
    if (diagram case ChatMermaidPacketDiagram()) {
      return ChatMarkdownMermaidPacketView(
        diagram: diagram,
        textStyle: textStyle,
        backgroundColor: backgroundColor,
      );
    }
    if (diagram case ChatMermaidRequirementDiagram()) {
      return ChatMarkdownMermaidRequirementView(
        diagram: diagram,
        textStyle: textStyle,
        backgroundColor: backgroundColor,
      );
    }
    return SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      child: Text(widget.source.trim(), style: textStyle),
    );
  }
}

class _MermaidHeaderButton extends StatelessWidget {
  const _MermaidHeaderButton({
    required this.tooltip,
    required this.icon,
    required this.iconColor,
    this.onPressed,
  });

  static const double _buttonExtent = 24;
  static const double _iconSize = 14;

  final String tooltip;
  final IconData icon;
  final Color iconColor;
  final VoidCallback? onPressed;

  @override
  Widget build(BuildContext context) {
    return IconButton(
      tooltip: tooltip,
      onPressed: onPressed,
      icon: Icon(icon),
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
