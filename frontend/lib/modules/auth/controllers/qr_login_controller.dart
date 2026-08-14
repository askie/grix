import 'dart:async';

import 'package:get/get.dart';

import '../../../data/providers/auth_service.dart';
import '../../../data/providers/qr_login_service.dart';

class QrLoginController extends GetxController {
  QrLoginController({AuthService? authService, QrLoginService? qrLoginService})
      : _authService = authService ?? Get.find<AuthService>(),
        _injectedQrLoginService = qrLoginService;

  static const int maxConsecutivePollFailures = 5;

  final AuthService _authService;
  final QrLoginService? _injectedQrLoginService;
  QrLoginService get _qrLoginService =>
      _injectedQrLoginService ?? Get.find<QrLoginService>();

  final isLoading = false.obs;
  final isPolling = false.obs;
  final errorMessage = RxnString();
  final qrText = RxnString();
  final status = RxnString();
  final scannerNickname = RxnString();
  final expiresIn = 0.obs;

  String _sessionId = '';
  String _pollToken = '';
  int _pollIntervalMs = 1000;
  Timer? _pollTimer;
  Timer? _expiryTimer;
  bool _pollingInFlight = false;
  int _consecutivePollFailures = 0;
  // 流程代次：每次刷新/停止自增。二维码会话切换后，上一代的在途响应
  // 凭代次比对被丢弃，防止旧会话状态污染新二维码。
  int _flowEpoch = 0;

  Future<void> startDesktopFlow({String deviceLabel = ''}) async {
    if (isLoading.value ||
        isPolling.value ||
        qrText.value?.isNotEmpty == true) {
      return;
    }
    await refreshDesktopFlow(deviceLabel: deviceLabel);
  }

  Future<void> refreshDesktopFlow({String deviceLabel = ''}) async {
    if (isLoading.value) return;
    final epoch = ++_flowEpoch;
    _stopPolling();
    _cancelExpiryTimer();
    isLoading.value = true;
    errorMessage.value = null;
    status.value = null;
    scannerNickname.value = null;
    expiresIn.value = 0;

    final result = await _qrLoginService.createSession(
      deviceLabel: deviceLabel,
    );
    // 请求期间流程被停止（收起面板/销毁），丢弃结果，不再起轮询；
    // isLoading 已由停止方复位，旧响应不得再碰（可能抹掉新流程的 loading）。
    if (epoch != _flowEpoch) return;
    isLoading.value = false;
    if (!result.ok || result.data == null) {
      errorMessage.value =
          result.message.isEmpty ? 'login_qr_create_failed'.tr : result.message;
      qrText.value = null;
      _sessionId = '';
      _pollToken = '';
      return;
    }

    final data = result.data!;
    qrText.value = data.qrText;
    _sessionId = data.qrSessionId;
    _pollToken = data.pollToken;
    _pollIntervalMs = data.pollIntervalMs > 0 ? data.pollIntervalMs : 1000;
    expiresIn.value = data.expiresIn;
    status.value = 'pending_scan';
    _startExpiryTimer(data.expiresIn);
    _startPolling();
  }

  void stopDesktopFlow() {
    _flowEpoch++;
    _stopPolling();
    _cancelExpiryTimer();
    // 在途的建码/兑换请求已按代次作废，这里必须复位 isLoading，
    // 否则马上重新展开面板会被 startDesktopFlow 的守卫挡成白板。
    isLoading.value = false;
    qrText.value = null;
    _sessionId = '';
    _pollToken = '';
    status.value = null;
    scannerNickname.value = null;
    expiresIn.value = 0;
    errorMessage.value = null;
  }

  @override
  void onClose() {
    _flowEpoch++;
    _stopPolling();
    _cancelExpiryTimer();
    isLoading.value = false;
    super.onClose();
  }

  // 本地过期兜底：断网时轮询拿不到服务端的 expired 状态，
  // 到期后由本地计时器强制收敛到过期态，避免界面永远停在等待中。
  void _startExpiryTimer(int expiresInSeconds) {
    _cancelExpiryTimer();
    if (expiresInSeconds <= 0) return;
    _expiryTimer = Timer(Duration(seconds: expiresInSeconds), _onLocalExpiry);
  }

  void _cancelExpiryTimer() {
    _expiryTimer?.cancel();
    _expiryTimer = null;
  }

