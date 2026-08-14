import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:get/get.dart';

/// Creating-page status that does not use a Material quarter-arc spinner.
///
/// Past arc-based fixes were visually identical to a frozen
/// `CircularProgressIndicator(value: 0.25)` when stuck at angle 0, so TestFlight
/// screenshots could not prove the new code was running. This widget shows the
/// localized "creating" label plus a wall-clock ellipsis driven by [Timer],
/// independent of [TickerMode] / route [AnimationController]s.
class PrivateChatCreatingStatus extends StatefulWidget {
  const PrivateChatCreatingStatus({super.key});

  @override
  State<PrivateChatCreatingStatus> createState() =>
      PrivateChatCreatingStatusState();
}

@visibleForTesting
class PrivateChatCreatingStatusState extends State<PrivateChatCreatingStatus> {
  static const Duration _tickPeriod = Duration(milliseconds: 400);
  static const List<String> _ellipsisSteps = <String>['', '.', '..', '...'];

  Timer? _timer;
  int _step = 0;

  /// Current ellipsis suffix; exposed for regression tests.
  String get ellipsis => _ellipsisSteps[_step];

  /// Full on-screen label including ellipsis.
  String get label => '${'chat_creating_session'.tr}$ellipsis';

  @override
  void initState() {
    super.initState();
    _timer = Timer.periodic(_tickPeriod, _onTick);
  }

  void _onTick(Timer timer) {
    if (!mounted) {
      return;
    }
    setState(() => _step = (_step + 1) % _ellipsisSteps.length);
  }

  @override
  void dispose() {
    _timer?.cancel();
    _timer = null;
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Text(
      key: const Key('private_chat_creating_status'),
      label,
      style: theme.textTheme.titleMedium?.copyWith(
        color: theme.colorScheme.onSurface.withValues(alpha: 0.65),
      ),
    );
  }
}
