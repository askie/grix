// Web implementation of streaming fetch using the browser Fetch API
// with ReadableStream for true SSE streaming.
//
// Dio on web uses XMLHttpRequest which buffers the entire response.
// This module uses fetch() + response.body.getReader() to deliver
// chunks incrementally as they arrive from the server.

import 'dart:async';
import 'dart:js_interop';
import 'package:web/web.dart' as web;

/// Calls the browser's fetch() API and returns a [Stream<String>] that emits
/// text chunks as they arrive from the server's response body.
Stream<String> fetchStreamingText(String url, String body) {
  final controller = StreamController<String>();

  _doFetch(url, body, controller).then((_) {
    if (!controller.isClosed) controller.close();
  }).catchError((Object e) {
    if (!controller.isClosed) {
      controller.addError(e);
      controller.close();
    }
  });

  return controller.stream;
}

Future<void> _doFetch(
  String url,
  String body,
  StreamController<String> controller,
) async {
  final headers = {
    'Content-Type': 'application/json',
    'Accept': 'text/event-stream',
  }.jsify() as web.HeadersInit;

  final init = web.RequestInit(
    method: 'POST',
    headers: headers,
    body: body.toJS,
  );

  final response = await web.window.fetch(url.toJS, init).toDart;
  if (!response.ok) {
    throw Exception('HTTP ${response.status}: ${response.statusText}');
  }

  final readableBody = response.body;
  if (readableBody == null) {
    throw Exception('response body is null');
  }

  // getReader() returns ReadableStreamReader (typedef = JSObject).
  // Cast to ReadableStreamDefaultReader to access the read() method.
  final reader = readableBody.getReader() as web.ReadableStreamDefaultReader;
  final decoder = web.TextDecoder();

  while (true) {
    final result = await reader.read().toDart;
    if (result.done) break;

    final value = result.value;
    if (value == null) continue;

    // value is JSAny? — it's a Uint8Array from the ReadableStream.
    final chunk = decoder.decode(
      value as JSUint8Array,
      web.TextDecodeOptions(stream: true),
    );
    if (chunk.isNotEmpty && !controller.isClosed) {
      controller.add(chunk);
    }
  }
}
