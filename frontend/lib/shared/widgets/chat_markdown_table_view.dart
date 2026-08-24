import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:get/get.dart';

import 'chat_markdown_captured_preview_dialog.dart';
import '../markdown/chat_markdown_ast.dart';
import '../utils/capture_export_pixel_ratio.dart';
import '../utils/mermaid_image_exporter.dart';
import '../utils/toast_util.dart';
import 'chat_markdown_inline_renderer.dart';
import 'chat_markdown_image_preview_scope.dart';
import 'chat_markdown_style_sheet.dart';

class ChatMarkdownTableView extends StatefulWidget {
  const ChatMarkdownTableView({
    super.key,
    required this.tableNode,
    required this.styleSheet,
    this.imagePreviewCollection,
    this.onAgentFilePathTap,
  });

  final ChatMarkdownNode tableNode;
  final ChatMarkdownStyleSheet styleSheet;
  final ChatMarkdownImagePreviewCollection? imagePreviewCollection;
  final ValueChanged<String>? onAgentFilePathTap;

  @override
  State<ChatMarkdownTableView> createState() => _ChatMarkdownTableViewState();
}

class _ChatMarkdownTableViewState extends State<ChatMarkdownTableView> {
  static const double _maxViewportHeight = 360;
  static const double _scrollbarThickness = 8;

  late final ScrollController _horizontalController;
  late final ScrollController _verticalController;
  final GlobalKey _tableExportBoundaryKey = GlobalKey();

  bool _hasVerticalOverflow = false;
  bool _isPreparingTablePreview = false;

  ChatMarkdownNode get tableNode => widget.tableNode;
  ChatMarkdownStyleSheet get styleSheet => widget.styleSheet;

  @override
  void initState() {
    super.initState();
    _horizontalController = ScrollController();
    _verticalController = ScrollController();
    _scheduleOverflowSync();
  }

