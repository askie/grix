import 'package:flutter_test/flutter_test.dart';
import 'package:grix_admin/modules/admins/admins_view.dart';

void main() {
  test('admin role dialog exposes visitor ban permission', () {
    expect(kPermissionLabels['visitor_bans'], '访客封禁');
  });
}
