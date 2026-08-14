import 'dart:async';

import 'package:flutter/material.dart';
import 'package:image_cropper/image_cropper.dart';

class AvatarWebCropDialog extends StatefulWidget {
  const AvatarWebCropDialog({
    super.key,
    required this.cropper,
    required this.initCropper,
    required this.crop,
    required this.rotate,
    required this.sourcePath,
    required this.cropTimeout,
    required this.translations,
  });

  final Widget cropper;
  final VoidCallback initCropper;
  final Future<String?> Function() crop;
  final void Function(RotationAngle angle) rotate;
  final String sourcePath;
  final Duration cropTimeout;
  final WebTranslations translations;

  @override
  State<AvatarWebCropDialog> createState() => _AvatarWebCropDialogState();
}

class _AvatarWebCropDialogState extends State<AvatarWebCropDialog> {
  bool _processing = false;

  @override
  void initState() {
    super.initState();
    widget.initCropper();
  }

  @override
  Widget build(BuildContext context) {
    return Dialog(
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(12),
      ),
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 620),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(24, 16, 24, 14),
              child: Align(
                alignment: Alignment.centerLeft,
                child: Text(
                  widget.translations.title,
                  style: Theme.of(context).textTheme.headlineSmall,
                ),
              ),
            ),
            const Divider(height: 1, thickness: 1),
            Padding(
              padding: const EdgeInsets.fromLTRB(24, 20, 24, 8),
              child: Column(
                children: [
                  SizedBox(
                    width: 500,
                    height: 500,
                    child: ClipRect(child: widget.cropper),
                  ),
                  const SizedBox(height: 10),
                  Row(
                    mainAxisAlignment: MainAxisAlignment.spaceBetween,
                    children: [
                      IconButton(
                        onPressed: _processing
                            ? null
                            : () => widget.rotate(
                                  RotationAngle.counterClockwise90,
                                ),
                        tooltip: widget.translations.rotateLeftTooltip,
                        icon: const Icon(Icons.rotate_90_degrees_ccw_rounded),
                      ),
                      IconButton(
                        onPressed: _processing
                            ? null
                            : () => widget.rotate(
                                  RotationAngle.clockwise90,
                                ),
                        tooltip: widget.translations.rotateRightTooltip,
                        icon: const Icon(Icons.rotate_90_degrees_cw_outlined),
                      ),
                    ],
                  ),
                ],
              ),
            ),
            const Divider(height: 1, thickness: 1),
            Padding(
              padding: const EdgeInsets.fromLTRB(12, 10, 12, 10),
              child: _processing
                  ? const SizedBox(
                      width: 22,
                      height: 22,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : Row(
                      mainAxisAlignment: MainAxisAlignment.end,
                      children: [
                        TextButton(
                          onPressed: () => Navigator.of(context).pop(),
                          child: Text(widget.translations.cancelButton),
                        ),
                        const SizedBox(width: 8),
                        ElevatedButton(
                          onPressed: _handleCrop,
                          child: Text(widget.translations.cropButton),
                        ),
                      ],
                    ),
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _handleCrop() async {
    if (_processing) {
      return;
    }
    setState(() {
      _processing = true;
    });

    String? cropPath;
    try {
      cropPath = await widget.crop().timeout(widget.cropTimeout);
    } on TimeoutException {
      cropPath = null;
    } catch (_) {
      cropPath = null;
    }

    if (!mounted) {
      return;
    }
    Navigator.of(context).pop(cropPath ?? widget.sourcePath);
  }
}
