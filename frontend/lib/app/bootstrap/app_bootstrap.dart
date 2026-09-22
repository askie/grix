import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../data/providers/auth_service.dart';
import '../../modules/auth/privacy_consent_gate_view.dart';
import '../../shared/services/privacy_consent_store.dart';
import '../grix_app.dart';
import '../locale/app_material_localizations.dart';
import '../locale/locale_service.dart';
import '../themes/app_theme.dart';
import 'app_initializer.dart';
import 'bootstrap_loading_shell.dart';

class AppBootstrap extends StatefulWidget {
  const AppBootstrap({super.key, this.bootstrapLoader});

  /// Test seam: replaces [AppInitializer.bootstrap] so widget tests can exercise
  /// the real privacy-gate / GrixApp path without spinning up all services.
  @visibleForTesting
  final Future<AppBootstrapData> Function()? bootstrapLoader;

  @override
  State<AppBootstrap> createState() => _AppBootstrapState();
}

class _AppBootstrapState extends State<AppBootstrap> {
  bool _isLoading = true;
  Object? _bootstrapError;
  AppBootstrapData? _bootstrapData;
  bool _awaitingPrivacyConsent = false;

  @override
  void initState() {
    super.initState();
    _bootstrap();
  }

  Future<void> _bootstrap() async {
    if (mounted) {
      setState(() {
        _isLoading = true;
        _bootstrapError = null;
      });
    }

    try {
      final loader = widget.bootstrapLoader ?? AppInitializer.bootstrap;
      final data = await loader();
      // Android first-launch gate: do not mount GrixApp (which starts deferred
      // push / device-info init) until the user accepts the privacy policy.
      final awaitingPrivacy = PrivacyConsentStore.isGateRequired &&
          !await PrivacyConsentStore.hasAcceptedCurrentVersion();
      if (!mounted) {
        return;
      }
      if (awaitingPrivacy) {
        final locale =
            data.initialLocale ?? LocaleService.fallbackLocale;
        Get.locale = locale;
        Get.fallbackLocale = LocaleService.fallbackLocale;
        Get.addTranslations(data.translations.keys);
      }
      setState(() {
        _bootstrapData = data;
        _awaitingPrivacyConsent = awaitingPrivacy;
        _isLoading = false;
      });
    } catch (error, stackTrace) {
      debugPrint('App bootstrap failed: $error');
      debugPrintStack(stackTrace: stackTrace);
      if (!mounted) {
        return;
      }
      setState(() {
        _bootstrapError = error;
        _isLoading = false;
      });
    }
  }

  void _onPrivacyAccepted() {
    if (!mounted) {
      return;
    }
    setState(() {
      _awaitingPrivacyConsent = false;
    });
  }

  @override
  Widget build(BuildContext context) {
    final bootstrapData = _bootstrapData;
    if (!_isLoading && bootstrapData != null) {
      if (_awaitingPrivacyConsent) {
        // Use a plain MaterialApp (not GetMaterialApp) so the first-launch gate
        // does not claim GetX's singleton Navigator GlobalKey. Replacing that
        // temporary GetMaterialApp with GrixApp would otherwise reuse the same
        // key and keep the privacy-gate route stack — Agree appears to no-op.
        final locale =
            bootstrapData.initialLocale ?? LocaleService.fallbackLocale;
        return MaterialApp(
          debugShowCheckedModeBanner: false,
          theme: AppTheme.lightTheme,
          darkTheme: AppTheme.darkTheme,
          locale: locale,
          localizationsDelegates: AppMaterialLocalizations.delegates,
          supportedLocales: AppMaterialLocalizations.supportedLocales,
          home: PrivacyConsentGateView(
            onAccepted: () async => _onPrivacyAccepted(),
          ),
        );
      }

      final isLoggedIn =
          Get.isRegistered<AuthService>() && Get.find<AuthService>().isLoggedIn;
      return GrixApp(
        initialLocale: bootstrapData.initialLocale,
        initialRoute: bootstrapData.resolveInitialRoute(isLoggedIn: isLoggedIn),
        translations: bootstrapData.translations,
      );
    }

    return BootstrapLoadingShell(
      isLoading: _isLoading,
      errorMessage: _bootstrapError?.toString(),
      onRetry: _isLoading ? null : _bootstrap,
    );
  }
}
