import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/models/conversation_summary_model.dart';

void main() {
  group('ConversationSummaryModel.toLatestSessionModel time semantics', () {
    test('展示与排序都取最后一条可见消息，不跟活跃时间', () {
      // 复现反馈 bug：活跃时间(latest_active_at)被后台活动/已读回执顶到比正文晚
      // 5 分钟。展示时间与排序时间都必须停在「最后一条可见消息」(last_msg_time)。
      const visibleSec = 1700000000; // 最后可见消息时间
      const activeSec = 1700000300; // 活跃时间(晚 5 分钟)
      final summary = ConversationSummaryModel.fromJson(<String, dynamic>{
        'group_key': 'private:1:2',
        'conversation_type': 'private',
        'latest_session_id': 's1',
        'last_msg': 'hello',
        'last_msg_time': visibleSec,
        'latest_active_at': activeSec,
        'updated_at': activeSec,
      });

      final session = summary.toLatestSessionModel();

      // 展示时间 = 可见消息时间(ms)。
      expect(session.lastMessageTime, visibleSec * 1000);
      // 排序依据 activityAt 同样是可见消息时间，不被活跃时间顶起。
      expect(session.activityAt, visibleSec * 1000);
      // 活跃时间原样保留，仅在会话尚无可见消息时兜底。
      expect(session.updatedAt, activeSec * 1000);
    });

    test('无可见消息时(last_msg_time=0)展示回退到活跃时间', () {
      const activeSec = 1700000300;
      final summary = ConversationSummaryModel.fromJson(<String, dynamic>{
        'group_key': 'private:1:2',
        'conversation_type': 'private',
        'latest_session_id': 's2',
        'last_msg': '',
        'last_msg_time': 0,
        'latest_active_at': activeSec,
        'updated_at': activeSec,
      });

      final session = summary.toLatestSessionModel();

      expect(session.lastMessageTime, activeSec * 1000);
      expect(session.activityAt, activeSec * 1000);
    });
  });
}
