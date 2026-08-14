import 'dart:async';

import 'package:fake_async/fake_async.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/qr_login_service.dart';
import 'package:grix/modules/auth/controllers/qr_login_controller.dart';

class _FakeQrLoginService extends QrLoginService {
  ServiceResult<QRLoginCreateData> createResult =
      ServiceResult<QRLoginCreateData>.success(
    data: const QRLoginCreateData(
      qrSessionId: 'sid-1',
      qrText: 'grix://auth/qr-login?sid=sid-1&qt=qt-1',
      pollToken: 'poll-1',
      expiresIn: 120,
      pollIntervalMs: 1000,
    ),
  );
  FutureOr<ServiceResult<QRLoginCreateData>> Function(int call)? createBuilder;
  int createCalls = 0;
  FutureOr<ServiceResult<QRLoginStatusData>> Function(int call)? statusBuilder;
  int statusCalls = 0;

  @override
  Future<ServiceResult<QRLoginCreateData>> createSession({
    String deviceLabel = '',
  }) async {
    createCalls++;
    if (createBuilder != null) {
      return createBuilder!(createCalls);
    }
    return createResult;
  }

  @override
  Future<ServiceResult<QRLoginStatusData>> queryStatus({
    required String sessionId,
    required String pollToken,
  }) async {
    statusCalls++;
    return statusBuilder!(statusCalls);
  }
}

class _FakeAuthService extends AuthService {
  ServiceResult<void> qrExchangeResult = ServiceResult<void>.success();
  FutureOr<ServiceResult<void>> Function(int call)? exchangeBuilder;
  int exchangeCalls = 0;

  @override
  Future<ServiceResult<void>> loginWithQrCodeSession({
    required String qrSessionId,
    required String pollToken,
  }) async {
    exchangeCalls++;
    if (exchangeBuilder != null) {
      return exchangeBuilder!(exchangeCalls);
    }
    return qrExchangeResult;
  }
}

ServiceResult<QRLoginStatusData> _statusOk(
  String status, {
  int expiresIn = 100,
}) {
  return ServiceResult<QRLoginStatusData>.success(
    data: QRLoginStatusData(status: status, expiresIn: expiresIn),
  );
}

