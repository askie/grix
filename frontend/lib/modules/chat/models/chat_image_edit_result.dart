import 'dart:typed_data';

class ChatImageEditResult {
  const ChatImageEditResult({
    required this.bytes,
    required this.fileName,
    required this.contentType,
    required this.uploadOriginal,
  });

  final Uint8List bytes;
  final String fileName;
  final String contentType;
  final bool uploadOriginal;
}
