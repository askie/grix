import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_tts/flutter_tts.dart';
import 'package:speech_to_text/speech_recognition_error.dart';
import 'package:speech_to_text/speech_recognition_result.dart';
import 'package:speech_to_text/speech_to_text.dart';

import 'voice_command_io.dart';

/// Platforms where in-app system STT/TTS voice command has been verified.
///
/// Keep this narrow: do not open Web / Windows / Linux / Android / OpenHarmony
/// until those targets are explicitly validated.
bool isSystemVoiceCommandPlatformSupported({
  required bool isWeb,
  required TargetPlatform platform,
}) {
  if (isWeb) return false;
  return platform == TargetPlatform.iOS || platform == TargetPlatform.macOS;
}

class SystemVoiceTranscriber implements VoiceTranscriber {
  SystemVoiceTranscriber({
    SpeechToText? speech,
    SystemSpeechEngine? engine,
    this.finalResultDrainTimeout = const Duration(milliseconds: 2200),
    this.nativeCallbackQuiescence = const Duration(milliseconds: 350),
  }) : assert(speech == null || engine == null),
       _engine = engine ?? SpeechToTextEngine(speech: speech);

  final SystemSpeechEngine _engine;
  final Duration finalResultDrainTimeout;
  final Duration nativeCallbackQuiescence;
  _SystemVoiceSession? _activeSession;
  Future<void>? _listenOperation;
  Future<void>? _cancelOperation;
  Future<void>? _teardownBarrier;

  @override
  bool get isSupported => isSystemVoiceCommandPlatformSupported(
    isWeb: kIsWeb,
    platform: defaultTargetPlatform,
  );

  @override
  bool get isListening => _activeSession != null;

  @override
  Future<bool> initialize() {
    return _engine.initialize(
      // speech_to_text exposes status and error as process-wide callbacks.
      // They carry no listen identity, so forwarding them could attach a late
      // native event from an old recording to the next command. Lifecycle
      // callbacks below are instead derived from this adapter's owned call.
      onStatus: (_) {},
      onError: (_, _) {},
    );
  }

  @override
  Future<void> listen({
    required VoiceRecognitionCallback onResult,
    required VoiceStatusCallback onStatus,
    required VoiceErrorCallback onError,
    String? localeId,
  }) async {
    await _awaitNativeCallbackQuiescence();
    if (_activeSession != null) {
      throw StateError('A voice recognition session is already active');
    }
    final session = _SystemVoiceSession(
      onResult: onResult,
      onStatus: onStatus,
      onError: onError,
    );
    _activeSession = session;
    Future<void>? operation;
    try {
      operation = _engine.listen(
        onResult: (result) {
          if (_activeSession != session || !session.acceptCallbacks) return;
          if (result.isFinal && !session.finalResultSeen.isCompleted) {
            session.finalResultSeen.complete();
          }
          session.onResult(result);
        },
        localeId: localeId,
      );
      _listenOperation = operation;
      await operation;
      if (_activeSession == session && session.acceptCallbacks) {
        session.onStatus(VoiceTranscriberStatus.listening);
      }
    } catch (error) {
      if (_activeSession == session && session.acceptCallbacks) {
        session.acceptCallbacks = false;
        _activeSession = null;
        session.onError('$error', false);
      }
      rethrow;
    } finally {
      if (operation != null && identical(_listenOperation, operation)) {
        _listenOperation = null;
      }
    }
  }

  @override
  Future<void> stop() async {
    final session = _activeSession;
    if (session == null) return;
    try {
      await _awaitListenStartup();
      if (_activeSession != session) return;
      await _engine.stop();
      if (!session.finalResultSeen.isCompleted) {
        await Future.any<void>(<Future<void>>[
          session.finalResultSeen.future,
          session.ended.future,
          Future<void>.delayed(finalResultDrainTimeout),
        ]);
      }
      if (_activeSession != session || !session.acceptCallbacks) return;
      // speech_to_text keeps a process-wide result listener. Complete the
      // plugin's final-result window, then tear down the native recognizer
      // before a new listen is allowed to replace that listener.
      await _engine.cancel();
      if (_activeSession == session && session.acceptCallbacks) {
        session.onStatus(VoiceTranscriberStatus.done);
      }
    } finally {
      if (!session.ended.isCompleted) session.ended.complete();
      if (_activeSession == session) {
        session.acceptCallbacks = false;
        _activeSession = null;
      }
      _beginNativeCallbackQuiescence();
    }
  }

  @override
  Future<void> cancel() {
    final cancellation = _cancelOperation;
    if (cancellation != null) return cancellation;
    final session = _activeSession;
    if (session == null) return Future<void>.value();
    session.acceptCallbacks = false;
    late final Future<void> operation;
    operation = _cancelSession(session).whenComplete(() {
      if (identical(_cancelOperation, operation)) {
        _cancelOperation = null;
      }
    });
    _cancelOperation = operation;
    return operation;
  }

