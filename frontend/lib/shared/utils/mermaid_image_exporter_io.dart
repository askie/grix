import 'dart:io';

import 'package:flutter/services.dart';
import 'package:path/path.dart' as path;
import 'package:path_provider/path_provider.dart';

import 'mermaid_image_exporter_types.dart';

const MethodChannel _gallerySaveChannel = MethodChannel(
  'pub.dhf.grix/mermaid_image_saver',
);

Future<MermaidImageExportResult> exportMermaidPng(
  Uint8List bytes, {
  required String fileName,
}) async {
  if (Platform.isAndroid || Platform.isIOS) {
    final savedLocation = await _saveToSystemGallery(bytes, fileName: fileName);
    return MermaidImageExportResult(
      location: savedLocation,
      isDownload: false,
      isGallery: true,
    );
  }
  final outputDirectories = await _resolveOutputDirectories();
  FileSystemException? lastError;
  for (final outputDirectory in outputDirectories) {
    try {
      if (!await outputDirectory.exists()) {
        await outputDirectory.create(recursive: true);
      }
      final outputPath = path.join(outputDirectory.path, fileName);
      final outputFile = File(outputPath);
      await outputFile.writeAsBytes(bytes, flush: true);
      return MermaidImageExportResult(location: outputPath, isDownload: false);
    } on FileSystemException catch (error) {
      lastError = error;
    }
  }
  throw lastError ?? StateError('No writable output directory found');
}

Future<String> _saveToSystemGallery(
  Uint8List bytes, {
  required String fileName,
}) async {
  final location = await _gallerySaveChannel.invokeMethod<String>(
    'saveImageToGallery',
    <String, Object>{'bytes': bytes, 'fileName': fileName},
  );
  if (location == null || location.isEmpty) {
    throw StateError('Native gallery saver returned empty location');
  }
  return location;
}

Future<List<Directory>> _resolveOutputDirectories() async {
  final directories = <Directory>[];
  try {
    final downloadsDirectory = await getDownloadsDirectory();
    if (downloadsDirectory != null) {
      directories.add(downloadsDirectory);
    }
  } catch (_) {
    // Ignore and fallback to app document directory.
  }
  directories.add(await getApplicationDocumentsDirectory());
  return directories;
}
