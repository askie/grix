import 'package:get/get.dart';
import '../../shared/widgets/confirm_dialog.dart';
import 'connector_reports_controller.dart';
import 'connector_service.dart';

class ConnectorController extends GetxController {
  final RxList<ConnectorRelease> items = <ConnectorRelease>[].obs;
  final RxBool loading = false.obs;
  final RxnString error = RxnString();
  final RxString typeFilter = ''.obs;

  @override
  void onInit() { super.onInit(); load(); }

  void changeType(String value) {
    if (typeFilter.value == value) return;
    typeFilter.value = value;
    load();
  }

  Future<void> load() async {
    loading.value = true; error.value = null;
    try { items.assignAll(await ConnectorService.listReleases(clientType: typeFilter.value.isEmpty ? null : typeFilter.value)); }
    catch (e) { error.value = e.toString(); }
    finally { loading.value = false; }
  }

  Future<void> create(Map<String, dynamic> body) async {
    try { await ConnectorService.create(body); Toast.success('已创建'); await load(); }
    catch (e) { Toast.error(e.toString()); rethrow; }
  }
  Future<void> publish(ConnectorRelease r) => _act(() => ConnectorService.publish(r.id), '已发布');
  Future<void> pause(ConnectorRelease r) => _act(() => ConnectorService.pause(r.id), '已暂停');
  Future<void> resume(ConnectorRelease r) => _act(() => ConnectorService.resume(r.id), '已恢复');
  Future<void> revoke(ConnectorRelease r) => _act(() => ConnectorService.revoke(r.id), '已撤回');

  Future<void> pushUpgrade() async {
    final ok = await ConfirmDialog.show(title: '推送升级', message: '确定向所有在线 Agent 推送升级指令吗？', confirmText: '推送');
    if (!ok) return;
    try {
      final result = await ConnectorService.pushUpgrade();
      // 后端改走 Redis 广播到所有 ws 节点（异步派发，拿不到精确 agent 数）；
      // nodes 为收到广播的在线 ws 节点数。
      final nodes = result['nodes'] ?? 0;
      Toast.success('已向 $nodes 个节点广播升级指令');
    } catch (e) { Toast.error(e.toString()); }
  }

  Future<void> _act(Future<void> Function() fn, String msg) async {
    try { await fn(); Toast.success(msg); await load(); }
    catch (e) { Toast.error(e.toString()); }
  }
}

class ConnectorBinding extends Bindings {
  @override
  void dependencies() {
    Get.lazyPut<ConnectorController>(() => ConnectorController());
    Get.lazyPut<ConnectorReportsController>(() => ConnectorReportsController());
  }
}
