import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../core/config/admin_region.dart';
import 'login_controller.dart';

/// 管理员登录页。
class LoginView extends GetView<LoginController> {
  const LoginView({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      body: Center(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(24),
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 380),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Icon(Icons.shield_moon_outlined,
                    size: 48, color: theme.colorScheme.primary),
                const SizedBox(height: 12),
                Text('塘主',
                    textAlign: TextAlign.center,
                    style: theme.textTheme.titleLarge),
                const SizedBox(height: 24),
                if (AdminRegionStore.shouldShowSelector) ...[
                  _RegionSelector(theme: theme),
                  const SizedBox(height: 20),
                ],
                TextField(
                  controller: controller.usernameCtrl,
                  decoration: const InputDecoration(
                    labelText: '管理员账号',
                    prefixIcon: Icon(Icons.person_outline),
                  ),
                  textInputAction: TextInputAction.next,
                ),
                const SizedBox(height: 16),
                TextField(
                  controller: controller.passwordCtrl,
                  obscureText: true,
                  decoration: const InputDecoration(
                    labelText: '密码',
                    prefixIcon: Icon(Icons.lock_outline),
                  ),
                  textInputAction: TextInputAction.done,
                  onSubmitted: (_) => controller.submit(),
                ),
                const SizedBox(height: 8),
                Obx(() {
                  final err = controller.error.value;
                  if (err == null) return const SizedBox(height: 8);
                  return Padding(
                    padding: const EdgeInsets.symmetric(vertical: 8),
                    child: Text(err,
                        style: TextStyle(color: theme.colorScheme.error)),
                  );
                }),
                const SizedBox(height: 4),
                Obx(() => Align(
                      alignment: Alignment.centerLeft,
                      child: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          SizedBox(
                            height: 36,
                            child: Checkbox(
                              value: controller.rememberCredentials.value,
                              onChanged: (v) => controller
                                  .rememberCredentials.value = v ?? false,
                            ),
                          ),
                          GestureDetector(
                            onTap: () => controller.rememberCredentials.value =
                                !controller.rememberCredentials.value,
                            child: Text('记住账号',
                                style: theme.textTheme.bodySmall),
                          ),
                        ],
                      ),
                    )),
                const SizedBox(height: 8),
                Obx(() => FilledButton(
                      onPressed:
                          controller.loading.value ? null : controller.submit,
                      child: controller.loading.value
                          ? const SizedBox(
                              height: 20,
                              width: 20,
                              child: CircularProgressIndicator(strokeWidth: 2),
                            )
                          : const Text('登录'),
                    )),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// 区域选择器（中国大陆 / 全球）。
class _RegionSelector extends StatelessWidget {
  const _RegionSelector({required this.theme});

  final ThemeData theme;

  @override
  Widget build(BuildContext context) {
    final controller = Get.find<LoginController>();
    return Obx(() {
      final selected = controller.selectedRegion.value;
      return SegmentedButton<AdminRegion>(
        segments: const [
          ButtonSegment(
            value: AdminRegion.cn,
            label: Text('中国大陆'),
            icon: Icon(Icons.flag_outlined),
          ),
          ButtonSegment(
            value: AdminRegion.global,
            label: Text('全球'),
            icon: Icon(Icons.public_outlined),
          ),
        ],
        selected: {selected},
        onSelectionChanged: (set) {
          if (set.isNotEmpty) controller.changeRegion(set.first);
        },
        style: const ButtonStyle(
          visualDensity: VisualDensity.compact,
        ),
      );
    });
  }
}
