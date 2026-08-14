import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:url_launcher_platform_interface/link.dart';
import 'package:url_launcher_platform_interface/url_launcher_platform_interface.dart';

import 'package:dio/dio.dart';

import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/feature_flag_service.dart';
import 'package:grix/data/providers/gateway_service.dart';
import 'package:grix/modules/system/gateway_relay_panel_view.dart';

class _FakeAuthService extends AuthService {
  @override
  void attachAuthInterceptor(Dio dio) {}
}

/// 资金侧链路测试专用：余额卡片上的"充值"按钮 → 弹窗选通道填金额 →
/// 调 createTopup 下单 → 打开收银台；wallet/账单/充值记录返回可控假数据。
class _FakeGatewayService extends GatewayService {
  String? topupAmount;
  String? topupCurrency;
  String? topupChannel;
  int walletCalls = 0;
  int ledgerCalls = 0;
  int topupListCalls = 0;

  @override
  Future<GatewayWalletModel?> getWallet() async {
    walletCalls++;
    return const GatewayWalletModel(id: '1', balance: '3.14');
  }

  @override
  Future<List<GatewayLedgerEntryModel>> listLedger({
    int page = 1,
    int pageSize = 20,
  }) async {
    ledgerCalls++;
    return const [];
  }

  @override
  Future<List<GatewayTopupRecordModel>> listTopups({
    int page = 1,
    int pageSize = 20,
  }) async {
    topupListCalls++;
    return const [];
  }

  @override
  Future<GatewayTopupOrderModel?> createTopup({
    required String amount,
    required String currency,
    required String channel,
    String returnUrl = '',
  }) async {
    topupAmount = amount;
    topupCurrency = currency;
    topupChannel = channel;
    return const GatewayTopupOrderModel(
      topupOrderId: '1001',
      payUrl: 'https://pay.example.com/cashier?order=1001',
    );
  }
}

class _FakeUrlLauncher extends UrlLauncherPlatform {
  final launched = <String>[];

  @override
  LinkDelegate? get linkDelegate => null;

  @override
  Future<bool> canLaunch(String url) async => true;

  @override
  Future<bool> launchUrl(String url, LaunchOptions options) async {
    launched.add(url);
    return true;
  }
}

