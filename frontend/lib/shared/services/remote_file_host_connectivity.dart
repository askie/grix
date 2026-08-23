import 'package:dio/dio.dart';

import '../markdown/chat_markdown_uri_policy.dart';

typedef RemoteFileHostProbe = Future<bool> Function(String baseUrl);

class RemoteFileHostConnectivity {
  const RemoteFileHostConnectivity._();

  static Future<bool> isReachable(String rawBaseUrl) async {
    final baseUrl = rawBaseUrl.trim().replaceFirst(RegExp(r'/+$'), '');
    final baseUri = ChatMarkdownUriPolicy.resolveSafeLinkUri(baseUrl);
    if (baseUri == null ||
        (baseUri.scheme != 'http' && baseUri.scheme != 'https')) {
      return false;
    }

    final pingUri = Uri.tryParse('$baseUrl/ping');
    if (pingUri == null) return false;

    final dio = Dio(
      BaseOptions(
        connectTimeout: const Duration(seconds: 3),
        receiveTimeout: const Duration(seconds: 3),
      ),
    );
    try {
      await dio.getUri<void>(pingUri);
      return true;
    } catch (_) {
      return false;
    } finally {
      dio.close(force: true);
    }
  }
}
