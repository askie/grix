import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';

import '../../platform/platform_capability.dart';

const BorderRadius appDialogBorderRadius = BorderRadius.all(
  Radius.circular(10),
);
const RoundedRectangleBorder appDialogShape = RoundedRectangleBorder(
  borderRadius: appDialogBorderRadius,
);
const EdgeInsets appDialogInsetPadding = EdgeInsets.symmetric(
  horizontal: 12,
  vertical: 24,
);

/// 可用宽度小于该断点视为「紧凑（移动）」，否则视为「宽屏（PC/平板/桌面窗口）」。
const double kDialogCompactBreakpoint = 600;

/// 紧凑端弹窗左右外边距，宽度 = 屏宽 - 2 * 该值。
const double kDialogMobileMargin = 16;

/// 紧凑端按钮的最小触摸高度。
const double kDialogButtonMinHeight = 48;

/// 统一标题字号与字重（两端一致）。
const double kDialogTitleFontSize = 17;
const FontWeight kDialogTitleFontWeight = FontWeight.w600;

/// 宽屏端弹窗最大高度占屏高比例。
const double _kDialogMaxHeightFactor = 0.8;

/// 弹窗宽度档位：仅在宽屏端用于封顶最大宽度。
enum AppDialogSize {
  compact(360),
  standard(480),
  wide(640);

  const AppDialogSize(this.maxWidth);

  /// 宽屏端的最大宽度上限。
  final double maxWidth;
}

/// 当前可用宽度是否为紧凑（移动）布局。
bool isCompactDialogWidth(BuildContext context) =>
    MediaQuery.sizeOf(context).width < kDialogCompactBreakpoint;

/// 按可用宽度自适应解析弹窗约束：
/// 紧凑端取「屏宽 - 2*margin」全宽；宽屏端按档位封顶。高度统一限制为屏高的 0.8。
BoxConstraints resolveDialogConstraints(
  BuildContext context, {
  AppDialogSize size = AppDialogSize.standard,
}) {
  final mediaSize = MediaQuery.sizeOf(context);
  final compact = mediaSize.width < kDialogCompactBreakpoint;
  final rawMaxWidth = compact
      ? mediaSize.width - kDialogMobileMargin * 2
      : size.maxWidth;
  return BoxConstraints(
    maxWidth: rawMaxWidth < 0 ? 0 : rawMaxWidth,
    maxHeight: mediaSize.height * _kDialogMaxHeightFactor,
  );
}

