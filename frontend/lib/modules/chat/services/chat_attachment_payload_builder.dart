import 'package:file_picker/file_picker.dart';
import 'package:path/path.dart' as path;

import '../../../shared/models/chat_message_attachment.dart';
import '../../../shared/utils/chat_message_attachment_codec.dart';
import '../models/chat_attachment_type.dart';

class ChatAttachmentPayloadBuilder {
  const ChatAttachmentPayloadBuilder._();

  static const List<String> uploadableFileExtensions = <String>[
    'pdf',
    'doc',
    'docx',
    'xls',
    'xlsx',
    'ppt',
    'pptx',
    'txt',
    'md',
    'csv',
    'json',
    'xml',
    'zip',
    'rar',
    '7z',
    'tar',
    'gz',
  ];

  static const List<String> uploadableVideoExtensions = <String>[
    'mp4',
    'mov',
    'm4v',
    'webm',
    'mkv',
    'avi',
  ];

  static String resolveFileName(
    String rawName, {
    required ChatAttachmentType type,
  }) {
    final normalized = path.basename(rawName.trim());
    if (normalized.isNotEmpty) {
      return normalized;
    }

    final fallbackExt = switch (type) {
      ChatAttachmentType.image => 'png',
      ChatAttachmentType.video => 'mp4',
      ChatAttachmentType.file => 'bin',
    };
    return 'chat_${type.name}_${DateTime.now().millisecondsSinceEpoch}.$fallbackExt';
  }

  static bool isSupportedFile(String fileName) {
    final ext = _extensionOf(fileName);
    if (ext.isEmpty) {
      return false;
    }
    return uploadableFileExtensions.contains(ext);
  }

  /// 选文件时按平台决定 file_picker 的筛选方式。
  ///
  /// Web 端返回 [FileType.any]、不带 allowedExtensions：file_picker 在 Web 上
  /// 按自定义后缀拼出的 accept 串不可靠（每项带多余空格、还混入浏览器不认的
  /// 后缀），会导致选 txt 这类文件时选择器静默返回空。改用 any 让用户正常选，
  /// 选完再由 [isSupportedFile] 兜底校验。原生端用 [FileType.custom]+后缀清单，
  /// 以获得更好的系统弹窗筛选体验。
  static ({FileType type, List<String>? allowedExtensions}) filePickerConfig({
    required bool isWeb,
  }) {
    if (isWeb) {
      return (type: FileType.any, allowedExtensions: null);
    }
    return (
      type: FileType.custom,
      allowedExtensions: uploadableFileExtensions,
    );
  }

  static String resolveContentType(
    String fileName, {
    required ChatAttachmentType type,
  }) {
    final ext = _extensionOf(fileName);
    if (ext.isEmpty) {
      return switch (type) {
        ChatAttachmentType.image => 'image/png',
        ChatAttachmentType.video => 'video/mp4',
        ChatAttachmentType.file => 'application/octet-stream',
      };
    }

    switch (ext) {
      case 'jpg':
      case 'jpeg':
        return 'image/jpeg';
      case 'png':
        return 'image/png';
      case 'webp':
        return 'image/webp';
      case 'gif':
        return 'image/gif';
      case 'bmp':
        return 'image/bmp';
      case 'heic':
        return 'image/heic';
      case 'heif':
        return 'image/heif';
      case 'mp4':
        return 'video/mp4';
      case 'mov':
        return 'video/quicktime';
      case 'm4v':
        return 'video/x-m4v';
      case 'webm':
        return 'video/webm';
      case 'mkv':
        return 'video/x-matroska';
      case 'avi':
        return 'video/x-msvideo';
      case 'pdf':
        return 'application/pdf';
      case 'doc':
        return 'application/msword';
      case 'docx':
        return 'application/vnd.openxmlformats-officedocument.wordprocessingml.document';
      case 'xls':
        return 'application/vnd.ms-excel';
      case 'xlsx':
        return 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet';
      case 'ppt':
        return 'application/vnd.ms-powerpoint';
      case 'pptx':
        return 'application/vnd.openxmlformats-officedocument.presentationml.presentation';
      case 'txt':
        return 'text/plain';
      case 'md':
        return 'text/markdown';
      case 'csv':
        return 'text/csv';
      case 'json':
        return 'application/json';
      case 'xml':
        return 'application/xml';
      case 'zip':
        return 'application/zip';
      case 'rar':
        return 'application/vnd.rar';
      case '7z':
        return 'application/x-7z-compressed';
      case 'tar':
        return 'application/x-tar';
      case 'gz':
        return 'application/gzip';
      default:
        return switch (type) {
          ChatAttachmentType.image => 'image/png',
          ChatAttachmentType.video => 'video/mp4',
          ChatAttachmentType.file => 'application/octet-stream',
        };
    }
  }

  static String buildMessageContent(List<ChatMessageAttachment> attachments) {
    return ChatMessageAttachmentCodec.buildContent(attachments);
  }

  static Map<String, dynamic> buildMessageExtra(
    List<ChatMessageAttachment> attachments,
  ) {
    return ChatMessageAttachmentCodec.buildExtra(attachments);
  }

  static String _extensionOf(String fileName) {
    final dotIndex = fileName.lastIndexOf('.');
    if (dotIndex < 0 || dotIndex >= fileName.length - 1) {
      return '';
    }
    return fileName.substring(dotIndex + 1).toLowerCase();
  }
}
