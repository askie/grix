import 'package:flutter/widgets.dart';
import 'package:get/get.dart';

import '../../../shared/widgets/confirm_dialog.dart';
import 'pay_channel_settings_models.dart';
import 'pay_channel_settings_service.dart';

/// 支付通道（支付宝 / PayPal）设置控制器。
///
/// 维持一份脱敏的当前配置 `current`，用户改动通过表单 controller 收集；
/// 保存时构造 patch 提交（私钥 / Secret 字段空串表示保留原值）。
/// "测试连接" 用的是上一次已保存的凭证，不是表单里尚未保存的内容——必须先保存再测试。
class PayChannelSettingsController extends GetxController {
  final RxBool loading = false.obs;
  final RxBool saving = false.obs;
  final RxBool testingAlipay = false.obs;
  final RxBool testingPaypal = false.obs;
  final RxnString error = RxnString();
  final Rxn<PayChannelSettings> current = Rxn<PayChannelSettings>();

  /// 支付宝。
  final RxBool alipayEnabled = false.obs;
  final RxBool alipaySandbox = true.obs;
  final TextEditingController alipayAppIdCtrl = TextEditingController();
  final TextEditingController alipayPrivateKeyCtrl = TextEditingController();
  final TextEditingController alipayPublicKeyCtrl = TextEditingController();

  /// PayPal。
  final RxBool paypalEnabled = false.obs;
  final RxBool paypalSandbox = true.obs;
  final TextEditingController paypalClientIdCtrl = TextEditingController();
  final TextEditingController paypalClientSecretCtrl = TextEditingController();
  final TextEditingController paypalWebhookIdCtrl = TextEditingController();

  @override
  void onInit() {
    super.onInit();
    load();
  }

  Future<void> load() async {
    loading.value = true;
    error.value = null;
    try {
      final s = await PayChannelSettingsService.get();
      current.value = s;
      alipayEnabled.value = s.alipay.enabled;
      alipaySandbox.value = s.alipay.sandbox;
      alipayAppIdCtrl.text = s.alipay.appId;
      alipayPrivateKeyCtrl.clear();
      alipayPublicKeyCtrl.clear();
      paypalEnabled.value = s.paypal.enabled;
      paypalSandbox.value = s.paypal.sandbox;
      paypalClientIdCtrl.text = s.paypal.clientId;
      paypalClientSecretCtrl.clear();
      paypalWebhookIdCtrl.text = s.paypal.webhookId;
    } catch (e) {
      error.value = e.toString();
    } finally {
      loading.value = false;
    }
  }

  Future<void> save() async {
    final cur = current.value;
    if (cur == null) return;
    final patch = PayChannelSettingsPatch(
      alipay: PayAlipayPatch(
        enabled: alipayEnabled.value,
        sandbox: alipaySandbox.value,
        appId: alipayAppIdCtrl.text.trim(),
        privateKey: alipayPrivateKeyCtrl.text.trim(),
        alipayPublicKey: alipayPublicKeyCtrl.text.trim(),
        signType: cur.alipay.signType,
      ),
      paypal: PayPaypalPatch(
        enabled: paypalEnabled.value,
        sandbox: paypalSandbox.value,
        clientId: paypalClientIdCtrl.text.trim(),
        clientSecret: paypalClientSecretCtrl.text.trim(),
        webhookId: paypalWebhookIdCtrl.text.trim(),
      ),
    );
    saving.value = true;
    try {
      await PayChannelSettingsService.update(patch);
      Toast.success('设置已保存');
      await load();
    } catch (e) {
      Toast.error(e.toString());
    } finally {
      saving.value = false;
    }
  }

  Future<void> testAlipay() async {
    testingAlipay.value = true;
    try {
      await PayChannelSettingsService.test('alipay');
      Toast.success('支付宝凭证校验通过');
    } catch (e) {
      Toast.error(e.toString());
    } finally {
      testingAlipay.value = false;
    }
  }

  Future<void> testPaypal() async {
    testingPaypal.value = true;
    try {
      await PayChannelSettingsService.test('paypal');
      Toast.success('PayPal 凭证校验通过');
    } catch (e) {
      Toast.error(e.toString());
    } finally {
      testingPaypal.value = false;
    }
  }

  @override
  void onClose() {
    alipayAppIdCtrl.dispose();
    alipayPrivateKeyCtrl.dispose();
    alipayPublicKeyCtrl.dispose();
    paypalClientIdCtrl.dispose();
    paypalClientSecretCtrl.dispose();
    paypalWebhookIdCtrl.dispose();
    super.onClose();
  }
}
