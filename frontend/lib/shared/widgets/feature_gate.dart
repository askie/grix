import 'package:flutter/widgets.dart';
import 'package:grix/data/providers/feature_flag_service.dart';
import 'package:get/get.dart';

/// A widget that reactively shows/hides its child based on a feature flag.
///
/// Wraps the check in [Obx] so the widget rebuilds when features change
/// (e.g., after a background server refresh updates the cache).
///
/// Usage:
/// ```dart
/// FeatureGate(
///   feature: 'voice_call',
///   child: VoiceCallButton(),
/// )
/// ```
class FeatureGate extends StatelessWidget {
  const FeatureGate({
    super.key,
    required this.feature,
    required this.child,
    this.fallback,
  });

  /// The feature key to check.
  final String feature;

  /// The widget to show when the feature is enabled.
  final Widget child;

  /// Optional widget to show when the feature is disabled.
  /// If null, nothing is rendered when the feature is disabled.
  final Widget? fallback;

  @override
  Widget build(BuildContext context) {
    // If FeatureFlagService is not registered, default to hidden.
    if (!Get.isRegistered<FeatureFlagService>()) {
      return fallback ?? const SizedBox.shrink();
    }

    final service = Get.find<FeatureFlagService>();

    // Obx makes this reactive: rebuilds when service.features changes
    return Obx(() {
      if (service.isEnabled(feature)) {
        return child;
      }
      return fallback ?? const SizedBox.shrink();
    });
  }
}
