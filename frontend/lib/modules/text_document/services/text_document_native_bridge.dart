import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import '../models/text_document_descriptor.dart';

typedef NativeDocumentHandler =
    Future<void> Function(TextDocumentDescriptor descriptor, Uint8List bytes);

class TextDocumentNativeBridge {
  TextDocumentNativeBridge._();

  static const MethodChannel _channel = MethodChannel(
    'pub.dhf.grix/text_document',
  );
  static NativeDocumentHandler? _handler;
  static bool _initialized = false;

  static Future<void> initialize(NativeDocumentHandler handler) async {
    _handler = handler;
    if (kIsWeb || _initialized) return;
    _initialized = true;
    _channel.setMethodCallHandler((call) async {
      if (call.method == 'documentOpened') await _dispatch(call.arguments);
    });
    try {
      await _dispatch(
        await _channel.invokeMethod<Object?>('getInitialDocument'),
      );
    } on MissingPluginException {
      // Desktop and tests intentionally have no host implementation.
    } on PlatformException {
      // An inaccessible initial document must not block startup.
    }
  }

  static Future<void> _dispatch(Object? value) async {
    if (value is! Map) return;
    final rawDescriptor = value['descriptor'];
    final rawBytes = value['bytes'];
    if (rawDescriptor is! Map || rawBytes is! Uint8List) return;
    final descriptor = TextDocumentDescriptor.fromNativeMap(rawDescriptor);
    if (descriptor.handle.isEmpty) return;
    await _handler?.call(descriptor, rawBytes);
  }

  static Future<void> writeOriginal({
    required String handle,
    required Uint8List bytes,
  }) async {
    await _channel.invokeMethod<void>('writeDocument', {
      'handle': handle,
      'bytes': bytes,
    });
  }

  static Future<void> close(String handle) async {
    if (handle.isEmpty || kIsWeb) return;
    try {
      await _channel.invokeMethod<void>('closeDocument', {'handle': handle});
    } on MissingPluginException {
      // No native handle exists on this platform.
    }
  }
}
