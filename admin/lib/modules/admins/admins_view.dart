import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:intl/intl.dart';

import '../../app/theme/app_palette.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/async_view.dart';
import '../roles/role_item.dart';
import '../roles/role_service.dart';
import '../roles/roles_controller.dart';
import 'admin_item.dart';
import 'admins_controller.dart';

/// 管理员页：管理员 + 角色两个 Tab。
class AdminsView extends StatelessWidget {
  const AdminsView({super.key});

  @override
  Widget build(BuildContext context) {
    return DefaultTabController(
      length: 2,
      child: AdminScaffold(
        title: '管理员',
        actions: [
          Builder(
            builder: (context) {
              return IconButton(
                tooltip: '新建',
                onPressed: () {
                  final tabController = DefaultTabController.of(context);
                  if (tabController.index == 1) {
                    _RolesTab.showEditDialog(context, null);
                  } else {
                    _AdminsTab.showCreateDialog(context);
                  }
                },
                icon: const Icon(Icons.add),
              );
            },
          ),
          IconButton(
            tooltip: '刷新',
            onPressed: () {
              Get.find<AdminsController>().load();
              Get.find<RolesController>().load();
            },
            icon: const Icon(Icons.refresh),
          ),
        ],
        bottom: const TabBar(
          tabs: [
            Tab(text: '管理员'),
            Tab(text: '角色管理'),
          ],
        ),
        body: const TabBarView(children: [_AdminsTab(), _RolesTab()]),
      ),
    );
  }
}

// ============ 管理员 Tab ============

class _AdminsTab extends GetView<AdminsController> {
  const _AdminsTab();

  @override
  Widget build(BuildContext context) {
    return Obx(
      () => AsyncView(
        loading: controller.loading.value,
        error: controller.error.value,
        isEmpty: controller.items.isEmpty,
        onRetry: controller.load,
        emptyText: '暂无管理员',
        builder: (_) => ListView.separated(
          padding: const EdgeInsets.all(16),
          itemCount: controller.items.length,
          separatorBuilder: (_, __) => const SizedBox(height: 8),
          itemBuilder: (_, i) =>
              _AdminCard(item: controller.items[i], controller: controller),
        ),
      ),
    );
  }

