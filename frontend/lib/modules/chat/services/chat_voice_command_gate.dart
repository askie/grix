class ChatVoiceCommandGate {
  bool _released = false;
  bool _submitted = false;
  bool _cancelled = false;
  String _finalTranscript = '';

  void reset() {
    _released = false;
    _submitted = false;
    _cancelled = false;
    _finalTranscript = '';
  }

  void cancel() {
    _cancelled = true;
    _finalTranscript = '';
  }

  String? acceptRecognitionResult({
    required String transcript,
    required bool isFinal,
  }) {
    if (_cancelled) return null;
    if (isFinal) {
      _finalTranscript = transcript.trim();
    }
    return _takeIfReady();
  }

  String? release() {
    if (_cancelled) return null;
    _released = true;
    return _takeIfReady();
  }

  String? _takeIfReady() {
    if (_cancelled || !_released || _submitted || _finalTranscript.isEmpty) {
      return null;
    }
    _submitted = true;
    return _finalTranscript;
  }
}
