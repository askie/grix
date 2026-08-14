import 'dart:typed_data';

import 'package:dio/dio.dart';

class RemoteBinaryLoader {
  RemoteBinaryLoader._();

  static final Dio _dio = Dio(
    BaseOptions(
      responseType: ResponseType.bytes,
      receiveTimeout: const Duration(seconds: 20),
      sendTimeout: const Duration(seconds: 20),
    ),
  );

  static Future<Uint8List> load(Uri uri) async {
    final response = await _dio.getUri<List<int>>(uri);
    final statusCode = response.statusCode ?? 0;
    final bytes = response.data;

    if (statusCode < 200 ||
        statusCode >= 300 ||
        bytes == null ||
        bytes.isEmpty) {
      throw StateError('Failed to load remote bytes: $uri');
    }

    return Uint8List.fromList(bytes);
  }
}
