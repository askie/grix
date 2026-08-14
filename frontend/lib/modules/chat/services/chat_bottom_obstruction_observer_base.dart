import 'dart:async';

abstract interface class ChatBottomObstructionObserver {
  double get currentBottomObstruction;

  Stream<double> get onChanged;

  void dispose();
}