ServiceResult<QRLoginStatusData> _statusFail() {
  return ServiceResult<QRLoginStatusData>.failure(message: '');
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _FakeQrLoginService qrService;
  late _FakeAuthService authService;
  late QrLoginController controller;

  setUp(() {
    qrService = _FakeQrLoginService();
    authService = _FakeAuthService();
    controller = QrLoginController(
      authService: authService,
      qrLoginService: qrService,
    );
  });

  tearDown(() {
    controller.stopDesktopFlow();
    Get.reset();
  });

  test('轮询连续失败达到上限后自动停止并提示网络异常', () {
    fakeAsync((async) {
      qrService.statusBuilder = (_) => _statusFail();
      controller.startDesktopFlow();
      async.flushMicrotasks();
      expect(controller.isPolling.value, isTrue);

      // 首次立即轮询 + 每秒一次，推进到达到失败上限。
      async.elapse(const Duration(seconds: 10));
      expect(
        qrService.statusCalls,
        QrLoginController.maxConsecutivePollFailures,
      );
      expect(controller.isPolling.value, isFalse);
      expect(
        controller.errorMessage.value,
        'login_qr_poll_network_error'.tr,
      );

      // 停止后不再产生新的轮询请求。
      async.elapse(const Duration(seconds: 5));
      expect(
        qrService.statusCalls,
        QrLoginController.maxConsecutivePollFailures,
      );
    });
  });

  test('轮询失败后恢复成功会重置失败计数并清除错误提示', () {
    fakeAsync((async) {
      qrService.statusBuilder = (call) {
        if (call <= 3) return _statusFail();
        return _statusOk('pending_scan');
      };
      controller.startDesktopFlow();
      async.flushMicrotasks();

      // 立即轮询一次 + 每秒一次：t=0/1/2 三次均失败。
      async.elapse(const Duration(seconds: 2));
      expect(qrService.statusCalls, 3);
      expect(controller.errorMessage.value, isNotNull);

      // t=3 起恢复成功：失败计数清零、错误提示清除、轮询继续。
      async.elapse(const Duration(seconds: 2));
      expect(controller.isPolling.value, isTrue);
      expect(controller.errorMessage.value, isNull);

      async.elapse(const Duration(seconds: 10));
      expect(controller.isPolling.value, isTrue);
    });
  });

  test('断网时本地过期兜底：到期停止轮询并显示二维码过期', () {
    fakeAsync((async) {
      // 服务端一直返回等待扫码（模拟拿不到 expired 状态的场景由失败上限兜底，
      // 这里覆盖的是响应正常但永不推进、靠本地计时器收敛的情况）。
      qrService.statusBuilder = (_) => _statusOk('pending_scan');
      controller.startDesktopFlow();
      async.flushMicrotasks();
      expect(controller.isPolling.value, isTrue);

      async.elapse(const Duration(seconds: 121));
      expect(controller.isPolling.value, isFalse);
      expect(controller.status.value, 'expired');
      expect(
        controller.errorMessage.value,
        'login_qr_status_expired'.tr,
      );

      // 过期后不再轮询。
      final callsAtExpiry = qrService.statusCalls;
      async.elapse(const Duration(seconds: 5));
      expect(qrService.statusCalls, callsAtExpiry);
    });
  });

  test('确认后兑换失败：复位二维码并提示刷新，不停留在正在登录', () {
    fakeAsync((async) {
      qrService.statusBuilder = (_) => _statusOk('confirmed');
      authService.qrExchangeResult =
          ServiceResult<void>.failure(message: '');
      controller.startDesktopFlow();
      async.flushMicrotasks();
      async.elapse(const Duration(seconds: 1));

      expect(authService.exchangeCalls, 1);
      expect(controller.isPolling.value, isFalse);
      expect(controller.isLoading.value, isFalse);
      expect(controller.status.value, isNull);
      expect(controller.qrText.value, isNull);
      expect(
        controller.errorMessage.value,
        'login_qr_exchange_retry_hint'.tr,
      );

      // 终态后不再有任何轮询。
      final calls = qrService.statusCalls;
      async.elapse(const Duration(seconds: 5));
      expect(qrService.statusCalls, calls);
    });
  });

  test('确认后兑换成功：清除错误并停止轮询', () {
    fakeAsync((async) {
      qrService.statusBuilder = (_) => _statusOk('confirmed');
      controller.startDesktopFlow();
      async.flushMicrotasks();
      async.elapse(const Duration(seconds: 1));

      expect(authService.exchangeCalls, 1);
      expect(controller.isPolling.value, isFalse);
      expect(controller.errorMessage.value, isNull);
    });
  });

  test('刷新后旧会话的在途状态响应被丢弃，不污染新二维码', () {
    fakeAsync((async) {
      final stale = Completer<ServiceResult<QRLoginStatusData>>();
      qrService.statusBuilder = (call) {
        if (call == 1) return stale.future;
        return _statusOk('pending_scan');
      };
      controller.startDesktopFlow();
      async.flushMicrotasks();
      expect(controller.qrText.value, contains('sid-1'));

      // 旧会话第一次轮询还挂在路上时，用户点了刷新拿到新码。
      qrService.createResult = ServiceResult<QRLoginCreateData>.success(
        data: const QRLoginCreateData(
          qrSessionId: 'sid-2',
          qrText: 'grix://auth/qr-login?sid=sid-2&qt=qt-2',
          pollToken: 'poll-2',
          expiresIn: 120,
          pollIntervalMs: 1000,
        ),
      );
      controller.refreshDesktopFlow();
      async.flushMicrotasks();
      expect(controller.qrText.value, contains('sid-2'));
      expect(controller.isPolling.value, isTrue);

      // 旧会话的响应姗姗来迟（expired），必须被丢弃。
      stale.complete(_statusOk('expired', expiresIn: 0));
      async.flushMicrotasks();
      expect(controller.status.value, isNot('expired'));
      expect(controller.errorMessage.value, isNull);
      expect(controller.isPolling.value, isTrue);
      expect(controller.qrText.value, contains('sid-2'));

      // 新会话的轮询继续正常推进（在途标记没有被旧请求清坏）。
      final callsBefore = qrService.statusCalls;
      async.elapse(const Duration(seconds: 3));
      expect(qrService.statusCalls, greaterThan(callsBefore));
      expect(controller.isPolling.value, isTrue);
    });
  });

  test('失败上限停止轮询后，本地过期定时器仍会把二维码切到已过期', () {
    fakeAsync((async) {
      qrService.statusBuilder = (_) => _statusFail();
      controller.startDesktopFlow();
      async.flushMicrotasks();

      async.elapse(const Duration(seconds: 10));
      expect(controller.isPolling.value, isFalse);
      expect(
        controller.errorMessage.value,
        'login_qr_poll_network_error'.tr,
      );
      expect(controller.qrText.value, isNotNull);

      // create 返回 expiresIn=120，到期后不能把废码继续留给用户扫。
      async.elapse(const Duration(seconds: 115));
      expect(controller.status.value, 'expired');
      expect(
        controller.errorMessage.value,
        'login_qr_status_expired'.tr,
      );
    });
  });

  test('建码在途时收起面板再打开：能重新拉码，不留白板', () {
    fakeAsync((async) {
      final staleCreate = Completer<ServiceResult<QRLoginCreateData>>();
      qrService.createBuilder = (call) {
        if (call == 1) return staleCreate.future;
        return qrService.createResult;
      };
      qrService.statusBuilder = (_) => _statusOk('pending_scan');

      controller.startDesktopFlow();
      async.flushMicrotasks();
      expect(controller.isLoading.value, isTrue);

      // 建码还在路上，用户收起面板：isLoading 必须复位。
      controller.stopDesktopFlow();
      expect(controller.isLoading.value, isFalse);

      // 马上重新打开：必须真的重新建码，不能被守卫挡成白板。
      controller.startDesktopFlow();
      async.flushMicrotasks();
      expect(qrService.createCalls, 2);
      expect(controller.qrText.value, isNotNull);
      expect(controller.isPolling.value, isTrue);

      // 旧建码响应迟到：不得抹掉新流程状态。
      staleCreate.complete(qrService.createResult);
      async.flushMicrotasks();
      expect(controller.qrText.value, isNotNull);
      expect(controller.isPolling.value, isTrue);
      expect(controller.isLoading.value, isFalse);
    });
  });

  test('兑换在途时收起面板再打开：能重新拉码，旧兑换失败不污染新流程', () {
    fakeAsync((async) {
      final staleExchange = Completer<ServiceResult<void>>();
      qrService.statusBuilder = (call) {
        if (call == 1) return _statusOk('confirmed');
        return _statusOk('pending_scan');
      };
      authService.exchangeBuilder = (call) {
        if (call == 1) return staleExchange.future;
        return ServiceResult<void>.success();
      };

      controller.startDesktopFlow();
      async.flushMicrotasks();
      expect(controller.isLoading.value, isTrue); // 兑换中

      // 兑换还在路上，用户收起面板：isLoading 必须复位。
      controller.stopDesktopFlow();
      expect(controller.isLoading.value, isFalse);

      // 马上重新打开：必须真的重新建码。
      controller.startDesktopFlow();
      async.flushMicrotasks();
      expect(qrService.createCalls, 2);
      expect(controller.qrText.value, isNotNull);
      expect(controller.isPolling.value, isTrue);

      // 旧兑换失败迟到：不得复位新二维码、不得写错误提示。
      staleExchange.complete(ServiceResult<void>.failure(message: ''));
      async.flushMicrotasks();
      expect(controller.qrText.value, isNotNull);
      expect(controller.errorMessage.value, isNull);
      expect(controller.isPolling.value, isTrue);
      expect(controller.isLoading.value, isFalse);
    });
  });

  test('服务端返回过期状态时停止轮询（回归）', () {
    fakeAsync((async) {
      qrService.statusBuilder = (_) => _statusOk('expired', expiresIn: 0);
      controller.startDesktopFlow();
      async.flushMicrotasks();
      async.elapse(const Duration(seconds: 1));

      expect(controller.isPolling.value, isFalse);
      expect(
        controller.errorMessage.value,
        'login_qr_status_expired'.tr,
      );
    });
  });
}
