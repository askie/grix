/// Combines the remote `voice_command` feature gate with local platform
/// support. Keep this pure so gate × platform matrices stay unit-testable.
bool isVoiceCommandEntrySupported({
  required bool featureEnabled,
  required bool platformSupported,
}) => featureEnabled && platformSupported;
