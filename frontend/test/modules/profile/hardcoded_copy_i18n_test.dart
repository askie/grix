import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';

void main() {
  setUpAll(() {
    Get.addTranslations(AppTranslations().keys);
  });

  tearDownAll(Get.reset);

  test('widget site key and hidden-file tooltips resolve through i18n', () {
    Get.locale = const Locale('zh', 'CN');
    expect(
      'settings_widget_sites_site_key'.trParams({'key': 'sk_demo'}),
      '站点密钥：sk_demo',
    );
    expect('remote_file_picker_hide_hidden'.tr, '不显示隐藏文件');
    expect('remote_file_picker_show_hidden'.tr, '显示隐藏文件');
    expect('remote_file_picker_err_timeout'.tr, '超时');
    expect('remote_file_picker_root_label'.tr, '根目录');
    expect('remote_file_picker_go_home'.tr, '用户目录');
    expect('device_management_load_failed'.tr, '加载设备列表失败');
    expect('chat_image_editor_load_failed'.tr, '图片加载失败');
    expect('chat_voice_command_release_to_fill'.tr, '正在聆听');
    expect('chat_voice_command_hold_to_talk'.tr, '点击说话');
    expect('chat_voice_command_awaiting'.tr, '正在等待语音命令结果');
    expect('chat_voice_command_hold_hint'.tr, '点击麦克风开始说话');
    expect('chat_voice_command_no_speech'.tr, '没有识别到语音');
    expect(
      'chat_voice_command_start_failed'.trParams({'error': 'boom'}),
      '语音识别启动失败：boom',
    );

    Get.locale = const Locale('en', 'US');
    expect(
      'settings_widget_sites_site_key'.trParams({'key': 'sk_demo'}),
      'Site Key: sk_demo',
    );
    expect('remote_file_picker_hide_hidden'.tr, 'Hide hidden files');
    expect('remote_file_picker_show_hidden'.tr, 'Show hidden files');
    expect('remote_file_picker_err_timeout'.tr, 'Timed out');
    expect('remote_file_picker_root_label'.tr, 'Root');
    expect('remote_file_picker_go_home'.tr, 'Home directory');
    expect('device_management_load_failed'.tr, 'Failed to load devices');
    expect('chat_image_editor_load_failed'.tr, 'Failed to load image');
    expect('chat_voice_command_release_to_fill'.tr, 'Listening');
    expect('chat_voice_command_hold_to_talk'.tr, 'Tap to talk');
    expect(
      'chat_voice_command_awaiting'.tr,
      'Waiting for voice command result',
    );
    expect('chat_voice_command_hold_hint'.tr, 'Tap the microphone to talk');
    expect('chat_voice_command_no_speech'.tr, 'No speech was detected');
    expect(
      'chat_voice_command_start_failed'.trParams({'error': 'boom'}),
      'Failed to start voice recognition: boom',
    );
  });
}
