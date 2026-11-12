import 'package:flutter_test/flutter_test.dart';
import 'package:grix_widget/grix_widget.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('GrixUrlBuilder', () {
    test('puts all bootstrap parameters in the fragment', () {
      const config = GrixConfig(
        siteKey: 'wk_live_abc',
        apiBase: 'https://gb.grix.im/',
        locale: 'zh-CN',
        themeColor: '#0f766e',
        title: 'Grix \u5ba2\u670d',
        user: GrixUser(
          name: '\u5f20\u4e09',
          email: 'zhangsan@example.com',
        ),
      );
      final url = GrixUrlBuilder.buildAppUrl(
        config: config,
        hostPackageName: 'com.example.shop',
        visitorKey: 'vk_999',
      );
      final beforeFragment = url.split('#').first;
      expect(beforeFragment.contains('?'), false);
      expect(url, startsWith('https://gb.grix.im/public/widget/app.html#'));
      final fragment = url.substring(url.indexOf('#') + 1);
      expect(fragment, contains('site_key=wk_live_abc'));
      expect(fragment, contains('platform=flutter'));
      expect(fragment, contains('app_id=com.example.shop'));
      expect(fragment, contains('sdk_version=1.0.0'));
      expect(fragment, contains('visitor_key=vk_999'));
      expect(fragment, contains('locale=zh-CN'));
      expect(fragment, contains('theme_color=%230f766e'));
      expect(fragment, contains('visitor_email=zhangsan%40example.com'));
    });

    test('respects an explicit app id override', () {
      const config = GrixConfig(
        siteKey: 'wk_1',
        appIdOverride: 'com.example.override',
      );
      final url = GrixUrlBuilder.buildAppUrl(
        config: config,
        hostPackageName: 'com.example.host',
      );
      expect(url, contains('app_id=com.example.override'));
    });
  });

  group('GrixBridgeEvent', () {
    test('parses ready, close, session_closed, unread, and error', () {
      final ready = GrixBridgeEvent.parse(
        '{"type":"ready","visitor_key":"vk_1","session_id":"sess_1"}',
      );
      expect(ready, isA<GrixReadyEvent>());
      expect((ready as GrixReadyEvent).visitorKey, 'vk_1');

      expect(
        GrixBridgeEvent.parse('{"type":"close"}'),
        isA<GrixCloseEvent>(),
      );
      final closed = GrixBridgeEvent.parse(
        '{"type":"session_closed","reason":"closed"}',
      );
      expect((closed as GrixSessionClosedEvent).reason, 'closed');
      final unread = GrixBridgeEvent.parse('{"type":"unread","count":4}');
      expect((unread as GrixUnreadEvent).count, 4);
      final error = GrixBridgeEvent.parse(
        '{"type":"error","code":"4003","message":"origin not allowed"}',
      );
      expect((error as GrixErrorEvent).code, '4003');
    });

    test('opens only http(s) URLs from open_url', () {
      final event = GrixBridgeEvent.parse(
        '{"type":"open_url","url":"https://grix.im/docs"}',
      );
      expect((event as GrixOpenUrlEvent).url, 'https://grix.im/docs');
      expect(
        GrixBridgeEvent.parse(
          '{"type":"open_url","url":"javascript:alert(1)"}',
        ),
        isNull,
      );
    });

    test('rejects malformed bridge payloads', () {
      expect(GrixBridgeEvent.parse('not-json'), isNull);
      expect(GrixBridgeEvent.parse('{"type":"unknown"}'), isNull);
    });
  });

  group('GrixVisitorStore', () {
    test('scopes visitor keys by site key', () async {
      SharedPreferences.setMockInitialValues(<String, Object>{});
      final store = SharedPreferencesGrixVisitorStore();
      await store.write('site_a', 'vk_a');
      await store.write('site_b', 'vk_b');
      expect(await store.read('site_a'), 'vk_a');
      expect(await store.read('site_b'), 'vk_b');
      expect(await store.read('site_c'), isNull);
    });

    test('in-memory store behaves the same', () async {
      final store = InMemoryGrixVisitorStore();
      await store.write('site_a', 'vk_a');
      expect(await store.read('site_a'), 'vk_a');
      expect(await store.read('site_b'), isNull);
    });
  });

  group('GrixWidget callbacks', () {
    test('unread updates notify the host', () {
      var observed = -1;
      GrixWidget.onUnreadChange = (count) => observed = count;
      GrixWidget.updateUnread(7);
      expect(observed, 7);
      GrixWidget.onUnreadChange = null;
    });
  });
}
