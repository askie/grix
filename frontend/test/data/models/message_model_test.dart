import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/models/message_model.dart';

void main() {
  group('MessageModel.fromJson content fallback', () {
    test('uses content_preview when content is empty', () {
      final model = MessageModel.fromJson({
        'msg_id': '1',
        'session_id': 's1',
        'sender_id': 'u1',
        'content': '',
        'content_preview': 'preview text',
        'created_at': 1710000000,
      });

      expect(model.content, 'preview text');
    });

    test('uses summary in extra when direct fields are empty', () {
      final model = MessageModel.fromJson({
        'msg_id': '2',
        'session_id': 's1',
        'sender_id': 'u1',
        'content': '',
        'extra': '{"summary":"extra summary"}',
        'created_at': 1710000000,
      });

      expect(model.content, 'extra summary');
    });
  });

  group('MessageModel.fromJson client identity', () {
    test('restores a persisted local_seq when client_msg_id is absent', () {
      final model = MessageModel.fromJson({
        'msg_id': 'temp_local-command',
        'session_id': 's1',
        'sender_id': 'me',
        'content': 'pending',
        'local_seq': 'local-command',
        'status': 'sending',
        'created_at': 1710000000,
      });

      expect(model.clientMsgId, 'local-command');
    });

    test('prefers explicit client_msg_id over local_seq', () {
      final model = MessageModel.fromJson({
        'msg_id': 'server-message',
        'session_id': 's1',
        'sender_id': 'me',
        'content': 'sent',
        'client_msg_id': 'explicit-command',
        'local_seq': 'persisted-command',
        'created_at': 1710000000,
      });

      expect(model.clientMsgId, 'explicit-command');
    });

    test('ordinary server message keeps a null client identity', () {
      final model = MessageModel.fromJson({
        'msg_id': 'server-only',
        'session_id': 's1',
        'sender_id': 'peer',
        'content': 'hello',
        'created_at': 1710000000,
      });

      expect(model.clientMsgId, isNull);
    });
  });
}
