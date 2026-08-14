import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/utils/app_region_config.dart';

void main() {
  group('resolveRegionApiBaseUrl', () {
    test('App 端按区域返回不同端点', () {
      final cn = resolveRegionApiBaseUrl(AppRegion.cn, isWeb: false);
      final global = resolveRegionApiBaseUrl(AppRegion.global, isWeb: false);

      expect(cn, apiUrlForRegion(AppRegion.cn));
      expect(global, apiUrlForRegion(AppRegion.global));
      expect(cn, isNot(equals(global)));
    });

    test('Web 端忽略区域，两个区域解析为同一个（同源）端点', () {
      final cn = resolveRegionApiBaseUrl(AppRegion.cn, isWeb: true);
      final global = resolveRegionApiBaseUrl(AppRegion.global, isWeb: true);

      expect(cn, equals(global));
    });
  });

  group('resolveRegionWsUrl', () {
    test('App 端按区域返回不同 WS 端点', () {
      final cn = resolveRegionWsUrl(AppRegion.cn, isWeb: false);
      final global = resolveRegionWsUrl(AppRegion.global, isWeb: false);

      expect(cn, wsUrlForRegion(AppRegion.cn));
      expect(global, wsUrlForRegion(AppRegion.global));
      expect(cn, isNot(equals(global)));
    });

    test('Web 端按页面域名解析，与所选区域无关', () {
      final cn = resolveRegionWsUrl(AppRegion.cn, isWeb: true);
      final global = resolveRegionWsUrl(AppRegion.global, isWeb: true);

      expect(cn, equals(global));
    });

    test('Web 端全球区域名(gb.grix.im) WS 指向 ws.grix.im 跨域端点', () {
      final ws = resolveRegionWsUrl(
        AppRegion.cn, // 区域参数不影响 web 端结果
        isWeb: true,
        baseUri: Uri.parse('https://gb.grix.im/'),
      );

      expect(ws, wsUrlForRegion(AppRegion.global));
      expect(Uri.parse(ws).host, 'ws.grix.im');
    });

    test('Web 端 CN 域名(grix.dhf.pub) WS 同域名(同源一致)', () {
      final ws = resolveRegionWsUrl(
        AppRegion.global, // 区域参数不影响 web 端结果
        isWeb: true,
        baseUri: Uri.parse('https://grix.dhf.pub/'),
      );

      expect(ws, wsUrlForRegion(AppRegion.cn));
      expect(Uri.parse(ws).host, 'grix.dhf.pub');
    });

    test('Web 端未知域名(本地开发)不误匹配区域固定 WS 域名', () {
      final ws = resolveRegionWsUrl(
        AppRegion.global,
        isWeb: true,
        baseUri: Uri.parse('http://localhost:34123/'),
      );

      // 未知域名走同源回退，不应被错配到 CN/全球区固定 WS 域名
      expect(Uri.parse(ws).host, isNot('ws.grix.im'));
      expect(Uri.parse(ws).host, isNot('grix.dhf.pub'));
    });
  });

  group('resolveDefaultWsUrl', () {
    test('Web 端全球区域名(gb.grix.im)默认 WS 指向 ws.grix.im', () {
      final ws = resolveDefaultWsUrl(
        isWeb: true,
        baseUri: Uri.parse('https://gb.grix.im/'),
      );

      expect(Uri.parse(ws).host, 'ws.grix.im');
    });

    test('Web 端 CN 域名(grix.dhf.pub)默认 WS 同域名', () {
      final ws = resolveDefaultWsUrl(
        isWeb: true,
        baseUri: Uri.parse('https://grix.dhf.pub/'),
      );

      expect(Uri.parse(ws).host, 'grix.dhf.pub');
    });
  });

  // 区域切换是命脉功能：源码默认地址一旦被改坏，整个区域不可用。
  // 下面这组守卫锁死两区域必须指向预期生产域名、协议与路径正确，
  // 任何人改错 app_region_config.dart 的默认值，这里立即变红。
  group('区域端点守卫（编译期默认值，防源码被改坏）', () {
    test('中国区接口指向 grix.dhf.pub，https + 以 /v1 结尾', () {
      final cn = apiUrlForRegion(AppRegion.cn);
      expect(cn, startsWith('https://'));
      expect(cn, endsWith('/v1'));
      expect(Uri.parse(cn).host, 'grix.dhf.pub');
    });

    test('全球区接口指向 AWS 全球区 gb.grix.im，https + 以 /v1 结尾', () {
      final global = apiUrlForRegion(AppRegion.global);
      expect(global, startsWith('https://'));
      expect(global, endsWith('/v1'));
      expect(Uri.parse(global).host, 'gb.grix.im');
    });

    test('全球区 WS 指向 ws.grix.im，wss 协议', () {
      final ws = wsUrlForRegion(AppRegion.global);
      expect(ws, startsWith('wss://'));
      expect(Uri.parse(ws).host, 'ws.grix.im');
    });

    test('中国区 WS 指向 grix.dhf.pub，wss 协议', () {
      final ws = wsUrlForRegion(AppRegion.cn);
      expect(ws, startsWith('wss://'));
      expect(Uri.parse(ws).host, 'grix.dhf.pub');
    });

    test('两区域接口/WS 地址必须互不相同', () {
      expect(
        apiUrlForRegion(AppRegion.cn),
        isNot(equals(apiUrlForRegion(AppRegion.global))),
      );
      expect(
        wsUrlForRegion(AppRegion.cn),
        isNot(equals(wsUrlForRegion(AppRegion.global))),
      );
    });

    test('apiUrlForRegion 不重复追加 /v1', () {
      for (final region in AppRegion.values) {
        final url = apiUrlForRegion(region);
        expect('/v1'.allMatches(url).length, 1, reason: '$region: $url');
      }
    });

    test('绝不指向已废弃、无后端的全球区裸域名 grix.im', () {
      for (final region in AppRegion.values) {
        expect(Uri.parse(apiUrlForRegion(region)).host, isNot('grix.im'));
        expect(Uri.parse(wsUrlForRegion(region)).host, isNot('grix.im'));
      }
    });
  });

  group('detectRegionFromWebBaseUri', () {
    test('CN 生产域名(grix.dhf.pub)识别为 cn', () {
      final region = detectRegionFromWebBaseUri(
        Uri.parse('https://grix.dhf.pub/'),
      );
      expect(region, AppRegion.cn);
    });

    test('全球区生产域名(gb.grix.im)识别为 global', () {
      final region = detectRegionFromWebBaseUri(
        Uri.parse('https://gb.grix.im/'),
      );
      expect(region, AppRegion.global);
    });

    test('未知域名(本地开发)回退到按系统语言推断，不崩溃', () {
      // 不抛异常，且返回的值是合法 AppRegion
      final region = detectRegionFromWebBaseUri(
        Uri.parse('http://localhost:3000/'),
      );
      expect(AppRegion.values, contains(region));
    });

    test('页面在 CN 域名子路径时仍识别为 cn', () {
      final region = detectRegionFromWebBaseUri(
        Uri.parse('https://grix.dhf.pub/login?redirect=/home'),
      );
      expect(region, AppRegion.cn);
    });

    test('页面在全球区域名子路径时仍识别为 global', () {
      final region = detectRegionFromWebBaseUri(
        Uri.parse('https://gb.grix.im/login'),
      );
      expect(region, AppRegion.global);
    });
  });

  // 回归：网页切换分区曾把带 /v1 的接口地址当页面地址跳转，跳到
  // https://grix.dhf.pub/v1 导致 404 白屏。域名根必须去掉 /v1。
  group('regionWebRootUrl（分区切换整页跳转目标）', () {
    test('CN 区跳转目标是 grix.dhf.pub 域名根，绝不带 /v1 接口路径', () {
      final url = regionWebRootUrl(AppRegion.cn);
      expect(Uri.parse(url).host, 'grix.dhf.pub');
      expect(url, startsWith('https://'));
      expect(url, isNot(contains('/v1')));
      expect(Uri.parse(url).path, '/');
    });

    test('全球区跳转目标是 gb.grix.im 域名根，绝不带 /v1 接口路径', () {
      final url = regionWebRootUrl(AppRegion.global);
      expect(Uri.parse(url).host, 'gb.grix.im');
      expect(url, startsWith('https://'));
      expect(url, isNot(contains('/v1')));
      expect(Uri.parse(url).path, '/');
    });

    test('两区域跳转目标互不相同', () {
      expect(
        regionWebRootUrl(AppRegion.cn),
        isNot(equals(regionWebRootUrl(AppRegion.global))),
      );
    });

    // 直击 bug 现场：生产构建注入的是带 /v1 的接口地址，必须被剥成域名根。
    test('带 /v1 的接口地址被剥成域名根（复现并锁死 404 白屏）', () {
      expect(webRootOfUrl('https://grix.dhf.pub/v1'), 'https://grix.dhf.pub/');
      expect(webRootOfUrl('https://gb.grix.im/v1'), 'https://gb.grix.im/');
    });

    test('带端口的地址保留端口', () {
      expect(
        webRootOfUrl('http://127.0.0.1:27180/v1'),
        'http://127.0.0.1:27180/',
      );
    });

    test('已是域名根的地址保持不变', () {
      expect(webRootOfUrl('https://grix.dhf.pub'), 'https://grix.dhf.pub/');
      expect(webRootOfUrl('https://grix.dhf.pub/'), 'https://grix.dhf.pub/');
    });
  });

  group('regionOfApiBaseUrl', () {
    test('识别 CN 区 API 地址', () {
      expect(regionOfApiBaseUrl('https://grix.dhf.pub/v1'), AppRegion.cn);
    });

    test('识别全球区 API 地址', () {
      expect(regionOfApiBaseUrl('https://gb.grix.im/v1'), AppRegion.global);
    });

    test('未知域名（本地开发）返回 null', () {
      expect(regionOfApiBaseUrl('http://localhost:27180/v1'), isNull);
      expect(regionOfApiBaseUrl(''), isNull);
    });
  });
}
