import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Persists first-launch privacy consent for Android (incl. HarmonyOS APK).
///
/// Bump [policyVersion] when the published privacy policy / user agreement
/// changes in a way that requires re-prompting.
class PrivacyConsentStore {
  PrivacyConsentStore._();

  static const String policyVersion = '1';
  static const String prefKey = 'privacy_consent_policy_version';

  /// Huawei / China Android store compliance gate.
  static bool get isGateRequired =>
      !kIsWeb && defaultTargetPlatform == TargetPlatform.android;

  static Future<bool> hasAcceptedCurrentVersion({
    SharedPreferences? prefs,
  }) async {
    if (!isGateRequired) {
      return true;
    }
    final store = prefs ?? await SharedPreferences.getInstance();
    return store.getString(prefKey) == policyVersion;
  }

  static Future<void> acceptCurrentVersion({SharedPreferences? prefs}) async {
    final store = prefs ?? await SharedPreferences.getInstance();
    await store.setString(prefKey, policyVersion);
  }

  /// Test helper: clear stored consent.
  static Future<void> clearForTest({SharedPreferences? prefs}) async {
    final store = prefs ?? await SharedPreferences.getInstance();
    await store.remove(prefKey);
  }
}
