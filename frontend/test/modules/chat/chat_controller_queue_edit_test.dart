import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/oss_service.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/chat/controllers/chat_controller.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeImService extends ImService {
  final List<Map<String, dynamic>> holdCalls = <Map<String, dynamic>>[];
  final List<Map<String, dynamic>> editCalls = <Map<String, dynamic>>[];
  EventLifecycleCmdResult holdResult = const EventLifecycleCmdResult(
    ok: true,
    held: true,
  );
  EventLifecycleCmdResult editResult = const EventLifecycleCmdResult(ok: true);

  @override
  void connect(String wsUrl) {}

  @override
  Future<EventLifecycleCmdResult> sendEventHold({
    required String sessionId,
    required String eventId,
    required bool hold,
    String reason = 'manual',
    int? ttlMs,
  }) async {
    holdCalls.add(<String, dynamic>{
      'session_id': sessionId,
      'event_id': eventId,
      'hold': hold,
      'reason': reason,
    });
    return holdResult;
  }

  @override
  Future<EventLifecycleCmdResult> sendQueueEdit({
    required String sessionId,
    required String eventId,
    required String content,
  }) async {
    editCalls.add(<String, dynamic>{
      'session_id': sessionId,
      'event_id': eventId,
      'content': content,
    });
    return editResult;
  }
}

class _FakeAuthService extends AuthService {
  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => '1001';
}

class _FakeAgentService extends AgentService {
  @override
  Future<void> loadAgents({String? categoryId}) async {}
}

class _FakeSessionService extends SessionService {}

class _FakeOssService extends OssService {}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _FakeImService imService;
  late ChatController controller;

  EventLifecycleQueueItem buildItem(
    String eventId, {
    int position = 1,
    String content = '',
    String contentPreview = '',
  }) {
    return EventLifecycleQueueItem(
      eventId: eventId,
      sessionId: 'sess-edit',
      messageId: '',
      clientMsgId: '',
      contentPreview: contentPreview,
      state: 'queued',
      queuePosition: position,
      actions: const <String>['cancel'],
      updatedAt: 1000,
      content: content,
    );
  }

  setUp(() {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues(<String, Object>{});
    imService = _FakeImService();
    Get.put<ImService>(imService);
    Get.put<AuthService>(_FakeAuthService());
    Get.put<AgentService>(_FakeAgentService());
    Get.put<SessionService>(_FakeSessionService());
    Get.put<OssService>(_FakeOssService());
    // 直接构造（不 Get.put）跳过路由态 onInit，仅测编辑态逻辑
    controller = ChatController();
    controller.sessionId = 'sess-edit';
  });

  tearDown(() {
    // 退出可能残留的编辑态，回收续期定时器
    controller.cancelQueueTaskEdit();
    Get.reset();
  });

  group('进入编辑模式', () {
    test('hold ok：暂存草稿、任务全文填入输入框、光标置尾', () async {
      controller.inputController.text = '我的草稿';
      final entered = await controller.startQueueTaskEdit(
        buildItem('e1', content: '排队任务全文', contentPreview: '排队任务全…'),
      );

      expect(entered, isTrue);
      expect(controller.isEditingQueueTask, isTrue);
      expect(controller.editingQueueTaskEventId.value, 'e1');
      expect(controller.inputController.text, '排队任务全文');
      expect(controller.inputController.selection.baseOffset, '排队任务全文'.length);
      expect(imService.holdCalls, hasLength(1));
      expect(imService.holdCalls.single['hold'], isTrue);
      expect(imService.holdCalls.single['reason'], 'editing');
      expect(imService.holdCalls.single['event_id'], 'e1');
    });

    test('content 缺失回退 contentPreview 填入', () async {
      await controller.startQueueTaskEdit(
        buildItem('e1', contentPreview: '仅预览文本'),
      );
      expect(controller.inputController.text, '仅预览文本');
    });

    test('hold 失败（任务已开跑）：不进入编辑，输入框不动', () async {
      imService.holdResult = const EventLifecycleCmdResult(
        ok: false,
        error: 'not_found',
      );
      controller.inputController.text = '我的草稿';

      final entered = await controller.startQueueTaskEdit(
        buildItem('e1', content: '排队任务全文'),
      );

      expect(entered, isFalse);
      expect(controller.isEditingQueueTask, isFalse);
      expect(controller.inputController.text, '我的草稿');
    });
  });

  group('退出编辑模式', () {
    test('点 × 取消：发 hold:false 并还原草稿', () async {
      controller.inputController.text = '我的草稿';
      await controller.startQueueTaskEdit(buildItem('e1', content: '任务全文'));

      controller.cancelQueueTaskEdit();

      expect(controller.isEditingQueueTask, isFalse);
      expect(controller.inputController.text, '我的草稿');
      expect(imService.holdCalls, hasLength(2));
      expect(imService.holdCalls.last['hold'], isFalse);
      expect(imService.holdCalls.last['event_id'], 'e1');
    });

    test('提交成功：改发 queue_edit、清输入框退出编辑并还原草稿', () async {
      controller.inputController.text = '我的草稿';
      await controller.startQueueTaskEdit(buildItem('e1', content: '任务全文'));
      controller.inputController.text = '改写后的任务全文';

      await controller.submitQueueTaskEdit();

      expect(imService.editCalls, hasLength(1));
      expect(imService.editCalls.single['event_id'], 'e1');
      expect(imService.editCalls.single['content'], '改写后的任务全文');
      expect(controller.isEditingQueueTask, isFalse);
      expect(controller.inputController.text, '我的草稿');
    });

    test('提交失败（任务已不在队列）：退出编辑但文字保留输入框', () async {
      imService.editResult = const EventLifecycleCmdResult(
        ok: false,
        error: 'not_found',
      );
      controller.inputController.text = '我的草稿';
      await controller.startQueueTaskEdit(buildItem('e1', content: '任务全文'));
      controller.inputController.text = '改写后的任务全文';

      await controller.submitQueueTaskEdit();

      expect(controller.isEditingQueueTask, isFalse);
      expect(
        controller.inputController.text,
        '改写后的任务全文',
        reason: '失败时文字保留，用户可当新消息直接发送',
      );
    });
  });

  group('发送动作路由', () {
    test('编辑态下 dispatchCurrentInputMessage 改发 queue_edit 而非新消息', () async {
      await controller.startQueueTaskEdit(buildItem('e1', content: '任务全文'));
      controller.inputController.text = '编辑后内容';

      final handled = controller.dispatchCurrentInputMessage();
      await Future<void>.delayed(Duration.zero);

      expect(handled, isTrue);
      expect(imService.editCalls, hasLength(1));
      expect(imService.editCalls.single['content'], '编辑后内容');
    });

    test('编辑态下空文本发送为 no-op', () async {
      await controller.startQueueTaskEdit(buildItem('e1', content: '任务全文'));
      controller.inputController.text = '   ';

      final handled = controller.dispatchCurrentInputMessage();
      await Future<void>.delayed(Duration.zero);

      expect(handled, isFalse);
      expect(imService.editCalls, isEmpty);
      expect(controller.isEditingQueueTask, isTrue);
    });
  });
}