class AppDialogTheme extends StatelessWidget {
  const AppDialogTheme({super.key, required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    final baseTheme = Theme.of(context);
    return Theme(data: _buildDialogTheme(baseTheme), child: child);
  }
}

Future<T?> showAppDialog<T>({
  required BuildContext context,
  required WidgetBuilder builder,
  bool barrierDismissible = true,
  Color? barrierColor,
  bool useSafeArea = true,
  bool useRootNavigator = true,
  RouteSettings? routeSettings,
}) {
  return showDialog<T>(
    context: context,
    barrierDismissible: barrierDismissible,
    barrierColor: barrierColor,
    useSafeArea: useSafeArea,
    useRootNavigator: useRootNavigator,
    routeSettings: routeSettings,
    builder: (dialogContext) => AppDialogTheme(
      child: Builder(builder: (styledContext) => builder(styledContext)),
    ),
  );
}

Future<T?> showAppGetDialog<T>(
  Widget dialog, {
  bool barrierDismissible = true,
  Color? barrierColor,
}) {
  return Get.dialog<T>(
    AppDialogTheme(child: dialog),
    barrierDismissible: barrierDismissible,
    barrierColor: barrierColor,
  );
}

ThemeData _buildDialogTheme(ThemeData base) {
  final textTheme = base.textTheme;
  final inputTheme = base.inputDecorationTheme;
  final colorScheme = base.colorScheme;
  const inputBorder = OutlineInputBorder(
    borderRadius: BorderRadius.zero,
    borderSide: BorderSide(width: 1),
  );

  TextStyle? shrink(TextStyle? style, double delta) {
    if (style == null) {
      return null;
    }
    final sourceSize = style.fontSize ?? 14;
    final nextSize = (sourceSize - delta).clamp(10.0, 64.0);
    return style.copyWith(fontSize: nextSize.toDouble());
  }

  return base.copyWith(
    dialogTheme: base.dialogTheme.copyWith(
      shape: appDialogShape,
      insetPadding: appDialogInsetPadding,
      constraints: const BoxConstraints(maxWidth: 760),
      titleTextStyle: (textTheme.titleLarge ?? const TextStyle()).copyWith(
        fontSize: kDialogTitleFontSize,
        fontWeight: kDialogTitleFontWeight,
        color: colorScheme.onSurface,
      ),
    ),
    textTheme: textTheme.copyWith(
      titleLarge: shrink(textTheme.titleLarge, 1),
      titleMedium: shrink(textTheme.titleMedium, 1),
      titleSmall: shrink(textTheme.titleSmall, 1),
      bodyLarge: shrink(textTheme.bodyLarge, 1),
      bodyMedium: shrink(textTheme.bodyMedium, 1),
      bodySmall: shrink(textTheme.bodySmall, 1),
      labelLarge: shrink(textTheme.labelLarge, 1),
      labelMedium: shrink(textTheme.labelMedium, 1),
    ),
    inputDecorationTheme: inputTheme.copyWith(
      border: inputBorder.copyWith(
        borderSide: BorderSide(
          color: colorScheme.outline.withValues(alpha: 0.5),
        ),
      ),
      enabledBorder: inputBorder.copyWith(
        borderSide: BorderSide(
          color: colorScheme.outline.withValues(alpha: 0.5),
        ),
      ),
      focusedBorder: inputBorder.copyWith(
        borderSide: BorderSide(color: colorScheme.primary, width: 1.2),
      ),
      errorBorder: inputBorder.copyWith(
        borderSide: BorderSide(color: colorScheme.error),
      ),
      focusedErrorBorder: inputBorder.copyWith(
        borderSide: BorderSide(color: colorScheme.error, width: 1.2),
      ),
      hintStyle: shrink(inputTheme.hintStyle, 1),
      contentPadding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
    ),
  );
}

// ─────────────────────────────────────────────
// 标准弹窗组件族
// ─────────────────────────────────────────────

/// 统一的弹窗按钮样式：紧凑端保证 48 触摸高，危险操作用 error 色。
ButtonStyle _dialogActionStyle(
  BuildContext context, {
  bool compact = false,
  bool destructive = false,
}) {
  return TextButton.styleFrom(
    foregroundColor: destructive ? Theme.of(context).colorScheme.error : null,
    minimumSize: compact ? const Size(64, kDialogButtonMinHeight) : null,
  );
}

/// 将内容约束到自适应宽度（紧凑端全宽、宽屏端按档位封顶）。
Widget _boundedDialogContent(
  BuildContext context,
  AppDialogSize size,
  Widget child,
) {
  return SizedBox(
    width: resolveDialogConstraints(context, size: size).maxWidth,
    child: child,
  );
}

/// 桌面端为弹窗绑定 Esc=取消 / Enter=确认；移动端原样返回。
Widget _withDesktopShortcuts({
  required Widget child,
  VoidCallback? onConfirm,
  VoidCallback? onCancel,
  bool autofocus = true,
}) {
  if (!PlatformCapability.isDesktop) {
    return child;
  }
  return CallbackShortcuts(
    bindings: <ShortcutActivator, VoidCallback>{
      if (onConfirm != null)
        const SingleActivator(LogicalKeyboardKey.enter): onConfirm,
      if (onConfirm != null)
        const SingleActivator(LogicalKeyboardKey.numpadEnter): onConfirm,
      if (onCancel != null)
        const SingleActivator(LogicalKeyboardKey.escape): onCancel,
    },
    child: Focus(autofocus: autofocus, child: child),
  );
}

/// 标准确认框：返回 true 表示确认，false / 关闭表示取消。
Future<bool> showAppConfirmDialog({
  required BuildContext context,
  required String title,
  required String message,
  String? confirmText,
  String? cancelText,
  bool isDestructive = false,
  AppDialogSize size = AppDialogSize.standard,
  bool barrierDismissible = true,
}) async {
  final result = await showAppDialog<bool>(
    context: context,
    barrierDismissible: barrierDismissible,
    builder: (ctx) {
      final compact = isCompactDialogWidth(ctx);
      void cancel() => Navigator.of(ctx).pop(false);
      void confirm() => Navigator.of(ctx).pop(true);
      return _withDesktopShortcuts(
        onConfirm: confirm,
        onCancel: cancel,
        child: AlertDialog(
          title: Text(title),
          content: _boundedDialogContent(ctx, size, Text(message)),
          actions: [
            TextButton(
              style: _dialogActionStyle(ctx, compact: compact),
              onPressed: cancel,
              child: Text(cancelText ?? 'common_cancel'.tr),
            ),
            TextButton(
              style: _dialogActionStyle(
                ctx,
                compact: compact,
                destructive: isDestructive,
              ),
              onPressed: confirm,
              child: Text(confirmText ?? 'common_confirm'.tr),
            ),
          ],
        ),
      );
    },
  );
  return result ?? false;
}

/// 标准信息框：单按钮关闭。
Future<void> showAppMessageDialog({
  required BuildContext context,
  required String title,
  required String message,
  String? dismissText,
  AppDialogSize size = AppDialogSize.standard,
  bool barrierDismissible = true,
}) {
  return showAppDialog<void>(
    context: context,
    barrierDismissible: barrierDismissible,
    builder: (ctx) {
      final compact = isCompactDialogWidth(ctx);
      void dismiss() => Navigator.of(ctx).pop();
      return _withDesktopShortcuts(
        onConfirm: dismiss,
        onCancel: dismiss,
        child: AlertDialog(
          title: Text(title),
          content: _boundedDialogContent(ctx, size, Text(message)),
          actions: [
            TextButton(
              style: _dialogActionStyle(ctx, compact: compact),
              onPressed: dismiss,
              child: Text(dismissText ?? 'common_done'.tr),
            ),
          ],
        ),
      );
    },
  );
}

/// 标准输入框：确认返回输入文本，取消 / 关闭返回 null。
Future<String?> showAppInputDialog({
  required BuildContext context,
  required String title,
  String initialValue = '',
  String? hintText,
  String? helperText,
  int? maxLength,
  String? confirmText,
  String? cancelText,
  AppDialogSize size = AppDialogSize.standard,
  bool barrierDismissible = true,
}) {
  return showAppDialog<String>(
    context: context,
    barrierDismissible: barrierDismissible,
    builder: (ctx) => _AppInputDialog(
      title: title,
      initialValue: initialValue,
      hintText: hintText,
      helperText: helperText,
      maxLength: maxLength,
      confirmText: confirmText,
      cancelText: cancelText,
      size: size,
    ),
  );
}

class _AppInputDialog extends StatefulWidget {
  const _AppInputDialog({
    required this.title,
    required this.initialValue,
    required this.hintText,
    required this.helperText,
    required this.maxLength,
    required this.confirmText,
    required this.cancelText,
    required this.size,
  });

