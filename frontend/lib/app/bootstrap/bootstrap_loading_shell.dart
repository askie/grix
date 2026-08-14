import 'package:flutter/material.dart';

import '../themes/app_theme.dart';

class BootstrapLoadingShell extends StatelessWidget {
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
  Widget build(BuildContext context) {
    return MaterialApp(
      debugShowCheckedModeBanner: false,
      title: 'Grix',
      theme: AppTheme.lightTheme,
      onGenerateRoute: (settings) => MaterialPageRoute<void>(
        settings: settings,
        builder: (_) => _BootstrapLoadingShellBody(
          isLoading: isLoading,
          errorMessage: errorMessage,
          onRetry: onRetry,
        ),
      ),
      onUnknownRoute: (settings) => MaterialPageRoute<void>(
        settings: settings,
        builder: (_) => _BootstrapLoadingShellBody(
          isLoading: isLoading,
          errorMessage: errorMessage,
          onRetry: onRetry,
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
  });

  final bool isLoading;
  final String? errorMessage;
  final VoidCallback? onRetry;

  bool get _hasError => errorMessage != null && errorMessage!.trim().isNotEmpty;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: DecoratedBox(
        decoration: const BoxDecoration(
          gradient: LinearGradient(
            begin: Alignment.topCenter,
            end: Alignment.bottomCenter,
            colors: [
              Color(0xFFF7FAFC),
              Color(0xFFEFF4FF),
            ],
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
                        '启动失败，请重试',
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
                          style:
                              Theme.of(context).textTheme.bodySmall?.copyWith(
                                    color: const Color(0xFF9F2D20),
                                    height: 1.45,
                                  ),
                        ),
                      ),
                      const SizedBox(height: 16),
                      FilledButton(
                        onPressed: onRetry,
                        child: const Text('重试'),
                      ),
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
