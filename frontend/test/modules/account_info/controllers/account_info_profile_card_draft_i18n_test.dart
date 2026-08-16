import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';

void main() {
  setUpAll(() {
    Get.addTranslations(AppTranslations().keys);
  });

  tearDownAll(Get.reset);

  test('profile card agent draft keys resolve through i18n', () {
    Get.locale = const Locale('zh', 'CN');
    expect('chat_profile_card_draft_heading'.tr, '联系人名片：');
    expect(
      'chat_profile_card_draft_name'.trParams({'name': '老王'}),
      '昵称：老王',
    );

    Get.locale = const Locale('en', 'US');
    expect('chat_profile_card_draft_heading'.tr, 'Contact card:');
    expect(
      'chat_profile_card_draft_name'.trParams({'name': 'Lao Wang'}),
      'Name: Lao Wang',
    );
  });
}
