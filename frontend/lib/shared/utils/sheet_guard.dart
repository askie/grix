import 'package:flutter/widgets.dart';

/// 弹层防重复点击守卫。
///
/// - [SheetGuard.run]：防止同一弹层在关闭前被重复打开（快速连点入口叠出多层）。
/// - [popSheetOnce]：防止弹层内条目被快速双击后重复 pop（多弹掉一层页面）
///   或后续动作被执行两遍。
class SheetGuard {
  SheetGuard._();

  static final Set<String> _openTags = <String>{};

  /// 以 [tag] 标识一类弹层；该弹层未关闭前再次触发直接忽略并返回 null。
  /// [open] 必须 await 弹层的完整生命周期（例如 await showModalBottomSheet）。
  static Future<T?> run<T>(String tag, Future<T?> Function() open) async {
    if (!_openTags.add(tag)) {
      return null;
    }
    try {
      return await open();
    } finally {
      _openTags.remove(tag);
    }
  }

  @visibleForTesting
  static void reset() => _openTags.clear();
}

/// 仅当 [sheetContext] 所在路由仍是栈顶时才 pop 并返回 true。
/// 双击时第二次点击落在退场动画期间，路由已不在栈顶，直接忽略。
bool popSheetOnce<T>(BuildContext sheetContext, [T? result]) {
  final route = ModalRoute.of(sheetContext);
  if (route == null || !route.isCurrent) {
    return false;
  }
  Navigator.of(sheetContext).pop<T>(result);
  return true;
}
