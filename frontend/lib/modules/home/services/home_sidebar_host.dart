import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../account_info/account_info_view.dart';
import '../../account_info/controllers/account_info_controller.dart';

/// Desktop three-column mode: the middle column (conversation list etc.) can
/// temporarily show an account-info page on top of the tab content, while the
/// chat pane on the right stays untouched.
///
/// Controllers are created and deleted here explicitly because the page is not
/// a GetX route.
class HomeSidebarHost {
  const HomeSidebarHost._();

  static bool _attached = false;
  static final RxnString _accountInfoTag = RxnString();
  static int _seq = 0;

  static bool get isAvailable => _attached;
  static bool get showsAccountInfo => _accountInfoTag.value != null;
  static RxnString get accountInfoTagRx => _accountInfoTag;

  static void attach() {
    _attached = true;
  }

  /// Called when the slot unmounts (window narrowed or home disposed).
  static void detach() {
    _attached = false;
    final tag = _accountInfoTag.value;
    _accountInfoTag.value = null;
    if (tag != null && Get.isRegistered<AccountInfoController>(tag: tag)) {
      Get.delete<AccountInfoController>(tag: tag);
    }
  }

  /// Show an account-info page in the middle column. Returns false when the
  /// slot is not mounted.
  static bool openAccountInfo({
    required Map<String, dynamic> arguments,
    required Map<String, String?> parameters,
  }) {
    if (!_attached) return false;
    final previousTag = _accountInfoTag.value;
    final tag = 'sidebar_account_info_${++_seq}';
    Get.put<AccountInfoController>(
      AccountInfoController(
        initialArguments: arguments,
        initialParameters: parameters,
      ),
      tag: tag,
    );
    _accountInfoTag.value = tag;
    _deleteAfterFrame(previousTag);
    return true;
  }

  /// Back to the tab content.
  static void closeAccountInfo() {
    final tag = _accountInfoTag.value;
    if (tag == null) return;
    _accountInfoTag.value = null;
    _deleteAfterFrame(tag);
  }

  // Delete after the replaced page has been unmounted so its widgets never
  // observe a closed controller during the swap frame.
  static void _deleteAfterFrame(String? tag) {
    if (tag == null) return;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (tag == _accountInfoTag.value) return;
      if (Get.isRegistered<AccountInfoController>(tag: tag)) {
        Get.delete<AccountInfoController>(tag: tag);
      }
    });
  }
}

/// Middle-column slot: keeps [child] (the tab content) mounted and overlays the
/// account-info page when one is open. Registers with [HomeSidebarHost].
class HomeSidebarSlot extends StatefulWidget {
  const HomeSidebarSlot({super.key, required this.child});

  final Widget child;

  @override
  State<HomeSidebarSlot> createState() => _HomeSidebarSlotState();
}

class _HomeSidebarSlotState extends State<HomeSidebarSlot> {
  @override
  void initState() {
    super.initState();
    HomeSidebarHost.attach();
  }

  @override
  void dispose() {
    HomeSidebarHost.detach();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Obx(() {
      final tag = HomeSidebarHost.accountInfoTagRx.value;
      return Stack(
        fit: StackFit.expand,
        children: [
          widget.child,
          if (tag != null)
            AccountInfoView(
              key: ValueKey(tag),
              controllerTag: tag,
              onBack: HomeSidebarHost.closeAccountInfo,
            ),
        ],
      );
    });
  }
}
