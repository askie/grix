import 'dart:async';

class VoiceRecognitionUpdate {
  const VoiceRecognitionUpdate({
    required this.transcript,
    required this.isFinal,
  });

  final String transcript;
  final bool isFinal;
}

typedef VoiceRecognitionCallback = void Function(VoiceRecognitionUpdate update);
typedef VoiceErrorCallback = void Function(String message, bool permanent);
typedef VoiceStatusCallback = void Function(VoiceTranscriberStatus status);

enum VoiceTranscriberStatus { listening, done, notListening }

abstract interface class VoiceTranscriber {
  bool get isSupported;
  bool get isListening;

  Future<bool> initialize();

  Future<void> listen({
    required VoiceRecognitionCallback onResult,
    required VoiceStatusCallback onStatus,
    required VoiceErrorCallback onError,
    String? localeId,
  });

  Future<void> stop();
  Future<void> cancel();
}

abstract interface class VoiceSpeaker {
  Future<void> stop();

  Future<void> speak(String text, {String? languageTag});
}

enum VoiceCommandAgentState { idle, busy, completed, failed, stopped }

class VoiceCommandDispatch {
  const VoiceCommandDispatch({
    required this.sessionId,
    required this.clientMessageId,
    required this.messageIdsBeforeSend,
  });

  final String sessionId;
  final String clientMessageId;
  final Set<String> messageIdsBeforeSend;
}

class VoiceCommandResponse {
  const VoiceCommandResponse({required this.text});

  final String text;
}

typedef VoiceCommandObserverDisposer = void Function();

abstract interface class VoiceCommandChatPort {
  String get sessionId;
  bool get isEligibleSession;
  bool get isBusy;
  bool get hasConflictingComposerState;
  String get draftText;
  String? get speechLocaleId;
  String? get speechLanguageTag;
  VoiceCommandAgentState agentStateFor(VoiceCommandDispatch dispatch);

  Future<VoiceCommandDispatch?> dispatchFinalTranscript(String text);

  VoiceCommandResponse? latestCompletedResponseAfter(
    VoiceCommandDispatch dispatch,
  );

  VoiceCommandObserverDisposer observe({required void Function() onChanged});
}

typedef VoiceCommandNotice = void Function(String message, {bool isError});
