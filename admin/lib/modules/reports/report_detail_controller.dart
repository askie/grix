import 'package:get/get.dart';

import '../../shared/widgets/confirm_dialog.dart';
import 'report_models.dart';
import 'report_service.dart';

/// 举报详情控制器：加载详情、处理举报、获取附件地址。
class ReportDetailController extends GetxController {
  ReportDetailController(this.reportId);

  final String reportId;

  final Rxn<ReportDetail> detail = Rxn<ReportDetail>();
  final RxBool loading = false.obs;
  final RxnString error = RxnString();

  /// 处理后是否发生变更（用于返回列表时刷新）。
  bool changed = false;

  @override
  void onInit() {
    super.onInit();
    load();
  }

  Future<void> load() async {
    loading.value = true;
    error.value = null;
    try {
      detail.value = await ReportService.detail(reportId);
    } catch (e) {
      error.value = e.toString();
    } finally {
      loading.value = false;
    }
  }

  Future<String> attachmentUrl(String attachmentId) {
    return ReportService.attachmentUrl(reportId, attachmentId);
  }

  /// 处理举报。action 见后端定义。
  Future<void> resolve(String action, String actionLabel) async {
    final note = await ConfirmDialog.showWithReason(
      title: '$actionLabel · 填写处理备注',
      hint: '处理备注（必填）',
      confirmText: '提交',
      danger: action == 'ban_user' || action == 'ban_group',
    );
    if (note == null) return;
    if (note.isEmpty) {
      Toast.error('处理备注不能为空');
      return;
    }
    try {
      await ReportService.resolve(reportId, action, note);
      Toast.success('举报已处理');
      changed = true;
      await load();
    } catch (e) {
      Toast.error(e.toString());
    }
  }
}
