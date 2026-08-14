import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';

class AvatarDesktopCropDialog extends StatefulWidget {
  const AvatarDesktopCropDialog({
    super.key,
    required this.imageBytes,
    required this.title,
    required this.hint,
    required this.zoomLabel,
    required this.zoomOutLabel,
    required this.zoomInLabel,
    required this.cancelLabel,
    required this.saveLabel,
  });

  final Uint8List imageBytes;
  final String title;
  final String hint;
  final String zoomLabel;
  final String zoomOutLabel;
  final String zoomInLabel;
  final String cancelLabel;
  final String saveLabel;

  @override
  State<AvatarDesktopCropDialog> createState() =>
      _AvatarDesktopCropDialogState();
}

class _AvatarDesktopCropDialogState extends State<AvatarDesktopCropDialog> {
  static const double _cropSize = 320;
  static const int _outputSize = 512;

  ui.Image? _image;
  bool _isLoading = true;
  bool _isSaving = false;
  String? _error;

  double _minScale = 1;
  double _scale = 1;
  Offset _offset = Offset.zero;

  @override
  void initState() {
    super.initState();
    _decodeImage();
  }

  @override
  void dispose() {
    _image?.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Dialog(
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 460),
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 16, 20, 14),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text(
                widget.title,
                style: Theme.of(context).textTheme.titleMedium,
              ),
              const SizedBox(height: 12),
              _buildCropArea(),
              const SizedBox(height: 10),
              Text(
                widget.hint,
                style: Theme.of(context).textTheme.bodySmall,
              ),
              const SizedBox(height: 8),
              _buildScaleSlider(),
              const SizedBox(height: 12),
              _buildActions(),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildCropArea() {
    if (_isLoading) {
      return const SizedBox(
        width: _cropSize,
        height: _cropSize,
        child: Center(child: CircularProgressIndicator(strokeWidth: 2)),
      );
    }
    if (_error != null) {
      return SizedBox(
        width: _cropSize,
        height: _cropSize,
        child: Center(
          child: Text(
            _error!,
            textAlign: TextAlign.center,
          ),
        ),
      );
    }

    final image = _image!;
    final displayWidth = image.width * _scale;
    final displayHeight = image.height * _scale;
    final left = (_cropSize - displayWidth) / 2 + _offset.dx;
    final top = (_cropSize - displayHeight) / 2 + _offset.dy;

    return Center(
      child: SizedBox(
        width: _cropSize,
        height: _cropSize,
        child: ClipRect(
          child: GestureDetector(
            onPanUpdate: _isSaving
                ? null
                : (details) {
                    setState(() {
                      _offset = _clampOffset(_offset + details.delta);
                    });
                  },
            child: Stack(
              fit: StackFit.expand,
              children: [
                Container(color: Colors.black.withValues(alpha: 0.85)),
                Positioned(
                  left: left,
                  top: top,
                  width: displayWidth,
                  height: displayHeight,
                  child: RawImage(image: image, fit: BoxFit.fill),
                ),
                IgnorePointer(
                  child: Container(
                    decoration: BoxDecoration(
                      border: Border.all(color: Colors.white, width: 2),
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildScaleSlider() {
    if (_isLoading || _error != null) {
      return const SizedBox(height: 68);
    }
    final theme = Theme.of(context);
    final colorScheme = theme.colorScheme;
    final maxScale = _minScale * 4;
    final scalePercent = ((_scale / _minScale) * 100).round();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            Text(widget.zoomLabel, style: theme.textTheme.labelMedium),
            const Spacer(),
            Text(
              '$scalePercent%',
              style: theme.textTheme.labelMedium?.copyWith(
                color: colorScheme.primary,
                fontWeight: FontWeight.w600,
              ),
            ),
          ],
        ),
        const SizedBox(height: 6),
        Container(
          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(10),
            border: Border.all(
              color: colorScheme.outline.withValues(alpha: 0.36),
            ),
            color: colorScheme.surface.withValues(alpha: 0.65),
          ),
          child: Row(
            children: [
              Icon(
                Icons.zoom_out_rounded,
                size: 18,
                color: theme.textTheme.bodySmall?.color,
              ),
              Expanded(
                child: SliderTheme(
                  data: SliderTheme.of(context).copyWith(
                    trackHeight: 6,
                    activeTrackColor: colorScheme.primary,
                    inactiveTrackColor:
                        colorScheme.outline.withValues(alpha: 0.42),
                    thumbColor: colorScheme.primary,
                    overlayColor: colorScheme.primary.withValues(alpha: 0.14),
                    thumbShape:
                        const RoundSliderThumbShape(enabledThumbRadius: 10),
                    overlayShape:
                        const RoundSliderOverlayShape(overlayRadius: 18),
                  ),
                  child: Slider(
                    value: _scale,
                    min: _minScale,
                    max: maxScale,
                    semanticFormatterCallback: (value) {
                      final percent = ((value / _minScale) * 100).round();
                      return '$percent%';
                    },
                    onChanged: _isSaving
                        ? null
                        : (nextScale) {
                            setState(() {
                              _scale = nextScale;
                              _offset = _clampOffset(_offset);
                            });
                          },
                  ),
                ),
              ),
              Icon(
                Icons.zoom_in_rounded,
                size: 18,
                color: theme.textTheme.bodySmall?.color,
              ),
            ],
          ),
        ),
        const SizedBox(height: 4),
        Row(
          children: [
            Text(widget.zoomOutLabel, style: theme.textTheme.bodySmall),
            const Spacer(),
            Text(widget.zoomInLabel, style: theme.textTheme.bodySmall),
          ],
        ),
      ],
    );
  }

  Widget _buildActions() {
    if (_isSaving) {
      return const Align(
        alignment: Alignment.centerRight,
        child: SizedBox(
          width: 20,
          height: 20,
          child: CircularProgressIndicator(strokeWidth: 2),
        ),
      );
    }

    return Row(
      mainAxisAlignment: MainAxisAlignment.end,
      children: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: Text(widget.cancelLabel),
        ),
        const SizedBox(width: 8),
        ElevatedButton(
          onPressed: (_isLoading || _error != null) ? null : _saveCrop,
          child: Text(widget.saveLabel),
        ),
      ],
    );
  }

  Future<void> _decodeImage() async {
    try {
      final codec = await ui.instantiateImageCodec(widget.imageBytes);
      final frame = await codec.getNextFrame();
      if (!mounted) {
        frame.image.dispose();
        return;
      }
      _image?.dispose();
      final image = frame.image;
      final minScale = _resolveMinScale(
        imageWidth: image.width.toDouble(),
        imageHeight: image.height.toDouble(),
      );
      setState(() {
        _image = image;
        _minScale = minScale;
        _scale = minScale;
        _offset = Offset.zero;
        _error = null;
        _isLoading = false;
      });
    } catch (_) {
      if (!mounted) {
        return;
      }
      setState(() {
        _error = 'Failed to load image';
        _isLoading = false;
      });
    }
  }

  double _resolveMinScale({
    required double imageWidth,
    required double imageHeight,
  }) {
    final widthScale = _cropSize / imageWidth;
    final heightScale = _cropSize / imageHeight;
    return widthScale > heightScale ? widthScale : heightScale;
  }

  Offset _clampOffset(Offset offset) {
    final image = _image;
    if (image == null) {
      return offset;
    }
    final displayWidth = image.width * _scale;
    final displayHeight = image.height * _scale;
    final maxDx = ((displayWidth - _cropSize) / 2).clamp(0.0, double.infinity);
    final maxDy = ((displayHeight - _cropSize) / 2).clamp(0.0, double.infinity);
    return Offset(
      offset.dx.clamp(-maxDx, maxDx).toDouble(),
      offset.dy.clamp(-maxDy, maxDy).toDouble(),
    );
  }

  Rect _resolveSourceRect(ui.Image image) {
    final displayWidth = image.width * _scale;
    final displayHeight = image.height * _scale;
    final left = (_cropSize - displayWidth) / 2 + _offset.dx;
    final top = (_cropSize - displayHeight) / 2 + _offset.dy;

    double sourceX = (-left) / _scale;
    double sourceY = (-top) / _scale;
    double sourceW = _cropSize / _scale;
    double sourceH = _cropSize / _scale;

    sourceX = sourceX.clamp(0.0, image.width.toDouble());
    sourceY = sourceY.clamp(0.0, image.height.toDouble());
    if (sourceX + sourceW > image.width) {
      sourceW = image.width - sourceX;
    }
    if (sourceY + sourceH > image.height) {
      sourceH = image.height - sourceY;
    }

    if (sourceW <= 0 || sourceH <= 0) {
      return Rect.fromLTWH(
        0,
        0,
        image.width.toDouble(),
        image.height.toDouble(),
      );
    }
    return Rect.fromLTWH(sourceX, sourceY, sourceW, sourceH);
  }

  Future<void> _saveCrop() async {
    final image = _image;
    if (image == null || _isSaving) {
      return;
    }
    setState(() {
      _isSaving = true;
    });
    try {
      final sourceRect = _resolveSourceRect(image);
      final recorder = ui.PictureRecorder();
      final canvas = Canvas(recorder);
      canvas.drawImageRect(
        image,
        sourceRect,
        Rect.fromLTWH(0, 0, _outputSize.toDouble(), _outputSize.toDouble()),
        Paint(),
      );
      final picture = recorder.endRecording();
      final outputImage = await picture.toImage(_outputSize, _outputSize);
      final byteData =
          await outputImage.toByteData(format: ui.ImageByteFormat.png);
      outputImage.dispose();
      if (!mounted) {
        return;
      }
      final bytes = byteData?.buffer.asUint8List();
      if (bytes == null || bytes.isEmpty) {
        Navigator.of(context).pop(widget.imageBytes);
        return;
      }
      Navigator.of(context).pop(bytes);
    } catch (_) {
      if (!mounted) {
        return;
      }
      Navigator.of(context).pop(widget.imageBytes);
    } finally {
      if (mounted) {
        setState(() {
          _isSaving = false;
        });
      }
    }
  }
}
