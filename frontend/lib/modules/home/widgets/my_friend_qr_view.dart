import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:get/get.dart';
import 'package:qr_flutter/qr_flutter.dart';

import '../../../data/providers/friend_qr_service.dart';
import '../../../shared/utils/mermaid_image_exporter.dart';
import '../../../shared/utils/toast_util.dart';

class MyFriendQrView extends StatefulWidget {
  const MyFriendQrView({super.key});

  @override
  State<MyFriendQrView> createState() => _MyFriendQrViewState();
}

class _MyFriendQrViewState extends State<MyFriendQrView> {
  final FriendQrService _friendQrService = Get.find<FriendQrService>();
  final GlobalKey _qrExportBoundaryKey = GlobalKey();

  FriendQrCodeInfo? _qrInfo;
  bool _isLoading = true;
  bool _isDownloading = false;

  @override
  void initState() {
    super.initState();
    _loadQrInfo();
  }

  Future<void> _loadQrInfo() async {
    final info = await _friendQrService.fetchMyQrCode();
    if (!mounted) return;
    setState(() {
      _qrInfo = info;
      _isLoading = false;
    });
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
        CustomToast.show('conversations_my_qr_download_failed'.tr);
        return;
      }

      final fileName = 'friend_qr_${DateTime.now().millisecondsSinceEpoch}.png';
      final result = await exportMermaidPng(
        imageBytes,
        fileName: fileName,
      );
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
    } catch (error) {
      debugPrint('Failed to download friend qr image: $error');
      if (mounted) {
        CustomToast.show('conversations_my_qr_download_failed'.tr);
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

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      backgroundColor: theme.scaffoldBackgroundColor,
      appBar: AppBar(
        title: Text('conversations_my_qr_title'.tr),
      ),
      body: _buildBody(theme),
    );
  }

  Widget _buildBody(ThemeData theme) {
    if (_isLoading) {
      return const Center(
        child: CircularProgressIndicator(strokeWidth: 2),
      );
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
                'conversations_my_qr_load_failed'.tr,
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
              Text(
                'conversations_my_qr_hint'.tr,
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
              const SizedBox(height: 18),
              SizedBox(
                width: double.infinity,
                child: OutlinedButton.icon(
                  onPressed: _isDownloading ? null : _downloadQrImage,
                  icon: _isDownloading
                      ? const SizedBox(
                          width: 16,
                          height: 16,
                          child: CircularProgressIndicator(
                            strokeWidth: 2,
                          ),
                        )
                      : const Icon(Icons.download_rounded, size: 18),
                  label: Text('conversations_my_qr_download'.tr),
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }
}
