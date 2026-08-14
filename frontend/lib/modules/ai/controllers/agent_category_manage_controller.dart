import 'package:flutter/material.dart';
import 'package:get/get.dart';
import '../../../data/providers/agent_category_service.dart';
import '../../../shared/utils/toast_util.dart';

class CategoryNode {
  CategoryNode({
    required this.model,
    required this.children,
    required this.depth,
  });
  final AgentCategoryModel model;
  final List<CategoryNode> children;
  final int depth;
}

class AgentCategoryManageController extends GetxController {
  AgentCategoryManageController({AgentCategoryService? service})
    : _service = service ?? Get.find<AgentCategoryService>();

  final AgentCategoryService _service;
  final isLoading = false.obs;

  @override
  void onInit() {
    super.onInit();
    if (!_service.hasLoaded.value) {
      _loadData();
    }
  }

  Future<void> _loadData() async {
    isLoading.value = true;
    try {
      await _service.loadCategories();
    } finally {
      isLoading.value = false;
    }
  }

  Future<void> refreshData() async {
    await _loadData();
  }

  /// Returns a properly nested tree of category nodes (root nodes only, with children populated).
  List<CategoryNode> get treeNodes {
    final categories = _service.categories;
    final parentMap = <String, List<AgentCategoryModel>>{};
    for (final c in categories) {
      parentMap.putIfAbsent(c.parentId, () => []).add(c);
    }

    void sortList(List<AgentCategoryModel> list) {
      list.sort((a, b) {
        if (a.sortOrder != b.sortOrder) {
          return a.sortOrder.compareTo(b.sortOrder);
        }
        return a.id.compareTo(b.id);
      });
    }

    CategoryNode buildNode(AgentCategoryModel current, int depth) {
      final childModels = parentMap[current.id] ?? [];
      sortList(childModels);
      final childNodes = childModels
          .map((c) => buildNode(c, depth + 1))
          .toList();
      return CategoryNode(model: current, children: childNodes, depth: depth);
    }

    final roots = parentMap['0'] ?? [];
    sortList(roots);
    return roots.map((r) => buildNode(r, 0)).toList();
  }

  /// Returns a flat pre-order list of all nodes (for ListView.builder rendering).
  List<CategoryNode> get flatNodes {
    final roots = treeNodes;
    final result = <CategoryNode>[];
    void flatten(CategoryNode node) {
      result.add(node);
      for (final child in node.children) {
        flatten(child);
      }
    }

    for (final root in roots) {
      flatten(root);
    }
    return result;
  }

  Future<void> createCategory(String parentId, String name) async {
    if (name.trim().isEmpty) return;
    isLoading.value = true;
    final res = await _service.createCategory(
      name: name.trim(),
      parentId: parentId,
    );
    isLoading.value = false;
    if (res == null) {
      CustomToast.show(
        _service.lastOperationError.isNotEmpty
            ? _service.lastOperationError
            : 'ai_agent_category_create_failed'.tr,
        isError: true,
      );
    } else {
      CustomToast.show('ai_agent_category_create_success'.tr, isError: false);
    }
  }

  Future<void> updateCategory(String id, String parentId, String name) async {
    if (name.trim().isEmpty) return;
    isLoading.value = true;
    final res = await _service.updateCategory(
      id,
      name: name.trim(),
      parentId: parentId,
    );
    isLoading.value = false;
    if (res == null) {
      CustomToast.show(
        _service.lastOperationError.isNotEmpty
            ? _service.lastOperationError
            : 'ai_agent_category_update_failed'.tr,
        isError: true,
      );
    } else {
      CustomToast.show('ai_agent_category_update_success'.tr, isError: false);
    }
  }

  Future<void> deleteCategory(String id) async {
    isLoading.value = true;
    final res = await _service.deleteCategory(id);
    isLoading.value = false;
    if (!res) {
      CustomToast.show(
        _service.lastOperationError.isNotEmpty
            ? _service.lastOperationError
            : 'ai_agent_category_delete_failed'.tr,
        isError: true,
      );
    } else {
      CustomToast.show('ai_agent_category_delete_success'.tr, isError: false);
    }
  }

  void showEditDialog(
    BuildContext context, {
    AgentCategoryModel? category,
    String parentId = '0',
  }) {
    final isEdit = category != null;
    final nameController = TextEditingController(
      text: isEdit ? category.name : '',
    );
    final formKey = GlobalKey<FormState>();

    Get.bottomSheet(
      Container(
        padding: EdgeInsets.only(
          left: 16,
          right: 16,
          top: 16,
          bottom: MediaQuery.of(context).viewInsets.bottom + 16,
        ),
        decoration: BoxDecoration(
          color: Theme.of(context).scaffoldBackgroundColor,
          borderRadius: const BorderRadius.vertical(top: Radius.circular(16)),
        ),
        child: Form(
          key: formKey,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text(
                isEdit
                    ? 'ai_agent_category_edit'.tr
                    : 'ai_agent_category_create'.tr,
                style: const TextStyle(
                  fontSize: 18,
                  fontWeight: FontWeight.bold,
                ),
                textAlign: TextAlign.center,
              ),
              const SizedBox(height: 16),
              TextFormField(
                controller: nameController,
                autofocus: true,
                decoration: InputDecoration(
                  labelText: 'ai_agent_category_name'.tr,
                  border: const OutlineInputBorder(),
                ),
                validator: (v) => v?.trim().isEmpty == true
                    ? 'ai_agent_category_name_required'.tr
                    : null,
              ),
              const SizedBox(height: 16),
              ElevatedButton(
                onPressed: () {
                  if (formKey.currentState?.validate() == true) {
                    Get.back();
                    if (isEdit) {
                      updateCategory(
                        category.id,
                        category.parentId,
                        nameController.text,
                      );
                    } else {
                      createCategory(parentId, nameController.text);
                    }
                  }
                },
                child: Text('common_save'.tr),
              ),
            ],
          ),
        ),
      ),
      isScrollControlled: true,
    );
  }
}
