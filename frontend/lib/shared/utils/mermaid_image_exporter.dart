import 'dart:typed_data';

import 'mermaid_image_exporter_stub.dart'
    if (dart.library.js_interop) 'mermaid_image_exporter_web.dart'
    if (dart.library.io) 'mermaid_image_exporter_io.dart'
    as impl;
import 'mermaid_image_exporter_types.dart';

Future<MermaidImageExportResult> exportMermaidPng(
  Uint8List bytes, {
  required String fileName,
}) {
  return impl.exportMermaidPng(bytes, fileName: fileName);
}
