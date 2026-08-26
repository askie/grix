import 'package:file_picker/file_picker.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';

import '../../shared/utils/toast_util.dart';
import '../../shared/widgets/app_dialog_style.dart';
import 'models/text_document_content.dart';
import 'models/text_document_descriptor.dart';
import 'services/text_document_codec.dart';
import 'services/text_document_format_registry.dart';
import 'services/text_document_native_bridge.dart';
import 'widgets/markdown_document_preview.dart';

enum _DocumentMode { preview, source, edit }

class TextDocumentPage extends StatefulWidget {
  const TextDocumentPage({
    super.key,
    required this.descriptor,
    required this.bytes,
  });

  final TextDocumentDescriptor descriptor;
  final Uint8List bytes;

  @override
  State<TextDocumentPage> createState() => _TextDocumentPageState();
}

class _TextDocumentPageState extends State<TextDocumentPage> {
  late final TextEditingController _controller;
  TextDocumentContent? _content;
  Object? _loadError;
  _DocumentMode _mode = _DocumentMode.preview;
  bool _dirty = false;
  bool _saving = false;

  bool get _isMarkdown =>
      TextDocumentFormatRegistry.isMarkdown(widget.descriptor.displayName);
  bool get _canEdit =>
      widget.bytes.length <= TextDocumentCodec.maxEditableBytes;

  @override
  void initState() {
    super.initState();
    try {
      _content = TextDocumentCodec.decode(widget.bytes);
      _controller = TextEditingController(text: _content!.text);
      _controller.addListener(_handleTextChanged);
      if (!_isMarkdown || !_canEdit) _mode = _DocumentMode.source;
    } catch (error) {
      _loadError = error;
      _controller = TextEditingController();
    }
  }

  @override
  void dispose() {
    _controller
      ..removeListener(_handleTextChanged)
      ..dispose();
    TextDocumentNativeBridge.close(widget.descriptor.handle);
    super.dispose();
  }

  void _handleTextChanged() {
    final dirty = _content != null && _controller.text != _content!.text;
    if (dirty != _dirty) setState(() => _dirty = dirty);
  }

