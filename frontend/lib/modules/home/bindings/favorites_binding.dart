import 'package:get/get.dart';

import '../../../data/providers/user_session_favorite_service.dart';
import '../controllers/favorites_controller.dart';

class FavoritesBinding extends Bindings {
  @override
  void dependencies() {
    if (!Get.isRegistered<UserSessionFavoriteService>()) {
      Get.lazyPut<UserSessionFavoriteService>(
        () => UserSessionFavoriteService(),
        fenix: true,
      );
    }
    Get.lazyPut<FavoritesController>(
      () => FavoritesController(
        service: Get.find<UserSessionFavoriteService>(),
      ),
    );
  }
}
