import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/services/chat_voice_command_support.dart';

void main() {
  group('isVoiceCommandEntrySupported', () {
    test('requires both feature gate and platform support', () {
      expect(
        isVoiceCommandEntrySupported(
          featureEnabled: true,
          platformSupported: true,
        ),
        isTrue,
      );
      expect(
        isVoiceCommandEntrySupported(
          featureEnabled: false,
          platformSupported: true,
        ),
        isFalse,
      );
      expect(
        isVoiceCommandEntrySupported(
          featureEnabled: true,
          platformSupported: false,
        ),
        isFalse,
      );
      expect(
        isVoiceCommandEntrySupported(
          featureEnabled: false,
          platformSupported: false,
        ),
        isFalse,
      );
    });
  });
}
