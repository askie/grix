import 'dart:async';

import 'package:get/get.dart';

import '../services/chat_voice_command_gate.dart';
import '../services/voice_command_io.dart';

class ChatVoiceCommandController {
  ChatVoiceCommandController({
    required VoiceCommandChatPort chat,
    required VoiceTranscriber transcriber,
    required VoiceSpeaker speaker,
    required VoiceCommandNotice notice,
    this.responseTimeout = const Duration(minutes: 2),
  }) : _chat = chat,
       _transcriber = transcriber,
       _speaker = speaker,
       _notice = notice;

  final VoiceCommandChatPort _chat;
  final VoiceTranscriber _transcriber;
  final VoiceSpeaker _speaker;
  final VoiceCommandNotice _notice;
  final Duration responseTimeout;
  final ChatVoiceCommandGate _submissionGate = ChatVoiceCommandGate();

  final RxBool isListening = false.obs;
  final RxBool isAwaitingResponse = false.obs;
  final RxString transcriptPreview = ''.obs;

  VoiceCommandObserverDisposer? _disposeObserver;
  Timer? _responseTimer;
  bool _speechInitialized = false;
  bool _initializing = false;
  int? _listenStartOwner;
  bool _pressActive = false;
  bool _startedWithDraft = false;
  bool _submitting = false;
  bool _disposed = false;
  Future<void>? _stopOperation;
  int _recognitionGeneration = 0;
  int _lifecycleGeneration = 0;
  int _commandGeneration = 0;
  int? _speakingCommandGeneration;
  String _transcript = '';
  VoiceCommandDispatch? _pendingDispatch;

  bool get isSupported => _transcriber.isSupported;

  void bind() {
    if (_disposed || !isSupported || _disposeObserver != null) return;
    _disposeObserver = _chat.observe(
      onChanged: () => unawaited(_trySpeakCompletedResponse()),
    );
  }

  Future<void> startListening() async {
    if (_disposed || !isSupported || _initializing || isListening.value) {
      return;
    }
    if (_listenStartOwner != null) {
      _notice('语音识别正在停止，请稍后重试');
      return;
    }
    if (!_chat.isEligibleSession) {
      _notice('语音命令当前仅支持 Agent 私聊');
      return;
    }
    if (_chat.hasConflictingComposerState) {
      _notice('请先完成附件、引用或队列编辑操作');
      return;
    }

    _pressActive = true;
    _startedWithDraft = _chat.draftText.trim().isNotEmpty;
    _submissionGate.reset();
    _transcript = '';
    transcriptPreview.value = '';
    final generation = ++_recognitionGeneration;
    _listenStartOwner = generation;
    try {
      await _speaker.stop();
      if (generation != _recognitionGeneration) return;
      final initialized = await _ensureSpeechInitialized();
      if (generation != _recognitionGeneration) return;
      if (!initialized) {
        _pressActive = false;
        _submissionGate.cancel();
        _notice('无法使用系统语音识别，请检查麦克风和语音识别权限', isError: true);
        return;
      }
      // 首次授权弹窗期间如果用户已点别处停止，则只完成授权，
      // 不在回调后偷偷开始录音，下一次点击才进入监听。
      if (!_pressActive || generation != _recognitionGeneration) return;

      isListening.value = true;
      await _transcriber.listen(
        localeId: _chat.speechLocaleId,
        onResult: (update) {
          if (generation != _recognitionGeneration) return;
          _handleRecognitionResult(update);
        },
        onStatus: (status) {
          if (generation != _recognitionGeneration) return;
          if (status == VoiceTranscriberStatus.done ||
              status == VoiceTranscriberStatus.notListening) {
            if (_pressActive && _stopOperation == null) {
              unawaited(stopListeningAndSubmit());
            } else {
              isListening.value = false;
            }
          }
        },
        onError: (message, permanent) {
          if (generation != _recognitionGeneration) return;
          if (permanent) _speechInitialized = false;
          _failRecognition('语音识别失败：$message');
        },
      );
      if (generation != _recognitionGeneration || !_pressActive) {
        // cancel() owns the native listen startup barrier. It must run even
        // when the platform has not reported isListening yet.
        await _transcriber.cancel();
        isListening.value = false;
      }
    } catch (error) {
      if (generation != _recognitionGeneration) return;
      _failRecognition('语音识别启动失败：$error');
    } finally {
      if (_listenStartOwner == generation) {
        _listenStartOwner = null;
      }
    }
  }

