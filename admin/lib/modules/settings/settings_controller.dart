import 'package:flutter/widgets.dart';
import 'package:get/get.dart';

import '../../shared/widgets/confirm_dialog.dart';
import 'settings_models.dart';
import 'settings_service.dart';

/// 系统设置控制器：认证设置、群组设置。
class SettingsController extends GetxController {
  final RxBool loading = false.obs;
  final RxnString error = RxnString();

  final Rxn<AuthSettings> auth = Rxn<AuthSettings>();
  final RxBool savingAuth = false.obs;

  final TextEditingController customerIdCtrl = TextEditingController();
  final TextEditingController inviteThresholdCtrl = TextEditingController();
  final RxBool savingGroup = false.obs;

  // 语音模型清单
  final RxList<VoiceModelOption> voiceModels = <VoiceModelOption>[].obs;
  final RxList<String> supportedProviders = <String>[].obs;
  final RxBool savingVoiceModels = false.obs;

  @override
  void onInit() {
    super.onInit();
    load();
  }

  Future<void> load() async {
    loading.value = true;
    error.value = null;
    try {
      final bundle = await SettingsService.get();
      auth.value = bundle.auth;
      customerIdCtrl.text = bundle.auth.autoAddCustomerUserId;
      inviteThresholdCtrl.text = '${bundle.group.memberInviteThreshold}';

      final voice = await SettingsService.getVoiceModels();
      voiceModels.assignAll(voice.options);
      supportedProviders.assignAll(voice.supportedProviders);
    } catch (e) {
      error.value = e.toString();
    } finally {
      loading.value = false;
    }
  }

  void addVoiceModel() {
    voiceModels.add(
      VoiceModelOption(
        provider: supportedProviders.isNotEmpty ? supportedProviders.first : '',
      ),
    );
  }

  void removeVoiceModel(int index) {
    if (index < 0 || index >= voiceModels.length) return;
    voiceModels.removeAt(index);
  }

  Future<void> saveVoiceModels() async {
    for (var i = 0; i < voiceModels.length; i++) {
      final o = voiceModels[i];
      if (o.label.trim().isEmpty) {
        Toast.error('第 ${i + 1} 项缺少显示名');
        return;
      }
      if (o.provider.trim().isEmpty) {
        Toast.error('第 ${i + 1} 项缺少供应商');
        return;
      }
      if (o.model.trim().isEmpty) {
        Toast.error('第 ${i + 1} 项缺少模型');
        return;
      }
      final ep = o.endpoint.trim();
      if (!ep.startsWith('ws://') && !ep.startsWith('wss://')) {
        Toast.error('第 ${i + 1} 项接入地址需以 ws:// 或 wss:// 开头');
        return;
      }
    }
    savingVoiceModels.value = true;
    try {
      await SettingsService.updateVoiceModels(voiceModels.toList());
      Toast.success('语音模型清单已保存');
      await load();
    } catch (e) {
      Toast.error(e.toString());
    } finally {
      savingVoiceModels.value = false;
    }
  }

  Future<void> saveAuth() async {
    final a = auth.value;
    if (a == null) return;
    a.autoAddCustomerUserId = customerIdCtrl.text.trim();
    savingAuth.value = true;
    try {
      await SettingsService.updateAuth(a);
      Toast.success('认证设置已保存');
    } catch (e) {
      Toast.error(e.toString());
    } finally {
      savingAuth.value = false;
    }
  }

  Future<void> saveGroup() async {
    final th = int.tryParse(inviteThresholdCtrl.text.trim()) ?? 0;
    if (th <= 0) {
      Toast.error('群成员邀请阈值必须为正整数');
      return;
    }
    savingGroup.value = true;
    try {
      await SettingsService.updateGroup(th);
      Toast.success('群组设置已保存');
    } catch (e) {
      Toast.error(e.toString());
    } finally {
      savingGroup.value = false;
    }
  }

  @override
  void onClose() {
    customerIdCtrl.dispose();
    inviteThresholdCtrl.dispose();
    super.onClose();
  }
}
