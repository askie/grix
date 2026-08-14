import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
// rewriteTailnetMediaUrl 从条件导入入口取（同时被 _io / _stub 导出，避免歧义）。
import 'package:grix/shared/utils/local_tailnet_proxy.dart';
// 测试钩子只在 _io 实现里，单独 hide 重叠名避免 ambiguous_import。
import 'package:grix/shared/utils/local_tailnet_proxy_io.dart'
    hide rewriteTailnetMediaUrl;

void main() {
  group('end-to-end 反代行为（fake 上游 + 真实 _handle）', () {
    late HttpServer upstream;
    late Uri upstreamBase;
    // 用一个固定 body 模拟视频内容；包含足量字节方便切 Range。
    final body = List<int>.generate(2048, (i) => i & 0xff);

    HttpOverrides? prevOverrides;

    setUpAll(() async {
      // flutter_test 默认装 MockHttpOverrides 拒绝所有真实网络 → 反代发不出请求。
      // 这里临时卸掉，让 loopback HttpClient 通行。
      prevOverrides = HttpOverrides.current;
      HttpOverrides.global = null;
      upstream =
          await HttpServer.bind(InternetAddress.loopbackIPv4, 0, shared: false);
      upstreamBase = Uri(
        scheme: 'http',
        host: upstream.address.address,
        port: upstream.port,
      );
      upstream.listen((req) async {
        final res = req.response;
        res.headers.set('content-type', 'video/mp4');
        res.headers.set('x-echo-ua', req.headers.value('user-agent') ?? '');
        final rangeHeader = req.headers.value('range');
        if (rangeHeader != null && rangeHeader.startsWith('bytes=')) {
          final spec = rangeHeader.substring(6);
          final parts = spec.split('-');
          final start = int.parse(parts[0]);
          final end = parts.length > 1 && parts[1].isNotEmpty
              ? int.parse(parts[1])
              : body.length - 1;
          res.statusCode = HttpStatus.partialContent;
          res.headers.set(
            'content-range',
            'bytes $start-$end/${body.length}',
          );
          res.add(body.sublist(start, end + 1));
        } else {
          res.statusCode = HttpStatus.ok;
          res.headers.set('content-length', '${body.length}');
          res.add(body);
        }
        await res.close();
      });
      // 测试钩子：允许 loopback http 走反代，模拟 tailnet 段。
      debugSetProxyEligibilityOverride((u) => u.host == '127.0.0.1');
    });

    tearDownAll(() async {
      debugSetProxyEligibilityOverride(null);
      await upstream.close(force: true);
      HttpOverrides.global = prevOverrides;
    });

    test('GET 200 透传 body / content-type / 透传请求头', () async {
      final proxyUri = await rewriteTailnetMediaUrl(upstreamBase.resolve('/v'));
      final client = HttpClient();
      try {
        final req = await client.getUrl(proxyUri);
        req.headers.set('user-agent', 'grix-e2e-test/1.0');
        final res = await req.close();
        expect(res.statusCode, 200);
        expect(res.headers.value('content-type'), 'video/mp4');
        // 上游 echo 回的 UA 头应该跟我们发出去的一致，证明请求头穿透了反代。
        expect(res.headers.value('x-echo-ua'), 'grix-e2e-test/1.0');
        final bytes = await res
            .fold<List<int>>(<int>[], (acc, chunk) => acc..addAll(chunk));
        expect(bytes, body);
      } finally {
        client.close(force: true);
      }
    });

    test('Range 请求 → 206 + Content-Range 透传，body 是切片', () async {
      final proxyUri = await rewriteTailnetMediaUrl(upstreamBase.resolve('/v'));
      final client = HttpClient();
      try {
        final req = await client.getUrl(proxyUri);
        req.headers.set('range', 'bytes=100-199');
        final res = await req.close();
        expect(res.statusCode, 206);
        expect(res.headers.value('content-range'), 'bytes 100-199/2048');
        final bytes = await res
            .fold<List<int>>(<int>[], (acc, chunk) => acc..addAll(chunk));
        expect(bytes.length, 100);
        expect(bytes, body.sublist(100, 200));
      } finally {
        client.close(force: true);
      }
    });

    test('伪造一个 token 指向非 tailnet → 403 拒绝', () async {
      // 让反代 server 起来，拿到端口。
      await rewriteTailnetMediaUrl(upstreamBase.resolve('/v'));
      // 临时关掉钩子，让 _shouldProxy 走默认规则 → 拒绝 loopback。
      debugSetProxyEligibilityOverride(null);
      try {
        // 把 token 指向一个 8.8.8.8 公网 host，验证 _handle 不会去转发。
        final evil = Uri.parse('https://8.8.8.8/secret');
        final token = base64Url.encode(utf8.encode(evil.toString()));
        final anyProxy = await rewriteTailnetMediaUrl(
          Uri.parse('https://100.64.0.1/x'),
        );
        // anyProxy 不是 loopback override 下产出的，因为钩子已重置；
        // 但反代 server 端口已经分配过；重新发一个走默认规则的 URL 即可。
        // 我们直接构造 proxy URL 调用 server。
        // 由于钩子刚关掉，rewriteTailnetMediaUrl(https://100.64.0.1) 会走默认 → 是 tailnet → 走反代。
        final proxyBase = anyProxy.replace(
          path: '/u',
          queryParameters: {'t': token},
        );
        final client = HttpClient();
        try {
          final req = await client.getUrl(proxyBase);
          final res = await req.close();
          expect(res.statusCode, 403);
          await res.drain<void>();
        } finally {
          client.close(force: true);
        }
      } finally {
        // 恢复钩子给后续测试用。
        debugSetProxyEligibilityOverride((u) => u.host == '127.0.0.1');
      }
    });

    test('未知路径 → 404', () async {
      await rewriteTailnetMediaUrl(upstreamBase.resolve('/v'));
      // 通过 ensure 一次拿到 proxy host:port，再把 path 换成 /unknown。
      final any = await rewriteTailnetMediaUrl(upstreamBase.resolve('/v'));
      final wrong = any.replace(path: '/unknown', queryParameters: const {});
      final client = HttpClient();
      try {
        final req = await client.getUrl(wrong);
        final res = await req.close();
        expect(res.statusCode, 404);
        await res.drain<void>();
      } finally {
        client.close(force: true);
      }
    });
  });


  group('rewriteTailnetMediaUrl', () {
    test('tailnet HTTPS 改写成 loopback http /u?t=<base64>', () async {
      final original = Uri.parse('https://100.64.0.4:55852/d/abc?x=1');
      final rewritten = await rewriteTailnetMediaUrl(original);
      expect(rewritten.scheme, 'http');
      expect(rewritten.host, '127.0.0.1');
      expect(rewritten.port, greaterThan(0));
      expect(rewritten.path, '/u');
      final token = rewritten.queryParameters['t'];
      expect(token, isNotNull);
      final decoded = utf8.decode(base64Url.decode(token!));
      expect(decoded, original.toString());
    });

    test('外部 HTTPS 原样返回，不会启动反代', () async {
      final external = Uri.parse('https://cdn.example.com/v.mp4');
      expect(await rewriteTailnetMediaUrl(external), external);
    });

    test('反复改写复用同一个 loopback 端口（单例 server）', () async {
      final a = await rewriteTailnetMediaUrl(
        Uri.parse('https://100.64.0.4/a'),
      );
      final b = await rewriteTailnetMediaUrl(
        Uri.parse('https://100.64.0.5/b'),
      );
      expect(a.port, b.port);
    });
  });

  group('debugShouldProxy', () {
    test('tailnet HTTPS 需要走反代', () {
      expect(
        debugShouldProxy(Uri.parse('https://100.64.0.4:55852/d/abc')),
        isTrue,
      );
      expect(
        debugShouldProxy(Uri.parse('https://100.127.1.2/file/x')),
        isTrue,
      );
    });

    test('tailnet 但是 HTTP 不走反代（无证书问题）', () {
      expect(
        debugShouldProxy(Uri.parse('http://100.64.0.4:8080/x')),
        isFalse,
      );
    });

    test('外部 HTTPS / HTTP 不走反代', () {
      expect(
        debugShouldProxy(Uri.parse('https://grix.dhf.pub/d/abc')),
        isFalse,
      );
      expect(
        debugShouldProxy(Uri.parse('https://cdn.example.com/v.mp4')),
        isFalse,
      );
      expect(
        debugShouldProxy(Uri.parse('http://192.168.1.1/x')),
        isFalse,
      );
    });

    test('rtsp / file / data 等非 http(s) scheme 不走反代', () {
      expect(
        debugShouldProxy(Uri.parse('rtsp://100.64.0.4/stream')),
        isFalse,
      );
      expect(
        debugShouldProxy(Uri.parse('file:///tmp/v.mp4')),
        isFalse,
      );
    });

    test('空 host / 不合法 URL 不走反代', () {
      expect(debugShouldProxy(Uri.parse('https:///x')), isFalse);
    });
  });
}
