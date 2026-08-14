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
}
