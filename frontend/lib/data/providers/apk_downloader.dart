import 'dart:async';
import 'dart:io';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';

/// 更新失败原因的固定枚举，与服务端 `app_download_reports.error_msg` 共用。
///
/// 早期版本直接把 `e.toString()` 塞进上报，异常文本随 Dio 版本和机型飘，统计端
/// 没法聚合。这里收敛成有限集合，新增原因必须同时更新服务端的白名单说明。
class UpdateErrorCode {
  UpdateErrorCode._();

  static const permissionBlocked = 'permission_blocked';
  static const downloadTimeout = 'download_timeout';
  static const downloadFailed = 'download_failed';
  static const sha256Mismatch = 'sha256_mismatch';
  static const installerNotFound = 'installer_not_found';
  static const lowStorage = 'low_storage';
  static const installNotCompleted = 'install_not_completed';
}

/// 断点续传时对本地半成品文件的处理方式。
enum ResumeMode {
  /// 服务端接受了 Range（206），已下载部分有效，追加写。
  append,

  /// 服务端忽略 Range 返回整包（200），已下载部分作废，从头写。
  restart,
}

/// 一次下载请求的续传计划。
@immutable
class ResumePlan {
  const ResumePlan({required this.startOffset, required this.headers});

  /// 请求的起始字节偏移；0 表示整包下载。
  final int startOffset;

  /// 附加到请求上的头（续传时含 Range）。
  final Map<String, String> headers;
}

/// 抛出以标记「空闲超时」——连续一段时间没有收到任何字节。
///
/// 与 Dio 的 receiveTimeout 不同：那个在 `download()` 里是整段下载的总时长上限，
/// 60MB 的包在弱网上正常也会超过 3 分钟，导致本来能成的下载被掐断。
class DownloadIdleTimeoutException implements Exception {
  const DownloadIdleTimeoutException(this.idleTimeout);

  final Duration idleTimeout;

  @override
  String toString() =>
      'DownloadIdleTimeoutException(no data for ${idleTimeout.inSeconds}s)';
}

/// 支持 Range 断点续传的 APK 下载器。
class ApkDownloader {
  ApkDownloader({
    Dio? dio,
    this.idleTimeout = const Duration(seconds: 45),
    this.connectTimeout = const Duration(seconds: 15),
  }) : _dio =
           dio ?? Dio(BaseOptions(connectTimeout: const Duration(seconds: 15)));

  final Dio _dio;

  /// 连续多久没收到字节判定为超时。按空闲时间算，不按总时长算。
  final Duration idleTimeout;

  final Duration connectTimeout;

  /// 根据本地已下载的字节数构造续传计划。
  ///
  /// `existingBytes <= 0` 时不带 Range，服务端返回 200 整包。
  static ResumePlan planResume({required int existingBytes}) {
    if (existingBytes <= 0) {
      return const ResumePlan(startOffset: 0, headers: <String, String>{});
    }
    return ResumePlan(
      startOffset: existingBytes,
      headers: {'range': 'bytes=$existingBytes-'},
    );
  }

  /// 判断服务端响应该如何落到本地文件上。
  ///
  /// 只有「请求带了 Range」且「服务端确实用 206 应答」时才能追加；服务端忽略
  /// Range 直接返回 200 时必须整包重写，否则会把新包接在旧半包后面损坏文件。
  static ResumeMode resolveResumeMode({
    required int statusCode,
    required int requestedOffset,
  }) {
    if (requestedOffset > 0 && statusCode == HttpStatus.partialContent) {
      return ResumeMode.append;
    }
    return ResumeMode.restart;
  }

  /// 从 `Content-Range: bytes 100-999/1000` 里取总长度，取不到返回 null。
  static int? parseTotalFromContentRange(String? contentRange) {
    if (contentRange == null || contentRange.isEmpty) return null;
    final slash = contentRange.lastIndexOf('/');
    if (slash < 0 || slash + 1 >= contentRange.length) return null;
    return int.tryParse(contentRange.substring(slash + 1).trim());
  }

