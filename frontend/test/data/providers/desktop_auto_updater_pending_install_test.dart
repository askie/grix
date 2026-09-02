import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/desktop_auto_updater.dart';

/// 锁住桌面自动更新的两个坑：
///
/// 1. Sparkle 已经下载完一个更新、挂在那儿等应用退出安装时，更新周期是挂起的，
///    再调 checkForUpdates 原生层直接忽略——用户点"检查更新"完全没反应，
///    也感知不到服务端后来发布的更多新版本。这时必须由我们自己给出可见反馈。
/// 2. 公钥门禁没过时更新器从未初始化，任何入口都不能绕过去碰原生更新器。
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  const updaterChannel = MethodChannel('dev.leanflutter.plugins/auto_updater');
  const eventChannel = MethodChannel(
    'dev.leanflutter.plugins/auto_updater_event',
  );

  late List<MethodCall> calls;
  Map<Object?, Object?>? pendingStatus;
  bool feedUrlFails = false;

  List<String> methodsCalled() => calls.map((c) => c.method).toList();

  Widget testApp() => GetMaterialApp(
    translations: AppTranslations(),
    locale: const Locale('zh', 'CN'),
    home: const Scaffold(),
  );

  setUp(() {
    Get.testMode = true;
    Get.reset();
    calls = <MethodCall>[];
    pendingStatus = null;
    feedUrlFails = false;

    final messenger =
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
    messenger.setMockMethodCallHandler(updaterChannel, (call) async {
      calls.add(call);
      switch (call.method) {
        case 'setFeedURL':
          if (feedUrlFails) {
            throw PlatformException(code: 'feed_url_rejected');
          }
          return true;
        case 'setScheduledCheckInterval':
        case 'checkForUpdates':
          return true;
        case 'getUpdateSessionStatus':
          return pendingStatus ??
              <Object?, Object?>{'hasPendingInstall': false};
        case 'installPendingUpdate':
          return pendingStatus != null;
      }
      return null;
    });
    // auto_updater 的单例在首次访问时就会 listen 事件通道，不接住会抛未处理异常。
    messenger.setMockMethodCallHandler(eventChannel, (call) async => null);
  });

  tearDown(() {
    final messenger =
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
    messenger.setMockMethodCallHandler(updaterChannel, null);
    messenger.setMockMethodCallHandler(eventChannel, null);
    Get.reset();
  });

  void markPending() {
    pendingStatus = <Object?, Object?>{
      'hasPendingInstall': true,
      'pendingVersion': '3.2.6',
      'pendingBuild': '886',
      'pendingSinceEpochMs': DateTime.now().millisecondsSinceEpoch,
    };
  }

  testWidgets('已有下载完成待重启安装的更新时，手动检查必须弹出提示而不是静默无反应', (tester) async {
    markPending();
    final service = await DesktopAutoUpdaterService().init();

    await tester.pumpWidget(testApp());
    await tester.pump();

    calls.clear();
    await service.checkForUpdatesInteractive();
    await tester.pumpAndSettle();
    service.onClose(); // 停掉 init 起的挂起提醒定时器

    expect(
      find.byType(AlertDialog),
      findsOneWidget,
      reason: '原生层会忽略这次检查，必须由我们自己告诉用户"已下载，重启后生效"',
    );
    expect(
      find.textContaining('3.2.6 (886)'),
      findsOneWidget,
      reason: '热修版本展示版本号可能不变，不带构建号用户看不出这条提示指的是哪个包',
    );
    expect(find.text('立即重启'), findsOneWidget);
    expect(
      methodsCalled(),
      isNot(contains('checkForUpdates')),
      reason: '挂起状态下再去调原生检查只会石沉大海',
    );
  }, skip: !Platform.isMacOS);

  testWidgets('弹窗里的"立即重启"会真的去装已下载的更新', (tester) async {
    markPending();
    final service = await DesktopAutoUpdaterService().init();

    await tester.pumpWidget(testApp());
    await tester.pump();

    await service.checkForUpdatesInteractive();
    await tester.pumpAndSettle();
    calls.clear();

    await tester.tap(find.text('立即重启'));
    await tester.pumpAndSettle();
    service.onClose(); // 停掉 init 起的挂起提醒定时器

    expect(methodsCalled(), contains('installPendingUpdate'));
    expect(find.byType(AlertDialog), findsNothing);
  }, skip: !Platform.isMacOS);

  testWidgets('没有挂起安装时，手动检查照旧交给原生层弹窗（inBackground=false）', (tester) async {
    final service = await DesktopAutoUpdaterService().init();

    await tester.pumpWidget(testApp());
    await tester.pump();

    calls.clear();
    await service.checkForUpdatesInteractive();
    await tester.pumpAndSettle();
    service.onClose(); // 停掉 init 起的挂起提醒定时器

    final check = calls.lastWhere((c) => c.method == 'checkForUpdates');
    expect((check.arguments as Map)['inBackground'], isFalse);
    expect(find.byType(AlertDialog), findsNothing);
  }, skip: !Platform.isMacOS);

  test('更新器未就绪时手动检查被拒绝，且完全不碰原生更新器', () async {
    feedUrlFails = true;
    final service = await DesktopAutoUpdaterService().init();

    calls.clear();
    await expectLater(
      service.checkForUpdatesInteractive(),
      throwsA(isA<StateError>()),
    );
    expect(calls, isEmpty, reason: '验签门禁没过就不能查挂起状态，更不能触发检查');
    expect(await service.pendingUpdate(), isNull);
    expect(await service.installPendingUpdateNow(), isFalse);
    expect(calls, isEmpty);
  });

  test('待安装版本文案带上构建号，只有版本号缺失时才退回构建号', () {
    const withBoth = PendingDesktopUpdate(
      version: '3.2.6',
      build: '886',
      since: null,
    );
    expect(withBoth.displayVersion, '3.2.6 (886)');

    const versionOnly = PendingDesktopUpdate(
      version: '3.2.6',
      build: '',
      since: null,
    );
    expect(versionOnly.displayVersion, '3.2.6');

    const buildOnly = PendingDesktopUpdate(
      version: '',
      build: '886',
      since: null,
    );
    expect(buildOnly.displayVersion, '886');
  });
}
