import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../../../shared/utils/hardware_facade.dart';
import '../../../shared/utils/toast_util.dart';

class UserQrScannerView extends StatefulWidget {
  const UserQrScannerView({super.key});

  @override
  State<UserQrScannerView> createState() => _UserQrScannerViewState();
}

class _UserQrScannerViewState extends State<UserQrScannerView>
    with WidgetsBindingObserver {
  final MobileScannerController _scannerController = MobileScannerController();
  bool _resolved = false;
  bool _isPickingFromGallery = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _scannerController.dispose();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    switch (state) {
      case AppLifecycleState.resumed:
        if (_resolved || _isPickingFromGallery) {
          return;
        }
        unawaited(_resumeScanner());
        return;
      case AppLifecycleState.inactive:
      case AppLifecycleState.hidden:
      case AppLifecycleState.paused:
      case AppLifecycleState.detached:
        unawaited(_pauseScanner());
        return;
    }
  }

  String _extractFirstRawValue(BarcodeCapture? capture) {
    if (capture == null) {
      return '';
    }

    for (final barcode in capture.barcodes) {
      final rawValue = barcode.rawValue?.trim() ?? '';
      if (rawValue.isNotEmpty) {
        return rawValue;
      }
    }

    return '';
  }

  Future<void> _pauseScanner() async {
    final scannerState = _scannerController.value;
    if (!scannerState.isInitialized || !scannerState.isRunning) {
      return;
    }
    await _scannerController.stop();
  }

  Future<void> _resumeScanner() async {
    final scannerState = _scannerController.value;
    if (!scannerState.isInitialized || scannerState.isRunning) {
      return;
    }
    await _scannerController.start();
  }

  Future<void> _resolveScanResult(String rawValue) async {
    final normalized = rawValue.trim();
    if (_resolved || normalized.isEmpty) {
      return;
    }

    _resolved = true;
    await _pauseScanner();
    if (!mounted) {
      return;
    }
    Get.back<String>(result: normalized);
  }

  Future<void> _handleDetection(BarcodeCapture capture) async {
    if (_resolved || _isPickingFromGallery) {
      return;
    }

    final rawValue = _extractFirstRawValue(capture);
    await _resolveScanResult(rawValue);
  }

  Future<void> _scanFromGallery() async {
    if (_resolved || _isPickingFromGallery) {
      return;
    }

    if (mounted) {
      setState(() {
        _isPickingFromGallery = true;
      });
    } else {
      _isPickingFromGallery = true;
    }

    try {
      await _pauseScanner();

      final pickedImage = await HardwareFacade.pickImage();
      if (pickedImage == null || _resolved) {
        return;
      }

      if (kIsWeb) {
        if (mounted) {
          CustomToast.show('common_unknown_error'.tr);
        }
        return;
      }

      final capture = await _scannerController.analyzeImage(pickedImage.path);
      final rawValue = _extractFirstRawValue(capture);
      if (rawValue.isEmpty) {
        if (mounted) {
          CustomToast.show('conversations_scan_invalid_qr'.tr);
        }
        return;
      }

      await _resolveScanResult(rawValue);
    } catch (error) {
      debugPrint('UserQrScannerView._scanFromGallery error: $error');
      if (mounted) {
        CustomToast.show('common_unknown_error'.tr);
      }
    } finally {
      if (mounted) {
        setState(() {
          _isPickingFromGallery = false;
        });
      } else {
        _isPickingFromGallery = false;
      }

      if (!_resolved) {
        try {
          await _resumeScanner();
        } catch (error) {
          debugPrint('UserQrScannerView._resumeScanner error: $error');
        }
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      backgroundColor: Colors.black,
      appBar: AppBar(
        title: Text('conversations_scan_user_qr'.tr),
        backgroundColor: Colors.black,
        foregroundColor: Colors.white,
        actions: [
          IconButton(
            onPressed: _isPickingFromGallery ? null : _scanFromGallery,
            tooltip: 'profile_avatar_pick_gallery'.tr,
            icon: _isPickingFromGallery
                ? const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.photo_library_outlined),
          ),
        ],
      ),
      body: Stack(
        children: [
          MobileScanner(
            controller: _scannerController,
            onDetect: (capture) {
              _handleDetection(capture);
            },
          ),
          Positioned(
            left: 0,
            right: 0,
            bottom: 28,
            child: Center(
              child: Container(
                margin: const EdgeInsets.symmetric(horizontal: 20),
                padding:
                    const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
                decoration: BoxDecoration(
                  color: Colors.black.withValues(alpha: 0.56),
                  borderRadius: BorderRadius.circular(10),
                ),
                child: Text(
                  'conversations_scan_user_qr_hint'.tr,
                  textAlign: TextAlign.center,
                  style: theme.textTheme.bodyMedium?.copyWith(
                    color: Colors.white,
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}
