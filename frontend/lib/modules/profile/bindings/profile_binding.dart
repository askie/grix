import 'package:get/get.dart';
import '../controllers/profile_controller.dart';
import '../services/avatar_cropper_service.dart';

class ProfileBinding extends Bindings {
  @override
  void dependencies() {
    if (!Get.isRegistered<AvatarCropperService>()) {
      Get.put<AvatarCropperService>(
        AvatarCropperService(),
        permanent: true,
      );
    }
    Get.lazyPut<ProfileController>(() => ProfileController());
  }
}
