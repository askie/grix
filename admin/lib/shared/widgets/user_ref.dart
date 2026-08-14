import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/theme/app_palette.dart';
import '../controllers/user_directory.dart';
import 'user_detail_sheet.dart';

/// 用户引用组件：把用户 ID 渲染成"头像 + 昵称"，点击弹出用户详情卡。
///
/// 任何页面拿到一个用户 ID 就能用：
/// ```dart
/// UserRef(wallet.ownerId)                    // 头像+昵称，可点击
/// UserRef(id, showId: true)                  // 追加显示 ID
/// UserRef(id, placeholderName: item.name)    // 解析完成前先显示已知名字
/// ```
/// 昵称经全局 [UserDirectory] 批量解析并缓存，同屏多个 ID 合并成一次请求。
class UserRef extends StatelessWidget {
  const UserRef(
    this.userId, {
    super.key,
    this.showId = false,
    this.placeholderName = '',
    this.style,
    this.tappable = true,
  });

  final String userId;

  /// 昵称后追加 `(ID xxx)`，用于需要核对 ID 的场景。
  final bool showId;

  /// 解析完成前的占位名（调用方数据里已有的昵称/账号）。
  final String placeholderName;

  final TextStyle? style;
  final bool tappable;

  @override
  Widget build(BuildContext context) {
    final id = userId.trim();
    if (id.isEmpty) return Text('-', style: style);

    return Obx(() {
      final user = UserDirectory.instance.resolve(id);

      // 解析优先级：目录昵称 → 调用方占位名 → 裸 ID 兜底。
      String name;
      if (user != null) {
        name = user.displayName;
      } else if (placeholderName.isNotEmpty) {
        name = placeholderName;
      } else {
        name = id;
      }
      final label = showId ? '$name（ID $id）' : name;

      final baseStyle = style ?? DefaultTextStyle.of(context).style;
      final avatarSize = (baseStyle.fontSize ?? 14) + 6;

      final content = Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          _Avatar(
            name: name,
            avatarUrl: user?.avatarUrl ?? '',
            size: avatarSize,
            banned: user?.isBanned ?? false,
          ),
          const SizedBox(width: 5),
          Flexible(
            child: Text(
              label,
              style: user?.isBanned == true
                  ? baseStyle.copyWith(
                      color: AppPalette.danger,
                      decoration: TextDecoration.lineThrough,
                    )
                  : baseStyle,
              overflow: TextOverflow.ellipsis,
            ),
          ),
        ],
      );

      if (!tappable) return content;
      return InkWell(
        borderRadius: BorderRadius.circular(6),
        onTap: () => UserDetailSheet.show(id),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 2, vertical: 1),
          child: content,
        ),
      );
    });
  }
}

class _Avatar extends StatelessWidget {
  const _Avatar({
    required this.name,
    required this.avatarUrl,
    required this.size,
    required this.banned,
  });

  final String name;
  final String avatarUrl;
  final double size;
  final bool banned;

  @override
  Widget build(BuildContext context) {
    final initial = name.isEmpty ? '?' : String.fromCharCode(name.runes.first);
    return CircleAvatar(
      radius: size / 2,
      backgroundColor: banned ? AppPalette.dangerSoft : AppPalette.brandSoft,
      foregroundImage: avatarUrl.isNotEmpty ? NetworkImage(avatarUrl) : null,
      child: Text(
        initial,
        style: TextStyle(
          fontSize: size * 0.5,
          color: banned ? AppPalette.danger : AppPalette.brandDark,
        ),
      ),
    );
  }
}
