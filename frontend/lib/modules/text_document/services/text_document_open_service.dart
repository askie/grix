import 'dart:async';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../shared/markdown/chat_markdown_uri_policy.dart';
import '../../../shared/models/chat_message_attachment.dart';
import '../models/text_document_descriptor.dart';
import '../text_document_page.dart';
import 'text_document_codec.dart';
import 'text_document_format_registry.dart';
import 'text_document_native_bridge.dart';

class TextDocumentOpenService {
  TextDocumentOpenService._();

  static final Dio _dio = Dio(
    BaseOptions(
      connectTimeout: const Duration(seconds: 15),
      receiveTimeout: const Duration(seconds: 30),
    ),
  );
  static bool _initialized = false;
  static bool _opening = false;
  static final List<({TextDocumentDescriptor descriptor, Uint8List bytes})>
  _pending = [];

  static Future<void> initialize() async {
    if (_initialized) return;
    _initialized = true;
    await TextDocumentNativeBridge.initialize(_enqueueNativeDocument);
  }

  static bool supportsAttachment(ChatMessageAttachment attachment) {
    return supportsRemoteFile(
      fileName: attachment.fileName,
      mimeType: attachment.contentType,
    );
  }

  static bool supportsRemoteFile({
    required String fileName,
    String mimeType = '',
  }) {
    return TextDocumentFormatRegistry.isSupported(
      fileName: fileName,
      mimeType: mimeType,
    );
  }

  static Future<void> openRemoteAttachment(
    ChatMessageAttachment attachment,
  ) async {
    if (!supportsAttachment(attachment)) {
      throw const FormatException('text_document_unsupported');
    }
    await _downloadAndOpen(
      url: attachment.url,
      displayName: attachment.fileName,
      mimeType: attachment.contentType,
      source: TextDocumentSource.remoteAttachment,
      handleSeed: attachment.url,
    );
  }

  /// Reads a text file from an HTTP(S) file service into memory and opens the
  /// shared full-screen document viewer. Remote files are intentionally
  /// read-only; edits use Save As instead of writing back to the host.
  static Future<void> openRemoteFile({
    required String url,
    required String fileName,
    String mimeType = '',
    Map<String, dynamic>? queryParameters,
    String? handleSeed,
  }) async {
    if (!supportsRemoteFile(fileName: fileName, mimeType: mimeType)) {
      throw const FormatException('text_document_unsupported');
    }
    await _downloadAndOpen(
      url: url,
      displayName: fileName,
      mimeType: mimeType,
      queryParameters: queryParameters,
      source: TextDocumentSource.remoteFileBrowser,
      handleSeed: handleSeed ?? '$url:$fileName',
    );
  }

  static Future<void> _downloadAndOpen({
    required String url,
    required String displayName,
    required String mimeType,
    required TextDocumentSource source,
    required String handleSeed,
    Map<String, dynamic>? queryParameters,
  }) async {
    final uri = ChatMarkdownUriPolicy.resolveSafeLinkUri(url);
    if (uri == null || (uri.scheme != 'http' && uri.scheme != 'https')) {
      throw const FormatException('text_document_unsafe_url');
    }
    final response = await _dio.get<List<int>>(
      uri.toString(),
      queryParameters: queryParameters,
      options: Options(
        responseType: ResponseType.bytes,
        followRedirects: true,
        receiveDataWhenStatusError: false,
      ),
      onReceiveProgress: (received, total) {
        if (received > TextDocumentCodec.maxPreviewBytes ||
            total > TextDocumentCodec.maxPreviewBytes) {
          throw const FormatException('text_document_too_large');
        }
      },
    );
    final raw = response.data;
    if (raw == null) {
      throw const FormatException('text_document_download_empty');
    }
    final bytes = Uint8List.fromList(raw);
    TextDocumentCodec.decode(bytes);
    final descriptor = TextDocumentDescriptor(
      handle: 'remote:${handleSeed.hashCode}',
      displayName: displayName.trim().isEmpty
          ? 'document.txt'
          : displayName.trim(),
      mimeType: mimeType,
      canWrite: false,
      source: source,
      byteLength: bytes.length,
    );
    await openDocument(descriptor: descriptor, bytes: bytes);
  }

  static Future<void> openDocument({
    required TextDocumentDescriptor descriptor,
    required Uint8List bytes,
  }) async {
    await Get.to<void>(
      () => TextDocumentPage(descriptor: descriptor, bytes: bytes),
      transition: Transition.rightToLeft,
    );
  }

  static Future<void> _enqueueNativeDocument(
    TextDocumentDescriptor descriptor,
    Uint8List bytes,
  ) async {
    _pending.add((descriptor: descriptor, bytes: bytes));
    await _drainPending();
  }

  static Future<void> _drainPending() async {
    if (_opening || _pending.isEmpty) return;
    if (Get.key.currentState == null) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        unawaited(_drainPending());
      });
      return;
    }
    _opening = true;
    try {
      while (_pending.isNotEmpty) {
        final next = _pending.removeAt(0);
        await openDocument(descriptor: next.descriptor, bytes: next.bytes);
      }
    } finally {
      _opening = false;
    }
  }
}