  Future<void> stopListeningAndSubmit() {
    if (!isSupported) return Future<void>.value();
    final inFlight = _stopOperation;
    if (inFlight != null) return inFlight;
    if (!_pressActive && _listenStartOwner == null && !isListening.value) {
      return Future<void>.value();
    }
    // Publish the join-gate before the body runs. Otherwise a nested stop
    // (send + tap-outside, or a native done callback) can start a second
    // teardown and the flush path will spin while isCapturingSpeech stays true.
    final gate = Completer<void>();
    _stopOperation = gate.future;
    unawaited(() async {
      try {
        await _stopListeningAndSubmitBody();
        if (!gate.isCompleted) gate.complete();
      } catch (error, stack) {
        if (!gate.isCompleted) gate.completeError(error, stack);
      } finally {
        if (identical(_stopOperation, gate.future)) {
          _stopOperation = null;
        }
        if (!gate.isCompleted) gate.complete();
      }
    }());
    return gate.future;
  }

  Future<void> _stopListeningAndSubmitBody() async {
    final generation = _recognitionGeneration;
    _pressActive = false;
    try {
      if (_listenStartOwner != null) {
        _recognitionGeneration += 1;
        _submissionGate.cancel();
        await _transcriber.cancel();
        isListening.value = false;
        return;
      }
      if (_transcriber.isListening) {
        try {
          await _transcriber.stop();
        } catch (error) {
          if (generation != _recognitionGeneration) return;
          _failRecognition('停止语音识别失败：$error');
          return;
        }
      }
      if (generation != _recognitionGeneration) return;
      isListening.value = false;
      final finalTranscript = _submissionGate.release();
      if (finalTranscript != null) {
        await _submitFinalTranscript(finalTranscript);
        return;
      }
      if (!isAwaitingResponse.value) {
        if (_transcript.trim().isEmpty) {
          _notice('没有识别到语音');
        } else {
          _notice('语音识别未产生最终结果');
        }
      }
      transcriptPreview.value = '';
    } finally {
      if (generation == _recognitionGeneration) {
        isListening.value = false;
        _pressActive = false;
      }
    }
  }

  Future<void> cancelListening() async {
    _pressActive = false;
    _recognitionGeneration += 1;
    _submissionGate.cancel();
    transcriptPreview.value = '';
    try {
      await _transcriber.cancel();
    } catch (error) {
      _notice('取消语音识别失败：$error', isError: true);
    }
    isListening.value = false;
  }

  Future<bool> _ensureSpeechInitialized() async {
    if (_speechInitialized) return true;
    _initializing = true;
    try {
      _speechInitialized = await _transcriber.initialize();
      return _speechInitialized;
    } finally {
      _initializing = false;
    }
  }

  void _handleRecognitionResult(VoiceRecognitionUpdate update) {
    final words = update.transcript.trim();
    _transcript = words;
    transcriptPreview.value = words;
    final ready = _submissionGate.acceptRecognitionResult(
      transcript: words,
      isFinal: update.isFinal,
    );
    if (!_pressActive && ready != null) {
      unawaited(_submitFinalTranscript(ready));
    }
  }

  void _failRecognition(String message) {
    _pressActive = false;
    _recognitionGeneration += 1;
    _submissionGate.cancel();
    transcriptPreview.value = '';
    isListening.value = false;
    _notice(message, isError: true);
  }

