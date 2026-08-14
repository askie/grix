import 'package:flutter/widgets.dart';
import 'package:get/get.dart';

import '../../core/network/page_result.dart';
import '../../shared/controllers/paged_list_controller.dart';
import '../../shared/widgets/confirm_dialog.dart';
import 'link_blocklist_models.dart';
import 'link_blocklist_service.dart';

/// 链接黑名单列表控制器：多维筛选 + CRUD + 批量 + 在线测试。
class LinkBlocklistController extends PagedListController<LinkBlocklistRule> {
  final TextEditingController searchCtrl = TextEditingController();
  final RxString keyword = ''.obs;
  final RxString kindFilter = ''.obs;
  final RxString severityFilter = ''.obs;
  final Rxn<bool> enabledFilter = Rxn<bool>();

  // 顶部统计概览（首次加载与每次刷新时拉取）
  final Rxn<LinkBlocklistStats> stats = Rxn<LinkBlocklistStats>();

  @override
  void onInit() {
    super.onInit();
    refreshStats();
  }

  Future<void> refreshStats() async {
    try {
      stats.value = await LinkBlocklistAnalytics.stats();
    } catch (_) {
      // 统计失败不影响列表
    }
  }

  Future<void> importCSV(String csv) async {
    try {
      final r = await LinkBlocklistAnalytics.importCSV(csv);
      Toast.success(
        '导入完成：新增 ${r.created} 条，跳过 ${r.skipped} 条'
        '${r.failures.isEmpty ? "" : "，失败 ${r.failures.length} 条"}',
      );
      await reloadFromFirstPage();
      await refreshStats();
    } catch (e) {
      Toast.error(e.toString());
    }
  }

  @override
  Future<PageResult<LinkBlocklistRule>> fetchPage() {
    return LinkBlocklistService.listRules(
      query: keyword.value,
      kind: kindFilter.value.isEmpty ? null : kindFilter.value,
      severity:
          severityFilter.value.isEmpty ? null : severityFilter.value,
      enabled: enabledFilter.value,
      page: page.value,
      pageSize: pageSize.value,
    );
  }

  void applySearch(String value) {
    keyword.value = value.trim();
    reloadFromFirstPage();
  }

  void setKindFilter(String v) {
    kindFilter.value = v;
    reloadFromFirstPage();
  }

  void setSeverityFilter(String v) {
    severityFilter.value = v;
    reloadFromFirstPage();
  }

  void setEnabledFilter(bool? v) {
    enabledFilter.value = v;
    reloadFromFirstPage();
  }

  void resetFilters() {
    searchCtrl.clear();
    keyword.value = '';
    kindFilter.value = '';
    severityFilter.value = '';
    enabledFilter.value = null;
    reloadFromFirstPage();
  }

  Future<void> createRule({
    required String kind,
    required String value,
    required String severity,
    String source = 'manual',
    bool enabled = true,
    String note = '',
  }) async {
    await runAction(
      () async {
        await LinkBlocklistService.create(
          kind: kind,
          value: value,
          severity: severity,
          source: source,
          enabled: enabled,
          note: note,
        );
      },
      '规则已创建',
    );
  }

  Future<void> updateRule(
    int id, {
    required String kind,
    required String value,
    required String severity,
    required String source,
    required bool enabled,
    String note = '',
  }) async {
    await runAction(
      () async {
        await LinkBlocklistService.update(
          id,
          kind: kind,
          value: value,
          severity: severity,
          source: source,
          enabled: enabled,
          note: note,
        );
      },
      '已保存',
    );
  }

  Future<void> toggleEnabled(LinkBlocklistRule r) async {
    await runAction(
      () async {
        await LinkBlocklistService.update(
          r.id,
          kind: r.kind,
          value: r.value,
          severity: r.severity,
          source: r.source,
          enabled: !r.enabled,
          note: r.note,
        );
      },
      r.enabled ? '已禁用' : '已启用',
    );
  }

  Future<void> deleteRule(LinkBlocklistRule r) async {
    final ok = await ConfirmDialog.show(
      title: '删除规则',
      message: '确定删除规则「${r.value}」吗？此操作不可撤销。',
      confirmText: '删除',
      danger: true,
    );
    if (!ok) return;
    await runAction(() => LinkBlocklistService.remove(r.id), '已删除');
  }

  Future<LinkTestResult?> testUrl(String url) async {
    try {
      return await LinkBlocklistService.test(url);
    } catch (e) {
      Toast.error(e.toString());
      return null;
    }
  }

  @override
  void onClose() {
    searchCtrl.dispose();
    super.onClose();
  }
}
