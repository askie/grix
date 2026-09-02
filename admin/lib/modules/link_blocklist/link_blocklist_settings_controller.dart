import 'package:flutter/widgets.dart';
import 'package:get/get.dart';

import '../../shared/widgets/confirm_dialog.dart';
import 'link_blocklist_models.dart';
import 'link_blocklist_service.dart';

/// 链接黑名单设置控制器。
class LinkBlocklistSettingsController extends GetxController {
  final RxBool loading = false.obs;
  final RxBool saving = false.obs;
  final RxnString error = RxnString();

  final RxBool enabled = true.obs;
  final RxBool externalIntelEnable = false.obs;
  final RxInt maliciousTtlMin = 1440.obs; // 24h
  final RxInt cleanTtlMin = 10.obs;

  final TextEditingController whitelistCtrl = TextEditingController();
  final TextEditingController maliciousTtlCtrl = TextEditingController(
    text: '1440',
  );
  final TextEditingController cleanTtlCtrl = TextEditingController(text: '10');

  @override
  void onInit() {
    super.onInit();
    load();
  }

  Future<void> load() async {
    loading.value = true;
    error.value = null;
    try {
      final s = await LinkBlocklistService.getSettings();
      enabled.value = s.enabled;
      externalIntelEnable.value = s.externalIntelEnable;
      maliciousTtlMin.value = (s.maliciousCacheTtlMs / 60000).round();
      cleanTtlMin.value = (s.cleanCacheTtlMs / 60000).round();
      whitelistCtrl.text = s.ownDomainWhitelist.join('\n');
      maliciousTtlCtrl.text = '${maliciousTtlMin.value}';
      cleanTtlCtrl.text = '${cleanTtlMin.value}';
    } catch (e) {
      error.value = e.toString();
    } finally {
      loading.value = false;
    }
  }

  Future<void> save() async {
    final malTtl = int.tryParse(maliciousTtlCtrl.text.trim()) ?? 0;
    final cleanTtl = int.tryParse(cleanTtlCtrl.text.trim()) ?? 0;
    if (malTtl <= 0 || cleanTtl <= 0) {
      Toast.error('缓存时长必须为正整数（分钟）');
      return;
    }
    final whitelist = whitelistCtrl.text
        .split('\n')
        .map((e) => e.trim())
        .where((e) => e.isNotEmpty)
        .toList();

    saving.value = true;
    try {
      await LinkBlocklistService.updateSettings(
        LinkSafetySettings(
          enabled: enabled.value,
          ownDomainWhitelist: whitelist,
          maliciousCacheTtlMs: malTtl * 60000,
          cleanCacheTtlMs: cleanTtl * 60000,
          externalIntelEnable: externalIntelEnable.value,
        ),
      );
      Toast.success('设置已保存');
    } catch (e) {
      Toast.error(e.toString());
    } finally {
      saving.value = false;
    }
  }

  @override
  void onClose() {
    whitelistCtrl.dispose();
    maliciousTtlCtrl.dispose();
    cleanTtlCtrl.dispose();
    super.onClose();
  }
}
