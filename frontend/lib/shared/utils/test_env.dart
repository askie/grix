import 'test_env_impl_stub.dart'
    if (dart.library.js_interop) 'test_env_impl_web.dart' as impl;

/// 是否 flutter test 测试环境（web 上恒 false）。
///
/// 用于在测试里回落 SharedPreferences mock：widget 测试的 FakeAsync 区里
/// flutter_secure_storage 等平台通道永不返回，直连会挂死。
bool get isFlutterTestEnv => impl.isFlutterTestEnv;
