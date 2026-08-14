import 'package:dio/dio.dart';

import 'video_downloader_types.dart';

Future<VideoDownloadResult> downloadVideo(
  Uri videoUri, {
  required String fileName,
  CancelToken? cancelToken,
  String? localSourcePath,
}) async {
  throw UnsupportedError('Video download is not supported on this platform.');
}
