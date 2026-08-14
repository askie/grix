import 'dart:async';

import 'chat_bottom_obstruction_observer_base.dart';

ChatBottomObstructionObserver createChatBottomObstructionObserver() {
  return const _StubChatBottomObstructionObserver();
}

class _StubChatBottomObstructionObserver
    implements ChatBottomObstructionObserver {
  const _StubChatBottomObstructionObserver();

  @override
  double get currentBottomObstruction => 0;

  @override
  Stream<double> get onChanged => const Stream<double>.empty();

  @override
  void dispose() {}
}
