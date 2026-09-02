import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:image_cropper/image_cropper.dart';
import 'avatar_crop_localizations.dart';
import '../widgets/avatar_desktop_crop_dialog.dart';
import '../widgets/avatar_web_crop_dialog.dart';

class AvatarCropResult {
  const AvatarCropResult({required this.bytes});

  final Uint8List bytes;
}

class AvatarCropperService {
  AvatarCropperService({
    ImageCropper? imageCropper,
    Duration webCropTimeout = const Duration(seconds: 8),
  }) : _imageCropper = imageCropper ?? ImageCropper(),
       _webCropTimeout = webCropTimeout;

  final ImageCropper _imageCropper;
  final Duration _webCropTimeout;

  bool get _supportsNativeCropper {
    if (kIsWeb) {
      return true;
    }
    return defaultTargetPlatform == TargetPlatform.android ||
        defaultTargetPlatform == TargetPlatform.iOS;
  }

  AvatarCropStrings _resolveStrings(BuildContext? context) {
    return AvatarCropLocalizations.resolve(locale: _resolveLocale(context));
  }

  Locale? _resolveLocale(BuildContext? context) {
    final activeContext = context ?? Get.context;
    final contextLocale = activeContext == null
        ? null
        : Localizations.maybeLocaleOf(activeContext);
    return contextLocale ?? Get.locale ?? Get.deviceLocale;
  }

  Future<AvatarCropResult?> cropSquareAvatar({
    required String sourcePath,
    BuildContext? webContext,
  }) async {
    if (!_supportsNativeCropper) {
      return _cropOnDesktop(sourcePath: sourcePath, context: webContext);
    }

    final strings = _resolveStrings(webContext);
    final uiSettings = <PlatformUiSettings>[
      AndroidUiSettings(
        toolbarTitle: strings.title,
        initAspectRatio: CropAspectRatioPreset.square,
        lockAspectRatio: true,
      ),
      IOSUiSettings(
        title: strings.title,
        aspectRatioLockEnabled: true,
        resetAspectRatioEnabled: false,
      ),
    ];
    if (kIsWeb && webContext != null) {
      final webTranslations = strings.toWebTranslations();
      uiSettings.add(
        WebUiSettings(
          context: webContext,
          checkCrossOrigin: false,
          checkOrientation: false,
          translations: webTranslations,
          customDialogBuilder: (cropper, initCropper, crop, rotate, scale) {
            return AvatarWebCropDialog(
              cropper: cropper,
              initCropper: initCropper,
              crop: crop,
              rotate: rotate,
              sourcePath: sourcePath,
              cropTimeout: _webCropTimeout,
              translations: webTranslations,
            );
          },
        ),
      );
    }

    final croppedFile = await _imageCropper.cropImage(
      sourcePath: sourcePath,
      aspectRatio: const CropAspectRatio(ratioX: 1, ratioY: 1),
      compressFormat: ImageCompressFormat.jpg,
      compressQuality: 90,
      uiSettings: uiSettings,
    );
    if (croppedFile == null) {
      return null;
    }
    final bytes = await croppedFile.readAsBytes();
    if (bytes.isEmpty) {
      return null;
    }
    return AvatarCropResult(bytes: bytes);
  }

  Future<AvatarCropResult?> _cropOnDesktop({
    required String sourcePath,
    required BuildContext? context,
  }) async {
    final sourceBytes = await CroppedFile(sourcePath).readAsBytes();
    if (sourceBytes.isEmpty) {
      return null;
    }
    if (context == null) {
      return AvatarCropResult(bytes: sourceBytes);
    }
    if (!context.mounted) {
      return null;
    }
    final strings = _resolveStrings(context);
    final Uint8List? croppedBytes = await showDialog<Uint8List>(
      context: context,
      barrierDismissible: true,
      builder: (_) => AvatarDesktopCropDialog(
        imageBytes: sourceBytes,
        title: strings.title,
        hint: strings.hint,
        zoomLabel: strings.zoomLabel,
        zoomOutLabel: strings.zoomOutLabel,
        zoomInLabel: strings.zoomInLabel,
        cancelLabel: strings.cancelLabel,
        saveLabel: strings.saveLabel,
      ),
    );
    if (croppedBytes == null || croppedBytes.isEmpty) {
      return null;
    }
    return AvatarCropResult(bytes: croppedBytes);
  }
}
