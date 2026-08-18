import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/controllers/chat_voice_command_controller.dart';
import 'package:grix/modules/chat/services/voice_command_io.dart';

void main() {
  group('ChatVoiceCommandController', () {
    test('allows recording while the agent is busy', () async {
      final fixture = _Fixture()..chat.busy = true;

      await fixture.controller.startListening();
      fixture.transcriber.emit('忙碌时口述', isFinal: true);
      await fixture.controller.stopListeningAndSubmit();

      expect(fixture.transcriber.listenCalls, 1);
      expect(fixture.chat.applyCount, 1);
      expect(fixture.chat.appliedText, '忙碌时口述');
      expect(fixture.chat.dispatchCount, 0);
      expect(
        fixture.notices.where((notice) => notice.contains('正在处理任务')),
        isEmpty,
      );
      fixture.dispose();
    });

    test('rejects recording while composer has non-text state', () async {
      final fixture = _Fixture()..chat.conflictingComposerState = true;

      await fixture.controller.startListening();

      expect(fixture.transcriber.listenCalls, 0);
      expect(fixture.notices.single, contains('附件、引用或队列编辑'));
      fixture.dispose();
    });

    test(
      'partial is UI-only and final fills the input exactly once after release',
      () async {
        final fixture = _Fixture();

        await fixture.controller.startListening();
        fixture.transcriber.emit('部分指令', isFinal: false);
        expect(fixture.controller.transcriptPreview.value, '部分指令');
        expect(fixture.chat.draft, isEmpty);
        expect(fixture.chat.applyCount, 0);
        expect(fixture.chat.dispatchCount, 0);

        fixture.transcriber.emit('最终指令', isFinal: true);
        expect(fixture.chat.applyCount, 0);
        await fixture.controller.stopListeningAndSubmit();
        expect(fixture.chat.applyCount, 1);
        expect(fixture.chat.appliedText, '最终指令');
        expect(fixture.chat.draft, '最终指令');
        expect(fixture.chat.dispatchCount, 0);
        expect(fixture.speaker.spoken, isEmpty);

        fixture.transcriber.emit('重复最终指令', isFinal: true);
        await Future<void>.delayed(Duration.zero);
        expect(fixture.chat.applyCount, 1);
        expect(fixture.chat.draft, '最终指令');
        fixture.dispose();
      },
    );

    test(
      'late final fills the input once but cancelled recording rejects it',
      () async {
        final fixture = _Fixture();

        await fixture.controller.startListening();
        await fixture.controller.stopListeningAndSubmit();
        fixture.transcriber.emit('迟到最终指令', isFinal: true);
        await Future<void>.delayed(Duration.zero);
        expect(fixture.chat.applyCount, 1);
        expect(fixture.chat.draft, '迟到最终指令');
        expect(fixture.chat.dispatchCount, 0);

        fixture.chat.resetRun();
        fixture.controller.isAwaitingResponse.value = false;
        await fixture.controller.startListening();
        await fixture.controller.cancelListening();
        fixture.transcriber.emit('取消后的迟到结果', isFinal: true);
        await Future<void>.delayed(Duration.zero);
        expect(fixture.chat.applyCount, 0);
        expect(fixture.chat.draft, isEmpty);
        fixture.dispose();
      },
    );

    test('late final never overwrites text the user already typed', () async {
      final fixture = _Fixture();

      await fixture.controller.startListening();
      await fixture.controller.stopListeningAndSubmit();
      expect(fixture.notices.last, contains('没有识别到语音'));

      fixture.chat.draft = '用户手改的内容';
      fixture.transcriber.emit('迟到最终指令', isFinal: true);
      await Future<void>.delayed(Duration.zero);

      expect(fixture.chat.applyCount, 0);
      expect(fixture.chat.draft, '用户手改的内容');
      expect(
        fixture.notices.where((notice) => notice.contains('未能写入输入框')),
        isEmpty,
      );
      fixture.dispose();
    });

    test(
      'external composer action cancels voice partial and late final',
      () async {
        final fixture = _Fixture();

        await fixture.controller.startListening();
        fixture.transcriber.emit('只属于语音的 partial', isFinal: false);
        fixture.controller.deactivateForExternalAction();
        fixture.transcriber.emit('不应发送的 final', isFinal: true);
        await Future<void>.delayed(Duration.zero);

        expect(fixture.chat.draft, isEmpty);
        expect(fixture.controller.transcriptPreview.value, isEmpty);
        expect(fixture.chat.applyCount, 0);
        expect(fixture.chat.dispatchCount, 0);
        fixture.dispose();
      },
    );

    test('release fills the input and does not send or speak', () async {
      final fixture = _Fixture();

      await fixture.controller.startListening();
      fixture.transcriber.emit('执行命令', isFinal: true);
      await fixture.controller.stopListeningAndSubmit();
      fixture.chat
        ..state = VoiceCommandAgentState.completed
        ..response = const VoiceCommandResponse(text: '不应播报')
        ..notifyChanged();
      await Future<void>.delayed(Duration.zero);

      expect(fixture.chat.draft, '执行命令');
      expect(fixture.chat.dispatchCount, 0);
      expect(fixture.speaker.spoken, isEmpty);
      expect(fixture.controller.isAwaitingResponse.value, isFalse);
      fixture.dispose();
    });

    test('apply failure stays idle without sending', () async {
      final fixture = _Fixture()..chat.applySucceeds = false;

      await fixture.controller.startListening();
      fixture.transcriber.emit('无法写入的指令', isFinal: true);
      await fixture.controller.stopListeningAndSubmit();

      expect(fixture.chat.applyCount, 1);
      expect(fixture.chat.draft, isEmpty);
      expect(fixture.chat.dispatchCount, 0);
      expect(fixture.controller.isAwaitingResponse.value, isFalse);
      expect(fixture.notices.last, contains('未能写入输入框'));
      fixture.dispose();
    });

    test(
      'background-style deactivation cancels listen and rejects late final',
      () async {
        final fixture = _Fixture();

        await fixture.controller.startListening();
        fixture.transcriber.emit('后台前的 partial', isFinal: false);
        // Mirrors ChatController.didChangeAppLifecycleState(paused/hidden).
        fixture.controller.deactivateForExternalAction();
        fixture.transcriber.emit('后台后迟到 final', isFinal: true);
        await Future<void>.delayed(Duration.zero);

        expect(fixture.transcriber.cancelCalls, greaterThanOrEqualTo(1));
        expect(fixture.speaker.stopCalls, greaterThanOrEqualTo(1));
        expect(fixture.controller.isListening.value, isFalse);
        expect(fixture.controller.transcriptPreview.value, isEmpty);
        expect(fixture.chat.applyCount, 0);
        expect(fixture.chat.dispatchCount, 0);
        fixture.dispose();
      },
    );

    test(
      'release during permission initialization never starts recording',
      () async {
        final fixture = _Fixture();
        fixture.transcriber.initializeCompleter = Completer<bool>();

        final start = fixture.controller.startListening();
        await Future<void>.delayed(Duration.zero);
        await fixture.controller.stopListeningAndSubmit();
        fixture.transcriber.initializeCompleter!.complete(true);
        await start;

        expect(fixture.transcriber.listenCalls, 0);
        expect(fixture.chat.dispatchCount, 0);
        fixture.dispose();
      },
    );

    test('old recognition status and error callbacks are ignored', () async {
      final fixture = _Fixture();

      await fixture.controller.startListening();
      await fixture.controller.cancelListening();
      await fixture.controller.startListening();
      expect(fixture.controller.isListening.value, isTrue);

      fixture.transcriber.emitStatus(
        VoiceTranscriberStatus.done,
        sessionIndex: 0,
      );
      fixture.transcriber.emitError('old error', sessionIndex: 0);
      await Future<void>.delayed(Duration.zero);

      expect(fixture.controller.isListening.value, isTrue);
      expect(
        fixture.notices.where((notice) => notice.contains('old error')),
        isEmpty,
      );
      fixture.dispose();
    });

    test(
      'cancelled listen startup failure cannot affect the next recording',
      () async {
        final fixture = _Fixture();
        final oldListen = Completer<void>();
        fixture.transcriber.nextListenCompleter = oldListen;

        final oldStart = fixture.controller.startListening();
        await Future<void>.delayed(Duration.zero);
        final cancellation = fixture.controller.cancelListening();
        await Future<void>.delayed(Duration.zero);
        expect(fixture.transcriber.listenCalls, 1);

        oldListen.completeError(StateError('old listen failure'));
        await Future.wait(<Future<void>>[oldStart, cancellation]);
        await fixture.controller.startListening();

        expect(fixture.controller.isListening.value, isTrue);
        expect(
          fixture.notices.where(
            (notice) => notice.contains('old listen failure'),
          ),
          isEmpty,
        );
        fixture.dispose();
      },
    );

    test('release during listen startup cancels without submitting', () async {
      final fixture = _Fixture();
      final starting = Completer<void>();
      fixture.transcriber
        ..nextListenCompleter = starting
        ..markListeningBeforeAwait = false;

      final start = fixture.controller.startListening();
      await Future<void>.delayed(Duration.zero);
      final stop = fixture.controller.stopListeningAndSubmit();
      await Future<void>.delayed(Duration.zero);
      starting.complete();
      await Future.wait(<Future<void>>[start, stop]);
      fixture.transcriber.emit('启动后迟到的 final', isFinal: true);
      await Future<void>.delayed(Duration.zero);

      expect(fixture.transcriber.cancelCalls, 1);
      expect(fixture.transcriber.isListening, isFalse);
      expect(fixture.chat.dispatchCount, 0);
      fixture.dispose();
    });

    test('cancel during listen startup cancels without submitting', () async {
      final fixture = _Fixture();
      final starting = Completer<void>();
      fixture.transcriber
        ..nextListenCompleter = starting
        ..markListeningBeforeAwait = false;

      final start = fixture.controller.startListening();
      await Future<void>.delayed(Duration.zero);
      final cancellation = fixture.controller.cancelListening();
      await Future<void>.delayed(Duration.zero);
      starting.complete();
      await Future.wait(<Future<void>>[start, cancellation]);
      fixture.transcriber.emit('取消后迟到的 final', isFinal: true);
      await Future<void>.delayed(Duration.zero);

      expect(fixture.transcriber.cancelCalls, 1);
      expect(fixture.transcriber.isListening, isFalse);
      expect(fixture.chat.dispatchCount, 0);
      fixture.dispose();
    });

    test('an old pre-listen future keeps ownership until it exits', () async {
      final fixture = _Fixture();
      final oldSpeakerStop = Completer<void>();
      fixture.speaker.stopCompleter = oldSpeakerStop;

      final oldStart = fixture.controller.startListening();
      await Future<void>.delayed(Duration.zero);
      await fixture.controller.cancelListening();
      await fixture.controller.startListening();

      expect(fixture.transcriber.listenCalls, 0);
      expect(fixture.notices.last, contains('正在停止'));

      oldSpeakerStop.complete();
      await oldStart;
      await fixture.controller.startListening();

      expect(fixture.transcriber.listenCalls, 1);
      expect(fixture.controller.isListening.value, isTrue);
      fixture.dispose();
    });
  });
}

