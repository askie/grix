import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/services/chat_voice_command_gate.dart';

void main() {
  group('ChatVoiceCommandGate', () {
    test('partial transcript is never submitted', () {
      final gate = ChatVoiceCommandGate()..reset();

      expect(
        gate.acceptRecognitionResult(
          transcript: 'partial command',
          isFinal: false,
        ),
        isNull,
      );
      expect(gate.release(), isNull);
    });

    test('final transcript submits once after release', () {
      final gate = ChatVoiceCommandGate()..reset();

      expect(
        gate.acceptRecognitionResult(
          transcript: 'final command',
          isFinal: true,
        ),
        isNull,
      );
      expect(gate.release(), 'final command');
      expect(gate.release(), isNull);
    });

    test('late final transcript submits once after release', () {
      final gate = ChatVoiceCommandGate()..reset();

      expect(gate.release(), isNull);
      expect(
        gate.acceptRecognitionResult(
          transcript: ' late final command ',
          isFinal: true,
        ),
        'late final command',
      );
      expect(
        gate.acceptRecognitionResult(
          transcript: 'duplicate final command',
          isFinal: true,
        ),
        isNull,
      );
    });

    test('reset allows the next recording to submit', () {
      final gate = ChatVoiceCommandGate()..reset();
      gate.acceptRecognitionResult(transcript: 'first', isFinal: true);
      expect(gate.release(), 'first');

      gate.reset();
      gate.acceptRecognitionResult(transcript: 'second', isFinal: true);
      expect(gate.release(), 'second');
    });

    test('cancel rejects late final transcript', () {
      final gate = ChatVoiceCommandGate()..reset();

      gate.cancel();

      expect(gate.release(), isNull);
      expect(
        gate.acceptRecognitionResult(
          transcript: 'must not be submitted',
          isFinal: true,
        ),
        isNull,
      );
    });
  });
}
