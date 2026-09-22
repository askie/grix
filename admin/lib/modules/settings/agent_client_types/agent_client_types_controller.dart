import 'package:get/get.dart';

import '../../../shared/widgets/confirm_dialog.dart';
import 'agent_client_types_models.dart';

class AgentClientTypesSettingsController extends GetxController {
  final RxBool loading = false.obs;
  final RxBool saving = false.obs;
  final RxnString error = RxnString();
  final RxList<AgentClientTypeItem> items = <AgentClientTypeItem>[].obs;
  final RxBool allEnabledHint = false.obs;

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
      final s = await AgentClientTypesSettingsService.get();
      items.assignAll(s.items);
      allEnabledHint.value = s.allEnabled;
      _loaded = true;
    } catch (e) {
      error.value = e.toString();
    } finally {
      loading.value = false;
    }
  }

  void toggle(int index, bool value) {
    if (index < 0 || index >= items.length) return;
    items[index].enabled = value;
    items.refresh();
    allEnabledHint.value = false;
  }

  void selectAll(bool enabled) {
    for (final item in items) {
      item.enabled = enabled;
    }
    items.refresh();
    allEnabledHint.value = false;
  }

  Future<void> save() async {
    final enabled = items
        .where((item) => item.enabled)
        .map((item) => item.type)
        .toList();
    if (enabled.isEmpty) {
      Toast.error('至少启用一种智能体类型');
      return;
    }
    saving.value = true;
    try {
      await AgentClientTypesSettingsService.update(enabled);
      Toast.success('智能体类型配置已保存，最多 1 分钟生效');
      await load();
    } catch (e) {
      Toast.error(e.toString());
    } finally {
      saving.value = false;
    }
  }
}