  static Future<void> showCreateDialog(BuildContext context) async {
    final usernameCtrl = TextEditingController();
    final nicknameCtrl = TextEditingController();
    final passwordCtrl = TextEditingController();
    final confirmCtrl = TextEditingController();
    final formKey = GlobalKey<FormState>();
    final selectedRole = 1.obs; // 1=超管 2=自定义
    final selectedRoleId = RxnString();
    final roles = <RoleItem>[].obs;

    // 预加载角色列表
    RoleService.list().then((list) => roles.assignAll(list)).catchError((_) {});

    await Get.dialog<void>(
      AlertDialog(
        title: const Text('新建管理员'),
        content: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 380),
          child: Form(
            key: formKey,
            child: SingleChildScrollView(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  TextFormField(
                    controller: usernameCtrl,
                    decoration: const InputDecoration(labelText: '账号'),
                    validator: (v) => (v ?? '').trim().isEmpty ? '请输入账号' : null,
                  ),
                  const SizedBox(height: 12),
                  TextFormField(
                    controller: nicknameCtrl,
                    decoration: const InputDecoration(labelText: '昵称'),
                    validator: (v) => (v ?? '').trim().isEmpty ? '请输入昵称' : null,
                  ),
                  const SizedBox(height: 12),
                  TextFormField(
                    controller: passwordCtrl,
                    obscureText: true,
                    decoration: const InputDecoration(labelText: '密码（至少 12 位）'),
                    validator: (v) =>
                        (v ?? '').length < 12 ? '密码至少 12 位' : null,
                  ),
                  const SizedBox(height: 12),
                  TextFormField(
                    controller: confirmCtrl,
                    obscureText: true,
                    decoration: const InputDecoration(labelText: '确认密码'),
                    validator: (v) => v != passwordCtrl.text ? '两次密码不一致' : null,
                  ),
                  const SizedBox(height: 16),
                  Obx(
                    () => Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        DropdownButtonFormField<int>(
                          value: selectedRole.value,
                          decoration: const InputDecoration(labelText: '角色类型'),
                          items: const [
                            DropdownMenuItem(value: 1, child: Text('超级管理员')),
                            DropdownMenuItem(value: 2, child: Text('自定义角色')),
                          ],
                          onChanged: (v) {
                            selectedRole.value = v ?? 1;
                            if (v == 1) selectedRoleId.value = null;
                          },
                        ),
                        if (selectedRole.value == 2 && roles.isNotEmpty) ...[
                          const SizedBox(height: 12),
                          DropdownButtonFormField<String>(
                            value: selectedRoleId.value,
                            decoration: const InputDecoration(
                              labelText: '选择角色',
                            ),
                            validator: (v) =>
                                selectedRole.value == 2 &&
                                    (v == null || v.isEmpty)
                                ? '请选择角色'
                                : null,
                            items: roles
                                .map(
                                  (r) => DropdownMenuItem(
                                    value: r.id,
                                    child: Text(r.name),
                                  ),
                                )
                                .toList(),
                            onChanged: (v) => selectedRoleId.value = v,
                          ),
                        ],
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
        actions: [
          TextButton(onPressed: () => Get.back(), child: const Text('取消')),
          FilledButton(
            onPressed: () async {
              if (!(formKey.currentState?.validate() ?? false)) return;
              try {
                await Get.find<AdminsController>().create(
                  usernameCtrl.text.trim(),
                  nicknameCtrl.text.trim(),
                  passwordCtrl.text,
                  role: selectedRole.value,
                  roleId: selectedRoleId.value,
                );
                Get.back();
              } catch (_) {}
            },
            child: const Text('创建'),
          ),
        ],
      ),
    );
    usernameCtrl.dispose();
    nicknameCtrl.dispose();
    passwordCtrl.dispose();
    confirmCtrl.dispose();
  }
}

class _AdminCard extends StatelessWidget {
  const _AdminCard({required this.item, required this.controller});

  final AdminItem item;
  final AdminsController controller;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final df = DateFormat('yyyy-MM-dd HH:mm');
    final self = controller.isSelf(item);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Row(
          children: [
            CircleAvatar(
              backgroundColor: AppPalette.brandSoft,
              child: const Icon(Icons.person, color: AppPalette.brand),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Wrap(
                    spacing: 6,
                    runSpacing: 4,
                    crossAxisAlignment: WrapCrossAlignment.center,
                    children: [
                      Text(
                        item.displayName,
                        style: theme.textTheme.titleMedium,
                        overflow: TextOverflow.ellipsis,
                      ),
                      _pill(
                        item.roleDisplay,
                        item.isSuperAdmin ? AppPalette.brand : AppPalette.info,
                        item.isSuperAdmin
                            ? AppPalette.brandSoft
                            : AppPalette.infoSoft,
                      ),
                      _pill(
                        item.isActive ? '启用' : '禁用',
                        item.isActive
                            ? AppPalette.success
                            : AppPalette.textSecondary,
                        item.isActive
                            ? AppPalette.successSoft
                            : AppPalette.border,
                      ),
                      if (self)
                        _pill('本人', AppPalette.info, AppPalette.infoSoft),
                    ],
                  ),
                  const SizedBox(height: 2),
                  Text('@${item.username}', style: theme.textTheme.bodySmall),
                  if (item.lastLoginAt != null)
                    Text(
                      '上次登录 ${df.format(item.lastLoginAt!.toLocal())}',
                      style: theme.textTheme.bodySmall,
                    ),
                ],
              ),
            ),
            if (!self)
              PopupMenuButton<String>(
                onSelected: (v) {
                  switch (v) {
                    case 'enable':
                      controller.enable(item);
                    case 'disable':
                      controller.disable(item);
                    case 'delete':
                      controller.remove(item);
                  }
                },
                itemBuilder: (_) => [
                  if (!item.isActive)
                    const PopupMenuItem(value: 'enable', child: Text('启用'))
                  else
                    const PopupMenuItem(value: 'disable', child: Text('禁用')),
                  const PopupMenuItem(
                    value: 'delete',
                    child: Text(
                      '删除',
                      style: TextStyle(color: AppPalette.danger),
                    ),
                  ),
                ],
              ),
          ],
        ),
      ),
    );
  }

  Widget _pill(String text, Color fg, Color bg) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: bg,
        borderRadius: BorderRadius.circular(6),
      ),
      child: Text(
        text,
        style: TextStyle(fontSize: 12, color: fg, fontWeight: FontWeight.w600),
      ),
    );
  }
}

// ============ 角色 Tab ============

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

class _RolesTab extends GetView<RolesController> {
  const _RolesTab();

  @override
  Widget build(BuildContext context) {
    return Obx(() {
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
    });
  }

  Widget _buildList(BuildContext context) {
    return ListView.separated(
      padding: const EdgeInsets.all(16),
      itemCount: controller.roles.length,
      separatorBuilder: (_, __) => const SizedBox(height: 8),
      itemBuilder: (ctx, i) {
        final role = controller.roles[i];
        return Card(
          child: ListTile(
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
                  onPressed: () => showEditDialog(ctx, role),
                ),
                IconButton(
                  icon: const Icon(Icons.delete_outline, size: 20),
                  onPressed: () => _confirmDelete(ctx, role),
                ),
              ],
            ),
          ),
        );
      },
    );
  }

  static void showEditDialog(BuildContext context, RoleItem? existing) {
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
              final c = Get.find<RolesController>();
              bool ok;
              if (existing == null) {
                ok = await c.create(
                  name,
                  descCtrl.text.trim(),
                  selected.toList(),
                );
              } else {
                ok = await c.updateRole(
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

  static void _confirmDelete(BuildContext context, RoleItem role) {
    Get.dialog(
      AlertDialog(
        title: const Text('确认删除'),
        content: Text('确定删除角色「${role.name}」？'),
        actions: [
          TextButton(onPressed: () => Get.back(), child: const Text('取消')),
          FilledButton(
            onPressed: () async {
              Get.back();
              await Get.find<RolesController>().remove(role.id);
            },
            child: const Text('删除'),
          ),
        ],
      ),
    );
  }
}
