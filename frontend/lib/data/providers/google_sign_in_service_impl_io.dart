import 'package:flutter/services.dart';
import 'package:get/get.dart';

import 'auth_service.dart';

class GoogleSignInServiceImpl {
  static const MethodChannel _channel = MethodChannel(
    'pub.dhf.grix/google_sign_in',
  );

  bool get isSupported => GetPlatform.isAndroid || GetPlatform.isIOS;

  Future<ServiceResult<String>> signIn() async {
    if (!isSupported) {
      return ServiceResult<String>.failure(
        message: 'login_google_unsupported'.tr,
      );
    }

    try {
      final response = await _channel.invokeMapMethod<String, dynamic>(
        'signInWithGoogle',
      );
      final idToken = response?['idToken']?.toString().trim() ?? '';
      if (idToken.isEmpty) {
        return ServiceResult<String>.failure(
          message: 'login_google_error_failed'.tr,
        );
      }
      return ServiceResult<String>.success(data: idToken);
    } on PlatformException catch (error) {
      return ServiceResult<String>.failure(
        message: _messageForPlatformError(error),
      );
    } catch (_) {
      return ServiceResult<String>.failure(
        message: 'login_google_error_failed'.tr,
      );
    }
  }

  String _messageForPlatformError(PlatformException error) {
    switch (error.code.trim()) {
      case 'google_config_missing':
        return 'login_google_not_configured'.tr;
      case 'sign_in_cancelled':
        return 'login_google_cancelled'.tr;
      case 'unsupported_platform':
        return 'login_google_unsupported'.tr;
      default:
        return error.message?.trim().isNotEmpty == true
            ? error.message!.trim()
            : 'login_google_error_failed'.tr;
    }
  }
}

GoogleSignInServiceImpl createGoogleSignInServiceImpl() {
  return GoogleSignInServiceImpl();
}