class _Fixture {
  _Fixture({Duration responseTimeout = const Duration(seconds: 1)}) {
    controller = ChatVoiceCommandController(
      chat: chat,
      transcriber: transcriber,
      speaker: speaker,
      responseTimeout: responseTimeout,
      notice: (message, {isError = false}) => notices.add(message),
    )..bind();
  }

  final _FakeChatPort chat = _FakeChatPort();
  final _FakeTranscriber transcriber = _FakeTranscriber();
  final _FakeSpeaker speaker = _FakeSpeaker();
  final List<String> notices = <String>[];
  late final ChatVoiceCommandController controller;

  void dispose() => controller.dispose();
}

class _FakeTranscriber implements VoiceTranscriber {
  final List<_FakeListenSession> sessions = <_FakeListenSession>[];
  bool listening = false;
  bool sessionActive = false;
  int listenCalls = 0;
  int cancelCalls = 0;
  Completer<bool>? initializeCompleter;
  Completer<void>? nextListenCompleter;
  Future<void>? listenOperation;
  Future<void>? cancelOperation;
  bool markListeningBeforeAwait = true;

  @override
  bool get isListening => sessionActive;

  @override
  bool get isSupported => true;

  @override
  Future<bool> initialize() async {
    return initializeCompleter?.future ?? true;
  }

