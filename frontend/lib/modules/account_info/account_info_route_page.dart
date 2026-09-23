import 'package:flutter/material.dart';
import 'package:get/get.dart';

import 'account_info_view.dart';
import 'controllers/account_info_controller.dart';

/// Route entry for the account-info page.
///
/// Every pushed route gets its own tagged [AccountInfoController] whose
/// lifetime is bound to this widget, not to GetX route bookkeeping:
///
/// * an untagged lazy singleton is reused by every later `/account-info`
///   push while one is still registered, so a second profile pushed above
///   the first (profile -> chat -> another member's profile) showed the
///   first peer;
/// * GetX links a lazily created controller to whichever route was most
///   recently pushed when the page first builds; if a non-GetX route
///   (Flutter dialog, sheet, menu) was pushed or popped in the same tick,
///   the controller was linked to a route that never reports disposal and
///   leaked, after which every profile page showed that stale peer.
///
/// The controller is registered as permanent so GetX never deletes it on
/// its own; [dispose] removes it explicitly.
class AccountInfoRoutePage extends StatefulWidget {
  const AccountInfoRoutePage({super.key});

  @override
  State<AccountInfoRoutePage> createState() => _AccountInfoRoutePageState();
}

class _AccountInfoRoutePageState extends State<AccountInfoRoutePage> {
  static int _seq = 0;

  String? _tag;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_tag != null) return;
    final tag = 'route_account_info_${++_seq}';
    final route = ModalRoute.of(context);
    Get.put<AccountInfoController>(
      AccountInfoController(
        initialArguments: _readArguments(route?.settings.arguments),
        initialParameters: _readParameters(route?.settings.name),
      ),
      tag: tag,
      permanent: true,
    );
    _tag = tag;
  }

  @override
  void dispose() {
    final tag = _tag;
    if (tag != null && Get.isRegistered<AccountInfoController>(tag: tag)) {
      Get.delete<AccountInfoController>(tag: tag, force: true);
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AccountInfoView(controllerTag: _tag);
  }

  static Map<String, dynamic> _readArguments(Object? raw) {
    if (raw is Map<String, dynamic>) return raw;
    if (raw is Map) {
      return raw.map((key, value) => MapEntry(key.toString(), value));
    }
    return const <String, dynamic>{};
  }

  /// `Get.parameters` is a single global overwritten by every route
  /// generation, so read this route's own query string instead.
  static Map<String, String?> _readParameters(String? routeName) {
    final name = routeName?.trim() ?? '';
    if (name.isNotEmpty) {
      final query = Uri.tryParse(name)?.queryParameters;
      if (query != null && query.isNotEmpty) return query;
    }
    return Get.parameters;
  }
}
