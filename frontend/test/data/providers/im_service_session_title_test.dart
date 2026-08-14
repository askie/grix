import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:get/get.dart';

void main() {
  group('resolveSessionDisplayTitle', () {
    late ImService imService;

    setUp(() {
      Get.testMode = true;
      imService = ImService();
    });

    tearDown(() {
      imService.sessions.clear();
      Get.reset();
    });

    /// 辅助：向 ImService 注入一条会话并返回该 session。
    SessionModel _addSession({
      required String sessionId,
      String title = '',
      String type = 'private',
      String peerId = '',
      int peerType = 2,
      String peerNickname = '',
      String peerUsername = '',
    }) {
      final session = SessionModel(
        sessionId: sessionId,
        title: title,
        type: type,
        peerId: peerId,
        peerType: peerType,
        peerNickname: peerNickname,
        peerUsername: peerUsername,
        updatedAt: DateTime.now().millisecondsSinceEpoch,
        lastMessageTime: DateTime.now().millisecondsSinceEpoch,
      );
      imService.sessions.add(session);
      return session;
    }

    test(
      'agent session: title 与 peerNickname 不同时应优先返回 peerNickname',
      () {
        // 模拟真实 agent 会话：title 是创建时的 agent 名称，
        // peerNickname 是从服务端同步的 agent 显示名。
        final session = _addSession(
          sessionId: 'session-001',
          title: 'claude-code-1',
          peerNickname: 'Claude Code',
          peerType: 2,
        );

        final result = imService.resolveSessionDisplayTitle(session);

        // 期望：与左侧会话列表一致，返回 peerNickname
        expect(result, equals('Claude Code'));
      },
    );

    test(
      'agent session: peerNickname 为空时回退到 peerUsername',
      () {
        final session = _addSession(
          sessionId: 'session-002',
          title: 'codex-3',
          peerNickname: '',
          peerUsername: 'OpenAI Codex',
          peerType: 2,
        );

        final result = imService.resolveSessionDisplayTitle(session);

        expect(result, equals('OpenAI Codex'));
      },
    );

    test(
      'agent session: peerNickname 和 peerUsername 都为空时回退到 title',
      () {
        final session = _addSession(
          sessionId: 'session-003',
          title: 'my-agent',
          peerNickname: '',
          peerUsername: '',
          peerType: 2,
        );

        final result = imService.resolveSessionDisplayTitle(session);

        expect(result, equals('my-agent'));
      },
    );

    test(
      'private session: title 与 peerNickname 不同时 peerNickname 仍优先',
      () {
        // 左侧会话列表 _getConversationPrimaryTitle 的优先级是
        // peerNickname > session.title，这里对齐。
        final session = _addSession(
          sessionId: 'session-004',
          title: '我的工作助手',
          peerNickname: 'Claude Code',
          peerType: 2,
        );

        final result = imService.resolveSessionDisplayTitle(session);

        expect(result, equals('Claude Code'));
      },
    );

    test(
      'private human session: peerNickname 优先于 title',
      () {
        final session = _addSession(
          sessionId: 'session-005',
          title: '',
          peerNickname: '张三',
          peerUsername: 'zhangsan',
          peerType: 1,
        );

        final result = imService.resolveSessionDisplayTitle(session);

        expect(result, equals('张三'));
      },
    );
  });
}
