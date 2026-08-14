Future<Map<String, String>?> resolveWebPushBinding() async => null;

bool get isWebPushBindingSupported => false;

Future<String> requestNotificationPermissionWithGesture() async => 'denied';

String get notificationPermissionState => 'unsupported';
