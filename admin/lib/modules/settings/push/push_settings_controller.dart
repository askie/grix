import 'package:get/get.dart';

import '../../../shared/widgets/confirm_dialog.dart';
import 'push_settings_models.dart';
import 'push_settings_service.dart';

/// 离线推送通道开关控制器。
///
/// 四个独立开关：iOS APNs / 安卓 FCM / 网页 WebPush / 极光 JPush。
/// 关闭某通道后，push 服务在 1 分钟内对该通道停止发送（不重启）。
class PushSettingsController extends GetxController {
  final RxBool loading = false.obs;
  final RxBool saving = false.obs;
  final RxnString error = RxnString();

  final RxBool iosApn = false.obs;
  final RxBool androidFcm = false.obs;
  final RxBool webPush = false.obs;
  final RxBool jpush = false.obs;

  bool _loaded = false;
  bool get loaded => _loaded;

  @override
  void onInit() {
    super.onInit();
    load();
  }

  Future<void> load() async {
    loading.value = true;
    error.value = null;
    try {
      final s = await PushSettingsService.get();
      iosApn.value = s.iosApnEnabled;
      androidFcm.value = s.androidFcmEnabled;
      webPush.value = s.webPushEnabled;
      jpush.value = s.jpushEnabled;
      _loaded = true;
    } catch (e) {
      error.value = e.toString();
    } finally {
      loading.value = false;
    }
  }

  Future<void> save() async {
    saving.value = true;
    try {
      await PushSettingsService.update(PushSettings(
        iosApnEnabled: iosApn.value,
        androidFcmEnabled: androidFcm.value,
        webPushEnabled: webPush.value,
        jpushEnabled: jpush.value,
      ));
      Toast.success('推送通道设置已保存，最多 1 分钟生效');
    } catch (e) {
      Toast.error(e.toString());
    } finally {
      saving.value = false;
    }
  }
}
