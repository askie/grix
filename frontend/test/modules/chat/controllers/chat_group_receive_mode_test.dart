import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/friend_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/oss_service.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/chat/controllers/chat_controller.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  setUp(() {
    Get.testMode = true;
    Get.reset();
    // onClose → saveDraft 会碰 SharedPreferences；未 mock 时会 MissingPluginException
    // 变成 "failed after test completion"。
    SharedPreferences.setMockInitialValues({});
    Get.put<ImService>(ImService());
    Get.put<AuthService>(AuthService());
    Get.put<AgentService>(AgentService());
    Get.put<SessionService>(SessionService());
    Get.put<OssService>(OssService());
    Get.put<FriendService>(FriendService());
  });

  tearDown(() {
    Get.reset();
  });

  test('groupMemberAgentReceiveMode preserves server ModeAll value', () {
    final controller = ChatController();
    controller.sessionId = 'session_group_mode_all';
    controller.chatTitle = 'group';
    controller.chatType = 'group';
    addTearDown(() {
      if (!controller.isClosed) {
        controller.onClose();
      }
    });

    expect(
      controller.groupMemberAgentReceiveMode(const {'agent_receive_mode': 2}),
      2,
    );
  });
}