  /// 计算本次响应对应的总字节数。
  ///
  /// 追加模式下 Content-Length 只是剩余部分，必须加回已下载的偏移；有
  /// Content-Range 时以它给出的总长为准。
  static int resolveTotalBytes({
    required ResumeMode mode,
    required int startOffset,
    required int? contentLength,
    String? contentRange,
  }) {
    final fromRange = parseTotalFromContentRange(contentRange);
    if (fromRange != null && fromRange > 0) return fromRange;
    if (contentLength == null || contentLength <= 0) return 0;
    return mode == ResumeMode.append
        ? startOffset + contentLength
        : contentLength;
  }

  /// 下载 [url] 到 [savePath]，已存在半成品文件时自动续传。
  ///
  /// [onProgress] 的 total 为 0 表示服务端没给出长度。
  Future<void> download({
    required String url,
    required String savePath,
    void Function(int received, int total)? onProgress,
    CancelToken? cancelToken,
  }) async {
    final file = File(savePath);
    final existing = file.existsSync() ? file.lengthSync() : 0;
    final plan = planResume(existingBytes: existing);

    final response = await _dio.get<ResponseBody>(
      url,
      cancelToken: cancelToken,
      options: Options(
        responseType: ResponseType.stream,
        headers: plan.headers,
        // 断点续传只在 200/206 上合法；416（Range 越界，本地文件比远端还大）
        // 也要拿到手动处理，不能让 Dio 直接抛。
        validateStatus: (s) => s != null && s < 400 || s == 416,
        // 收数据不设总时长上限：60MB 的包在弱网上正常也会超过任何固定值，
        // 掐断的判据是 idleTimeout（连续多久一个字节都没来）。
        receiveTimeout: null,
        connectTimeout: connectTimeout,
      ),
    );

    final status = response.statusCode ?? 0;
    if (status == 416) {
      // 本地文件已经不小于远端长度：多半是上次下完但没校验，删掉重来最省心。
      _deleteQuietly(file);
      throw const DownloadRangeNotSatisfiableException();
    }

    final mode = resolveResumeMode(
      statusCode: status,
      requestedOffset: plan.startOffset,
    );
    debugPrint(
      'ApkDownloader: status=$status offset=${plan.startOffset} mode=$mode',
    );

    final total = resolveTotalBytes(
      mode: mode,
      startOffset: plan.startOffset,
      contentLength: _headerInt(response.headers, Headers.contentLengthHeader),
      contentRange: _headerString(response.headers, 'content-range'),
    );

    final sink = file.openWrite(
      mode: mode == ResumeMode.append ? FileMode.append : FileMode.write,
    );
    var received = mode == ResumeMode.append ? plan.startOffset : 0;
    onProgress?.call(received, total);

    try {
      final stream = response.data!.stream.timeout(
        idleTimeout,
        onTimeout: (sink) =>
            sink.addError(DownloadIdleTimeoutException(idleTimeout)),
      );
      await for (final chunk in stream) {
        sink.add(chunk);
        received += chunk.length;
        onProgress?.call(received, total);
      }
      await sink.flush();
    } finally {
      await sink.close();
    }
  }

  static void _deleteQuietly(File file) {
    try {
      if (file.existsSync()) file.deleteSync();
    } catch (_) {}
  }

  static int? _headerInt(Headers headers, String name) {
    final raw = headers.value(name);
    if (raw == null) return null;
    return int.tryParse(raw);
  }

  static String? _headerString(Headers headers, String name) =>
      headers.value(name);
}

/// 服务端以 416 拒绝了 Range 请求：本地半成品比远端文件还长。
class DownloadRangeNotSatisfiableException implements Exception {
  const DownloadRangeNotSatisfiableException();

  @override
  String toString() => 'DownloadRangeNotSatisfiableException';
}