  Future<void> _cancelSession(_SystemVoiceSession session) async {
    if (!session.ended.isCompleted) session.ended.complete();
    try {
      await _awaitListenStartup();
      await _engine.cancel();
    } finally {
      if (_activeSession == session) {
        _activeSession = null;
      }
      _beginNativeCallbackQuiescence();
    }
  }

  Future<void> _awaitListenStartup() async {
    final operation = _listenOperation;
    if (operation == null) return;
    try {
      await operation;
    } catch (_) {
      // listen() reports its own correlated startup error. Teardown remains
      // idempotent and must not replace that error with a second failure.
    }
  }

  Future<void> _awaitNativeCallbackQuiescence() async {
    final barrier = _teardownBarrier;
    if (barrier == null) return;
    await barrier;
  }

  void _beginNativeCallbackQuiescence() {
    if (nativeCallbackQuiescence <= Duration.zero) {
      _teardownBarrier = null;
      return;
    }
    late final Future<void> barrier;
    barrier = Future<void>.delayed(nativeCallbackQuiescence).whenComplete(() {
      if (identical(_teardownBarrier, barrier)) {
        _teardownBarrier = null;
      }
    });
    _teardownBarrier = barrier;
  }
}

class SystemVoiceCommandIO {
  SystemVoiceCommandIO._();

  static final VoiceTranscriber transcriber = SystemVoiceTranscriber();
  static final VoiceSpeaker speaker = SystemVoiceSpeaker();
}

abstract interface class SystemSpeechEngine {
  Future<bool> initialize({
    required void Function(String status) onStatus,
    required void Function(String message, bool permanent) onError,
  });

  Future<void> listen({
    required VoiceRecognitionCallback onResult,
    String? localeId,
  });

  Future<void> stop();
  Future<void> cancel();
}

class SpeechToTextEngine implements SystemSpeechEngine {
  SpeechToTextEngine({SpeechToText? speech})
    : _speech = speech ?? SpeechToText();

  final SpeechToText _speech;

  @override
  Future<bool> initialize({
    required void Function(String status) onStatus,
    required void Function(String message, bool permanent) onError,
  }) {
    return _speech.initialize(
      onStatus: onStatus,
      onError: (SpeechRecognitionError error) {
        onError(error.errorMsg, error.permanent);
      },
    );
  }

  @override
  Future<void> listen({
    required VoiceRecognitionCallback onResult,
    String? localeId,
  }) async {
    await _speech.listen(
      onResult: (SpeechRecognitionResult result) {
        onResult(
          VoiceRecognitionUpdate(
            transcript: result.recognizedWords,
            isFinal: result.finalResult,
          ),
        );
      },
      listenOptions: SpeechListenOptions(
        cancelOnError: true,
        partialResults: true,
        listenMode: ListenMode.confirmation,
        autoPunctuation: true,
        localeId: localeId,
      ),
    );
  }

  @override
  Future<void> stop() => _speech.stop();

  @override
  Future<void> cancel() => _speech.cancel();
}

class _SystemVoiceSession {
  _SystemVoiceSession({
    required this.onResult,
    required this.onStatus,
    required this.onError,
  });

  final VoiceRecognitionCallback onResult;
  final VoiceStatusCallback onStatus;
  final VoiceErrorCallback onError;
  final Completer<void> finalResultSeen = Completer<void>();
  final Completer<void> ended = Completer<void>();
  bool acceptCallbacks = true;
}

class SystemVoiceSpeaker implements VoiceSpeaker {
  SystemVoiceSpeaker({FlutterTts? tts}) : _tts = tts ?? FlutterTts();

  final FlutterTts _tts;
  bool _configured = false;
  int _operationGeneration = 0;

  Future<void> _ensureConfigured() async {
    if (_configured) return;
    await _tts.awaitSpeakCompletion(true);
    _configured = true;
  }

  @override
  Future<void> stop() async {
    // ChatController disposal calls stop defensively even when voice output
    // was never used. Avoid touching the platform channel in that case.
    if (!_configured && _operationGeneration == 0) return;
    final generation = ++_operationGeneration;
    await _ensureConfigured();
    if (generation != _operationGeneration) return;
    await _tts.stop();
  }

  @override
  Future<void> speak(String text, {String? languageTag}) async {
    final generation = ++_operationGeneration;
    await _ensureConfigured();
    if (generation != _operationGeneration) return;
    final language = languageTag?.trim() ?? '';
    if (language.isNotEmpty) {
      await _tts.setLanguage(language);
      if (generation != _operationGeneration) return;
    }
    await _tts.speak(text);
  }
}
