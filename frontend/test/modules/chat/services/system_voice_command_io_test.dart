import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/services/system_voice_command_io.dart';
import 'package:grix/modules/chat/services/voice_command_io.dart';

void main() {
  test('native global callbacks cannot cross voice sessions', () async {
    final engine = _FakeSystemSpeechEngine();
    final transcriber = SystemVoiceTranscriber(
      engine: engine,
      nativeCallbackQuiescence: Duration.zero,
    );
    final firstResults = <String>[];
    final secondResults = <String>[];
    final secondStatuses = <VoiceTranscriberStatus>[];
    final secondErrors = <String>[];

    expect(await transcriber.initialize(), isTrue);
    await transcriber.listen(
      onResult: (result) => firstResults.add(result.transcript),
      onStatus: (_) {},
      onError: (_, _) {},
    );
    final oldNativeResult = engine.resultListeners.single;
    await transcriber.cancel();

    await transcriber.listen(
      onResult: (result) => secondResults.add(result.transcript),
      onStatus: secondStatuses.add,
      onError: (message, _) => secondErrors.add(message),
    );
    final newNativeResult = engine.resultListeners.last;

    engine.statusListener?.call('done');
    engine.errorListener?.call('late old error', true);
    oldNativeResult(_result('late old result'));
    newNativeResult(_result('current result'));

    expect(firstResults, isEmpty);
    expect(secondResults, ['current result']);
    expect(secondStatuses, [VoiceTranscriberStatus.listening]);
    expect(secondErrors, isEmpty);
  });

  test(
    'new listen waits for native callback quiescence after cancel',
    () async {
      final engine = _FakeSystemSpeechEngine();
      final transcriber = SystemVoiceTranscriber(
        engine: engine,
        nativeCallbackQuiescence: const Duration(milliseconds: 30),
      );
      final secondResults = <String>[];

      await transcriber.initialize();
      await transcriber.listen(
        onResult: (_) {},
        onStatus: (_) {},
        onError: (_, _) {},
      );
      final oldNativeResult = engine.resultListeners.single;
      await transcriber.cancel();

      final secondListen = transcriber.listen(
        onResult: (result) => secondResults.add(result.transcript),
        onStatus: (_) {},
        onError: (_, _) {},
      );
      await Future<void>.delayed(Duration.zero);

      expect(engine.resultListeners, hasLength(1));
      oldNativeResult(_result('late old result'));
      await secondListen;
      engine.resultListeners.last(_result('current result'));

      expect(secondResults, ['current result']);
    },
  );

  test('cancel waits for native listen startup before teardown', () async {
    final engine = _FakeSystemSpeechEngine();
    final starting = Completer<void>();
    engine.nextListenCompleter = starting;
    final transcriber = SystemVoiceTranscriber(engine: engine);

    await transcriber.initialize();
    final listen = transcriber.listen(
      onResult: (_) {},
      onStatus: (_) {},
      onError: (_, _) {},
    );
    await Future<void>.delayed(Duration.zero);
    final cancellation = transcriber.cancel();
    await Future<void>.delayed(Duration.zero);

    expect(engine.cancelCalls, 0);
    starting.complete();
    await Future.wait(<Future<void>>[listen, cancellation]);

    expect(engine.cancelCalls, 1);
    expect(transcriber.isListening, isFalse);
  });

  test('stop drains a late final before native teardown', () async {
    final engine = _FakeSystemSpeechEngine();
    final transcriber = SystemVoiceTranscriber(
      engine: engine,
      finalResultDrainTimeout: const Duration(milliseconds: 50),
    );
    final results = <VoiceRecognitionUpdate>[];

    await transcriber.initialize();
    await transcriber.listen(
      onResult: results.add,
      onStatus: (_) {},
      onError: (_, _) {},
    );
    engine.resultListeners.single(
      const VoiceRecognitionUpdate(transcript: 'partial', isFinal: false),
    );
    final stopping = transcriber.stop();
    await Future<void>.delayed(Duration.zero);

    expect(transcriber.isListening, isTrue);
    engine.resultListeners.single(_result('late final'));
    await stopping;

    expect(results.map((result) => result.transcript), [
      'partial',
      'late final',
    ]);
    expect(engine.cancelCalls, 1);
    expect(transcriber.isListening, isFalse);
  });
}

VoiceRecognitionUpdate _result(String words) =>
    VoiceRecognitionUpdate(transcript: words, isFinal: true);

class _FakeSystemSpeechEngine implements SystemSpeechEngine {
  void Function(String message, bool permanent)? errorListener;
  void Function(String status)? statusListener;
  final List<VoiceRecognitionCallback> resultListeners =
      <VoiceRecognitionCallback>[];
  int cancelCalls = 0;
  Completer<void>? nextListenCompleter;

  @override
  Future<bool> initialize({
    required void Function(String status) onStatus,
    required void Function(String message, bool permanent) onError,
  }) async {
    errorListener = onError;
    statusListener = onStatus;
    return true;
  }

  @override
  Future<void> listen({
    required VoiceRecognitionCallback onResult,
    String? localeId,
  }) async {
    resultListeners.add(onResult);
    final completer = nextListenCompleter;
    nextListenCompleter = null;
    if (completer != null) await completer.future;
  }

  @override
  Future<void> stop() async {}

  @override
  Future<void> cancel() async {
    cancelCalls += 1;
  }
}
