import 'package:flutter_test/flutter_test.dart';

import 'package:grix/data/models/login_device_session_model.dart';

void main() {
  test('parses login device session payload', () {
    final model = LoginDeviceSessionModel.fromJson({
      'session_id': 'session-1',
      'device_id': 'ios-device-1',
      'platform': 'ios',
      'online': true,
      'current': 1,
      'last_seen_at': '2026-03-15T08:30:00Z',
      'created_at': '2026-03-14T08:30:00Z',
    });

    expect(model.sessionId, 'session-1');
    expect(model.deviceId, 'ios-device-1');
    expect(model.platform, 'ios');
    expect(model.online, isTrue);
    expect(model.current, isTrue);
    expect(model.lastSeenAt, isNotNull);
    expect(model.createdAt, isNotNull);
  });
}
