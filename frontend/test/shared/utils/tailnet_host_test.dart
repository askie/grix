import 'package:grix/shared/utils/tailnet_host.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('isTailnetHostString', () {
    test('放行 CGNAT 段下边界 100.64.x.x', () {
      expect(isTailnetHostString('100.64.0.0'), isTrue);
      expect(isTailnetHostString('100.64.0.4'), isTrue);
      expect(isTailnetHostString('100.64.255.255'), isTrue);
    });

    test('放行 CGNAT 段上边界 100.127.x.x', () {
      expect(isTailnetHostString('100.127.0.1'), isTrue);
      expect(isTailnetHostString('100.127.255.255'), isTrue);
    });

    test('拒绝 CGNAT 段相邻的非 tailnet 地址', () {
      expect(isTailnetHostString('100.63.255.255'), isFalse);
      expect(isTailnetHostString('100.128.0.0'), isFalse);
      expect(isTailnetHostString('99.64.0.0'), isFalse);
      expect(isTailnetHostString('101.64.0.0'), isFalse);
    });

    test('拒绝常见公网地址', () {
      expect(isTailnetHostString('1.2.3.4'), isFalse);
      expect(isTailnetHostString('8.8.8.8'), isFalse);
      expect(isTailnetHostString('192.168.1.1'), isFalse);
      expect(isTailnetHostString('10.0.0.1'), isFalse);
    });

    test('拒绝 loopback', () {
      expect(isTailnetHostString('127.0.0.1'), isFalse);
    });

    test('拒绝域名 / 不合法输入', () {
      expect(isTailnetHostString('grix.dhf.pub'), isFalse);
      expect(isTailnetHostString('100.64.0'), isFalse);
      expect(isTailnetHostString('100.64.0.x'), isFalse);
      expect(isTailnetHostString(''), isFalse);
      expect(isTailnetHostString('::1'), isFalse);
      // IPv6 ts.net 暂不支持，明确返回 false（避免误判）
      expect(isTailnetHostString('fd7a:115c::1'), isFalse);
    });
  });
}
