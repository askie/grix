import 'package:flutter/material.dart';

import '../../app/routes/app_routes.dart';
import 'private_chat_creating_view.dart';

/// Dedicated route for the private-chat creating shell.
///
/// GetX [GetPageRoute] always inherits [PageRoute.allowSnapshotting] == true.
/// On Android (Zoom transitions) and some iOS handoffs, that lets
/// [SnapshotWidget] replace the live page with a frozen bitmap while the
/// route animation reports [AnimationStatus.forward]. This route disables
/// snapshotting and uses a zero-length transition so the creating status
/// text stays a live widget tree.
class PrivateChatCreatingRoute extends PageRoute<void> {
  PrivateChatCreatingRoute({required Map<String, dynamic> arguments})
    : super(
        settings: RouteSettings(
          name: AppRoutes.privateChatCreating,
          arguments: arguments,
        ),
        allowSnapshotting: false,
      );

  @override
  Color? get barrierColor => null;

  @override
  String? get barrierLabel => null;

  @override
  bool get maintainState => true;

  @override
  bool get opaque => true;

  @override
  Duration get transitionDuration => Duration.zero;

  @override
  Duration get reverseTransitionDuration => Duration.zero;

  @override
  Widget buildPage(
    BuildContext context,
    Animation<double> animation,
    Animation<double> secondaryAnimation,
  ) {
    return const PrivateChatCreatingView();
  }

  @override
  Widget buildTransitions(
    BuildContext context,
    Animation<double> animation,
    Animation<double> secondaryAnimation,
    Widget child,
  ) {
    return child;
  }
}
