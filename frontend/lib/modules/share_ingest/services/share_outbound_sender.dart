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

  static const int maxTextMessageRunes = 100000;

  @visibleForTesting
  static Uint8List encodeTextAsUtf8AttachmentBytes(String text) {
    return Uint8List.fromList(utf8.encode(text));
  }

  Future<void> sendManifestToSession({
    required ShareInboxManifest manifest,
    required String sessionId,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      throw ShareOutboundSendFailure('share_ingest_send_failed');
    }

    for (final item in manifest.items) {
      if (item.isText || item.isUrl) {
        await _sendTextItem(sid, item);
        continue;
      }
      if (item.isFile) {
        await _sendFileItem(sid, item);
      }
    }
  }

  Future<void> _sendTextItem(String sessionId, ShareInboxItem item) async {
    final text = item.text?.trim() ?? '';
    if (text.isEmpty) {
      return;
    }
    if (text.runes.length <= maxTextMessageRunes) {
      await _imService.sendMessage(
        text,
        sessionId,
        updateCurrentSessionUi: false,
      );
      return;
    }
    final bytes = encodeTextAsUtf8AttachmentBytes(text);
    await _sendBytesAsFile(
      sessionId: sessionId,
      bytes: bytes,
      fileName: 'shared_text_${DateTime.now().millisecondsSinceEpoch}.txt',
      contentType: 'text/plain',
    );
  }

  Future<void> _sendFileItem(String sessionId, ShareInboxItem item) async {
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
    await _sendBytesAsFile(
      sessionId: sessionId,
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

  Future<void> _sendBytesAsFile({
    required String sessionId,
    required Uint8List bytes,
    required String fileName,
    required String contentType,
    int? byteLength,
    ChatAttachmentType? attachmentType,
  }) async {
    final type =
        attachmentType ??
        _resolveType(
          ShareInboxItem(type: 'file', mime: contentType, fileName: fileName),
          fileName: fileName,
        );

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
        await _uploadAndSend(
          sessionId: sessionId,
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
        await _uploadAndSend(
          sessionId: sessionId,
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
        await _uploadAndSend(
          sessionId: sessionId,
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

  Future<void> _uploadAndSend({
    required String sessionId,
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
    final attachment = ChatMessageAttachment(
      url: accessUrl,
      type: type.name,
      fileName: fileName,
      contentType: contentType,
    );
    final content = ChatAttachmentPayloadBuilder.buildMessageContent(
      <ChatMessageAttachment>[attachment],
    );
    final extra = ChatAttachmentPayloadBuilder.buildMessageExtra(
      <ChatMessageAttachment>[attachment],
    );
    await _imService.sendMessage(
      content,
      sessionId,
      extra: extra,
      updateCurrentSessionUi: false,
    );
  }
}
