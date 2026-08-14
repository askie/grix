import 'chat_bottom_obstruction_observer_base.dart';
import 'chat_bottom_obstruction_observer_stub.dart'
    if (dart.library.js_interop) 'chat_bottom_obstruction_observer_web.dart'
    as impl;

export 'chat_bottom_obstruction_observer_base.dart';

ChatBottomObstructionObserver createChatBottomObstructionObserver() {
  return impl.createChatBottomObstructionObserver();
}
