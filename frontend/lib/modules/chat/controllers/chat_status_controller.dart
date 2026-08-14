part of 'chat_controller.dart';

enum _ChatStatusIndicatorSource { agentOutput, sessionActivity }

class _ChatStatusIndicatorState {
  const _ChatStatusIndicatorState({required this.source, required this.label});

  final _ChatStatusIndicatorSource source;
  final String label;
}

class _ChatStatusController {
  const _ChatStatusController(this.owner);

  final ChatController owner;

  _ChatStatusIndicatorState? get current {
    if (owner._chatDelegateController.hasActiveAgentOutput ||
        owner._chatDelegateController.hasVisibleStreamingAgentOutput) {
      final label = owner._chatDelegateController.agentOutputLabel.trim();
      if (label.isEmpty) {
        return null;
      }
      return _ChatStatusIndicatorState(
        source: _ChatStatusIndicatorSource.agentOutput,
        label: label,
      );
    }

    if (!owner._chatDelegateController.hasSessionActivity) {
      return null;
    }
    final label = owner._chatDelegateController.sessionActivityLabel.trim();
    if (label.isEmpty) {
      return null;
    }
    return _ChatStatusIndicatorState(
      source: _ChatStatusIndicatorSource.sessionActivity,
      label: label,
    );
  }
}
