import 'dart:typed_data';

import 'mermaid_image_exporter_types.dart';

Future<MermaidImageExportResult> exportMermaidPng(
  Uint8List bytes, {
  required String fileName,
}) async {
  throw UnsupportedError(
    'Mermaid image export is not supported on this platform.',
  );
}
