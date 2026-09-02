import 'package:flutter/widgets.dart';
import 'package:get/get.dart';

import '../../shared/widgets/confirm_dialog.dart';
import 'moderation_models.dart';
import 'moderation_service.dart';

/// 审查设置控制器：读取与保存开关、阈值、关键词。
class ModerationSettingsController extends GetxController {
  final RxBool loading = false.obs;
  final RxBool saving = false.obs;
  final RxnString error = RxnString();

  final RxBool enabled = false.obs;
  final RxInt threshold = 3.obs;
  final RxList<String> keywords = <String>[].obs;

  final TextEditingController thresholdCtrl = TextEditingController(text: '3');
  final TextEditingController keywordsCtrl = TextEditingController();

  @override
  void onInit() {
    super.onInit();
    load();
  }

  Future<void> load() async {
    loading.value = true;
    error.value = null;
    try {
      final s = await ModerationService.getSettings();
      enabled.value = s.enabled;
      threshold.value = s.humanMuteThreshold;
      keywords.assignAll(s.keywords);
      thresholdCtrl.text = '${s.humanMuteThreshold}';
      keywordsCtrl.text = s.keywords.join('\n');
    } catch (e) {
      error.value = e.toString();
    } finally {
      loading.value = false;
    }
  }

  Future<void> save() async {
    final th = int.tryParse(thresholdCtrl.text.trim()) ?? 0;
    if (th <= 0) {
      Toast.error('累计命中禁言阈值必须为正整数');
      return;
    }
    final kw = keywordsCtrl.text
        .split('\n')
        .map((e) => e.trim())
        .where((e) => e.isNotEmpty)
        .toList();

    saving.value = true;
    try {
      await ModerationService.updateSettings(
        ModerationSettings(
          enabled: enabled.value,
          keywords: kw,
          humanMuteThreshold: th,
        ),
      );
      keywords.assignAll(kw);
      threshold.value = th;
      Toast.success('设置已保存');
    } catch (e) {
      Toast.error(e.toString());
    } finally {
      saving.value = false;
    }
  }

  @override
  void onClose() {
    thresholdCtrl.dispose();
    keywordsCtrl.dispose();
    super.onClose();
  }
}
