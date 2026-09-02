import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/shared/utils/toast_util.dart';

void main() {
  setUpAll(() {
    Get.addTranslations(AppTranslations().keys);
  });

  tearDownAll(Get.reset);

  test('userFacingError strips Dart prefixes and skips Dio dumps', () {
    expect(
      userFacingError(Exception('im_skill_upload_timeout')),
      'im_skill_upload_timeout',
    );
    expect(
      userFacingError(
        const FormatException('chat_audit_detail_response_malformed'),
      ),
      'chat_audit_detail_response_malformed',
    );
    expect(
      userFacingError(
        DioException(
          requestOptions: RequestOptions(path: '/'),
          type: DioExceptionType.connectionTimeout,
        ),
        fallback: 'common_error',
      ),
      'common_error',
    );
    expect(
      userFacingError(StateError('boom'), fallback: 'common_error'),
      'common_error',
    );
  });

  test('update downloading toast resolves through i18n', () {
    Get.locale = const Locale('zh', 'CN');
    expect('update_downloading'.tr, '正在下载...');
    Get.locale = const Locale('en', 'US');
    expect('update_downloading'.tr, 'Downloading...');
  });
}
