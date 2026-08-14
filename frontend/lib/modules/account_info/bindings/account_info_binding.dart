import 'package:get/get.dart';

import '../controllers/account_info_controller.dart';

class AccountInfoBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<AccountInfoController>(() => AccountInfoController());
  }
}
