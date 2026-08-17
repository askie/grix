import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/services/chat_voice_command_response_filter.dart';

void main() {
  group('ChatVoiceCommandResponseFilter', () {
    test('allows plain text and rejects standalone grix cards', () {
      expect(
        ChatVoiceCommandResponseFilter.isSpeakablePlainText('任务完成'),
        isTrue,
      );
      expect(
        ChatVoiceCommandResponseFilter.isSpeakablePlainText(
          '[思考中](grix://card/thinking?content=working)',
        ),
        isFalse,
      );
      expect(
        ChatVoiceCommandResponseFilter.isSpeakablePlainText(
          'grix://card/thinking?content=working',
        ),
        isFalse,
      );
    });
  });
}
