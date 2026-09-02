// ignore_for_file: depend_on_referenced_packages
// plugin_platform_interface 是 url_launcher_platform_interface 的传递依赖，
// 仅在本测试用 MockPlatformInterfaceMixin 替换 url_launcher 平台时使用。

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:plugin_platform_interface/plugin_platform_interface.dart';
import 'package:url_launcher_platform_interface/link.dart';
import 'package:url_launcher_platform_interface/url_launcher_platform_interface.dart';

import 'package:grix/data/services/link_safety_service.dart';
import 'package:grix/shared/markdown/chat_markdown_dialect.dart';
import 'package:grix/shared/markdown/chat_markdown_normalizer.dart';
import 'package:grix/shared/markdown/chat_markdown_pipeline.dart';
import 'package:grix/shared/utils/app_external_links.dart';
import 'package:grix/shared/widgets/chat_markdown_fallback_view.dart';
import 'package:grix/shared/widgets/chat_markdown_style_sheet.dart';
import 'package:grix/shared/widgets/chat_markdown_view.dart';

import '../widgets/markdown_link_finder.dart';

/// 链接黑名单收口验证：
/// - AppExternalLinks.open 按 scheme 分流（http/https 走校验；mailto/tel/grix 直放行）。
/// - 三条聊天链接渲染路径（nativeAst 主渲染 / fallback LinkConfig / fallback customLinkGenerator）
///   点击都收口到 AppExternalLinks.open，从而被黑名单覆盖。
/// - 不合法 scheme 在主渲染路径降级为纯文本。

class _FakeLinkSafetyService extends LinkSafetyService {
  _FakeLinkSafetyService({this.verdict = LinkVerdictLevel.clean});

  final LinkVerdictLevel verdict;
  final List<String> checked = <String>[];

  @override
  Future<LinkVerdict> check(String rawUrl) async {
    checked.add(rawUrl);
    switch (verdict) {
      case LinkVerdictLevel.clean:
        return LinkVerdict.clean();
      case LinkVerdictLevel.suspicious:
        return LinkVerdict.suspicious();
      case LinkVerdictLevel.malicious:
        return LinkVerdict.malicious();
    }
  }
}

/// 替换 url_launcher 的平台实现，把 launchUrl 直接回写到列表里，
/// 测试环境下不调任何真平台通道。
class _FakeUrlLauncherPlatform extends UrlLauncherPlatform
    with MockPlatformInterfaceMixin {
  final List<String> launched = <String>[];

  @override
  LinkDelegate? get linkDelegate => null;

  @override
  Future<bool> canLaunch(String url) async => true;

  @override
  Future<bool> launch(
    String url, {
    required bool useSafariVC,
    required bool useWebView,
    required bool enableJavaScript,
    required bool enableDomStorage,
    required bool universalLinksOnly,
    required Map<String, String> headers,
    String? webOnlyWindowName,
  }) async {
    launched.add(url);
    return true;
  }

  @override
  Future<bool> launchUrl(String url, LaunchOptions options) async {
    launched.add(url);
    return true;
  }

  @override
  Future<void> closeWebView() async {}
}

ChatMarkdownStyleSheet _styleSheet(BuildContext context) {
  return ChatMarkdownStyleSheet.fromTheme(
    theme: Theme.of(context),
    textColor: const Color(0xFF111111),
    isMine: false,
  );
}

final _pipeline = ChatMarkdownPipeline(
  normalizer: const ChatMarkdownNormalizer(),
  parser: ChatMarkdownDialect.buildParserAdapter(),
);

