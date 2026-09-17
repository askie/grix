import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:get/get.dart';

import '../../../data/providers/im_service.dart';
import '../../../data/providers/oss_service.dart';
import '../../../shared/models/chat_message_attachment.dart';
import '../../chat/models/chat_attachment_type.dart';
import '../../chat/services/chat_attachment_limit_policy.dart';
import '../../chat/services/chat_attachment_payload_builder.dart';
import '../../chat/services/chat_image_compression_service.dart';
import '../models/share_inbox_manifest.dart';
import 'share_inbox_item_filter.dart';

class ShareOutboundSendFailure implements Exception {
  ShareOutboundSendFailure(this.messageKey);

  final String messageKey;
}

class ShareOutboundSender {
  ShareOutboundSender({
    ImService? imService,
    OssService? ossService,
    ChatImageCompressionService? imageCompressionService,
  }) : _imService = imService ?? Get.find<ImService>(),
       _ossService = ossService ?? Get.find<OssService>(),
       _imageCompression =
           imageCompressionService ?? const ChatImageCompressionService();

  final ImService _imService;
  final OssService _ossService;
  final ChatImageCompressionService _imageCompression;

  /// Max characters for share note and inline shared text before spilling to .txt.
  static const int maxShareTextCharacters = 10000;

  @visibleForTesting
  static Uint8List encodeTextAsUtf8AttachmentBytes(String text) {
    return Uint8List.fromList(utf8.encode(text));
  }

  Future<void> sendManifestToSession({
    required ShareInboxManifest manifest,
    required String sessionId,
    String caption = '',
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      throw ShareOutboundSendFailure('share_ingest_send_failed');
    }

    final trimmedCaption = caption.trim();
    if (trimmedCaption.length > maxShareTextCharacters) {
      throw ShareOutboundSendFailure('share_ingest_caption_too_long');
    }

    final sharedTextBody = ShareInboxItemFilter.collectSharedTextBody(
      manifest.items,
    );
    final spillSharedTextToFile =
        sharedTextBody.length > maxShareTextCharacters;

    final attachments = <ChatMessageAttachment>[];

    if (spillSharedTextToFile) {
      final bytes = encodeTextAsUtf8AttachmentBytes(sharedTextBody);
      attachments.add(
        await _uploadBytesAsAttachment(
          bytes: bytes,
          fileName:
              'shared_text_${DateTime.now().millisecondsSinceEpoch}.txt',
          contentType: 'text/plain',
          attachmentType: ChatAttachmentType.file,
        ),
      );
    }

    for (final item in manifest.items) {
      if (!item.isFile) {
        continue;
      }
      attachments.add(await _uploadFileItem(item));
    }

    final messageTextParts = <String>[];
    if (trimmedCaption.isNotEmpty) {
      messageTextParts.add(trimmedCaption);
    }
    if (!spillSharedTextToFile && sharedTextBody.isNotEmpty) {
      messageTextParts.add(sharedTextBody);
    }
    final leadingText = messageTextParts.join('\n');

    if (attachments.isEmpty) {
      if (leadingText.isEmpty) {
        return;
      }
      await _imService.sendMessage(
        leadingText,
        sid,
        updateCurrentSessionUi: false,
      );
      return;
    }

    final attachmentContent = ChatAttachmentPayloadBuilder.buildMessageContent(
      attachments,
    );
    final content = leadingText.isNotEmpty
        ? '$leadingText\n$attachmentContent'
        : attachmentContent;
    final extra = ChatAttachmentPayloadBuilder.buildMessageExtra(attachments);
    await _imService.sendMessage(
      content,
      sid,
      extra: extra,
      updateCurrentSessionUi: false,
    );
  }

  Future<ChatMessageAttachment> _uploadFileItem(ShareInboxItem item) async {
    final path = item.absolutePath?.trim() ?? '';
    if (path.isEmpty) {
      throw ShareOutboundSendFailure('share_ingest_file_missing');
    }
    if (kIsWeb) {
      throw ShareOutboundSendFailure('share_ingest_send_failed');
    }
    final file = File(path);
    if (!await file.exists()) {
      throw ShareOutboundSendFailure('share_ingest_file_missing');
    }
    final length = item.size ?? await file.length();
    final rawName = item.fileName?.trim().isNotEmpty == true
        ? item.fileName!.trim()
        : file.uri.pathSegments.last;
    final type = _resolveType(item, fileName: rawName);
    final fileName = ChatAttachmentPayloadBuilder.resolveFileName(
      rawName,
      type: type,
    );
    final contentType = ChatAttachmentPayloadBuilder.resolveContentType(
      fileName,
      type: type,
    );
    final bytes = await file.readAsBytes();
    return _uploadBytesAsAttachment(
      bytes: bytes,
      fileName: fileName,
      contentType: contentType,
      byteLength: length,
      attachmentType: type,
    );
  }

