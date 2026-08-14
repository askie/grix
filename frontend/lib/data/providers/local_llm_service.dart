import 'dart:async';
import 'dart:convert';
import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';

import 'web_fetch_stream_stub.dart'
    if (dart.library.js_interop) 'web_fetch_stream_web.dart' as web_fetch;

class _StreamingStringSink extends StringConversionSinkBase {
  _StreamingStringSink(this.onChunk);

  final void Function(String chunk) onChunk;

  @override
  void add(String str) {
    if (str.isNotEmpty) {
      onChunk(str);
    }
  }

  @override
  void addSlice(String str, int start, int end, bool isLast) {
    if (start >= end) {
      return;
    }
    onChunk(str.substring(start, end));
  }

  @override
  void close() {}
}

@visibleForTesting
class StreamingUtf8LineDecoder {
  StreamingUtf8LineDecoder() {
    _byteSink = const Utf8Decoder().startChunkedConversion(
      _StreamingStringSink(_onDecodedChunk),
    );
  }

  late final ByteConversionSink _byteSink;
  String _pendingText = '';
  final List<String> _readyLines = <String>[];

  List<String> add(List<int> bytes) {
    _readyLines.clear();
    if (bytes.isEmpty) {
      return const <String>[];
    }
    _byteSink.add(bytes);
    return List<String>.from(_readyLines);
  }

  List<String> close() {
    _readyLines.clear();
    _byteSink.close();
    if (_pendingText.isNotEmpty) {
      _readyLines.add(_pendingText);
      _pendingText = '';
    }
    return List<String>.from(_readyLines);
  }

  void _onDecodedChunk(String chunk) {
    if (chunk.isEmpty) {
      return;
    }
    _pendingText += chunk;
    final lines = _pendingText.split('\n');
    _pendingText = lines.removeLast();
    _readyLines.addAll(lines);
  }
}

/// Lightweight OpenAI-compatible client for local LLM inference (Ollama, LM Studio, etc.).
class LocalLlmService {
  static final LocalLlmService _instance = LocalLlmService._();
  factory LocalLlmService() => _instance;
  LocalLlmService._();

  final _dio = Dio(BaseOptions(
    connectTimeout: const Duration(seconds: 10),
    // No receive timeout — streaming can be long-lived.
  ));

  /// Cancellation tokens keyed by sessionId to allow aborting in-flight inference.
  final _cancelTokens = <String, CancelToken>{};

  /// Whether an active cancel has been requested for a session (used by web path).
  final _webCancelled = <String>{};

  /// Cancel any in-flight local inference for [sessionId].
  void cancel(String sessionId) {
    _cancelTokens.remove(sessionId)?.cancel('user_cancelled');
    _webCancelled.add(sessionId);
  }

  /// Streams chat completions from a local OpenAI-compatible endpoint.
  ///
  /// [endpoint] — base URL, e.g. `http://localhost:11434`
  /// [model] — model name, e.g. `gemma3:4b`
  /// [messages] — OpenAI chat messages format
  /// [onChunk] — called for each delta content chunk
  /// [onFinish] — called with the full accumulated content when done
  /// [onError] — called on error
  Future<void> streamChat({
    required String sessionId,
    required String endpoint,
    required String model,
    required List<Map<String, String>> messages,
    required void Function(String chunk) onChunk,
    required void Function(String fullContent) onFinish,
    void Function(String error)? onError,
  }) async {
    // Cancel any previous inference for this session.
    cancel(sessionId);
    _webCancelled.remove(sessionId);

    if (kIsWeb) {
      await _streamChatWeb(
        sessionId: sessionId,
        endpoint: endpoint,
        model: model,
        messages: messages,
        onChunk: onChunk,
        onFinish: onFinish,
        onError: onError,
      );
    } else {
      await _streamChatNative(
        sessionId: sessionId,
        endpoint: endpoint,
        model: model,
        messages: messages,
        onChunk: onChunk,
        onFinish: onFinish,
        onError: onError,
      );
    }
  }

