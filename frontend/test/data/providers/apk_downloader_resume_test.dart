import 'dart:async';
import 'dart:io';
import 'dart:math';

import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/apk_downloader.dart';

/// 一个只懂 Range 的最小静态文件服务端。
///
/// 直接跑在 ServerSocket 上而不是 HttpServer：本用例要在响应体写到一半时把
/// 连接掐掉模拟断网，dart:io 的 HttpResponse 在头已发出后不允许再 detachSocket。
class _RangeServer {
  _RangeServer(this.body);

  final List<int> body;
  ServerSocket? _server;

  /// 每次请求最多吐这么多字节后断开；null 表示完整返回。
  int? cutAfterBytes;

  /// 记录每次请求收到的 Range 头与返回的状态码，供断言。
  final List<String?> receivedRanges = [];
  final List<int> sentStatuses = [];

  String get url =>
      'http://${_server!.address.address}:${_server!.port}/grix.apk';

  Future<void> start() async {
    _server = await ServerSocket.bind(InternetAddress.loopbackIPv4, 0);
    _server!.listen(_handle);
  }

  Future<void> stop() async => _server?.close();

  void _handle(Socket socket) {
    final buffer = <int>[];
    socket.listen(
      (chunk) {
        buffer.addAll(chunk);
        final text = String.fromCharCodes(buffer);
        if (!text.contains('\r\n\r\n')) return;

        String? range;
        for (final line in text.split('\r\n')) {
          if (line.toLowerCase().startsWith('range:')) {
            range = line.substring(6).trim();
          }
        }
        receivedRanges.add(range);

        var start = 0;
        if (range != null && range.startsWith('bytes=')) {
          start = int.parse(range.substring(6).split('-').first);
        }
        final slice = body.sublist(start);

        final headers = StringBuffer();
        if (range != null) {
          sentStatuses.add(206);
          headers.write('HTTP/1.1 206 Partial Content\r\n');
          headers.write(
            'Content-Range: bytes $start-${body.length - 1}/${body.length}\r\n',
          );
        } else {
          sentStatuses.add(200);
          headers.write('HTTP/1.1 200 OK\r\n');
        }
        headers.write('Content-Length: ${slice.length}\r\n');
        headers.write(
          'Content-Type: application/vnd.android.package-archive\r\n',
        );
        headers.write('Connection: close\r\n\r\n');
        socket.add(headers.toString().codeUnits);

        final cut = cutAfterBytes;
        if (cut != null && cut < slice.length) {
          socket.add(slice.sublist(0, cut));
          // 声明了完整长度却只发一部分再断开，客户端就会读到不完整的响应。
          unawaited(socket.flush().then((_) => socket.destroy()));
          return;
        }
        socket.add(slice);
        unawaited(socket.flush().then((_) => socket.close()));
      },
      onError: (_) {},
      cancelOnError: true,
    );
  }
}

void main() {
  // flutter_test 默认给所有 HttpClient 装了 mock（一律 400）；这两个用例要打
  // 本地真实 HttpServer，必须先摘掉这个覆盖。
  setUp(() => HttpOverrides.global = null);

  test('断网后重试带 Range 续传，服务端 206，最终 SHA256 通过', () async {
    final rand = Random(7);
    final body = List<int>.generate(256 * 1024, (_) => rand.nextInt(256));
    final expectedSha = sha256.convert(body).toString();

    final server = _RangeServer(body);
    await server.start();
    addTearDown(server.stop);

    final dir = await Directory.systemTemp.createTemp('apk-resume-e2e');
    addTearDown(() => dir.deleteSync(recursive: true));
    final savePath = '${dir.path}/grix-3.2.7-3000.apk';

    final downloader = ApkDownloader(idleTimeout: const Duration(seconds: 5));

    // 第一次：服务端在 100KB 处断开，本地留下半成品。
    server.cutAfterBytes = 100 * 1024;
    await expectLater(
      downloader.download(url: server.url, savePath: savePath),
      throwsA(anything),
    );
    final partial = File(savePath).lengthSync();
    expect(partial, greaterThan(0));
    expect(partial, lessThan(body.length));
    expect(server.receivedRanges.first, isNull, reason: '首次请求不该带 Range');
    expect(server.sentStatuses.first, 200);

    // 第二次：不删半成品，直接重试。
    server.cutAfterBytes = null;
    await downloader.download(url: server.url, savePath: savePath);

    expect(server.receivedRanges.last, 'bytes=$partial-');
    expect(server.sentStatuses.last, 206);

    final file = File(savePath);
    expect(file.lengthSync(), body.length);
    final actualSha = sha256.convert(file.readAsBytesSync()).toString();
    expect(actualSha, expectedSha);
  });

  test('服务端忽略 Range 返回 200 时整包重写，不会把新包接在半包后面', () async {
    final body = List<int>.generate(64 * 1024, (i) => i % 251);
    final expectedSha = sha256.convert(body).toString();

    // 一个永远无视 Range、总是 200 全量返回的服务端。
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    addTearDown(() => server.close(force: true));
    unawaited(() async {
      await for (final request in server) {
        request.response.statusCode = HttpStatus.ok;
        request.response.headers.contentLength = body.length;
        request.response.add(body);
        await request.response.close();
      }
    }());

    final dir = await Directory.systemTemp.createTemp('apk-no-range');
    addTearDown(() => dir.deleteSync(recursive: true));
    final savePath = '${dir.path}/grix-3.2.7-3000.apk';
    // 预置一段半成品，模拟上次中断。
    File(savePath).writeAsBytesSync(body.sublist(0, 10 * 1024));

    await ApkDownloader().download(
      url: 'http://${server.address.host}:${server.port}/grix.apk',
      savePath: savePath,
    );

    final file = File(savePath);
    expect(file.lengthSync(), body.length, reason: '必须整包重写而不是追加');
    expect(sha256.convert(file.readAsBytesSync()).toString(), expectedSha);
  });
}