  @override
  Widget build(BuildContext context) {
    return PopScope<void>(
      canPop: !_dirty,
      onPopInvokedWithResult: (didPop, _) async {
        if (!didPop) await _confirmClose();
      },
      child: Scaffold(
        appBar: AppBar(
          title: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                widget.descriptor.displayName,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
              ),
              Text(_statusLabel, style: Theme.of(context).textTheme.labelSmall),
            ],
          ),
          actions: _buildActions(),
        ),
        body: _buildBody(),
      ),
    );
  }

  String get _statusLabel {
    final encoding = _content?.encoding.name ?? '';
    final access = widget.descriptor.canWrite
        ? 'text_document_writable'.tr
        : 'text_document_read_only'.tr;
    return [encoding, access].where((value) => value.isNotEmpty).join(' · ');
  }

  List<Widget> _buildActions() {
    if (_loadError != null) return const [];
    return [
      if (_isMarkdown && _canEdit)
        IconButton(
          tooltip: _mode == _DocumentMode.preview
              ? 'text_document_view_source'.tr
              : 'text_document_preview'.tr,
          onPressed: () {
            setState(() {
              _mode = _mode == _DocumentMode.preview
                  ? _DocumentMode.source
                  : _DocumentMode.preview;
            });
          },
          icon: Icon(
            _mode == _DocumentMode.preview
                ? Icons.code_outlined
                : Icons.visibility_outlined,
          ),
        ),
      if (_canEdit && _mode != _DocumentMode.edit)
        IconButton(
          tooltip: 'common_edit'.tr,
          onPressed: () => setState(() => _mode = _DocumentMode.edit),
          icon: const Icon(Icons.edit_outlined),
        ),
      if (_dirty)
        IconButton(
          tooltip: widget.descriptor.canWrite
              ? 'common_save'.tr
              : 'text_document_save_as'.tr,
          onPressed: _saving ? null : _save,
          icon: _saving
              ? const SizedBox.square(
                  dimension: 20,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              : const Icon(Icons.save_outlined),
        ),
      PopupMenuButton<String>(
        onSelected: (value) {
          if (value == 'copy') {
            Clipboard.setData(ClipboardData(text: _controller.text));
          } else if (value == 'saveAs') {
            _saveAs();
          }
        },
        itemBuilder: (_) => [
          PopupMenuItem(
            value: 'copy',
            child: Text('text_document_copy_all'.tr),
          ),
          PopupMenuItem(
            value: 'saveAs',
            child: Text('text_document_save_as'.tr),
          ),
        ],
      ),
    ];
  }

  Widget _buildBody() {
    final error = _loadError;
    if (error != null) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Text(_errorMessage(error), textAlign: TextAlign.center),
        ),
      );
    }
    if (_mode == _DocumentMode.edit) {
      return TextField(
        controller: _controller,
        expands: true,
        maxLines: null,
        minLines: null,
        keyboardType: TextInputType.multiline,
        textAlignVertical: TextAlignVertical.top,
        style: TextStyle(
          fontFamily: 'monospace',
          fontFamilyFallback: AppTheme.textFontFallbackOrNull,
          fontSize: 14,
          height: 1.5,
        ),
        decoration: const InputDecoration(
          border: InputBorder.none,
          contentPadding: EdgeInsets.all(16),
        ),
      );
    }
    if (_isMarkdown && _mode == _DocumentMode.preview) {
      return SingleChildScrollView(
        padding: const EdgeInsets.fromLTRB(20, 16, 20, 40),
        child: MarkdownDocumentPreview(source: _controller.text),
      );
    }
    if (!_canEdit) {
      final lines = _controller.text.split('\n');
      return ListView.builder(
        padding: const EdgeInsets.symmetric(vertical: 12),
        itemCount: lines.length,
        itemBuilder: (context, index) => SingleChildScrollView(
          scrollDirection: Axis.horizontal,
          padding: const EdgeInsets.symmetric(horizontal: 16),
          child: SelectableText(
            lines[index],
            style: TextStyle(
              fontFamily: 'monospace',
              fontFamilyFallback: AppTheme.textFontFallbackOrNull,
              fontSize: 14,
              height: 1.5,
            ),
          ),
        ),
      );
    }
    return Scrollbar(
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(16),
        child: SingleChildScrollView(
          scrollDirection: Axis.horizontal,
          child: SelectionArea(
            child: Text(
              _controller.text,
              style: TextStyle(
                fontFamily: 'monospace',
                fontFamilyFallback: AppTheme.textFontFallbackOrNull,
                fontSize: 14,
                height: 1.5,
              ),
            ),
          ),
        ),
      ),
    );
  }

  Future<void> _save() async {
    if (!widget.descriptor.canWrite) {
      await _saveAs();
      return;
    }
    final content = _content;
    if (content == null) return;
    setState(() => _saving = true);
    try {
      final bytes = TextDocumentCodec.encode(
        _controller.text,
        content.encoding,
      );
      await TextDocumentNativeBridge.writeOriginal(
        handle: widget.descriptor.handle,
        bytes: bytes,
      );
      if (!mounted) return;
      _markSaved(content.encoding, bytes);
      CustomToast.show('common_saved'.tr, isError: false);
    } on PlatformException catch (error) {
      if (!mounted) return;
      CustomToast.show(
        error.message ?? 'text_document_save_failed'.tr,
        isError: true,
      );
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  Future<void> _saveAs() async {
    final content = _content;
    if (content == null) return;
    final bytes = TextDocumentCodec.encode(_controller.text, content.encoding);
    final path = await FilePicker.platform.saveFile(
      dialogTitle: 'text_document_save_dialog_title'.tr,
      fileName: widget.descriptor.displayName,
      type: FileType.any,
      bytes: bytes,
    );
    if (path == null || !mounted) return;
    _markSaved(content.encoding, bytes, returnToPreview: false);
    CustomToast.show('text_document_file_saved'.tr, isError: false);
  }

  void _markSaved(
    TextDocumentEncoding encoding,
    Uint8List bytes, {
    bool returnToPreview = true,
  }) {
    setState(() {
      _content = TextDocumentContent(
        text: _controller.text,
        encoding: encoding,
        lineEnding: _controller.text.contains('\r\n') ? '\r\n' : '\n',
        originalBytes: bytes,
      );
      _dirty = false;
      if (returnToPreview) {
        _mode = _isMarkdown ? _DocumentMode.preview : _DocumentMode.source;
      }
    });
  }

  Future<void> _confirmClose() async {
    final result = await showAppContentDialog<String>(
      context: context,
      title: 'text_document_unsaved_title'.tr,
      content: Text('text_document_unsaved_body'.tr),
      actions: [
        Builder(
          builder: (ctx) => TextButton(
            onPressed: () => Navigator.pop(ctx, 'cancel'),
            child: Text('common_cancel'.tr),
          ),
        ),
        Builder(
          builder: (ctx) => TextButton(
            onPressed: () => Navigator.pop(ctx, 'discard'),
            child: Text('text_document_discard'.tr),
          ),
        ),
        Builder(
          builder: (ctx) => FilledButton(
            onPressed: () => Navigator.pop(ctx, 'save'),
            child: Text('common_save'.tr),
          ),
        ),
      ],
    );
    if (!mounted || result == null || result == 'cancel') return;
    if (result == 'save') {
      await _save();
      if (_dirty || !mounted) return;
    }
    if (mounted) Navigator.of(context).pop();
  }

  String _errorMessage(Object error) {
    final value = error.toString();
    if (value.contains('too_large')) {
      return 'text_document_too_large'.tr;
    }
    if (value.contains('binary')) {
      return 'text_document_not_plain'.tr;
    }
    return 'text_document_decode_failed'.tr;
  }
}
