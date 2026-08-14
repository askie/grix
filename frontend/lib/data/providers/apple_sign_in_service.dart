import 'package:flutter/services.dart';
import 'package:get/get.dart';

import 'auth_service.dart';

class AppleSignInService {
  static const MethodChannel _channel = MethodChannel(
    'pub.dhf.grix/apple_sign_in',
  );

  bool get isSupported => GetPlatform.isIOS || GetPlatform.isMacOS;

  Future<ServiceResult<String>> signIn() async {
    if (!isSupported) {
      return ServiceResult<String>.failure(
        message: 'login_apple_unsupported'.tr,
      );
    }

    try {
      final response = await _channel.invokeMapMethod<String, dynamic>(
        'signInWithApple',
      );
      final idToken = response?['idToken']?.toString().trim() ?? '';
      if (idToken.isEmpty) {
        return ServiceResult<String>.failure(
          message: 'login_apple_error_failed'.tr,
        );
      }
      return ServiceResult<String>.success(data: idToken);
    } on PlatformException catch (error) {
      return ServiceResult<String>.failure(
        message: _messageForPlatformError(error),
      );
    } catch (_) {
      return ServiceResult<String>.failure(
        message: 'login_apple_error_failed'.tr,
      );
    }
  }

  String _messageForPlatformError(PlatformException error) {
    switch (error.code.trim()) {
      case 'sign_in_cancelled':
        return 'login_apple_cancelled'.tr;
      case 'unsupported_platform':
        return 'login_apple_unsupported'.tr;
      default:
        return error.message?.trim().isNotEmpty == true
            ? error.message!.trim()
            : 'login_apple_error_failed'.tr;
    }
  }
}
