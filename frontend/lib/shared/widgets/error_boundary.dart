import 'package:flutter/material.dart';
import 'package:get/get.dart';

typedef ErrorWidgetBuilder =
    Widget Function(BuildContext context, Object error, StackTrace? stackTrace);

class ErrorBoundary extends StatefulWidget {
  final Widget child;
  final ErrorWidgetBuilder? fallbackBuilder;

  const ErrorBoundary({super.key, required this.child, this.fallbackBuilder});

  @override
  State<ErrorBoundary> createState() => _ErrorBoundaryState();
}

class _ErrorBoundaryState extends State<ErrorBoundary> {
  Object? _error;
  StackTrace? _stackTrace;

  @override
  void initState() {
    super.initState();
    _error = null;
  }

  @override
  void didUpdateWidget(ErrorBoundary oldWidget) {
    if (widget.child != oldWidget.child) {
      setState(() {
        _error = null;
      });
    }
    super.didUpdateWidget(oldWidget);
  }

  @override
  Widget build(BuildContext context) {
    if (_error != null) {
      if (widget.fallbackBuilder != null) {
        return widget.fallbackBuilder!(context, _error!, _stackTrace);
      }
      return Container(
        padding: const EdgeInsets.all(8),
        color: Colors.red.withValues(alpha: 0.1),
        child: Text(
          'error_boundary_render_error'.tr,
          style: const TextStyle(color: Colors.red),
        ),
      );
    }

    // Custom error catching for this specific widget tree branch
    ErrorWidget.builder = (FlutterErrorDetails errorDetails) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && _error == null) {
          setState(() {
            _error = errorDetails.exception;
            _stackTrace = errorDetails.stack;
          });
        }
      });
      return const SizedBox.shrink(); // Hide the default red screen briefly
    };

    return widget.child;
  }
}
