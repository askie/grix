import 'dart:typed_data';

class ReportAttachmentDraft {
  const ReportAttachmentDraft({
    required this.fileName,
    required this.contentType,
    required this.bytes,
  });

  final String fileName;
  final String contentType;
  final Uint8List bytes;
}
