import 'chat_file_interceptor_base.dart';
import 'chat_file_interceptor_stub.dart'
    if (dart.library.js_interop) 'chat_file_interceptor_web.dart'
    as impl;

export 'chat_file_interceptor_base.dart';

ChatFileInterceptor createChatFileInterceptor() {
  return impl.createChatFileInterceptor();
}
