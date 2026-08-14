import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../data/providers/auth_service.dart';
import '../grix_app.dart';
import 'app_initializer.dart';
import 'bootstrap_loading_shell.dart';

class AppBootstrap extends StatefulWidget {
  const AppBootstrap({super.key});

  @override
  State<AppBootstrap> createState() => _AppBootstrapState();
}

class _AppBootstrapState extends State<AppBootstrap> {
  bool _isLoading = true;
  Object? _bootstrapError;
  AppBootstrapData? _bootstrapData;

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
      final data = await AppInitializer.bootstrap();
      if (!mounted) {
        return;
      }
      setState(() {
        _bootstrapData = data;
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

  @override
  Widget build(BuildContext context) {
    final bootstrapData = _bootstrapData;
    if (!_isLoading && bootstrapData != null) {
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
