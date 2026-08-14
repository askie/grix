class ChatMessageAttachment {
  const ChatMessageAttachment({
    required this.url,
    required this.type,
    required this.fileName,
    required this.contentType,
  });

  final String url;
  final String type;
  final String fileName;
  final String contentType;

  bool get isImage =>
      type == 'image' || contentType.toLowerCase().startsWith('image/');

  bool get isVideo =>
      type == 'video' || contentType.toLowerCase().startsWith('video/');

  Map<String, dynamic> toJson() {
    return <String, dynamic>{
      'media_url': url,
      'attachment_type': type,
      'file_name': fileName,
      'content_type': contentType,
    };
  }
}
