import 'package:flutter_test/flutter_test.dart';
import 'package:grix_admin/modules/users/admin_user_item.dart';

void main() {
  group('AdminUserItem.fromJson', () {
    test('完整字段解析', () {
      final user = AdminUserItem.fromJson({
        'id': '123',
        'username': 'alice',
        'email': 'a@example.com',
        'nickname': '爱丽丝',
        'status': 2,
        'login_locked': true,
        'lock_remaining': '5分钟',
        'banned_reason': 'spam',
        'moderation_muted': true,
        'moderation_mute_session_count': 3,
        'created_at': '2026-01-02T03:04:05Z',
      });
      expect(user.id, '123');
      expect(user.displayName, '爱丽丝');
      expect(user.isBanned, true);
      expect(user.loginLocked, true);
      expect(user.moderationMuteSessionCount, 3);
      expect(user.createdAt, isNotNull);
    });

    test('缺省与空值容错', () {
      final user = AdminUserItem.fromJson({'id': 9, 'username': 'bob'});
      expect(user.id, '9');
      expect(user.displayName, 'bob'); // nickname 为空回退到 username
      expect(user.isBanned, false);
      expect(user.moderationMuted, false);
      expect(user.createdAt, isNull);
    });
  });
}
