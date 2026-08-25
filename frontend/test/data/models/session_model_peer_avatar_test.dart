import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/models/session_model.dart';

void main() {
  test('session model round-trips cached peer avatar url', () {
    final session = SessionModel.fromJson({
      'session_id': 's1',
      'updated_at': 1,
      'last_message_time': 1,
      'peer_avatar_url': ' https://cdn.example.com/a.png ',
    });
    expect(session.cachedPeerAvatarUrl, 'https://cdn.example.com/a.png');
    expect(
      session.toJson()['peer_avatar_url'],
      'https://cdn.example.com/a.png',
    );
    expect(
      session
          .copyWith(cachedPeerAvatarUrl: '')
          .toJson()
          .containsKey('peer_avatar_url'),
      isFalse,
    );
  });
}
