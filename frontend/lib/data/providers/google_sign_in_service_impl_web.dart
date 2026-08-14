// ignore_for_file: non_constant_identifier_names

import 'dart:async';
import 'dart:js_interop';

import 'package:get/get.dart';
import 'package:web/web.dart' as web;

import 'auth_service.dart';

const String _googleIdentityClientScriptUrl =
    'https://accounts.google.com/gsi/client';
const String _configuredGoogleWebClientID = String.fromEnvironment(
  'GOOGLE_WEB_CLIENT_ID',
  defaultValue: '',
);

@JS('google')
external JSAny? get _googleAny;

extension type _Google._(JSObject _) implements JSObject {
  external _GoogleAccounts get accounts;
}

extension type _GoogleAccounts._(JSObject _) implements JSObject {
  external _GoogleIdentity get id;
}

extension type _GoogleIdentity._(JSObject _) implements JSObject {
  external void initialize(_GoogleIdentityConfiguration configuration);
  external void prompt(JSFunction callback);
}

extension type _GoogleIdentityConfiguration._(JSObject _) implements JSObject {
  external factory _GoogleIdentityConfiguration({
    String client_id,
    JSFunction callback,
    String ux_mode,
    bool auto_select,
    bool cancel_on_tap_outside,
    String context,
  });
}

extension type _GoogleCredentialResponse._(JSObject _) implements JSObject {
  external String? get credential;
}

extension type _GooglePromptMomentNotification._(JSObject _)
    implements JSObject {
  external bool isDismissedMoment();
  external bool isSkippedMoment();
  external bool isNotDisplayed();
  external String? getNotDisplayedReason();
}

class GoogleSignInServiceImpl {
  Future<void>? _scriptReadyFuture;
  Completer<ServiceResult<String>>? _pendingSignIn;

  bool get isSupported => _configuredGoogleWebClientID.trim().isNotEmpty;

  Future<ServiceResult<String>> signIn() async {
    if (!isSupported) {
      return ServiceResult<String>.failure(
        message: 'login_google_not_configured'.tr,
      );
    }

    final inflight = _pendingSignIn;
    if (inflight != null) {
      return inflight.future;
    }

    final completer = Completer<ServiceResult<String>>();
    _pendingSignIn = completer;
    try {
      await _ensureGoogleIdentityLoaded();

      final identityApi = _googleIdentityIdApi();
      if (identityApi == null) {
        return ServiceResult<String>.failure(
          message: 'login_google_error_failed'.tr,
        );
      }

      final credentialCallback = ((JSAny? responseAny) {
        if (completer.isCompleted) {
          return;
        }
        if (responseAny == null || responseAny.isUndefinedOrNull) {
          completer.complete(
            ServiceResult<String>.failure(
              message: 'login_google_error_failed'.tr,
            ),
          );
          return;
        }

        final response = responseAny as _GoogleCredentialResponse;
        final idToken = (response.credential ?? '').trim();
        if (idToken.isEmpty) {
          completer.complete(
            ServiceResult<String>.failure(
              message: 'login_google_error_failed'.tr,
            ),
          );
          return;
        }

        completer.complete(ServiceResult<String>.success(data: idToken));
      }).toJS;

      final promptCallback = ((JSAny? notificationAny) {
        if (completer.isCompleted ||
            notificationAny == null ||
            notificationAny.isUndefinedOrNull) {
          return;
        }

        final notification = notificationAny as _GooglePromptMomentNotification;
        if (notification.isDismissedMoment() ||
            notification.isSkippedMoment()) {
          completer.complete(
            ServiceResult<String>.failure(
              message: 'login_google_cancelled'.tr,
            ),
          );
          return;
        }

        if (notification.isNotDisplayed()) {
          completer.complete(
            ServiceResult<String>.failure(
              message: _notDisplayedMessage(notification),
            ),
          );
        }
      }).toJS;

      identityApi.initialize(
        _GoogleIdentityConfiguration(
          client_id: _configuredGoogleWebClientID.trim(),
          callback: credentialCallback,
          ux_mode: 'popup',
          auto_select: false,
          cancel_on_tap_outside: true,
          context: 'signin',
        ),
      );
      identityApi.prompt(promptCallback);

      return await completer.future.timeout(
        const Duration(seconds: 30),
        onTimeout: () =>
            ServiceResult<String>.failure(message: 'auth_error_timeout'.tr),
      );
    } catch (_) {
      return ServiceResult<String>.failure(
        message: 'login_google_error_failed'.tr,
      );
    } finally {
      if (identical(_pendingSignIn, completer)) {
        _pendingSignIn = null;
      }
    }
  }

  Future<void> _ensureGoogleIdentityLoaded() {
    if (_googleIdentityIdApi() != null) {
      return Future<void>.value();
    }

    final inflight = _scriptReadyFuture;
    if (inflight != null) {
      return inflight;
    }

    final future = _loadGoogleIdentityScript();
    _scriptReadyFuture = future;
    return future;
  }

  Future<void> _loadGoogleIdentityScript() async {
    final existing = web.document.querySelector(
      'script[data-google-identity-client="1"]',
    );
    if (existing == null) {
      final script = web.HTMLScriptElement()
        ..src = _googleIdentityClientScriptUrl
        ..async = true
        ..defer = true;
      script.setAttribute('data-google-identity-client', '1');
      web.document.head?.append(script);
    }

    final deadline = DateTime.now().add(const Duration(seconds: 8));
    while (DateTime.now().isBefore(deadline)) {
      if (_googleIdentityIdApi() != null) {
        return;
      }
      await Future<void>.delayed(const Duration(milliseconds: 100));
    }
    throw StateError('google identity script load timeout');
  }

  _GoogleIdentity? _googleIdentityIdApi() {
    final googleAny = _googleAny;
    if (googleAny == null || googleAny.isUndefinedOrNull) {
      return null;
    }
    final google = googleAny as _Google;
    return google.accounts.id;
  }

  String _notDisplayedMessage(_GooglePromptMomentNotification notification) {
    switch ((notification.getNotDisplayedReason() ?? '').trim()) {
      case 'invalid_client':
      case 'missing_client_id':
      case 'unregistered_origin':
        return 'login_google_not_configured'.tr;
      default:
        return 'login_google_error_failed'.tr;
    }
  }
}

GoogleSignInServiceImpl createGoogleSignInServiceImpl() {
  return GoogleSignInServiceImpl();
}
