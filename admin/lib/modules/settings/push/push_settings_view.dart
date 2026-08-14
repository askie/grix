import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../shared/widgets/admin_scaffold.dart';
import '../../../shared/widgets/async_view.dart';
import 'push_settings_controller.dart';

/// 离线推送通道开关设置页。
///
/// 四个独立开关：iOS APNs / 安卓 FCM / 网页 WebPush / 极光 JPush。
/// 关闭某通道后，push 服务在 1 分钟内对该通道停止发送（无需重启）。
class PushSettingsView extends GetView<PushSettingsController> {
  const PushSettingsView({super.key});

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: '推送通道设置',
      actions: [
        IconButton(
          tooltip: '刷新',
          onPressed: controller.load,
          icon: const Icon(Icons.refresh),
        ),
      ],
      body: Obx(
        () => AsyncView(
          loading: controller.loading.value,
          error: controller.error.value,
          isEmpty: !controller.loaded,
          onRetry: controller.load,
          builder: (_) => _Body(c: controller),
        ),
      ),
    );
  }
}

class _Body extends StatelessWidget {
  const _Body({required this.c});
  final PushSettingsController c;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 640),
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            _channelsCard(),
            const SizedBox(height: 20),
            Obx(
              () => FilledButton(
                onPressed: c.saving.value ? null : c.save,
                child: c.saving.value
                    ? const SizedBox(
                        height: 20,
                        width: 20,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Text('保存设置'),
              ),
            ),
            const SizedBox(height: 40),
          ],
        ),
      ),
    );
  }

  Widget _channelsCard() {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('离线推送通道',
                style: TextStyle(fontSize: 15, fontWeight: FontWeight.w700)),
            const SizedBox(height: 4),
            const Text(
              '关闭某通道后，离线消息不再走该通道下发。国内连不上 Google 时，可关掉「安卓 FCM」「网页 WebPush」两个走谷歌的通道，避免无意义的超时重试。改动最多 1 分钟生效，无需重启。',
              style: TextStyle(fontSize: 12, color: Colors.grey),
            ),
            const SizedBox(height: 12),
            Obx(() => SwitchListTile(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('iOS APNs'),
                  subtitle: const Text('苹果推送通知服务'),
                  value: c.iosApn.value,
                  onChanged: (v) => c.iosApn.value = v,
                )),
            const Divider(height: 8),
            Obx(() => SwitchListTile(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('安卓 FCM'),
                  subtitle: const Text('Google Firebase 推送（国内通常不可达）'),
                  value: c.androidFcm.value,
                  onChanged: (v) => c.androidFcm.value = v,
                )),
            const Divider(height: 8),
            Obx(() => SwitchListTile(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('网页 WebPush'),
                  subtitle: const Text('浏览器推送（Chrome 走 Google，国内通常不可达）'),
                  value: c.webPush.value,
                  onChanged: (v) => c.webPush.value = v,
                )),
            const Divider(height: 8),
            Obx(() => SwitchListTile(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('极光 JPush'),
                  subtitle: const Text('安卓第三方推送通道'),
                  value: c.jpush.value,
                  onChanged: (v) => c.jpush.value = v,
                )),
          ],
        ),
      ),
    );
  }
}
