import 'dart:js_interop';
import 'dart:typed_data';

import 'package:web/web.dart' as web;

import 'mermaid_image_exporter_types.dart';

Future<MermaidImageExportResult> exportMermaidPng(
  Uint8List bytes, {
  required String fileName,
}) async {
  final blob = web.Blob(
    [bytes.toJS].toJS,
    web.BlobPropertyBag(type: 'image/png'),
  );
  final url = web.URL.createObjectURL(blob);
  final anchor = web.HTMLAnchorElement()
    ..href = url
    ..download = fileName
    ..style.display = 'none';
  web.document.body?.append(anchor);
  anchor.click();
  anchor.remove();
  web.URL.revokeObjectURL(url);
  return MermaidImageExportResult(
    location: fileName,
    isDownload: true,
  );
}