  final String title;
  final String initialValue;
  final String? hintText;
  final String? helperText;
  final int? maxLength;
  final String? confirmText;
  final String? cancelText;
  final AppDialogSize size;

  @override
  State<_AppInputDialog> createState() => _AppInputDialogState();
}

class _AppInputDialogState extends State<_AppInputDialog> {
  late final TextEditingController _controller = TextEditingController(
    text: widget.initialValue,
  );

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _cancel() => Navigator.of(context).pop();
  void _submit() => Navigator.of(context).pop(_controller.text);

  @override
  Widget build(BuildContext context) {
    final compact = isCompactDialogWidth(context);
    return _withDesktopShortcuts(
      onCancel: _cancel,
      autofocus: false,
      child: AlertDialog(
        scrollable: true,
        title: Text(widget.title),
        content: _boundedDialogContent(
          context,
          widget.size,
          TextField(
            controller: _controller,
            autofocus: true,
            maxLength: widget.maxLength,
            onSubmitted: (_) => _submit(),
            decoration: InputDecoration(
              hintText: widget.hintText,
              helperText: widget.helperText,
            ),
          ),
        ),
        actions: [
          TextButton(
            style: _dialogActionStyle(context, compact: compact),
            onPressed: _cancel,
            child: Text(widget.cancelText ?? 'common_cancel'.tr),
          ),
          TextButton(
            style: _dialogActionStyle(context, compact: compact),
            onPressed: _submit,
            child: Text(widget.confirmText ?? 'common_save'.tr),
          ),
        ],
      ),
    );
  }
}

/// 标准内容弹窗：承载任意自定义内容与操作按钮，按档位自适应宽度并内置滚动。
Future<T?> showAppContentDialog<T>({
  required BuildContext context,
  required Widget content,
  String? title,
  List<Widget>? actions,
  AppDialogSize size = AppDialogSize.standard,
  bool barrierDismissible = true,
}) {
  return showAppDialog<T>(
    context: context,
    barrierDismissible: barrierDismissible,
    builder: (ctx) => AlertDialog(
      scrollable: true,
      title: title == null ? null : Text(title),
      content: _boundedDialogContent(ctx, size, content),
      actions: actions,
    ),
  );
}

/// 动作菜单条目。
class AppActionSheetItem {
  const AppActionSheetItem({
    required this.label,
    this.icon,
    this.onTap,
    this.isDestructive = false,
  });