  // ---------------------------------------------------------------------------
  // Native (iOS / Android / macOS / Linux / Windows) — uses Dio streaming.
  // ---------------------------------------------------------------------------
  Future<void> _streamChatNative({
    required String sessionId,
    required String endpoint,
    required String model,
    required List<Map<String, String>> messages,
    required void Function(String chunk) onChunk,
    required void Function(String fullContent) onFinish,
    void Function(String error)? onError,
  }) async {
    final cancelToken = CancelToken();
    _cancelTokens[sessionId] = cancelToken;

    final url = _normalizeEndpoint(endpoint);
    final buffer = StringBuffer();

    try {
      final response = await _dio.post<ResponseBody>(
        '$url/v1/chat/completions',
        data: {
          'model': model,
          'messages': messages,
          'stream': true,
        },
        options: Options(
          responseType: ResponseType.stream,
          headers: {'Accept': 'text/event-stream'},
        ),
        cancelToken: cancelToken,
      );

      final stream = response.data?.stream;
      if (stream == null) {
        onError?.call('empty response stream');
        return;
      }

      final lineDecoder = StreamingUtf8LineDecoder();
      await for (final bytes in stream) {
        if (cancelToken.isCancelled) break;

        for (final line in lineDecoder.add(bytes)) {
          final chunk = _parseSseLine(line);
          if (chunk != null) {
            buffer.write(chunk);
            onChunk(chunk);
          }
        }

        // Yield to event loop so UI flush timers can fire.
        await Future.delayed(Duration.zero);
      }

      for (final line in lineDecoder.close()) {
        final chunk = _parseSseLine(line);
        if (chunk != null) {
          buffer.write(chunk);
          onChunk(chunk);
        }
      }

      onFinish(buffer.toString());
    } on DioException catch (e) {
      if (CancelToken.isCancel(e)) {
        debugPrint(
            'LocalLlmService: inference cancelled for session=$sessionId');
        if (buffer.isNotEmpty) {
          onFinish(buffer.toString());
        }
        return;
      }
      final msg = _extractDioError(e);
      debugPrint('LocalLlmService: error for session=$sessionId: $msg');
      onError?.call(msg);
    } catch (e) {
      debugPrint(
          'LocalLlmService: unexpected error for session=$sessionId: $e');
      onError?.call(e.toString());
    } finally {
      _cancelTokens.remove(sessionId);
    }
  }

  // ---------------------------------------------------------------------------
  // Web — uses browser Fetch API + ReadableStream for true SSE streaming.
  //
  // Dio on web uses XMLHttpRequest under the hood, which buffers the entire
  // HTTP response before delivering it. This prevents the typewriter effect.
  // The browser's fetch() API with response.body.getReader() supports true
  // incremental streaming.
  // ---------------------------------------------------------------------------
  Future<void> _streamChatWeb({
    required String sessionId,
    required String endpoint,
    required String model,
    required List<Map<String, String>> messages,
    required void Function(String chunk) onChunk,
    required void Function(String fullContent) onFinish,
    void Function(String error)? onError,
  }) async {
    final url = _normalizeEndpoint(endpoint);
    final buffer = StringBuffer();

    try {
      final fetchUrl = '$url/v1/chat/completions';
      final body = jsonEncode({
        'model': model,
        'messages': messages,
        'stream': true,
      });

      final textStream = web_fetch.fetchStreamingText(fetchUrl, body);

      String leftover = '';
      await for (final text in textStream) {
        if (_webCancelled.contains(sessionId)) break;

        final combined = leftover + text;
        final lines = combined.split('\n');
        leftover = lines.removeLast();

        for (final line in lines) {
          final chunk = _parseSseLine(line);
          if (chunk != null) {
            buffer.write(chunk);
            onChunk(chunk);
          }
        }
      }

      onFinish(buffer.toString());
    } catch (e) {
      if (_webCancelled.contains(sessionId)) {
        debugPrint(
            'LocalLlmService: web inference cancelled session=$sessionId');
        if (buffer.isNotEmpty) {
          onFinish(buffer.toString());
        }
        return;
      }
      debugPrint('LocalLlmService: web error session=$sessionId: $e');
      onError?.call(e.toString());
    } finally {
      _webCancelled.remove(sessionId);
    }
  }

  // ---------------------------------------------------------------------------
  // Shared helpers
  // ---------------------------------------------------------------------------

  /// Parse a single SSE `data: ...` line and extract the delta content, or null.
  String? _parseSseLine(String line) {
    final trimmed = line.trim();
    if (trimmed.isEmpty || !trimmed.startsWith('data: ')) return null;
    final data = trimmed.substring(6);
    if (data == '[DONE]') return null;
    try {
      final json = jsonDecode(data) as Map<String, dynamic>;
      final choices = json['choices'] as List?;
      if (choices == null || choices.isEmpty) return null;
      final delta = (choices[0] as Map<String, dynamic>)['delta']
          as Map<String, dynamic>?;
      final content = delta?['content']?.toString();
      return (content != null && content.isNotEmpty) ? content : null;
    } catch (_) {
      return null;
    }
  }

  String _normalizeEndpoint(String endpoint) {
    var url = endpoint.trim();
    if (url.endsWith('/')) url = url.substring(0, url.length - 1);
    return url;
  }

  String _extractDioError(DioException e) {
    if (e.response != null) {
      final data = e.response?.data;
      if (data is Map && data['error'] != null) {
        final error = data['error'];
        if (error is Map) {
          return error['message']?.toString() ?? error.toString();
        }
        return error.toString();
      }
      return 'HTTP ${e.response!.statusCode}: ${e.response!.statusMessage}';
    }
    if (e.type == DioExceptionType.connectionTimeout ||
        e.type == DioExceptionType.receiveTimeout) {
      return 'connection timeout';
    }
    if (e.type == DioExceptionType.connectionError) {
      return 'connection error: ${e.message}';
    }
    return e.message ?? e.toString();
  }
}
