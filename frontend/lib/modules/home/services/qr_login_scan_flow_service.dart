import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../data/providers/auth_service.dart';
import '../../../data/providers/qr_login_service.dart';
import '../../../shared/utils/app_region_config.dart';
import '../../../shared/utils/toast_util.dart';
import '../../../shared/widgets/app_dialog_style.dart';

/// 服务端错误码：二维码属于其他区域（跨区扫码）。
const int _kQrScanRegionMismatchCode = 10011;

/// 服务端错误码：二维码无效 / 记录不存在。
const int _kQrScanInvalidCode = 10004;

class QrLoginScanFlowService {
  QrLoginScanFlowService({
    QrLoginService? qrLoginService,
    AuthService? authService,
  }) : _qrLoginService = qrLoginService ?? Get.find<QrLoginService>(),
       _authService = authService ?? Get.find<AuthService>();

  final QrLoginService _qrLoginService;
  final AuthService _authService;

  Future<bool> handleScannedText(String rawContent) async {
    final normalized = rawContent.trim();
    if (!_qrLoginService.isQrLoginPayload(normalized)) {
      return false;
    }

    if (!_authService.isLoggedIn) {
      CustomToast.show('auth_error_unauthorized'.tr);
      return true;
    }

    // 跨区预检：二维码携带区域标记且与当前账号所在区域不符时，直接给出
    // 本地化的换址引导，不再发请求（服务端 10011 是同一判断的兜底）。
    final qrRegion = _qrLoginService.qrPayloadRegion(normalized);
    final ownRegion = regionOfApiBaseUrl(_authService.currentApiBaseUrl);
    if (qrRegion != null && ownRegion != null && qrRegion != ownRegion) {
      CustomToast.show(_regionMismatchMessage(ownRegion));
      return true;
    }

    final scanResult = await _qrLoginService.scan(rawPayload: normalized);
    if (!scanResult.ok || scanResult.data == null) {
      CustomToast.show(_scanFailureMessage(scanResult, ownRegion));
      return true;
    }

    final shouldApprove = await _showConfirmDialog();
    if (shouldApprove == null) {
      return true;
    }

    final confirmResult = await _qrLoginService.confirm(
      qrSessionId: scanResult.data!.qrSessionId,
      approve: shouldApprove,
    );
    if (!confirmResult.ok) {
      CustomToast.show(
        confirmResult.message.isEmpty
            ? 'login_qr_confirm_failed'.tr
            : confirmResult.message,
      );
      return true;
    }

    if (shouldApprove) {
      CustomToast.show('login_qr_confirm_success'.tr, isError: false);
    } else {
      CustomToast.show('login_qr_reject_success'.tr, isError: false);
    }
    return true;
  }

  /// 跨区扫码的本地化引导：告知用户应打开自己账号所在区域的网页地址。
  String _regionMismatchMessage(AppRegion? ownRegion) {
    final region = ownRegion ?? AppRegion.global;
    return 'login_qr_scan_region_mismatch'.trParams({
      'region': region == AppRegion.cn ? 'region_cn'.tr : 'region_global'.tr,
      'url': regionWebRootUrl(region),
    });
  }

  /// 扫码失败的本地化文案：已知错误码映射本地词条（保证多语言），
  /// 其余场景沿用服务端消息。
  String _scanFailureMessage(
    ServiceResult<QRLoginScanData> result,
    AppRegion? ownRegion,
  ) {
    switch (result.code) {
      case _kQrScanRegionMismatchCode:
        return _regionMismatchMessage(ownRegion);
      case _kQrScanInvalidCode:
        return 'login_qr_scan_invalid_hint'.tr;
      default:
        return result.message.isEmpty
            ? 'login_qr_scan_failed'.tr
            : result.message;
    }
  }

  Future<bool?> _showConfirmDialog() async {
    final result = await showAppGetDialog<bool>(
      AlertDialog(
        title: Text('login_qr_confirm_title'.tr),
        content: Text('login_qr_confirm_body'.tr),
        actions: [
          TextButton(
            onPressed: () => Get.back(result: false),
            child: Text('login_qr_reject_btn'.tr),
          ),
          TextButton(
            onPressed: () => Get.back(result: true),
            child: Text('login_qr_confirm_btn'.tr),
          ),
        ],
      ),
    );
    return result;
  }
}