  Future<void> _submitFinalTranscript(String value) async {
    if (_submitting) return;
    final normalized = value.trim();
    if (normalized.isEmpty) return;
    _submitting = true;
    try {
      transcriptPreview.value = '';
      // 录音开始时输入框是空的，停止后用户又打了字：迟到结果不要再追加进去。
      if (!_startedWithDraft && _chat.draftText.trim().isNotEmpty) return;
      if (!_chat.applyTranscriptToDraft(normalized)) {
        _notice('语音内容未能写入输入框，请检查输入状态', isError: true);
      }
    } finally {
      _submitting = false;
    }
  }

  Future<void> _trySpeakCompletedResponse() async {
    if (_disposed || !isAwaitingResponse.value) return;
    final dispatch = _pendingDispatch;
    if (dispatch == null || _chat.sessionId != dispatch.sessionId) {
      _clearPendingResponse();
      return;
    }
    final commandGeneration = _commandGeneration;
    if (_speakingCommandGeneration == commandGeneration) return;
    switch (_chat.agentStateFor(dispatch)) {
      case VoiceCommandAgentState.busy:
        return;
      case VoiceCommandAgentState.failed:
      case VoiceCommandAgentState.stopped:
        _clearPendingResponse();
        return;
      case VoiceCommandAgentState.idle:
      case VoiceCommandAgentState.completed:
        break;
    }

    final response = _chat.latestCompletedResponseAfter(dispatch);
    if (response == null || response.text.trim().isEmpty) return;
    final lifecycleGeneration = _lifecycleGeneration;
    _speakingCommandGeneration = commandGeneration;
    try {
      await _speaker.stop();
      if (!_ownsPendingCommand(
        dispatch: dispatch,
        commandGeneration: commandGeneration,
        lifecycleGeneration: lifecycleGeneration,
      )) {
        return;
      }
      await _speaker.speak(
        response.text.trim(),
        languageTag: _chat.speechLanguageTag,
      );
      if (_ownsPendingCommand(
        dispatch: dispatch,
        commandGeneration: commandGeneration,
        lifecycleGeneration: lifecycleGeneration,
      )) {
        _clearPendingResponse();
      }
    } catch (error) {
      if (!_ownsPendingCommand(
        dispatch: dispatch,
        commandGeneration: commandGeneration,
        lifecycleGeneration: lifecycleGeneration,
      )) {
        return;
      }
      _clearPendingResponse();
      _notice('语音播报失败：$error', isError: true);
    } finally {
      if (_speakingCommandGeneration == commandGeneration) {
        _speakingCommandGeneration = null;
      }
    }
  }

  bool _ownsPendingCommand({
    required VoiceCommandDispatch dispatch,
    required int commandGeneration,
    required int lifecycleGeneration,
  }) {
    return !_disposed &&
        lifecycleGeneration == _lifecycleGeneration &&
        commandGeneration == _commandGeneration &&
        identical(_pendingDispatch, dispatch) &&
        _chat.sessionId == dispatch.sessionId;
  }

  void _clearPendingResponse() {
    _responseTimer?.cancel();
    _responseTimer = null;
    isAwaitingResponse.value = false;
    _pendingDispatch = null;
  }

  bool get hasActiveLifecycle =>
      isCapturingSpeech ||
      isAwaitingResponse.value ||
      _speakingCommandGeneration != null ||
      transcriptPreview.value.isNotEmpty;

  bool get isCapturingSpeech =>
      _pressActive ||
      _listenStartOwner != null ||
      isListening.value ||
      _stopOperation != null;

  void deactivateForExternalAction() {
    if (_disposed || !hasActiveLifecycle) return;
    _lifecycleGeneration += 1;
    _commandGeneration += 1;
    _pressActive = false;
    _recognitionGeneration += 1;
    _submissionGate.cancel();
    transcriptPreview.value = '';
    isListening.value = false;
    _clearPendingResponse();
    _speakingCommandGeneration = null;
    unawaited(_transcriber.cancel());
    unawaited(_speaker.stop());
  }

  void dispose() {
    if (_disposed) return;
    deactivateForExternalAction();
    _disposed = true;
    _disposeObserver?.call();
    _disposeObserver = null;
    unawaited(_transcriber.cancel());
    unawaited(_speaker.stop());
  }
}
