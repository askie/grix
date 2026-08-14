import 'package:get/get.dart';

import 'users_controller.dart';

class UsersBinding extends Bindings {
  static const onlineUsersTag = 'online-users';

  UsersBinding({this.onlineOnly = false});

  final bool onlineOnly;

  @override
  void dependencies() {
    Get.lazyPut<UsersController>(
      () => UsersController(onlineOnly: onlineOnly),
      tag: onlineOnly ? onlineUsersTag : null,
    );
  }
}
