import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';

void main() {
  setUpAll(() {
    Get.addTranslations(AppTranslations().keys);
  });

  tearDownAll(Get.reset);

  test('egg market fallback errors resolve through i18n', () {
    Get.locale = const Locale('zh', 'CN');
    expect('eggs_pond_invalid_response'.tr, '响应无效');
    expect('eggs_pond_request_failed'.tr, '请求失败');

    Get.locale = const Locale('en', 'US');
    expect('eggs_pond_invalid_response'.tr, 'Invalid response');
    expect('eggs_pond_request_failed'.tr, 'Request failed');
  });
}
