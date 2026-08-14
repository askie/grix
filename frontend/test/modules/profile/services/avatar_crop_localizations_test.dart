import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/profile/services/avatar_crop_localizations.dart';

void main() {
  setUpAll(() {
    Get.addTranslations(AppTranslations().keys);
  });

  tearDownAll(Get.reset);
  test('resolves english avatar crop strings from app translations', () {
    final strings = AvatarCropLocalizations.resolve(
      locale: const Locale('en', 'US'),
    );

    expect(strings.title, 'Crop Avatar');
    expect(strings.hint, 'Drag and zoom image, then save');
    expect(strings.zoomLabel, 'Zoom');
    expect(strings.zoomOutLabel, 'Zoom out');
    expect(strings.zoomInLabel, 'Zoom in');
    expect(strings.cancelLabel, 'Cancel');
    expect(strings.saveLabel, 'Save');
    expect(strings.rotateLeftTooltip, 'Rotate counterclockwise');
    expect(strings.rotateRightTooltip, 'Rotate clockwise');
  });

  test('resolves chinese avatar crop strings from app translations', () {
    final strings = AvatarCropLocalizations.resolve(
      locale: const Locale('zh', 'CN'),
    );

    expect(strings.title, '裁剪头像');
    expect(strings.hint, '拖动图片并缩放，然后点击保存');
    expect(strings.zoomLabel, '缩放');
    expect(strings.zoomOutLabel, '缩小');
    expect(strings.zoomInLabel, '放大');
    expect(strings.cancelLabel, '取消');
    expect(strings.saveLabel, '保存');
    expect(strings.rotateLeftTooltip, '逆时针旋转');
    expect(strings.rotateRightTooltip, '顺时针旋转');
  });
}
