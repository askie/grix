import '../models/chat_message_attachment.dart';

class ChatMessageAttachmentCodec {
  const ChatMessageAttachmentCodec._();

  static List<ChatMessageAttachment> readFromExtra(Map<String, dynamic> extra) {
    final raw = extra['attachments'];
    if (raw is! List) {
      return const <ChatMessageAttachment>[];
    }

    final attachments = <ChatMessageAttachment>[];
    for (final item in raw) {
      if (item is! Map) {
        continue;
      }
      final attachment = _readAttachment(Map<String, dynamic>.from(item));
      if (attachment != null) {
        attachments.add(attachment);
      }
    }
    return attachments;
  }

  static Map<String, dynamic> buildExtra(
    List<ChatMessageAttachment> attachments,
  ) {
    return <String, dynamic>{
      'attachments': attachments.map((item) => item.toJson()).toList(),
    };
  }

  static String buildContent(List<ChatMessageAttachment> attachments) {
    return attachments
        .map((item) => _buildAttachmentContent(item))
        .where((item) => item.isNotEmpty)
        .join('\n');
  }

  static String stripGeneratedAttachmentContent(
    String content,
    List<ChatMessageAttachment> attachments,
  ) {
    if (content.trim().isEmpty || attachments.isEmpty) {
      return content;
    }

    final generatedLines = buildContent(attachments)
        .split(RegExp(r'\r?\n'))
        .map((line) => line.trim())
        .where((line) => line.isNotEmpty)
        .toSet();
    if (generatedLines.isEmpty) {
      return content;
    }

    final sourceLines = content.split(RegExp(r'\r?\n'));
    final filteredLines = <String>[];
    var removedCount = 0;
    for (final line in sourceLines) {
      if (generatedLines.contains(line.trim())) {
        removedCount += 1;
        continue;
      }
      filteredLines.add(line);
    }
    if (removedCount == 0) {
      return content;
    }
    return filteredLines.join('\n');
  }

  static ChatMessageAttachment? _readAttachment(Map<String, dynamic> json) {
    final url = json['media_url']?.toString().trim() ?? '';
    final type = json['attachment_type']?.toString().trim() ?? '';
    final fileName = json['file_name']?.toString().trim() ?? '';
    final contentType = json['content_type']?.toString().trim() ?? '';
    if (url.isEmpty || type.isEmpty) {
      return null;
    }
    return ChatMessageAttachment(
      url: url,
      type: type,
      fileName: fileName,
      contentType: contentType,
    );
  }

  static String _buildAttachmentContent(ChatMessageAttachment attachment) {
    final safeUrl = '<${attachment.url.trim()}>';
    if (attachment.isImage) {
      return '![image]($safeUrl)';
    }
    final label = _escapeMarkdownLabel(attachment.fileName.trim());
    final fallbackLabel = label.isEmpty ? attachment.type : label;
    return '[$fallbackLabel]($safeUrl)';
  }

  static String _escapeMarkdownLabel(String raw) {
    return raw
        .replaceAll('\\', r'\\')
        .replaceAll('[', r'\[')
        .replaceAll(']', r'\]');
  }
}
