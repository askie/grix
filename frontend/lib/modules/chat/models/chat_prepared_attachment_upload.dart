import 'dart:typed_data';

import 'chat_attachment_type.dart';

class ChatPreparedAttachmentUpload {
  const ChatPreparedAttachmentUpload({
    required this.type,
    required this.fileName,
    required this.contentType,
    this.bytes,
    this.stream,
    this.contentLength,
  });

  final ChatAttachmentType type;
  final String fileName;
  final String contentType;
  final Uint8List? bytes;
  final Stream<List<int>>? stream;
  final int? contentLength;
}
