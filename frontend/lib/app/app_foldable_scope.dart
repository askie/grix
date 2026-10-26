import 'package:foldable/foldable.dart';
import 'package:flutter/material.dart';

class AppFoldableScope extends StatelessWidget {
  const AppFoldableScope({super.key, required this.child, this.debugData});

  final Widget child;
  final Stream<FoldableData>? debugData;

  @override
  Widget build(BuildContext context) {
    return FoldableProvider(
      bridgeMode: DisplayFeatureBridgeMode.full,
      debugData: debugData,
      child: ColoredBox(
        color: Theme.of(context).scaffoldBackgroundColor,
        child: DisplayFeatureSubScreen(child: child),
      ),
    );
  }
}