  @override
  Future<void> listen({
    required VoiceRecognitionCallback onResult,
    required VoiceStatusCallback onStatus,
    required VoiceErrorCallback onError,
    String? localeId,
  }) async {
    listenCalls += 1;
    sessionActive = true;
    if (markListeningBeforeAwait) {
      listening = true;
    }
    sessions.add(
      _FakeListenSession(
        onResult: onResult,
        onStatus: onStatus,
        onError: onError,
      ),
    );
    final completer = nextListenCompleter;
    nextListenCompleter = null;
    final operation = completer?.future ?? Future<void>.value();
    listenOperation = operation;
    try {
      await operation;
      listening = true;
    } finally {
      if (identical(listenOperation, operation)) {
        listenOperation = null;
      }
    }
  }

  void emit(String transcript, {required bool isFinal, int? sessionIndex}) {
    _session(sessionIndex).onResult(
      VoiceRecognitionUpdate(transcript: transcript, isFinal: isFinal),
    );
  }

  void emitError(String message, {bool permanent = false, int? sessionIndex}) {
    _session(sessionIndex).onError(message, permanent);
  }

  void emitStatus(VoiceTranscriberStatus status, {int? sessionIndex}) {
    _session(sessionIndex).onStatus(status);
  }

  _FakeListenSession _session(int? index) =>
      sessions[index ?? sessions.length - 1];

