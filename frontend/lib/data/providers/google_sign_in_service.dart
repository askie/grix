import 'google_sign_in_service_impl_io.dart'
    if (dart.library.js_interop) 'google_sign_in_service_impl_web.dart'
    as impl;

import 'auth_service.dart';

class GoogleSignInService {
  GoogleSignInService({impl.GoogleSignInServiceImpl? implementation})
    : _implementation = implementation ?? impl.createGoogleSignInServiceImpl();

  final impl.GoogleSignInServiceImpl _implementation;

  bool get isSupported => _implementation.isSupported;

  Future<ServiceResult<String>> signIn() => _implementation.signIn();
}
