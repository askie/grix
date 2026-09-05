import 'package:flutter/material.dart';
import 'package:flutter_svg/flutter_svg.dart';
import 'package:get/get.dart';

import '../../../shared/utils/app_region_config.dart';

/// 区域切换按钮，用于注册/登录/重置密码页面。
/// [selectedRegion] 当前选中区域（响应式）。
/// [onChanged] 用户切换区域时的回调。
class RegionSwitcher extends StatelessWidget {
  const RegionSwitcher({
    super.key,
    required this.selectedRegion,
    required this.onChanged,
    this.compact = false,
  });

  final Rx<AppRegion> selectedRegion;
  final ValueChanged<AppRegion> onChanged;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final horizontalPadding = compact ? 10.0 : 12.0;
    final verticalPadding = compact ? 6.0 : 8.0;

    return Obx(() {
      final region = selectedRegion.value;
      final label = region == AppRegion.cn
          ? 'region_cn'.tr
          : 'region_global'.tr;

      return InkWell(
        borderRadius: BorderRadius.circular(20),
        onTap: () => _showPicker(context),
        child: Container(
          padding: EdgeInsets.symmetric(
            horizontal: horizontalPadding,
            vertical: verticalPadding,
          ),
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(20),
            border: Border.all(
              color: theme.colorScheme.outline.withValues(alpha: 0.4),
            ),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              _RegionIcon(region: region, height: 10),
              const SizedBox(width: 6),
              Text(
                label,
                style: theme.textTheme.bodySmall?.copyWith(
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(width: 2),
              const Icon(Icons.arrow_drop_down_rounded, size: 16),
            ],
          ),
        ),
      );
    });
  }

  void _showPicker(BuildContext context) {
    showModalBottomSheet<void>(
      context: context,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const SizedBox(height: 12),
            Container(
              width: 36,
              height: 4,
              decoration: BoxDecoration(
                color: Theme.of(ctx).colorScheme.outline.withValues(alpha: 0.3),
                borderRadius: BorderRadius.circular(2),
              ),
            ),
            const SizedBox(height: 8),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
              child: Text(
                'region_select_title'.tr,
                style: Theme.of(ctx).textTheme.titleSmall,
              ),
            ),
            Obx(() {
              final current = selectedRegion.value;
              return Column(
                children: [
                  ListTile(
                    leading: const _RegionIcon(
                      region: AppRegion.cn,
                      height: 14,
                    ),
                    title: Text('region_cn'.tr),
                    subtitle: Text('region_cn_desc'.tr),
                    trailing: current == AppRegion.cn
                        ? Icon(
                            Icons.check_rounded,
                            color: Theme.of(ctx).primaryColor,
                          )
                        : null,
                    onTap: () {
                      Navigator.of(ctx).pop();
                      onChanged(AppRegion.cn);
                    },
                  ),
                  ListTile(
                    leading: const _RegionIcon(
                      region: AppRegion.global,
                      height: 14,
                    ),
                    title: Text('region_global'.tr),
                    subtitle: Text('region_global_desc'.tr),
                    trailing: current == AppRegion.global
                        ? Icon(
                            Icons.check_rounded,
                            color: Theme.of(ctx).primaryColor,
                          )
                        : null,
                    onTap: () {
                      Navigator.of(ctx).pop();
                      onChanged(AppRegion.global);
                    },
                  ),
                ],
              );
            }),
            const SizedBox(height: 8),
          ],
        ),
      ),
    );
  }
}

/// 区域图标，不依赖系统 emoji 字体：国内用内置矢量国旗，海外用 Material 图标。
class _RegionIcon extends StatelessWidget {
  const _RegionIcon({required this.region, required this.height});

  final AppRegion region;
  final double height;

  @override
  Widget build(BuildContext context) {
    if (region == AppRegion.cn) {
      return ClipRRect(
        borderRadius: BorderRadius.circular(2),
        child: SvgPicture.asset(
          'assets/icons/region_cn.svg',
          width: height * 1.5,
          height: height,
        ),
      );
    }
    return Icon(
      Icons.public,
      size: height * 1.5,
      color: Theme.of(context).colorScheme.onSurfaceVariant,
    );
  }
}
