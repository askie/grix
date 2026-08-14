import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/utils/chat_draft_index.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    ChatDraftIndex.resetForTest();
  });

  tearDown(() {
    ChatDraftIndex.resetForTest();
  });

  test('ensureLoaded scans persisted text drafts for the given user', () async {
    SharedPreferences.setMockInitialValues(<String, Object>{
      'chat_draft_u1_s-text': 'hello draft',
      'chat_draft_u1_s-blank': '   ',
      'chat_draft_u1_s-attach_attach': '[]',
      'chat_draft_u1_s-reply_reply': 'msg-1',
      'chat_draft_u2_s-other-user': 'other user draft',
      'unrelated_key': 'x',
    });

    await ChatDraftIndex.ensureLoaded('u1');

    expect(ChatDraftIndex.hasDraft('s-text'), isTrue);
    // 空白草稿不算草稿
    expect(ChatDraftIndex.hasDraft('s-blank'), isFalse);
    // 附件/回复派生 key 不算文字草稿
    expect(ChatDraftIndex.hasDraft('s-attach_attach'), isFalse);
    expect(ChatDraftIndex.hasDraft('s-reply_reply'), isFalse);
    // 其他用户的草稿不串号
    expect(ChatDraftIndex.hasDraft('s-other-user'), isFalse);
  });

  test('update adds and removes sessions and bumps version', () {
    final v0 = ChatDraftIndex.version.value;

    ChatDraftIndex.update(sessionId: 's1', hasDraft: true);
    expect(ChatDraftIndex.hasDraft('s1'), isTrue);
    expect(ChatDraftIndex.version.value, v0 + 1);

    // 重复登记不再自增版本
    ChatDraftIndex.update(sessionId: 's1', hasDraft: true);
    expect(ChatDraftIndex.version.value, v0 + 1);

    ChatDraftIndex.update(sessionId: 's1', hasDraft: false);
    expect(ChatDraftIndex.hasDraft('s1'), isFalse);
    expect(ChatDraftIndex.version.value, v0 + 2);

    // 摘除不存在的会话不自增版本
    ChatDraftIndex.update(sessionId: 's-none', hasDraft: false);
    expect(ChatDraftIndex.version.value, v0 + 2);
  });

  test('ensureLoaded keeps live updates reported before scan lands', () async {
    SharedPreferences.setMockInitialValues(<String, Object>{
      'chat_draft_u1_s-persisted': 'persisted draft',
    });

    ChatDraftIndex.update(sessionId: 's-live', hasDraft: true);
    await ChatDraftIndex.ensureLoaded('u1');

    expect(ChatDraftIndex.hasDraft('s-live'), isTrue);
    expect(ChatDraftIndex.hasDraft('s-persisted'), isTrue);
  });

  test('switching user rebuilds the index', () async {
    SharedPreferences.setMockInitialValues(<String, Object>{
      'chat_draft_u1_s-u1': 'u1 draft',
      'chat_draft_u2_s-u2': 'u2 draft',
    });

    await ChatDraftIndex.ensureLoaded('u1');
    expect(ChatDraftIndex.hasDraft('s-u1'), isTrue);

    await ChatDraftIndex.ensureLoaded('u2');
    expect(ChatDraftIndex.hasDraft('s-u1'), isFalse);
    expect(ChatDraftIndex.hasDraft('s-u2'), isTrue);
  });
}
