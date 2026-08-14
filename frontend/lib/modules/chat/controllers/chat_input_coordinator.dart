import '../services/chat_managed_input.dart';
import '../services/chat_viewport_intent.dart';

class ChatInputCoordinator {
  ChatManagedInputId? _activeInputId;
  ChatManagedInputPolicy? _activeInputPolicy;
  ChatViewportAnchor? _capturedAnchor;
  ChatViewportAnchor? _pendingRestoreAnchor;
  bool _activationStartedAtBottom = false;
  bool _userScrolledAfterActivation = false;
  bool _pendingKeepBottom = false;

  ChatManagedInputId? get activeInputId => _activeInputId;
  ChatManagedInputPolicy? get activeInputPolicy => _activeInputPolicy;
  ChatViewportAnchor? get pendingRestoreAnchor => _pendingRestoreAnchor;
  bool get hasManagedInputFocus => _activeInputId != null;
  bool get shouldRestoreBottom => _pendingKeepBottom;

  void activate({
    required ChatManagedInputId inputId,
    required ChatManagedInputPolicy policy,
    required bool startedAtBottom,
    ChatViewportAnchor? anchor,
  }) {
    _activeInputId = inputId;
    _activeInputPolicy = policy;
    _capturedAnchor = anchor;
    _activationStartedAtBottom = startedAtBottom;
    _userScrolledAfterActivation = false;
    _pendingRestoreAnchor = null;
    _pendingKeepBottom = false;
  }

  void deactivate(ChatManagedInputId inputId) {
    if (_activeInputId != inputId) {
      return;
    }

    if (!_userScrolledAfterActivation) {
      switch (_activeInputPolicy?.restoreMode ??
          ChatManagedInputRestoreMode.none) {
        case ChatManagedInputRestoreMode.keepBottom:
          _pendingKeepBottom = _activationStartedAtBottom;
          _pendingRestoreAnchor = null;
          break;
        case ChatManagedInputRestoreMode.restoreAnchor:
          _pendingRestoreAnchor = _capturedAnchor;
          _pendingKeepBottom = false;
          break;
        case ChatManagedInputRestoreMode.none:
          _pendingKeepBottom = false;
          _pendingRestoreAnchor = null;
          break;
      }
    } else {
      _pendingKeepBottom = false;
      _pendingRestoreAnchor = null;
    }

    _activeInputId = null;
    _activeInputPolicy = null;
    _capturedAnchor = null;
    _activationStartedAtBottom = false;
    _userScrolledAfterActivation = false;
  }

  void onUserScrollTakeover() {
    if (_activeInputId == null &&
        !_pendingKeepBottom &&
        _pendingRestoreAnchor == null) {
      return;
    }
    _userScrolledAfterActivation = true;
    _pendingKeepBottom = false;
    _pendingRestoreAnchor = null;
  }

  void clearPendingRestore() {
    _pendingKeepBottom = false;
    _pendingRestoreAnchor = null;
  }
}
