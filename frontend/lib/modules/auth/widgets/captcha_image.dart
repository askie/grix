import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:get/get.dart';

class CaptchaImage extends StatelessWidget {
  final String b64s;

  const CaptchaImage({super.key, required this.b64s});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final bytes = _decodeBase64Image(b64s);

    if (bytes == null) {
      return Container(
        width: 120,
        height: 44,
        decoration: BoxDecoration(
          borderRadius: BorderRadius.circular(8),
          color: theme.colorScheme.surfaceContainerHighest,
        ),
        alignment: Alignment.center,
        child: Text('captcha_unavailable'.tr, style: theme.textTheme.bodySmall),
      );
    }

    return ClipRRect(
      borderRadius: BorderRadius.circular(8),
      child: Image.memory(
        bytes,
        width: 120,
        height: 44,
        fit: BoxFit.cover,
        gaplessPlayback: true,
      ),
    );
  }

  Uint8List? _decodeBase64Image(String source) {
    final normalized = source.trim();
    if (normalized.isEmpty) return null;

    final markerIndex = normalized.indexOf('base64,');
    final pureBase64 = markerIndex >= 0
        ? normalized.substring(markerIndex + 7)
        : normalized;

    try {
      return base64Decode(pureBase64);
    } catch (_) {
      return null;
    }
  }
}
