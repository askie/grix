part of 'im_service.dart';

class _StreamingSessionPreviewState {
  _StreamingSessionPreviewState({
    required this.msgId,
    required this.sessionId,
    required this.activityAt,
  });

  final String msgId;
  final String sessionId;
  int activityAt;
  String pendingText = '';
  int lastPublishedAtMs = 0;
  bool hasPublished = false;
  bool isComplete = false;
  Timer? timer;
}

extension _ImServiceStreamPreview on ImService {
  // The first visible fragment is intentionally not length-gated: short
  // replies such as "OK" / "收到" are useful interaction signals. A small
  // leading debounce merges token-sized chunks; later updates are throttled
  // to keep the conversation list stable while the answer streams.
  static const Duration _streamingPreviewInitialDelay = Duration(
    milliseconds: 180,
  );
  static const Duration _streamingPreviewThrottle = Duration(milliseconds: 650);
  // A conversation row only renders one line. Once a cleaned preview reaches
  // this size, later chunks cannot improve the visible summary, so stop
  // repeatedly summarizing the ever-growing stream.
  static const int _streamingPreviewMaxRunes = 120;

  String _streamingSessionPreviewForSession(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return '';
    _streamingSessionPreviewTick.value;
    return _streamingSessionPreviewTexts[sid] ?? '';
  }

  int _streamingSessionPreviewUpdatedAtForSession(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return 0;
    // Read the Rx map as well so an Obx consumer that only asks for the
    // timestamp still subscribes to preview changes.
    _streamingSessionPreviewTexts[sid];
    return _streamingSessionPreviewUpdatedAt[sid] ?? 0;
  }

  void _stageStreamingSessionPreview({
    required String msgId,
    required String sessionId,
    required int activityAt,
    required bool isThinking,
  }) {
    final normalizedMsgId = msgId.trim();
    final sid = sessionId.trim();
    if (normalizedMsgId.isEmpty || sid.isEmpty) return;

    if (isThinking) {
      _discardStreamingSessionPreview(normalizedMsgId);
      return;
    }

    final state = _streamingSessionPreviewStates.putIfAbsent(
      normalizedMsgId,
      () => _StreamingSessionPreviewState(
        msgId: normalizedMsgId,
        sessionId: sid,
        activityAt: activityAt,
      ),
    );
    if (state.sessionId != sid) {
      _discardStreamingSessionPreview(normalizedMsgId);
      _stageStreamingSessionPreview(
        msgId: normalizedMsgId,
        sessionId: sid,
        activityAt: activityAt,
        isThinking: false,
      );
      return;
    }
    if (state.isComplete) {
      return;
    }
    if (activityAt > state.activityAt) {
      state.activityAt = activityAt;
    }

    final accumulated = MessageStreamController.peekContent(normalizedMsgId);
    final summarized = ChatMessagePreview.summarize(accumulated).trim();
    if (summarized.isEmpty) {
      return;
    }
    final previewRunes = summarized.runes
        .take(_streamingPreviewMaxRunes)
        .toList(growable: false);
    state.isComplete = previewRunes.length >= _streamingPreviewMaxRunes;
    state.pendingText = state.isComplete
        ? String.fromCharCodes(previewRunes).trimRight()
        : summarized;

    if (state.isComplete) {
      state.timer?.cancel();
      state.timer = null;
      _publishStreamingSessionPreview(normalizedMsgId);
      return;
    }

    final nowMs = DateTime.now().millisecondsSinceEpoch;
    if (!state.hasPublished) {
      state.timer ??= Timer(_streamingPreviewInitialDelay, () {
        _publishStreamingSessionPreview(normalizedMsgId);
      });
      return;
    }

    final elapsedMs = nowMs - state.lastPublishedAtMs;
    final throttleMs = _streamingPreviewThrottle.inMilliseconds;
    if (elapsedMs >= throttleMs) {
      state.timer?.cancel();
      state.timer = null;
      _publishStreamingSessionPreview(normalizedMsgId);
      return;
    }
    state.timer ??= Timer(
      Duration(milliseconds: throttleMs - elapsedMs),
      () => _publishStreamingSessionPreview(normalizedMsgId),
    );
  }

  void _publishStreamingSessionPreview(String msgId) {
    final state = _streamingSessionPreviewStates[msgId];
    if (state == null) return;
    state.timer?.cancel();
    state.timer = null;
    if (!_activeStreamingMsgIds.contains(msgId) ||
        _locallyStoppedStreamMsgIds.contains(msgId) ||
        state.pendingText.isEmpty) {
      return;
    }

    final sid = state.sessionId;
    _streamingSessionPreviewOwnerBySession[sid] = msgId;
    _streamingSessionPreviewUpdatedAt[sid] =
        DateTime.now().millisecondsSinceEpoch;
    _streamingSessionPreviewTexts[sid] = state.pendingText;
    _streamingSessionPreviewTick.value++;

    if (!state.hasPublished) {
      final activityAt = state.activityAt > 0
          ? state.activityAt
          : DateTime.now().millisecondsSinceEpoch;
      _bumpSessionActivityInMemory(sid, activityAt);
    }
    state.hasPublished = true;
    state.lastPublishedAtMs = DateTime.now().millisecondsSinceEpoch;
  }

  void _discardStreamingSessionPreview(String msgId) {
    final normalizedMsgId = msgId.trim();
    if (normalizedMsgId.isEmpty) return;
    final state = _streamingSessionPreviewStates.remove(normalizedMsgId);
    state?.timer?.cancel();
    if (state == null) return;
    final sid = state.sessionId;
    if (_streamingSessionPreviewOwnerBySession[sid] != normalizedMsgId) {
      return;
    }
    _streamingSessionPreviewOwnerBySession.remove(sid);
    _streamingSessionPreviewUpdatedAt.remove(sid);
    _streamingSessionPreviewTexts.remove(sid);
    _streamingSessionPreviewTick.value++;
  }

  void _clearAllStreamingSessionPreviews() {
    for (final state in _streamingSessionPreviewStates.values) {
      state.timer?.cancel();
    }
    final hadPublishedPreviews = _streamingSessionPreviewTexts.isNotEmpty;
    _streamingSessionPreviewStates.clear();
    _streamingSessionPreviewOwnerBySession.clear();
    _streamingSessionPreviewUpdatedAt.clear();
    _streamingSessionPreviewTexts.clear();
    if (hadPublishedPreviews) {
      _streamingSessionPreviewTick.value++;
    }
  }
}
