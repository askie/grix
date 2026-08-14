import 'package:get/get.dart';

import '../../../data/providers/apple_sign_in_service.dart';
import '../../../data/providers/google_sign_in_service.dart';
import '../controllers/login_controller.dart';
import '../controllers/phone_login_controller.dart';
import '../controllers/qr_login_controller.dart';
import '../controllers/register_controller.dart';
import '../controllers/reset_password_controller.dart';
import '../controllers/splash_controller.dart';

class SplashBinding extends Bindings {
  @override
  void dependencies() {
    Get.put<SplashController>(SplashController());
  }
}

class LoginBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<GoogleSignInService>(() => GoogleSignInService());
    Get.lazyPut<AppleSignInService>(() => AppleSignInService());
    Get.lazyPut<LoginController>(() => LoginController());
    Get.lazyPut<QrLoginController>(() => QrLoginController());
  }
}

class RegisterBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<RegisterController>(() => RegisterController());
  }
}

class ResetPasswordBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<ResetPasswordController>(() => ResetPasswordController());
  }
}

class PhoneLoginBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<PhoneLoginController>(() => PhoneLoginController());
  }
}
