import 'package:flutter_test/flutter_test.dart';
import 'package:grix_admin/modules/visitor_bans/visitor_ban_item.dart';

void main() {
  test('VisitorBanItem parses string ids and status helpers', () {
    final item = VisitorBanItem.fromJson({
      'id': '1',
      'site_id': '2',
      'site_name': 'Demo',
      'site_key': 'site_key',
      'owner_user_id': '3',
      'owner_username': 'owner',
      'owner_nickname': '',
      'visitor_id': '4',
      'visitor_key': 'vk',
      'visitor_name': '',
      'visitor_email': 'v@example.com',
      'session_id': 's1',
      'last_page_url': 'https://example.com',
      'last_init_ip_prefix': '203.0.113.0/24',
      'status': 3,
      'has_ip_ban': true,
      'created_at': '2026-08-10T12:00:00Z',
      'updated_at': '2026-08-10T12:01:00Z',
      'last_active_at': '2026-08-10T12:02:00Z',
      'last_init_at': '2026-08-10T12:03:00Z',
    });

    expect(item.isBanned, isTrue);
    expect(item.visitorDisplayName, '访客 4');
    expect(item.ownerDisplayName, 'owner');
    expect(item.hasIpBan, isTrue);
    expect(item.updatedAt, isNotNull);
  });
}
