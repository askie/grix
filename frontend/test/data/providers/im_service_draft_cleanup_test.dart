import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/shared/utils/chat_draft_index.dart';
import 'package:grix/shared/widgets/message_bubble.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeAuthService extends AuthService {
  _FakeAuthService(this.userIdValue);

  final String userIdValue;

  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => userIdValue;

  @override
  String? get token => 'test_access_token';

  @override
  Future<void> logout({bool notifyServer = true}) async {}
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late String userId;
  late ImService service;

  setUp(() async {
    Get.testMode = true;
    Get.reset();
    MessageStreamController.resetForTest();
    ChatDraftIndex.resetForTest();
    userId = 'draft-clean-${DateTime.now().microsecondsSinceEpoch}';
    SharedPreferences.setMockInitialValues({});
    Get.put<AuthService>(_FakeAuthService(userId));
    await LocalDb.setActiveUser(userId);
    service = ImService();
  });

  tearDown(() async {
    service.onClose();
    ChatDraftIndex.resetForTest();
    MessageStreamController.resetForTest();
    await LocalDb.setActiveUser(null);
    Get.reset();
  });

  test(
    'deleteConversation clears draft prefs keys and ChatDraftIndex entry',
    () async {
      const sid = 's-draft-delete';
      final textKey = ChatDraftIndex.textKey(userId: userId, sessionId: sid);
      final attachKey = ChatDraftIndex.attachmentKey(
        userId: userId,
        sessionId: sid,
      );
      final replyKey = ChatDraftIndex.replyKey(userId: userId, sessionId: sid);

      final prefs = await SharedPreferences.getInstance();
      await prefs.setString(textKey, 'unsent text');
      await prefs.setString(attachKey, '[]');
      await prefs.setString(replyKey, 'msg-1');
      ChatDraftIndex.update(sessionId: sid, hasDraft: true);
      expect(ChatDraftIndex.hasDraft(sid), isTrue);

      await service.deleteConversation(sid);

      expect(ChatDraftIndex.hasDraft(sid), isFalse);
      expect(prefs.getString(textKey), isNull);
      expect(prefs.getString(attachKey), isNull);
      expect(prefs.getString(replyKey), isNull);
    },
  );

  test(
    'revokeSessionAccess clears draft prefs keys and ChatDraftIndex entry',
    () async {
      const sid = 's-draft-revoke';
      final textKey = ChatDraftIndex.textKey(userId: userId, sessionId: sid);

      final prefs = await SharedPreferences.getInstance();
      await prefs.setString(textKey, 'revoked leftover');
      ChatDraftIndex.update(sessionId: sid, hasDraft: true);

      await service.revokeSessionAccess(sid);

      expect(ChatDraftIndex.hasDraft(sid), isFalse);
      expect(prefs.getString(textKey), isNull);
    },
  );
}
