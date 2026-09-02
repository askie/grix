import 'package:get/get.dart';
import 'package:permission_handler/permission_handler.dart';

import '../../../data/providers/deep_link_service.dart';
import '../../../data/providers/qr_login_service.dart';
import '../../../shared/utils/hardware_facade.dart';
import '../../../shared/utils/toast_util.dart';
import '../widgets/my_friend_qr_view.dart';
import '../widgets/user_qr_scanner_view.dart';
import 'qr_login_scan_flow_service.dart';

class FriendQrFlowService {
  FriendQrFlowService({
    DeepLinkService? deepLinkService,
    QrLoginScanFlowService? qrLoginScanFlowService,
  }) : _deepLinkService = deepLinkService,
       _qrLoginScanFlowService = qrLoginScanFlowService;

  final DeepLinkService? _deepLinkService;
  final QrLoginScanFlowService? _qrLoginScanFlowService;

  DeepLinkService? _resolveDeepLinkService() {
    if (_deepLinkService != null) {
      return _deepLinkService;
    }
    if (!Get.isRegistered<DeepLinkService>()) {
      return null;
    }
    return Get.find<DeepLinkService>();
  }

  QrLoginScanFlowService? _resolveQrLoginScanFlowService() {
    if (_qrLoginScanFlowService != null) {
      return _qrLoginScanFlowService;
    }
    if (!Get.isRegistered<QrLoginService>()) {
      return null;
    }
    if (!Get.isRegistered<QrLoginScanFlowService>()) {
      Get.put<QrLoginScanFlowService>(QrLoginScanFlowService());
    }
    return Get.find<QrLoginScanFlowService>();
  }

  Future<void> openUserQrScanner() async {
    final granted = await HardwareFacade.requestPermission(Permission.camera);
    if (!granted) {
      CustomToast.show('conversations_scan_permission_denied'.tr);
      return;
    }

    final scanned = await Get.to<String>(() => const UserQrScannerView());
    final normalized = scanned?.trim() ?? '';
    if (normalized.isEmpty) {
      return;
    }

    final qrLoginFlowService = _resolveQrLoginScanFlowService();
    if (qrLoginFlowService != null) {
      final handledByQrLogin = await qrLoginFlowService.handleScannedText(
        normalized,
      );
      if (handledByQrLogin) {
        return;
      }
    }

    final deepLinkService = _resolveDeepLinkService();
    if (deepLinkService == null) {
      CustomToast.show('common_unknown_error'.tr);
      return;
    }

    final result = await deepLinkService.handleScannedText(normalized);
    switch (result.status) {
      case DeepLinkScanStatus.handled:
      case DeepLinkScanStatus.queued:
        return;
      case DeepLinkScanStatus.invalidCode:
      case DeepLinkScanStatus.unsupported:
        CustomToast.show('conversations_scan_invalid_qr'.tr);
        return;
      case DeepLinkScanStatus.resolveFailed:
        final msg = result.message.trim();
        CustomToast.show(msg.isEmpty ? 'common_unknown_error'.tr : msg);
        return;
    }
  }

  void openMyFriendQr() {
    Get.to<void>(() => const MyFriendQrView());
  }
}