void main() {
  late _FakeGatewayService service;
  late _FakeUrlLauncher launcher;
  late FeatureFlagService featureFlags;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    // GatewayService.onInit 会向 AuthService 注册鉴权拦截器（不注册就没 token，
    // 线上表现为充值下单 401）。测试环境同样要提供这个依赖。
    Get.put<AuthService>(_FakeAuthService());
    featureFlags = FeatureFlagService();
    featureFlags.features.assignAll(['gateway_topup_paypal']);
    Get.put<FeatureFlagService>(featureFlags);
    service = _FakeGatewayService();
    Get.put<GatewayService>(service);
    launcher = _FakeUrlLauncher();
    UrlLauncherPlatform.instance = launcher;
  });

  tearDown(() {
    Get.reset();
  });

  Future<void> pumpPanel(WidgetTester tester, {bool isActive = true}) async {
    await tester.pumpWidget(
      GetMaterialApp(
        theme: AppTheme.lightTheme,
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        fallbackLocale: const Locale('zh', 'CN'),
        home: Scaffold(body: GatewayRelayPanelView(isActive: isActive)),
      ),
    );
    await tester.pumpAndSettle();
  }

  testWidgets('余额卡片展示充值按钮，点击弹出充值弹窗', (tester) async {
    await pumpPanel(tester);

    expect(find.text('\$3.14'), findsOneWidget);
    expect(find.text('充值'), findsOneWidget);

    await tester.tap(find.text('充值'));
    await tester.pumpAndSettle();

    expect(find.text('支付宝'), findsOneWidget);
    expect(find.text('PayPal'), findsOneWidget);
    expect(find.text('去支付'), findsOneWidget);
    expect(find.text('充值金额（CNY）'), findsOneWidget);

    await tester.tap(find.text('取消'));
    await tester.pumpAndSettle();
    expect(find.text('去支付'), findsNothing);
  });

  testWidgets('PayPal feature 关闭时充值弹窗只展示支付宝', (tester) async {
    featureFlags.features.clear();
    await pumpPanel(tester);

    await tester.tap(find.text('充值'));
    await tester.pumpAndSettle();

    expect(find.text('支付宝'), findsOneWidget);
    expect(find.text('PayPal'), findsNothing);
    expect(find.text('充值金额（CNY）'), findsOneWidget);

    await tester.enterText(find.byType(TextField), '20');
    await tester.tap(find.text('去支付'));
    await tester.pumpAndSettle();

    expect(service.topupAmount, '20');
    expect(service.topupCurrency, 'CNY');
    expect(service.topupChannel, 'alipay');
    await tester.pump(const Duration(seconds: 4));
  });

  testWidgets('填金额提交后按所选通道下单并打开收银台', (tester) async {
    await pumpPanel(tester);

    await tester.tap(find.text('充值'));
    await tester.pumpAndSettle();

    // 切到 PayPal，币种提示应跟着变成 USD。
    await tester.tap(find.text('PayPal'));
    await tester.pumpAndSettle();
    expect(find.text('充值金额（USD）'), findsOneWidget);

    await tester.enterText(find.byType(TextField), '10.50');
    await tester.tap(find.text('去支付'));
    await tester.pumpAndSettle();

    expect(service.topupAmount, '10.50');
    expect(service.topupCurrency, 'USD');
    expect(service.topupChannel, 'paypal');
    expect(launcher.launched, ['https://pay.example.com/cashier?order=1001']);
    // 下单成功后弹窗应关闭。
    expect(find.text('去支付'), findsNothing);

    // 支付跳出去再回来：_showTopupDialog 收尾时会再刷一次余额和充值记录
    //（initState 首载各调一次，这里各 +1）。
    expect(service.walletCalls, 2);
    expect(service.topupListCalls, 2);

    // 等提示 toast 的自动消失计时器走完，避免残留 pending timer。
    await tester.pump(const Duration(seconds: 4));
  });

  testWidgets('金额超单笔上限被拦下，不发下单请求', (tester) async {
    await pumpPanel(tester);

    await tester.tap(find.text('充值'));
    await tester.pumpAndSettle();

    await tester.enterText(find.byType(TextField), '100001');
    await tester.tap(find.text('去支付'));
    await tester.pumpAndSettle();

    expect(service.topupAmount, isNull);
    expect(find.text('去支付'), findsOneWidget);

    await tester.tap(find.text('取消'));
    await tester.pumpAndSettle();
    await tester.pump(const Duration(seconds: 4));
  });

  testWidgets('打开消费流水和充值记录页面时自动刷新对应数据', (tester) async {
    await pumpPanel(tester);

    expect(service.ledgerCalls, 1);
    expect(service.topupListCalls, 1);

    // 子 tab 现在只有两页：0=消费流水、1=充值记录。面板默认停在消费流水，
    // 先切到充值记录触发该页刷新，再切回消费流水触发另一页刷新。
    await tester.tap(find.text('充值记录'));
    await tester.pumpAndSettle();
    expect(service.ledgerCalls, 1);
    expect(service.topupListCalls, 2);

    await tester.tap(find.text('消费流水'));
    await tester.pumpAndSettle();
    expect(service.ledgerCalls, 2);
    expect(service.topupListCalls, 2);
  });

  testWidgets('外层大模型设置页面真正打开时自动刷新余额', (tester) async {
    // 外层 TabBarView 提前构建页面时会先做首次加载，但此时页面尚不可见。
    await pumpPanel(tester, isActive: false);
    expect(service.walletCalls, 1);

    // 用户切到“大模型设置”时，父页面把 isActive 改为 true，应重新拉取余额。
    await pumpPanel(tester, isActive: true);
    expect(service.walletCalls, 2);
  });
}
