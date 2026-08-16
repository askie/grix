import 'package:flutter/material.dart';

import '../locale/locale_service.dart';
import '../themes/app_theme.dart';
import '../translations/app_translations.dart';

class BootstrapLoadingShell extends StatefulWidget {
  const BootstrapLoadingShell({
    super.key,
    required this.isLoading,
    this.errorMessage,
    this.onRetry,
  });

  final bool isLoading;
  final String? errorMessage;
  final VoidCallback? onRetry;

  @override
  State<BootstrapLoadingShell> createState() => _BootstrapLoadingShellState();
}

class _BootstrapLoadingShellState extends State<BootstrapLoadingShell> {
  AppTranslations _translations = AppTranslations();
  bool _translationLoadStarted = false;

  @override
  void initState() {
    super.initState();
    if (_hasError(widget.errorMessage)) {
      _loadTranslations();
    }
  }

  @override
  void didUpdateWidget(covariant BootstrapLoadingShell oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!_hasError(oldWidget.errorMessage) && _hasError(widget.errorMessage)) {
      _loadTranslations();
    }
  }

  Future<void> _loadTranslations() async {
    if (_translationLoadStarted) return;
    _translationLoadStarted = true;
    try {
      final loaded = await AppTranslations.load();
      if (!mounted) return;
      setState(() => _translations = loaded);
    } catch (_) {
      // 启动失败页仍用已缓存/空翻译兜底，避免二次把启动流程打挂。
    }
  }

  Locale get _locale {
    final raw = WidgetsBinding.instance.platformDispatcher.locale;
    for (final entry in LocaleService.supportedLocales) {
      if (entry.locale.languageCode == raw.languageCode) {
        return entry.locale;
      }
    }
    return const Locale('en', 'US');
  }

  bool _hasError(String? message) =>
      message != null && message.trim().isNotEmpty;

  String _text(String key, String fallback) {
    final locale = _locale;
    final language = locale.languageCode;
    final country = locale.countryCode;
    final localeKey = country == null || country.isEmpty
        ? language
        : '${language}_$country';
    final keys = _translations.keys;
    return keys[localeKey]?[key] ??
        keys[language]?[key] ??
        keys['en_US']?[key] ??
        fallback;
  }

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      debugShowCheckedModeBanner: false,
      title: 'Grix',
      theme: AppTheme.lightTheme,
      onGenerateRoute: (settings) => MaterialPageRoute<void>(
        settings: settings,
        builder: (_) => _BootstrapLoadingShellBody(
          isLoading: widget.isLoading,
          errorMessage: widget.errorMessage,
          onRetry: widget.onRetry,
          failedText: _text(
            'bootstrap_failed',
            'Startup failed. Please retry.',
          ),
          retryText: _text('common_retry', 'Retry'),
        ),
      ),
      onUnknownRoute: (settings) => MaterialPageRoute<void>(
        settings: settings,
        builder: (_) => _BootstrapLoadingShellBody(
          isLoading: widget.isLoading,
          errorMessage: widget.errorMessage,
          onRetry: widget.onRetry,
          failedText: _text(
            'bootstrap_failed',
            'Startup failed. Please retry.',
          ),
          retryText: _text('common_retry', 'Retry'),
        ),
      ),
    );
  }
}

class _BootstrapLoadingShellBody extends StatelessWidget {
  const _BootstrapLoadingShellBody({
    required this.isLoading,
    required this.errorMessage,
    required this.onRetry,
    required this.failedText,
    required this.retryText,
  });

  final bool isLoading;
  final String? errorMessage;
  final VoidCallback? onRetry;
  final String failedText;
  final String retryText;

  bool get _hasError => errorMessage != null && errorMessage!.trim().isNotEmpty;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: DecoratedBox(
        decoration: const BoxDecoration(
          gradient: LinearGradient(
            begin: Alignment.topCenter,
            end: Alignment.bottomCenter,
            colors: [Color(0xFFF7FAFC), Color(0xFFEFF4FF)],
          ),
        ),
        child: SafeArea(
          child: Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 320),
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 24),
                child: Column(
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: [
                    Text(
                      'Grix',
                      style: Theme.of(context).textTheme.headlineMedium
                          ?.copyWith(
                            fontSize: 44,
                            fontWeight: FontWeight.w800,
                            letterSpacing: 0.4,
                            color: AppTheme.primaryDark,
                          ),
                    ),
                    SizedBox(height: _hasError ? 10 : 28),
                    if (_hasError)
                      Text(
                        failedText,
                        textAlign: TextAlign.center,
                        style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                          color: const Color(0xFF5B6574),
                          height: 1.5,
                        ),
                      ),
                    if (_hasError) const SizedBox(height: 28),
                    if (isLoading)
                      const SizedBox(
                        width: 28,
                        height: 28,
                        child: CircularProgressIndicator(strokeWidth: 2.4),
                      ),
                    if (_hasError) ...[
                      Container(
                        width: double.infinity,
                        padding: const EdgeInsets.all(12),
                        decoration: BoxDecoration(
                          color: const Color(0xFFFFF4F2),
                          borderRadius: BorderRadius.circular(12),
                          border: Border.all(color: const Color(0xFFFFD6CF)),
                        ),
                        child: Text(
                          errorMessage!,
                          textAlign: TextAlign.center,
                          style: Theme.of(context).textTheme.bodySmall
                              ?.copyWith(
                                color: const Color(0xFF9F2D20),
                                height: 1.45,
                              ),
                        ),
                      ),
                      const SizedBox(height: 16),
                      FilledButton(onPressed: onRetry, child: Text(retryText)),
                    ],
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}