  final String label;
  final IconData? icon;
  final VoidCallback? onTap;
  final bool isDestructive;
}

/// 自适应动作菜单：紧凑端为底部 sheet（拖拽条 + SafeArea），宽屏端为居中弹窗。
Future<void> showAppActionSheet({
  required BuildContext context,
  required List<AppActionSheetItem> items,
  String? title,
  AppDialogSize size = AppDialogSize.standard,
}) {
  if (isCompactDialogWidth(context)) {
    return showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      useSafeArea: true,
      isScrollControlled: true,
      builder: (ctx) => SafeArea(
        child: SingleChildScrollView(
          child: _actionSheetList(ctx, title, items),
        ),
      ),
    );
  }
  return showAppDialog<void>(
    context: context,
    builder: (ctx) => AlertDialog(
      contentPadding: const EdgeInsets.symmetric(vertical: 12),
      content: _boundedDialogContent(
        ctx,
        size,
        SingleChildScrollView(child: _actionSheetList(ctx, title, items)),
      ),
    ),
  );
}

Widget _actionSheetList(
  BuildContext context,
  String? title,
  List<AppActionSheetItem> items,
) {
  final colorScheme = Theme.of(context).colorScheme;
  return Column(
    mainAxisSize: MainAxisSize.min,
    children: [
      if (title != null)
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 8, 16, 12),
          child: Text(
            title,
            style: TextStyle(
              fontSize: kDialogTitleFontSize,
              fontWeight: kDialogTitleFontWeight,
              color: colorScheme.onSurface,
            ),
          ),
        ),
      for (final item in items)
        ListTile(
          leading: item.icon == null
              ? null
              : Icon(
                  item.icon,
                  color: item.isDestructive ? colorScheme.error : null,
                ),
          title: Text(
            item.label,
            style: item.isDestructive
                ? TextStyle(color: colorScheme.error)
                : null,
          ),
          onTap: () {
            Navigator.of(context).pop();
            item.onTap?.call();
          },
        ),
    ],
  );
}
