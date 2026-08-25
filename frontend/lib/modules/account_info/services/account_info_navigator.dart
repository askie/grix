import 'package:get/get.dart';

import '../../../app/routes/app_routes.dart';
import '../../chat/services/chat_pane_host.dart';

/// Single entry for opening the account-info page: desktop three-column mode
/// shows it in the right pane while the root navigator stays on home;
/// otherwise it is pushed as a full-screen route.
class AccountInfoNavigator {
  const AccountInfoNavigator._();

  static void open({
    required Map<String, dynamic> arguments,
    required Map<String, String> parameters,
  }) {
    if (AppRoutes.isCurrentHomePath &&
        ChatPaneHost.openAccountInfo(
          arguments: arguments,
          parameters: parameters,
        )) {
      return;
    }
    Get.toNamed(
      AppRoutes.accountInfo,
      arguments: arguments,
      parameters: parameters,
    );
  }
}