  @override
  void didUpdateWidget(covariant ChatMarkdownTableView oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.tableNode != tableNode) {
      _scheduleOverflowSync();
    }
  }

  @override
  void dispose() {
    _horizontalController.dispose();
    _verticalController.dispose();
    super.dispose();
  }

  void _scheduleOverflowSync() {
    WidgetsBinding.instance.addPostFrameCallback((_) => _syncOverflowFlags());
  }

  void _syncOverflowFlags() {
    if (!mounted) {
      return;
    }
    final hasVerticalOverflow =
        _verticalController.hasClients &&
        _verticalController.position.maxScrollExtent > 0;
    if (hasVerticalOverflow == _hasVerticalOverflow) {
      return;
    }
    setState(() {
      _hasVerticalOverflow = hasVerticalOverflow;
    });
  }

  bool _onMetricsChanged(ScrollMetricsNotification _) {
    _scheduleOverflowSync();
    return false;
  }

  @override
  Widget build(BuildContext context) {
    _scheduleOverflowSync();

    final sections = _extractSections(tableNode);
    final rows = <_TableRowData>[
      ...sections.headerRows.map(
        (row) => _TableRowData(cells: row, isHeader: true),
      ),
      ...sections.bodyRows.map(
        (row) => _TableRowData(cells: row, isHeader: false),
      ),
    ];

    final columnCount = _maxColumnCount(rows);
    if (rows.isEmpty || columnCount == 0) {
      return const SizedBox.shrink();
    }

    final controlsIconColor =
        (styleSheet.preTextStyle.color ?? const Color(0xFF2A2214)).withValues(
          alpha: 0.86,
        );

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Expanded(child: Text('chat_table_label'.tr, style: styleSheet.preLabelStyle)),
            _TableExportButton(
              tooltip: 'chat_export_download_table'.tr,
              iconColor: controlsIconColor,
              onPressed: _isPreparingTablePreview ? null : _exportTableAsImage,
            ),
          ],
        ),
        const SizedBox(height: 8),
        Container(
          decoration: BoxDecoration(
            border: Border.all(color: styleSheet.tableBorderColor, width: 1),
            borderRadius: BorderRadius.circular(8),
          ),
          clipBehavior: Clip.antiAlias,
          child: ConstrainedBox(
            key: const ValueKey('markdown_table_viewport'),
            constraints: const BoxConstraints(maxHeight: _maxViewportHeight),
            child: NotificationListener<ScrollMetricsNotification>(
              onNotification: _onMetricsChanged,
              child: Scrollbar(
                controller: _verticalController,
                scrollbarOrientation: ScrollbarOrientation.right,
                thumbVisibility: _hasVerticalOverflow,
                trackVisibility: _hasVerticalOverflow,
                interactive: true,
                thickness: _scrollbarThickness,
                radius: const Radius.circular(999),
                child: SingleChildScrollView(
                  controller: _verticalController,
                  child: SingleChildScrollView(
                    controller: _horizontalController,
                    scrollDirection: Axis.horizontal,
                    child: KeyedSubtree(
                      key: const ValueKey('markdown_table_export_boundary'),
                      child: RepaintBoundary(
                        key: _tableExportBoundaryKey,
                        child: _buildTable(rows, columnCount),
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ),
        ),
      ],
    );
  }

  Future<void> _exportTableAsImage() async {
    if (_isPreparingTablePreview) {
      return;
    }
    setState(() {
      _isPreparingTablePreview = true;
    });
    try {
      await showChatMarkdownCapturedPreview(
        context: context,
        bytesFuture: _captureTableAsPngBytes(),
        onSave: _saveTableImage,
        backgroundColor: styleSheet.preBackgroundColor,
        errorText: 'chat_export_preview_failed'.trParams({
          'kind': 'chat_export_kind_table'.tr,
        }),
      );
    } catch (_) {
      if (mounted) {
        CustomToast.show(
          'chat_export_download_failed'.trParams({
            'kind': 'chat_export_kind_table'.tr,
          }),
        );
      }
    } finally {
      if (mounted) {
        setState(() {
          _isPreparingTablePreview = false;
        });
      }
    }
  }

  Future<Uint8List?> _captureTableAsPngBytes() async {
    final buildContext = _tableExportBoundaryKey.currentContext;
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

  Future<bool> _saveTableImage(Uint8List imageBytes) async {
    try {
      final now = DateTime.now().millisecondsSinceEpoch;
      final fileName = 'markdown_table_$now.png';
      final result = await exportMermaidPng(imageBytes, fileName: fileName);
      if (!mounted) {
        return false;
      }
      CustomToast.show(
        localizedExportResultMessage(
          isDownload: result.isDownload,
          isGallery: result.isGallery,
          location: result.location,
          kindKey: 'chat_export_kind_table',
        ),
        isError: false,
      );
      return true;
    } catch (error) {
      debugPrint('Failed to save markdown table image: $error');
      if (mounted) {
        CustomToast.show(
          'chat_export_save_failed'.trParams({
            'kind': 'chat_export_kind_table'.tr,
          }),
        );
      }
      return false;
    }
  }

  Widget _buildTable(List<_TableRowData> rows, int columnCount) {
    return Table(
      defaultVerticalAlignment: TableCellVerticalAlignment.middle,
      border: TableBorder(
        horizontalInside: BorderSide(
          color: styleSheet.tableBorderColor,
          width: 1,
        ),
        verticalInside: BorderSide(
          color: styleSheet.tableBorderColor,
          width: 1,
        ),
      ),
      columnWidths: <int, TableColumnWidth>{
        for (var i = 0; i < columnCount; i += 1)
          i: const IntrinsicColumnWidth(),
      },
      children: [for (final row in rows) _buildTableRow(row, columnCount)],
    );
  }

  TableRow _buildTableRow(_TableRowData row, int columnCount) {
    return TableRow(
      decoration: row.isHeader
          ? BoxDecoration(color: styleSheet.tableHeaderBackgroundColor)
          : null,
      children: [
        for (var i = 0; i < columnCount; i += 1)
          _buildCell(
            cell: i < row.cells.length ? row.cells[i] : null,
            isHeader: row.isHeader,
          ),
      ],
    );
  }

  Widget _buildCell({required ChatMarkdownNode? cell, required bool isHeader}) {
    final style = isHeader
        ? styleSheet.tableHeaderStyle
        : styleSheet.tableBodyStyle;
    final alignAttr = cell?.attrs['align']?.toString();
    final inlineRenderer = ChatMarkdownInlineRenderer(
      styleSheet: styleSheet,
      imagePreviewCollection: widget.imagePreviewCollection,
      onAgentFilePathTap: widget.onAgentFilePathTap,
    );
    final children = cell?.children ?? const <ChatMarkdownNode>[];
    final spans = inlineRenderer.buildSpans(children, baseStyle: style);

    final content = children.isEmpty
        ? const SizedBox.shrink()
        : Text.rich(
            TextSpan(style: style, children: spans),
            textAlign: _textAlignFor(alignAttr),
          );

    return Padding(
      padding: styleSheet.tableCellPadding,
      child: Align(alignment: _alignmentFor(alignAttr), child: content),
    );
  }

  _TableSections _extractSections(ChatMarkdownNode table) {
    final headerRows = <List<ChatMarkdownNode>>[];
    final bodyRows = <List<ChatMarkdownNode>>[];

    for (final child in table.children) {
      switch (child.type) {
        case ChatMarkdownNodeType.tableHead:
          headerRows.addAll(_extractRows(child));
          break;
        case ChatMarkdownNodeType.tableBody:
          bodyRows.addAll(_extractRows(child));
          break;
        case ChatMarkdownNodeType.tableRow:
          bodyRows.add(child.children);
          break;
        case ChatMarkdownNodeType.document:
        case ChatMarkdownNodeType.heading:
        case ChatMarkdownNodeType.paragraph:
        case ChatMarkdownNodeType.thematicBreak:
        case ChatMarkdownNodeType.blockquote:
        case ChatMarkdownNodeType.list:
        case ChatMarkdownNodeType.listItem:
        case ChatMarkdownNodeType.taskItem:
        case ChatMarkdownNodeType.codeBlock:
        case ChatMarkdownNodeType.table:
        case ChatMarkdownNodeType.tableCell:
        case ChatMarkdownNodeType.mathBlock:
        case ChatMarkdownNodeType.mermaidBlock:
        case ChatMarkdownNodeType.htmlBlockText:
        case ChatMarkdownNodeType.footnoteDef:
        case ChatMarkdownNodeType.text:
        case ChatMarkdownNodeType.softBreak:
        case ChatMarkdownNodeType.hardBreak:
        case ChatMarkdownNodeType.emphasis:
        case ChatMarkdownNodeType.strong:
        case ChatMarkdownNodeType.strike:
        case ChatMarkdownNodeType.inlineCode:
        case ChatMarkdownNodeType.link:
        case ChatMarkdownNodeType.image:
        case ChatMarkdownNodeType.video:
        case ChatMarkdownNodeType.audio:
        case ChatMarkdownNodeType.mathInline:
        case ChatMarkdownNodeType.autolink:
        case ChatMarkdownNodeType.footnoteRef:
        case ChatMarkdownNodeType.escapedText:
        case ChatMarkdownNodeType.unknown:
          break;
      }
    }

    return _TableSections(
      headerRows: List.unmodifiable(headerRows),
      bodyRows: List.unmodifiable(bodyRows),
    );
  }

  List<List<ChatMarkdownNode>> _extractRows(ChatMarkdownNode section) {
    final rows = <List<ChatMarkdownNode>>[];
    for (final child in section.children) {
      if (child.type == ChatMarkdownNodeType.tableRow) {
        rows.add(List.unmodifiable(child.children));
      }
    }
    return List.unmodifiable(rows);
  }

  int _maxColumnCount(List<_TableRowData> rows) {
    var maxColumns = 0;
    for (final row in rows) {
      if (row.cells.length > maxColumns) {
        maxColumns = row.cells.length;
      }
    }
    return maxColumns;
  }

  Alignment _alignmentFor(String? align) {
    switch (align) {
      case 'center':
        return Alignment.center;
      case 'right':
        return Alignment.centerRight;
      case 'left':
      default:
        return Alignment.centerLeft;
    }
  }

  TextAlign _textAlignFor(String? align) {
    switch (align) {
      case 'center':
        return TextAlign.center;
      case 'right':
        return TextAlign.right;
      case 'left':
      default:
        return TextAlign.left;
    }
  }
}

class _TableExportButton extends StatelessWidget {
  const _TableExportButton({
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

class _TableSections {
  const _TableSections({required this.headerRows, required this.bodyRows});

  final List<List<ChatMarkdownNode>> headerRows;
  final List<List<ChatMarkdownNode>> bodyRows;
}

class _TableRowData {
  const _TableRowData({required this.cells, required this.isHeader});

  final List<ChatMarkdownNode> cells;
  final bool isHeader;
}