  ChatAttachmentType _resolveType(ShareInboxItem item, {required String fileName}) {
    final mime = item.mime?.toLowerCase() ?? '';
    if (mime.startsWith('image/')) {
      return ChatAttachmentType.image;
    }
    if (mime.startsWith('video/')) {
      return ChatAttachmentType.video;
    }
    final ext = fileName.contains('.')
        ? fileName.split('.').last.toLowerCase()
        : '';
    const imageExt = <String>{'jpg', 'jpeg', 'png', 'gif', 'webp', 'heic', 'heif'};
    const videoExt = <String>{'mp4', 'mov', 'm4v', 'webm', 'mkv', 'avi'};
    if (imageExt.contains(ext)) return ChatAttachmentType.image;
    if (videoExt.contains(ext)) return ChatAttachmentType.video;
    return ChatAttachmentType.file;
  }

  Future<ChatMessageAttachment> _uploadBytesAsAttachment({
    required Uint8List bytes,
    required String fileName,
    required String contentType,
    int? byteLength,
    required ChatAttachmentType attachmentType,
  }) async {
    final type = attachmentType;

    switch (type) {
      case ChatAttachmentType.image:
        if (!ChatAttachmentLimitPolicy.isImageWithinLimit(bytes.length) &&
            !ChatAttachmentLimitPolicy.shouldCompressImage(bytes.length)) {
          throw ShareOutboundSendFailure('chat_attachment_image_too_large');
        }
        final prepared = await _imageCompression.prepareForUpload(
          bytes: bytes,
          fileName: fileName,
          contentType: contentType,
        );
        if (prepared == null) {
          throw ShareOutboundSendFailure('chat_attachment_image_too_large');
        }
        return _uploadToOss(
          fileName: prepared.fileName,
          contentType: prepared.contentType,
          bytes: prepared.bytes,
          type: ChatAttachmentType.image,
        );
      case ChatAttachmentType.video:
        final size = byteLength ?? bytes.length;
        if (!ChatAttachmentLimitPolicy.isVideoWithinLimit(size)) {
          throw ShareOutboundSendFailure('chat_attachment_video_too_large');
        }
        return _uploadToOss(
          fileName: fileName,
          contentType: contentType,
          bytes: bytes,
          type: ChatAttachmentType.video,
        );
      case ChatAttachmentType.file:
        final resolvedName = ChatAttachmentPayloadBuilder.resolveFileName(
          fileName,
          type: ChatAttachmentType.file,
        );
        if (!ChatAttachmentPayloadBuilder.isSupportedFile(resolvedName)) {
          throw ShareOutboundSendFailure('chat_attachment_file_unsupported');
        }
        if (bytes.isEmpty) {
          throw ShareOutboundSendFailure('chat_attachment_file_empty');
        }
        return _uploadToOss(
          fileName: resolvedName,
          contentType: ChatAttachmentPayloadBuilder.resolveContentType(
            resolvedName,
            type: ChatAttachmentType.file,
          ),
          bytes: bytes,
          type: ChatAttachmentType.file,
        );
    }
  }

  Future<ChatMessageAttachment> _uploadToOss({
    required String fileName,
    required String contentType,
    required Uint8List bytes,
    required ChatAttachmentType type,
  }) async {
    final presignRes = await _ossService.getPresignedUrl(fileName, contentType);
    if (presignRes == null) {
      throw ShareOutboundSendFailure('oss_upload_failed');
    }
    final uploadUrl = presignRes['uploadUrl']?.trim() ?? '';
    final accessUrl = presignRes['accessUrl']?.trim() ?? '';
    if (uploadUrl.isEmpty || accessUrl.isEmpty) {
      throw ShareOutboundSendFailure('oss_upload_failed');
    }
    final uploaded = await _ossService.uploadToOss(
      uploadUrl,
      bytes,
      contentType: contentType,
    );
    if (!uploaded) {
      throw ShareOutboundSendFailure('oss_upload_failed');
    }
    return ChatMessageAttachment(
      url: accessUrl,
      type: type.name,
      fileName: fileName,
      contentType: contentType,
    );
  }
}