  @override
  Future<void> stop() async {
    listening = false;
    sessionActive = false;
  }

  @override
  Future<void> cancel() {
    final activeCancellation = cancelOperation;
    if (activeCancellation != null) return activeCancellation;
    if (!sessionActive) return Future<void>.value();
    late final Future<void> operation;
    operation = _cancelActiveSession().whenComplete(() {
      if (identical(cancelOperation, operation)) cancelOperation = null;
    });
    cancelOperation = operation;
    return operation;
  }

  Future<void> _cancelActiveSession() async {
    final startup = listenOperation;
    if (startup != null) {
      try {
        await startup;
      } catch (_) {}
    }
    cancelCalls += 1;
    listening = false;
    sessionActive = false;
  }
}

class _FakeListenSession {
  const _FakeListenSession({
    required this.onResult,
    required this.onStatus,
    required this.onError,
  });

  final VoiceRecognitionCallback onResult;
  final VoiceStatusCallback onStatus;
  final VoiceErrorCallback onError;
}

class _FakeSpeaker implements VoiceSpeaker {
  final List<String> spoken = <String>[];
  Completer<void>? stopCompleter;
  Completer<void>? speakCompleter;
  int operationGeneration = 0;
  int stopCalls = 0;

  @override
  Future<void> speak(String text, {String? languageTag}) async {
    final generation = ++operationGeneration;
    await (speakCompleter?.future ?? Future<void>.value());
    if (generation != operationGeneration) return;
    spoken.add(text);
  }

  @override
  Future<void> stop() async {
    stopCalls += 1;
    operationGeneration += 1;
    await (stopCompleter?.future ?? Future<void>.value());
  }
}

class _FakeChatPort implements VoiceCommandChatPort {
  void Function()? _observer;
  String currentSessionId = 'session-a';
  bool eligible = true;
  bool busy = false;
  String draft = '';
  int applyCount = 0;
  String appliedText = '';
  int dispatchCount = 0;
  String dispatchedText = '';
  VoiceCommandAgentState state = VoiceCommandAgentState.idle;
  VoiceCommandResponse? response;
  bool applySucceeds = true;
  bool dispatchSucceeds = true;
  bool responseMatchesDispatch = true;
  bool conflictingComposerState = false;

  @override
  VoiceCommandAgentState agentStateFor(VoiceCommandDispatch dispatch) =>
      dispatch.sessionId == currentSessionId
      ? state
      : VoiceCommandAgentState.idle;

  @override
  String get sessionId => currentSessionId;

  @override
  String get draftText => draft;

  @override
  bool get isBusy => busy;

  @override
  bool get isEligibleSession => eligible;

  @override
  bool get hasConflictingComposerState => conflictingComposerState;

  @override
  String? get speechLanguageTag => 'zh-CN';

  @override
  String? get speechLocaleId => 'zh_CN';

  @override
  bool applyTranscriptToDraft(String text) {
    applyCount += 1;
    appliedText = text;
    if (!applySucceeds) return false;
    draft = text;
    return true;
  }

  @override
  Future<VoiceCommandDispatch?> dispatchFinalTranscript(String text) async {
    dispatchCount += 1;
    dispatchedText = text;
    if (!dispatchSucceeds) return null;
    return VoiceCommandDispatch(
      sessionId: currentSessionId,
      clientMessageId: 'voice-client-$dispatchCount',
      messageIdsBeforeSend: const <String>{'old'},
    );
  }

  @override
  VoiceCommandResponse? latestCompletedResponseAfter(
    VoiceCommandDispatch dispatch,
  ) => dispatch.sessionId == currentSessionId && responseMatchesDispatch
      ? response
      : null;

  @override
  VoiceCommandObserverDisposer observe({required void Function() onChanged}) {
    _observer = onChanged;
    return () => _observer = null;
  }

  void notifyChanged() => _observer?.call();

  void resetRun() {
    draft = '';
    applyCount = 0;
    appliedText = '';
    dispatchCount = 0;
    dispatchedText = '';
    state = VoiceCommandAgentState.idle;
    response = null;
    responseMatchesDispatch = true;
  }
}
