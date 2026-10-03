import 'package:get/get.dart';

import 'system_controls_models.dart';
import 'system_controls_service.dart';

class SystemControlsController extends GetxController {
  SystemControlsController({SystemControlsService? service})
    : _service = service ?? SystemControlsService();
  final SystemControlsService _service;
  final loading = false.obs;
  final error = RxnString();
  final items = <SystemControl>[].obs;
  final drafts = <String, bool>{}.obs;
  final savingKey = RxnString();
  final saveErrors = <String, String>{}.obs;
  final savedKeys = <String>{}.obs;

  bool get busy => loading.value || savingKey.value != null;
  bool dirty(SystemControl item) => drafts[item.key] != item.value;

  @override
  void onInit() {
    super.onInit();
    load();
  }

  void edit(SystemControl item, bool value) {
    if (busy || item.valueType != 'boolean') return;
    drafts[item.key] = value;
    saveErrors.remove(item.key);
    savedKeys.remove(item.key);
  }

  Future<void> load() async {
    if (busy || isClosed) return;
    loading.value = true;
    error.value = null;
    try {
      final next = await _service.get();
      if (isClosed) return;
      final dirtyKeys = items.where(dirty).map((item) => item.key).toSet();
      for (final item in next) {
        if (item.valueType == 'boolean' && !dirtyKeys.contains(item.key)) {
          drafts[item.key] = item.value as bool;
        }
      }
      items.assignAll(next);
      savedKeys.clear();
    } catch (e) {
      if (!isClosed) error.value = e.toString();
    } finally {
      if (!isClosed) loading.value = false;
    }
  }

  Future<void> save(SystemControl item) async {
    if (busy || isClosed || item.valueType != 'boolean') return;
    final value = drafts[item.key];
    if (value == null) return;
    savingKey.value = item.key;
    saveErrors.remove(item.key);
    savedKeys.remove(item.key);
    try {
      final saved = await _service.update(item.key, value);
      if (isClosed) return;
      if (saved.key != item.key ||
          saved.valueType != 'boolean' ||
          saved.value != value) {
        throw const FormatException('保存结果异常，请刷新核对');
      }
      final index = items.indexWhere((entry) => entry.key == item.key);
      if (index >= 0) items[index] = saved;
      drafts[item.key] = saved.value as bool;
      savedKeys.add(item.key);
    } catch (e) {
      if (!isClosed) saveErrors[item.key] = '保存失败：$e';
    } finally {
      if (!isClosed) savingKey.value = null;
    }
  }
}
