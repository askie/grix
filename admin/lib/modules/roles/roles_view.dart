import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../shared/widgets/admin_scaffold.dart';
import 'role_item.dart';
import 'roles_controller.dart';

/// 可授权的权限 key 及其显示名（与后端 AssignablePermissionKeys 对齐）。
const Map<String, String> kPermissionLabels = {
  'users': '用户管理',
  'eggs': '虾蛋运营',
  'reports': '举报管理',
  'moderation': '内容审查',
  'visitor_bans': '访客封禁',
  'link_blocklist': '链接黑名单',
  'connector': '插件管理',
  'app': 'App 版本管理',
  'feature_gates': 'Feature Gates',
  'settings': '系统设置',
  'gateway_billing': '计费网关',
};

class RolesView extends GetView<RolesController> {
  const RolesView({super.key});

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: '角色管理',
      actions: [
        LayoutBuilder(
          builder: (context, _) {
            final narrow = MediaQuery.of(context).size.width < 900;
            if (narrow) {
              return IconButton(
                tooltip: '新建角色',
                onPressed: () => _showEditDialog(context, null),
                icon: const Icon(Icons.add),
              );
            }
            return FilledButton.icon(
              onPressed: () => _showEditDialog(context, null),
              icon: const Icon(Icons.add, size: 18),
              label: const Text('新建角色'),
            );
          },
        ),
      ],
      body: Obx(() {
        if (controller.isLoading.value) {
          return const Center(child: CircularProgressIndicator());
        }
        if (controller.error.isNotEmpty) {
          return Center(child: Text(controller.error.value));
        }
        if (controller.roles.isEmpty) {
          return const Center(child: Text('暂无角色'));
        }
        return _buildList(context);
      }),
    );
  }

  Widget _buildList(BuildContext context) {
    return ListView.separated(
      padding: const EdgeInsets.all(20),
      itemCount: controller.roles.length,
      separatorBuilder: (_, __) => const Divider(height: 1),
      itemBuilder: (ctx, i) {
        final role = controller.roles[i];
        return ListTile(
          title: Text(role.name),
          subtitle: Text(
            role.permissions.map((k) => kPermissionLabels[k] ?? k).join('、'),
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
          ),
          trailing: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              IconButton(
                icon: const Icon(Icons.edit_outlined, size: 20),
                onPressed: () => _showEditDialog(ctx, role),
              ),
              IconButton(
                icon: const Icon(Icons.delete_outline, size: 20),
                onPressed: () => _confirmDelete(ctx, role),
              ),
            ],
          ),
        );
      },
    );
  }

  void _showEditDialog(BuildContext context, RoleItem? existing) {
    final nameCtrl = TextEditingController(text: existing?.name ?? '');
    final descCtrl = TextEditingController(text: existing?.description ?? '');
    final selected = <String>{...?existing?.permissions}.obs;

    Get.dialog(
      AlertDialog(
        title: Text(existing == null ? '新建角色' : '编辑角色'),
        content: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 400),
          child: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                TextField(
                  controller: nameCtrl,
                  decoration: const InputDecoration(labelText: '角色名称'),
                ),
                const SizedBox(height: 12),
                TextField(
                  controller: descCtrl,
                  decoration: const InputDecoration(labelText: '描述（可选）'),
                ),
                const SizedBox(height: 16),
                Align(
                  alignment: Alignment.centerLeft,
                  child: Text(
                    '权限',
                    style: Theme.of(context).textTheme.titleSmall,
                  ),
                ),
                Obx(
                  () => Column(
                    children: kPermissionLabels.entries.map((e) {
                      return CheckboxListTile(
                        dense: true,
                        title: Text(e.value),
                        value: selected.contains(e.key),
                        onChanged: (v) {
                          if (v == true) {
                            selected.add(e.key);
                          } else {
                            selected.remove(e.key);
                          }
                        },
                      );
                    }).toList(),
                  ),
                ),
              ],
            ),
          ),
        ),
        actions: [
          TextButton(onPressed: () => Get.back(), child: const Text('取消')),
          FilledButton(
            onPressed: () async {
              final name = nameCtrl.text.trim();
              if (name.isEmpty) return;
              bool ok;
              if (existing == null) {
                ok = await controller.create(
                  name,
                  descCtrl.text.trim(),
                  selected.toList(),
                );
              } else {
                ok = await controller.updateRole(
                  existing.id,
                  name,
                  descCtrl.text.trim(),
                  selected.toList(),
                );
              }
              if (ok) Get.back();
            },
            child: const Text('保存'),
          ),
        ],
      ),
    );
  }

  void _confirmDelete(BuildContext context, RoleItem role) {
    Get.dialog(
      AlertDialog(
        title: const Text('确认删除'),
        content: Text('确定删除角色「${role.name}」？'),
        actions: [
          TextButton(onPressed: () => Get.back(), child: const Text('取消')),
          FilledButton(
            onPressed: () async {
              Get.back();
              await controller.remove(role.id);
            },
            child: const Text('删除'),
          ),
        ],
      ),
    );
  }
}
