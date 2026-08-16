import 'package:flutter/material.dart';
import 'package:get/get.dart';
import '../../app/themes/app_theme.dart';

String localizedExportResultMessage({
  required bool isDownload,
  required bool isGallery,
  required String location,
  required String kindKey,
}) {
  if (isDownload) {
    return 'chat_export_download_started'.trParams({'location': location});
  }
  if (isGallery) {
    return 'chat_export_saved_gallery'.trParams({'kind': kindKey.tr});
  }
  return 'chat_export_saved_path'.trParams({
    'kind': kindKey.tr,
    'location': location,
  });
}

/// Strips `Exception:` / `FormatException:` prefixes so toasts don't show
/// Dart wrapper text. Framework dumps like `DioException` fall back.
String userFacingError(Object error, {String fallback = ''}) {
  if (error is StateError) {
    return fallback;
  }
  var text = error.toString().trim();
  const prefixes = <String>[
    'Exception: ',
    'FormatException: ',
  ];
  for (final prefix in prefixes) {
    if (text.startsWith(prefix)) {
      text = text.substring(prefix.length).trim();
      break;
    }
  }
  if (text.isEmpty || text.startsWith('DioException')) {
    return fallback;
  }
  return text;
}

class CustomToast {
  static void show(String message, {bool isError = true}) {
    final normalizedMessage = message.trim();
    if (normalizedMessage.isEmpty) return;
    _ToastQueue.instance.add(normalizedMessage, isError);
  }
}

class _ToastItem {
  final String message;
  final bool isError;
  final UniqueKey key = UniqueKey();
  _ToastItem(this.message, this.isError);
}

class _ToastQueue {
  static final instance = _ToastQueue._();
  _ToastQueue._();

  final List<_ToastItem> _items = [];
  OverlayEntry? _entry;

  void add(String message, bool isError, {bool retryIfUnavailable = true}) {
    try {
      final overlay = Get.key.currentState?.overlay;
      if (overlay == null || !overlay.mounted) {
        // On Web the navigator overlay may not be available immediately.
        // Retry once after the next frame.
        if (retryIfUnavailable) {
          _schedulePostFrame(() {
            add(message, isError, retryIfUnavailable: false);
          });
        }
        return;
      }

      final item = _ToastItem(message, isError);
      _items.add(item);

      final existingEntry = _entry;
      if (existingEntry != null) {
        if (existingEntry.mounted) {
          existingEntry.markNeedsBuild();
        }
      } else {
        final entry = OverlayEntry(builder: _buildOverlay);
        _entry = entry;
        _schedulePostFrame(() {
          if (entry != _entry) return;
          final currentOverlay = Get.key.currentState?.overlay;
          if (currentOverlay == null || !currentOverlay.mounted) {
            _entry = null;
            return;
          }
          currentOverlay.insert(entry);
        });
      }

      Future.delayed(const Duration(seconds: 3), () {
        _items.removeWhere((i) => i.key == item.key);
        if (_items.isEmpty) {
          _dismissOverlay();
        } else if (_entry != null && _entry!.mounted) {
          _entry!.markNeedsBuild();
        }
      });
    } catch (error, stackTrace) {
      debugPrint('CustomToast.show failed: $error');
      debugPrintStack(stackTrace: stackTrace);
    }
  }

  void _schedulePostFrame(VoidCallback callback) {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      callback();
    });
    WidgetsBinding.instance.ensureVisualUpdate();
  }

  void _dismissOverlay() {
    final entry = _entry;
    _entry = null;
    if (entry != null && entry.mounted) {
      entry.remove();
    }
  }

  Widget _buildOverlay(BuildContext context) {
    if (_items.isEmpty) return const SizedBox.shrink();

    final topPadding = MediaQuery.maybeOf(context)?.padding.top ?? 0;

    return Positioned(
      top: topPadding + 10,
      left: 20,
      right: 20,
      child: IgnorePointer(
        child: Material(
          color: Colors.transparent,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: _items
                .map((item) => Padding(
                      padding: const EdgeInsets.only(bottom: 8),
                      child: _buildItem(item),
                    ))
                .toList(),
          ),
        ),
      ),
    );
  }

  Widget _buildItem(_ToastItem item) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
      decoration: BoxDecoration(
        color: item.isError ? AppTheme.errorColor : AppTheme.successColor,
        borderRadius: BorderRadius.circular(8),
        boxShadow: const [
          BoxShadow(
            color: Colors.black26,
            blurRadius: 10,
            offset: Offset(0, 4),
          ),
        ],
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            item.isError
                ? Icons.error_outline_rounded
                : Icons.check_circle_outline_rounded,
            color: Colors.white,
            size: 20,
          ),
          const SizedBox(width: 8),
          Flexible(
            child: Text(
              item.message,
              style: const TextStyle(
                color: Colors.white,
                fontSize: 14,
                fontWeight: FontWeight.w500,
              ),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),
          ),
        ],
      ),
    );
  }
}
