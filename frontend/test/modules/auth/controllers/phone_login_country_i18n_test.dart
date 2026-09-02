import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/auth/controllers/phone_login_controller.dart';

void main() {
  setUpAll(() {
    Get.addTranslations(AppTranslations().keys);
  });

  tearDownAll(Get.reset);

  test('country dropdown names resolve through i18n', () {
    Get.locale = const Locale('zh', 'CN');
    expect(PhoneLoginController.commonCountries.first.nameKey.tr, '中国大陆');

    Get.locale = const Locale('en', 'US');
    expect(
      PhoneLoginController.commonCountries.first.nameKey.tr,
      'Mainland China',
    );
  });
}
