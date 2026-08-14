import 'package:get/get.dart';
import '../controllers/home_controller.dart';
import '../controllers/contacts_controller.dart';
import '../controllers/conversations_controller.dart';
import '../services/friend_qr_flow_service.dart';
import '../services/home_tab_url_sync.dart';
import '../../ai/controllers/agents_controller.dart';
import '../../eggs/controllers/egg_market_controller.dart';
import '../../profile/controllers/profile_controller.dart';
import '../../profile/services/avatar_cropper_service.dart';

class HomeBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<FriendQrFlowService>(() => FriendQrFlowService());
    Get.lazyPut<HomeController>(
      () => HomeController(urlSync: createHomeTabUrlSync()),
    );
    if (!Get.isRegistered<AvatarCropperService>()) {
      Get.put<AvatarCropperService>(AvatarCropperService(), permanent: true);
    }
    Get.lazyPut<AgentsController>(
      () => AgentsController(
        homeTabIndex: Get.find<HomeController>().currentIndex,
      ),
    );
    Get.lazyPut<EggMarketController>(() => EggMarketController());
    Get.lazyPut<ProfileController>(() => ProfileController());
    Get.lazyPut<ContactsController>(() => ContactsController());
    Get.lazyPut<ConversationsController>(() => ConversationsController());
  }
}