  void _onLocalExpiry() {
    _expiryTimer = null;
    // 只要二维码还挂在界面上就切过期态——即使轮询已因失败上限提前停止，
    // 也不能让用户继续扫一张实际已作废的码。
    if (qrText.value == null) return;
    _stopPolling();
    status.value = 'expired';
    expiresIn.value = 0;
    errorMessage.value = 'login_qr_status_expired'.tr;
  }

  void _startPolling() {
    _stopPolling();
    if (_sessionId.isEmpty || _pollToken.isEmpty) return;

    isPolling.value = true;
    _pollTimer = Timer.periodic(Duration(milliseconds: _pollIntervalMs), (_) {
      unawaited(_pollStatus());
    });
    unawaited(_pollStatus());
  }

  void _stopPolling() {
    _pollTimer?.cancel();
    _pollTimer = null;
    isPolling.value = false;
    _pollingInFlight = false;
    _consecutivePollFailures = 0;
  }

  Future<void> _pollStatus() async {
    if (_pollingInFlight) return;
    if (_sessionId.isEmpty || _pollToken.isEmpty) return;
    final epoch = _flowEpoch;
    _pollingInFlight = true;
    try {
      final result = await _qrLoginService.queryStatus(
        sessionId: _sessionId,
        pollToken: _pollToken,
      );
      // 请求在途期间流程可能已切换（手动刷新换码）或停止（本地过期/收起），
      // 凭代次与轮询状态双重比对，丢弃过期结果。
      if (epoch != _flowEpoch || !isPolling.value) return;
      if (!result.ok || result.data == null) {
        _consecutivePollFailures++;
        if (_consecutivePollFailures >= maxConsecutivePollFailures) {
          _stopPolling();
          errorMessage.value = 'login_qr_poll_network_error'.tr;
          return;
        }
        errorMessage.value = result.message.isEmpty
            ? 'login_qr_status_failed'.tr
            : result.message;
        return;
      }
      _consecutivePollFailures = 0;
      errorMessage.value = null;
      final data = result.data!;
      status.value = data.status;
      expiresIn.value = data.expiresIn;
      scannerNickname.value = data.scannerNickname;
      if (data.expiresIn <= 0 || data.status == 'expired') {
        _stopPolling();
        _cancelExpiryTimer();
        errorMessage.value = 'login_qr_status_expired'.tr;
        return;
      }
      if (data.status == 'consumed') {
        _stopPolling();
        _cancelExpiryTimer();
        errorMessage.value = 'login_qr_status_consumed'.tr;
        return;
      }
      if (data.status == 'canceled') {
        _stopPolling();
        _cancelExpiryTimer();
        errorMessage.value = 'login_qr_status_canceled'.tr;
        return;
      }
      if (data.status == 'confirmed') {
        await _exchangeSession();
      }
    } finally {
      // 代次已切换说明新流程已接管在途标记，旧请求不得清除。
      if (epoch == _flowEpoch) {
        _pollingInFlight = false;
      }
    }
  }

  Future<void> _exchangeSession() async {
    final epoch = _flowEpoch;
    _stopPolling();
    _cancelExpiryTimer();
    final sid = _sessionId;
    final token = _pollToken;
    if (sid.isEmpty || token.isEmpty) {
      _markExchangeFailed('login_qr_exchange_retry_hint'.tr);
      return;
    }

    isLoading.value = true;
    final result = await _authService.loginWithQrCodeSession(
      qrSessionId: sid,
      pollToken: token,
    );
    // 兑换期间流程被停止（收起面板/销毁），丢弃结果；
    // isLoading 已由停止方复位，旧响应不得再碰。
    if (epoch != _flowEpoch) return;
    isLoading.value = false;
    if (!result.ok) {
      _markExchangeFailed(
        result.message.isEmpty
            ? 'login_qr_exchange_retry_hint'.tr
            : result.message,
      );
      return;
    }
    errorMessage.value = null;
  }

  // 兑换失败按终态保守处理：复位界面提示刷新重扫，
  // 避免停留在"正在登录..."。
  void _markExchangeFailed(String message) {
    qrText.value = null;
    _sessionId = '';
    _pollToken = '';
    status.value = null;
    scannerNickname.value = null;
    expiresIn.value = 0;
    errorMessage.value = message;
  }
}
