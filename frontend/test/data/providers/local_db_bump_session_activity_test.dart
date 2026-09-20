import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/local_db.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  // 卡片、工具状态、流式占位这类不可预览的消息只推进会话的活跃时间
  // （updated_at），不得改变列表展示的时间。这个契约只有在该行已经知道
  // 自己最后一条可见消息时间时才成立：last_message_time 为 0 时展示时间
  // 正是 updated_at 撑着，推进它会把一批几个月没动的会话集体显示成收到
  // 这些事件的那一刻。
  test(
    'bumpSessionActivity skips rows that have no visible message time',
    () async {
      final userId = 'bump-activity-${DateTime.now().microsecondsSinceEpoch}';
      await LocalDb.initDatabaseFactory();
      await LocalDb.setActiveUser(userId);
      try {
        const oldMs = 1746600000000; // 2026-05 左右的历史时间
        const bumpMs = 1789895340000; // 收到一批不可预览事件的“现在”

        await LocalDb.upsertSession(<String, dynamic>{
          'session_id': 'no-msg-time',
          'title': '老会话：本地还不知道最后一条可见消息的时间',
          'type': 'private',
          'updated_at': oldMs,
          'last_message_time': 0,
        });
        await LocalDb.upsertSession(<String, dynamic>{
          'session_id': 'has-msg-time',
          'title': '有可见消息时间的会话',
          'type': 'private',
          'updated_at': oldMs,
          'last_message_time': oldMs,
        });

        await LocalDb.bumpSessionActivity('no-msg-time', bumpMs);
        await LocalDb.bumpSessionActivity('has-msg-time', bumpMs);

        final untouched = await LocalDb.getSessionRecord('no-msg-time');
        final bumped = await LocalDb.getSessionRecord('has-msg-time');

        // 没有可见消息时间的行保持原样，展示时间不会被顶成“现在”。
        expect(untouched?['updated_at'], oldMs);
        expect(untouched?['last_message_time'], 0);
        // 已有可见消息时间的行照常参与活跃置顶，展示时间仍由消息时间决定。
        expect(bumped?['updated_at'], bumpMs);
        expect(bumped?['last_message_time'], oldMs);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );
}
