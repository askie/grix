import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../shared/widgets/app_dialog_style.dart';
import 'controllers/agent_category_manage_controller.dart';

class AgentCategoryManageView extends StatefulWidget {
  const AgentCategoryManageView({super.key});

  @override
  State<AgentCategoryManageView> createState() => _AgentCategoryManageViewState();
}

class _AgentCategoryManageViewState extends State<AgentCategoryManageView> {
  final controller = Get.put(AgentCategoryManageController());

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text('ai_agent_category_manage'.tr),
        actions: [
          IconButton(
            icon: const Icon(Icons.add),
            tooltip: 'ai_agent_category_add_top_level'.tr,
            onPressed: () => controller.showEditDialog(context, parentId: '0'),
          ),
        ],
      ),
      body: Obx(() {
        if (controller.isLoading.value && controller.treeNodes.isEmpty) {
          return const Center(child: CircularProgressIndicator());
        }

        final nodes = controller.flatNodes;
        if (nodes.isEmpty) {
          return Center(
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                Icon(Icons.folder_open, size: 64, color: Colors.grey.withValues(alpha: 0.5)),
                const SizedBox(height: 16),
                Text('ai_agent_category_empty'.tr, style: const TextStyle(color: Colors.grey)),
                const SizedBox(height: 16),
                ElevatedButton.icon(
                  onPressed: () => controller.showEditDialog(context, parentId: '0'),
                  icon: const Icon(Icons.add),
                  label: Text('ai_agent_category_create'.tr),
                ),
              ],
            ),
          );
        }

        return RefreshIndicator(
          onRefresh: controller.refreshData,
          child: ListView.builder(
            padding: const EdgeInsets.symmetric(vertical: 8),
            itemCount: nodes.length,
            itemBuilder: (context, index) {
              final node = nodes[index];
              return _buildCategoryItem(context, node);
            },
          ),
        );
      }),
    );
  }

  Widget _buildCategoryItem(BuildContext context, CategoryNode node) {
    return InkWell(
      onTap: () {},
      child: Container(
        padding: EdgeInsets.only(
          left: 16.0 + (node.depth * 24.0),
          right: 8.0,
          top: 12.0,
          bottom: 12.0,
        ),
        child: Row(
          children: [
            Icon(
              node.depth == 0 ? Icons.folder : Icons.folder_open,
              color: Theme.of(context).primaryColor,
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Text(
                node.model.name,
                style: const TextStyle(fontSize: 16),
              ),
            ),
            PopupMenuButton<String>(
              icon: const Icon(Icons.more_vert),
              onSelected: (value) {
                if (value == 'add_child') {
                  controller.showEditDialog(context, parentId: node.model.id);
                } else if (value == 'edit') {
                  controller.showEditDialog(context, category: node.model);
                } else if (value == 'delete') {
                  _showDeleteConfirm(context, node);
                }
              },
              itemBuilder: (context) => [
                PopupMenuItem(
                  value: 'add_child',
                  child: Row(
                    children: [
                      const Icon(Icons.add, size: 20),
                      const SizedBox(width: 8),
                      Text('ai_agent_category_add_child'.tr),
                    ],
                  ),
                ),
                PopupMenuItem(
                  value: 'edit',
                  child: Row(
                    children: [
                      const Icon(Icons.edit, size: 20),
                      const SizedBox(width: 8),
                      Text('ai_agent_category_rename'.tr),
                    ],
                  ),
                ),
                PopupMenuItem(
                  value: 'delete',
                  child: Row(
                    children: [
                      const Icon(Icons.delete, size: 20, color: Colors.red),
                      const SizedBox(width: 8),
                      Text('ai_agent_category_delete'.tr, style: const TextStyle(color: Colors.red)),
                    ],
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  void _showDeleteConfirm(BuildContext context, CategoryNode node) async {
    final ok = await showAppConfirmDialog(
      context: context,
      title: 'ai_agent_category_delete'.tr,
      message: 'ai_agent_category_delete_confirm'.trParams({'name': node.model.name}),
      confirmText: 'common_delete'.tr,
      isDestructive: true,
    );
    if (ok) {
      controller.deleteCategory(node.model.id);
    }
  }
}
