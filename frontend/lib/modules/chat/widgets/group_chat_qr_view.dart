import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';
import 'package:qr_flutter/qr_flutter.dart';

import '../../../data/providers/group_qr_service.dart';
import '../../../shared/utils/mermaid_image_exporter.dart';
import '../../../shared/utils/toast_util.dart';

class GroupChatQrView extends StatefulWidget {
  const GroupChatQrView({
    super.key,
    required this.sessionId,
    this.groupName = '',
  });

  final String sessionId;
  final String groupName;

  @override
  State<GroupChatQrView> createState() => _GroupChatQrViewState();
}

class _GroupChatQrViewState extends State<GroupChatQrView> {
  final GroupQrService _groupQrService = Get.find<GroupQrService>();
  final GlobalKey _qrExportBoundaryKey = GlobalKey();

  GroupQrCodeInfo? _qrInfo;
  bool _isLoading = true;
  bool _isDownloading = false;

  @override
  void initState() {
    super.initState();
    _loadQrInfo();
  }

  Future<void> _loadQrInfo() async {
    final info = await _groupQrService.fetchGroupQrCode(widget.sessionId);
    if (!mounted) {
      return;
    }
    setState(() {
      _qrInfo = info;
      _isLoading = false;
    });
  }

  Future<void> _copyInviteLink() async {
    final info = _qrInfo;
    final shareURL = info?.shareUrl.trim() ?? '';
    if (shareURL.isEmpty) {
      return;
    }
    await Clipboard.setData(ClipboardData(text: shareURL));
    CustomToast.show('chat_copy_success'.tr, isError: false);
  }

  Future<void> _downloadQrImage() async {
    if (_isDownloading) {
      return;
    }
    final info = _qrInfo;
    if (info == null || info.shareUrl.trim().isEmpty) {
      return;
    }

    setState(() {
      _isDownloading = true;
    });
    try {
      final imageBytes = await _captureQrAsPngBytes();
      if (imageBytes == null || imageBytes.isEmpty) {
        CustomToast.show('chat_group_qr_download_failed'.tr);
        return;
      }

      final fileName =
          'group_qr_${_safeFileNameSegment(widget.sessionId)}_'
          '${DateTime.now().millisecondsSinceEpoch}.png';
      final result = await exportMermaidPng(imageBytes, fileName: fileName);
      if (!mounted) {
        return;
      }

      final message = result.isDownload
          ? 'conversations_my_qr_download_started'.trParams({
              'location': result.location,
            })
          : result.isGallery
          ? 'conversations_my_qr_download_gallery_saved'.tr
          : 'conversations_my_qr_download_saved'.trParams({
              'location': result.location,
            });
      CustomToast.show(message, isError: false);
    } catch (_) {
      if (mounted) {
        CustomToast.show('chat_group_qr_download_failed'.tr);
      }
    } finally {
      if (mounted) {
        setState(() {
          _isDownloading = false;
        });
      }
    }
  }

  Future<Uint8List?> _captureQrAsPngBytes() async {
    final buildContext = _qrExportBoundaryKey.currentContext;
    final renderObject = buildContext?.findRenderObject();
    if (renderObject is! RenderRepaintBoundary) {
      return null;
    }

    final pixelRatio = MediaQuery.of(context).devicePixelRatio.clamp(1, 4);
    await WidgetsBinding.instance.endOfFrame;
    ui.Image? image;
    try {
      image = await renderObject.toImage(pixelRatio: pixelRatio.toDouble());
      final byteData = await image.toByteData(format: ui.ImageByteFormat.png);
      return byteData?.buffer.asUint8List();
    } finally {
      image?.dispose();
    }
  }

  String _safeFileNameSegment(String raw) {
    final normalized = raw.trim().replaceAll(RegExp(r'[^a-zA-Z0-9_-]+'), '_');
    if (normalized.isEmpty) {
      return 'group';
    }
    return normalized;
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      backgroundColor: theme.scaffoldBackgroundColor,
      appBar: AppBar(title: Text('chat_group_qr_title'.tr)),
      body: _buildBody(theme),
    );
  }

  Widget _buildBody(ThemeData theme) {
    if (_isLoading) {
      return const Center(child: CircularProgressIndicator(strokeWidth: 2));
    }

    final info = _qrInfo;
    if (info == null || info.shareUrl.trim().isEmpty) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(
                'chat_group_qr_load_failed'.tr,
                style: theme.textTheme.bodyMedium,
              ),
              const SizedBox(height: 12),
              OutlinedButton(
                onPressed: () {
                  setState(() {
                    _isLoading = true;
                  });
                  _loadQrInfo();
                },
                child: Text('common_retry'.tr),
              ),
            ],
          ),
        ),
      );
    }

    final title = widget.groupName.trim();
    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 24),
      children: [
        Container(
          padding: const EdgeInsets.all(18),
          decoration: BoxDecoration(
            color: theme.colorScheme.surface,
            borderRadius: BorderRadius.circular(14),
          ),
          child: Column(
            children: [
              if (title.isNotEmpty) ...[
                Text(
                  title,
                  textAlign: TextAlign.center,
                  style: theme.textTheme.titleMedium?.copyWith(
                    fontWeight: FontWeight.w600,
                  ),
                ),
                const SizedBox(height: 8),
              ],
              Text(
                'chat_group_qr_hint'.tr,
                textAlign: TextAlign.center,
                style: theme.textTheme.bodyMedium?.copyWith(
                  color: theme.colorScheme.secondary.withValues(alpha: 0.8),
                ),
              ),
              const SizedBox(height: 16),
              RepaintBoundary(
                key: _qrExportBoundaryKey,
                child: Container(
                  padding: const EdgeInsets.all(14),
                  decoration: BoxDecoration(
                    color: Colors.white,
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: QrImageView(
                    data: info.shareUrl,
                    size: 220,
                    backgroundColor: Colors.white,
                    eyeStyle: const QrEyeStyle(
                      eyeShape: QrEyeShape.square,
                      color: Colors.black,
                    ),
                    dataModuleStyle: const QrDataModuleStyle(
                      dataModuleShape: QrDataModuleShape.square,
                      color: Colors.black,
                    ),
                  ),
                ),
              ),
              if (info.code.trim().isNotEmpty) ...[
                const SizedBox(height: 12),
                Text(
                  'chat_group_qr_code_value'.trParams({'code': info.code}),
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.secondary.withValues(alpha: 0.8),
                  ),
                ),
              ],
              const SizedBox(height: 18),
              Row(
                children: [
                  Expanded(
                    child: OutlinedButton.icon(
                      onPressed: _copyInviteLink,
                      icon: const Icon(Icons.link_rounded, size: 18),
                      label: Text('chat_group_qr_copy_link'.tr),
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: OutlinedButton.icon(
                      onPressed: _isDownloading ? null : _downloadQrImage,
                      icon: _isDownloading
                          ? const SizedBox(
                              width: 16,
                              height: 16,
                              child: CircularProgressIndicator(strokeWidth: 2),
                            )
                          : const Icon(Icons.download_rounded, size: 18),
                      label: Text('chat_group_qr_download'.tr),
                    ),
                  ),
                ],
              ),
            ],
          ),
        ),
      ],
    );
  }
}
