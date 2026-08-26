import 'package:flutter/widgets.dart';
import 'package:get/get.dart';

import '../../../shared/widgets/confirm_dialog.dart';
import 'sms_settings_models.dart';
import 'sms_settings_service.dart';

/// 短信登录注册设置控制器。
///
/// 维持一份脱敏的当前配置 `current`，用户改动通过表单 controller 收集；
/// 保存时构造 patch 提交（ak/sk 字段空串表示保留原值）。
class SmsSettingsController extends GetxController {
  final RxBool loading = false.obs;
  final RxBool saving = false.obs;
  final RxBool testing = false.obs;
  final RxnString error = RxnString();
  final Rxn<SmsSettings> current = Rxn<SmsSettings>();

  /// 四个总开关：注册 / 登录 × CN / Global。
  final RxBool registerCn = false.obs;
  final RxBool registerGlobal = false.obs;
  final RxBool loginCn = false.obs;
  final RxBool loginGlobal = false.obs;

  /// 区号白名单文本框（每行一条；`*` 表示放行全部）。
  final TextEditingController allowedCnCtrl = TextEditingController();
  final TextEditingController allowedGlobalCtrl = TextEditingController();

  /// 阿里云 dysmsapi 配置。
  final TextEditingController aliyunRegionCtrl = TextEditingController();
  final TextEditingController aliyunAkCtrl = TextEditingController();
  final TextEditingController aliyunSkCtrl = TextEditingController();
  final TextEditingController aliyunSignCtrl = TextEditingController();
  final TextEditingController aliyunTplRegCtrl = TextEditingController();
  final TextEditingController aliyunTplLoginCtrl = TextEditingController();
  final TextEditingController aliyunTplResetCtrl = TextEditingController();
  final TextEditingController aliyunTplMarketingCtrl = TextEditingController();
  final TextEditingController aliyunTplNotifyCtrl = TextEditingController();

  /// AWS SNS 配置。
  final TextEditingController awsRegionCtrl = TextEditingController();
  final TextEditingController awsAkCtrl = TextEditingController();
  final TextEditingController awsSkCtrl = TextEditingController();
  final TextEditingController awsSenderCtrl = TextEditingController();

  /// 测试发送的手机号（E.164 含 + 号）。
  final TextEditingController testPhoneCtrl = TextEditingController();

  @override
  void onInit() {
    super.onInit();
    load();
  }

  Future<void> load() async {
    loading.value = true;
    error.value = null;
    try {
      final s = await SmsSettingsService.get();
      current.value = s;
      registerCn.value = s.phoneRegisterEnabledCn;
      registerGlobal.value = s.phoneRegisterEnabledGlobal;
      loginCn.value = s.phoneLoginEnabledCn;
      loginGlobal.value = s.phoneLoginEnabledGlobal;
      allowedCnCtrl.text = s.allowedCountryCodesCn.join('\n');
      allowedGlobalCtrl.text = s.allowedCountryCodesGlobal.join('\n');
      aliyunRegionCtrl.text = s.aliyun.regionId;
      aliyunAkCtrl.clear();
      aliyunSkCtrl.clear();
      aliyunSignCtrl.text = s.aliyun.signName;
      aliyunTplRegCtrl.text = s.aliyun.templateCodeRegister;
      aliyunTplLoginCtrl.text = s.aliyun.templateCodeLogin;
      aliyunTplResetCtrl.text = s.aliyun.templateCodeReset;
      aliyunTplMarketingCtrl.text = s.aliyun.templateCodeMarketing;
      aliyunTplNotifyCtrl.text = s.aliyun.templateCodeNotify;
      awsRegionCtrl.text = s.awsSns.region;
      awsAkCtrl.clear();
      awsSkCtrl.clear();
      awsSenderCtrl.text = s.awsSns.senderId;
    } catch (e) {
      error.value = e.toString();
    } finally {
      loading.value = false;
    }
  }

  Future<void> save() async {
    final cur = current.value;
    if (cur == null) return;
    final patch = SmsSettingsPatch(
      phoneRegisterEnabledCn: registerCn.value,
      phoneRegisterEnabledGlobal: registerGlobal.value,
      phoneLoginEnabledCn: loginCn.value,
      phoneLoginEnabledGlobal: loginGlobal.value,
      allowedCountryCodesCn: _splitLines(allowedCnCtrl.text),
      allowedCountryCodesGlobal: _splitLines(allowedGlobalCtrl.text),
      cnSmsProvider: cur.cnSmsProvider,
      globalSmsProvider: cur.globalSmsProvider,
      aliyun: SmsAliyunPatch(
        regionId: aliyunRegionCtrl.text.trim(),
        accessKeyId: aliyunAkCtrl.text.trim(),
        accessKeySecret: aliyunSkCtrl.text.trim(),
        signName: aliyunSignCtrl.text.trim(),
        templateCodeRegister: aliyunTplRegCtrl.text.trim(),
        templateCodeLogin: aliyunTplLoginCtrl.text.trim(),
        templateCodeReset: aliyunTplResetCtrl.text.trim(),
        templateCodeMarketing: aliyunTplMarketingCtrl.text.trim(),
        templateCodeNotify: aliyunTplNotifyCtrl.text.trim(),
      ),
      awsSns: SmsAwsSnsPatch(
        region: awsRegionCtrl.text.trim(),
        accessKeyId: awsAkCtrl.text.trim(),
        accessKeySecret: awsSkCtrl.text.trim(),
        senderId: awsSenderCtrl.text.trim(),
      ),
    );
    saving.value = true;
    try {
      await SmsSettingsService.update(patch);
      Toast.success('设置已保存');
      await load();
    } catch (e) {
      Toast.error(e.toString());
    } finally {
      saving.value = false;
    }
  }

  Future<void> sendTestCode() async {
    final phone = testPhoneCtrl.text.trim();
    if (phone.isEmpty || !phone.startsWith('+')) {
      Toast.error('请输入 E.164 格式手机号，例如 +8613800138000');
      return;
    }
    testing.value = true;
    try {
      await SmsSettingsService.test(phoneE164: phone);
      Toast.success('测试码已发送到 $phone');
    } catch (e) {
      Toast.error(e.toString());
    } finally {
      testing.value = false;
    }
  }

  List<String> _splitLines(String text) {
    return text
        .split('\n')
        .map((e) => e.trim())
        .where((e) => e.isNotEmpty)
        .toList();
  }

  @override
  void onClose() {
    allowedCnCtrl.dispose();
    allowedGlobalCtrl.dispose();
    aliyunRegionCtrl.dispose();
    aliyunAkCtrl.dispose();
    aliyunSkCtrl.dispose();
    aliyunSignCtrl.dispose();
    aliyunTplRegCtrl.dispose();
    aliyunTplLoginCtrl.dispose();
    aliyunTplResetCtrl.dispose();
    aliyunTplMarketingCtrl.dispose();
    aliyunTplNotifyCtrl.dispose();
    awsRegionCtrl.dispose();
    awsAkCtrl.dispose();
    awsSkCtrl.dispose();
    awsSenderCtrl.dispose();
    testPhoneCtrl.dispose();
    super.onClose();
  }
}