/// 用 native ast 主渲染路径渲染一段 markdown（带 document → 走 ChatMarkdownAstView，
/// 链接即 TextSpan+TapGestureRecognizer），用于验证主渲染器点击收口到黑名单入口。
Widget _nativeAstView(String data) {
  final result = _pipeline.prepareFinalRender(data);
  return MaterialApp(
    home: Scaffold(
      body: ChatMarkdownView(
        data: result.normalizedText,
        textColor: const Color(0xFF111111),
        isMine: false,
        document: result.document,
        semantics: result.semantics,
      ),
    ),
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _FakeLinkSafetyService fakeSafety;
  late _FakeUrlLauncherPlatform fakeLauncher;
  late UrlLauncherPlatform originalLauncher;

  setUp(() {
    fakeSafety = _FakeLinkSafetyService();
    fakeLauncher = _FakeUrlLauncherPlatform();
    originalLauncher = UrlLauncherPlatform.instance;
    UrlLauncherPlatform.instance = fakeLauncher;
    Get.testMode = true;
    Get.put<LinkSafetyService>(fakeSafety);
  });

  tearDown(() {
    // LinkSafetyService 是 GetxService，必须 force:true 才能被卸下。
    Get.deleteAll(force: true);
    Get.reset();
    UrlLauncherPlatform.instance = originalLauncher;
  });

  group('AppExternalLinks.open scheme dispatch', () {
    test('http -> 调用黑名单校验 + launch', () async {
      final ok = await AppExternalLinks.open('http://example.com/a');
      expect(ok, true);
      expect(fakeSafety.checked, <String>['http://example.com/a']);
      expect(fakeLauncher.launched, <String>['http://example.com/a']);
    });

    test('https -> 调用黑名单校验 + launch', () async {
      final ok = await AppExternalLinks.open('https://example.com/b');
      expect(ok, true);
      expect(fakeSafety.checked, <String>['https://example.com/b']);
      expect(fakeLauncher.launched, <String>['https://example.com/b']);
    });

    test('HTTPS 大小写 -> 仍按 https 处理走校验', () async {
      final ok = await AppExternalLinks.open('HTTPS://Example.com/C');
      expect(ok, true);
      expect(fakeSafety.checked.length, 1);
    });

    test('mailto -> 不调用黑名单校验，直接 launch', () async {
      final ok = await AppExternalLinks.open('mailto:foo@bar.com');
      expect(ok, true);
      expect(fakeSafety.checked, isEmpty);
      expect(fakeLauncher.launched, <String>['mailto:foo@bar.com']);
    });

    test('tel -> 不调用黑名单校验，直接 launch', () async {
      final ok = await AppExternalLinks.open('tel:+8613800138000');
      expect(ok, true);
      expect(fakeSafety.checked, isEmpty);
      expect(fakeLauncher.launched, <String>['tel:+8613800138000']);
    });

    test('grix -> 不调用黑名单校验，直接 launch', () async {
      final ok = await AppExternalLinks.open('grix://card/foo');
      expect(ok, true);
      expect(fakeSafety.checked, isEmpty);
      expect(fakeLauncher.launched, <String>['grix://card/foo']);
    });

    test('空串 -> 直接 false，不调用 launch 也不调用校验', () async {
      final ok = await AppExternalLinks.open('   ');
      expect(ok, false);
      expect(fakeSafety.checked, isEmpty);
      expect(fakeLauncher.launched, isEmpty);
    });

    test('未注册 LinkSafetyService 时 http 也直接打开（兜底降级）', () async {
      // LinkSafetyService 是 GetxService，必须 force:true 才能真正卸下。
      await Get.delete<LinkSafetyService>(force: true);
      expect(Get.isRegistered<LinkSafetyService>(), false);

      final ok = await AppExternalLinks.open('http://x.com/y');
      expect(ok, true);
      expect(fakeSafety.checked, isEmpty, reason: '服务未注册时，校验不应被调用');
      expect(fakeLauncher.launched, <String>['http://x.com/y']);
    });
  });

  group('AppExternalLinks.open 校验结果分级', () {
    testWidgets('恶意(黑名单)链接 -> 静默不响应：返回 false、不 launch、不弹中间页', (
      WidgetTester tester,
    ) async {
      // GetxService 不能用普通 delete，需要 force:true。
      await Get.delete<LinkSafetyService>(force: true);
      final maliciousFake = _FakeLinkSafetyService(
        verdict: LinkVerdictLevel.malicious,
      );
      Get.put<LinkSafetyService>(maliciousFake);

      bool? result;
      await tester.pumpWidget(
        GetMaterialApp(
          home: Scaffold(
            body: Builder(
              builder: (context) {
                return ElevatedButton(
                  key: const Key('open-evil'),
                  onPressed: () async {
                    result = await AppExternalLinks.open('http://evil.com/x');
                  },
                  child: const Text('go'),
                );
              },
            ),
          ),
        ),
      );
      await tester.tap(find.byKey(const Key('open-evil')));
      await tester.pump();
      await tester.pump();

      expect(maliciousFake.checked, <String>['http://evil.com/x']);
      expect(
        result,
        false,
        reason: '黑名单链接静默不响应，AppExternalLinks.open 返回 false',
      );
      expect(fakeLauncher.launched, isEmpty, reason: '黑名单链接不能调 launch');
      // 产品决策：黑名单链接不再弹任何拦截中间页，点了就是没反应。
      expect(find.byType(Dialog), findsNothing, reason: '黑名单链接不弹中间页');
    });
  });

  group('三条聊天渲染路径点击 -> 收口到 AppExternalLinks.open', () {
    testWidgets('nativeAst 主渲染器链接点击 -> 触发校验', (WidgetTester tester) async {
      await tester.pumpWidget(
        _nativeAstView('[主渲染链接](https://main-renderer.example/a)'),
      );
      await tester.pumpAndSettle();

      // 主渲染路径链接是挂了点击手势的 TextSpan（不再是独立 widget）。
      expect(tappableLinkTexts(tester), contains('主渲染链接'));

      await tester.tapOnText(find.textRange.ofSubstring('主渲染链接'));
      await tester.pump();
      await tester.pump();

      expect(fakeSafety.checked, <String>['https://main-renderer.example/a']);
      expect(fakeLauncher.launched, <String>[
        'https://main-renderer.example/a',
      ]);
    });

    testWidgets('兜底渲染器 fallback customLinkGenerator [label](url) 点击 -> 触发校验', (
      WidgetTester tester,
    ) async {
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Builder(
              builder: (context) {
                return ChatMarkdownFallbackView(
                  data: '请看 [说明文档](https://fallback-builder.example/doc) 了解详情。',
                  styleSheet: _styleSheet(context),
                );
              },
            ),
          ),
        ),
      );

      // 链接文案在 RichText 子 span 上挂 TapGestureRecognizer，
      // 需要 tapOnText 才能精准命中链接对应的 sub-span。
      await tester.tapOnText(find.textRange.ofSubstring('说明文档'));
      await tester.pump();
      await tester.pump();

      expect(
        fakeSafety.checked,
        contains('https://fallback-builder.example/doc'),
      );
      expect(
        fakeLauncher.launched,
        contains('https://fallback-builder.example/doc'),
      );

      // 兜底渲染器内部用了 visibility_detector，残留的 timer 会让 teardown 报错，
      // 显式释放 widget 树并让 timer 跑完。
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
    });

    testWidgets('兜底渲染器 fallback LinkConfig 点击 auto-link -> 触发校验', (
      WidgetTester tester,
    ) async {
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Builder(
              builder: (context) {
                return ChatMarkdownFallbackView(
                  data: 'visit https://fallback-link.example/auto for details',
                  styleSheet: _styleSheet(context),
                );
              },
            ),
          ),
        ),
      );

      await tester.tapOnText(
        find.textRange.ofSubstring('https://fallback-link.example/auto'),
      );
      await tester.pump();
      await tester.pump();

      // markdown_widget 的 auto-link 由 customLinkGenerator 或 LinkConfig 接管，
      // 不论命中哪条都应进入 AppExternalLinks.open，再触发 fakeSafety.check。
      expect(
        fakeSafety.checked,
        contains('https://fallback-link.example/auto'),
      );
      expect(
        fakeLauncher.launched,
        contains('https://fallback-link.example/auto'),
      );

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
    });
  });

  group('防回归：不合法 scheme + 不再依赖 url_launcher Link', () {
    testWidgets('主渲染路径 javascript: -> 降级纯文本，不可点', (WidgetTester tester) async {
      await tester.pumpWidget(_nativeAstView('[可疑脚本](javascript:alert(1))'));
      await tester.pumpAndSettle();

      // 非法 scheme 降级为无点击手势的纯文本 span：文本仍在，但不构成可点链接。
      expect(tappableLinkSpans(tester), isEmpty);
      expect(find.text('可疑脚本'), findsOneWidget);
      expect(fakeSafety.checked, isEmpty);
      expect(fakeLauncher.launched, isEmpty);
    });
  });
}
