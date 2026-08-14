import 'dart:convert';
import 'dart:js_interop';
import 'dart:typed_data';

import 'package:web/web.dart' as web;

const String _webPushVapidPublicKey = String.fromEnvironment(
  'WEB_PUSH_VAPID_PUBLIC_KEY',
  defaultValue: '',
);

bool get isWebPushBindingSupported {
  if (_webPushVapidPublicKey.trim().isEmpty) {
    return false;
  }
  return _supportsWebPushRuntime();
}

bool _supportsWebPushRuntime() {
  try {
    web.Notification.permission;
    final serviceWorker = web.window.navigator.serviceWorker;
    serviceWorker.ready;
    return true;
  } catch (_) {
    return false;
  }
}

Future<Map<String, String>?> resolveWebPushBinding() async {
  if (!isWebPushBindingSupported) {
    return null;
  }

  final permission = await _ensureNotificationPermission();
  if (permission != 'granted') {
    return null;
  }

  final web.ServiceWorkerRegistration registration;
  try {
    registration = await web.window.navigator.serviceWorker.ready.toDart;
  } catch (_) {
    return null;
  }
  final applicationServerKey = _decodeVapidKey(_webPushVapidPublicKey);
  var subscription = await registration.pushManager.getSubscription().toDart;
  if (subscription != null &&
      !_subscriptionUsesApplicationServerKey(
        subscription,
        applicationServerKey,
      )) {
    final unsubscribed = await subscription.unsubscribe().toDart;
    if (!unsubscribed.toDart) {
      return null;
    }
    subscription = null;
  }
  subscription ??= await registration.pushManager
      .subscribe(
        web.PushSubscriptionOptionsInit(
          userVisibleOnly: true,
          applicationServerKey: applicationServerKey.toJS,
        ),
      )
      .toDart;

  final token = _serializeSubscription(subscription);
  if (token == null) {
    return null;
  }

  return <String, String>{
    'platform': 'web_push',
    'pushEnv': 'default',
    'deviceToken': token,
  };
}

Future<String> _ensureNotificationPermission() async {
  final current = notificationPermissionState;
  if (current == 'granted' || current == 'denied') {
    return current;
  }
  return current;
}

/// Request notification permission with a user gesture.
/// Must be called from a tap handler or similar user-initiated event.
Future<String> requestNotificationPermissionWithGesture() async {
  final current = notificationPermissionState;
  if (current == 'granted' || current == 'denied' || current == 'unsupported') {
    return current;
  }
  try {
    return (await web.Notification.requestPermission().toDart).toString();
  } catch (_) {
    return 'unsupported';
  }
}

/// Get current notification permission state without requesting.
/// Returns 'unsupported' if the Notification API is unavailable.
String get notificationPermissionState {
  try {
    return web.Notification.permission;
  } catch (_) {
    return 'unsupported';
  }
}

Uint8List _decodeVapidKey(String value) {
  final normalized = value.trim();
  final padded = base64.normalize(
    normalized.replaceAll('-', '+').replaceAll('_', '/'),
  );
  return base64.decode(padded);
}

bool _subscriptionUsesApplicationServerKey(
  web.PushSubscription subscription,
  Uint8List expectedKey,
) {
  final actualBuffer = subscription.options.applicationServerKey;
  if (actualBuffer == null) {
    return false;
  }
  final actual = actualBuffer.toDart.asUint8List();
  if (actual.length != expectedKey.length) {
    return false;
  }
  for (var i = 0; i < actual.length; i++) {
    if (actual[i] != expectedKey[i]) {
      return false;
    }
  }
  return true;
}

String? _serializeSubscription(web.PushSubscription subscription) {
  final raw = subscription.toJSON().dartify();
  if (raw is! Map) {
    return null;
  }

  final endpoint = raw['endpoint']?.toString().trim() ?? '';
  final keys = raw['keys'];
  if (endpoint.isEmpty || keys is! Map) {
    return null;
  }

  final p256dh = keys['p256dh']?.toString().trim() ?? '';
  final auth = keys['auth']?.toString().trim() ?? '';
  if (p256dh.isEmpty || auth.isEmpty) {
    return null;
  }

  return jsonEncode(<String, Object>{
    'endpoint': endpoint,
    'keys': <String, String>{'p256dh': p256dh, 'auth': auth},
  });
}
