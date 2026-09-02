import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

/// 守卫测试：普通信息/确认/输入类弹窗必须走 app_dialog_style 组件族，
/// 不得在 lib/ 中直接使用裸 dialog API。
///
/// 允许的统一入口：showAppConfirmDialog / showAppInputDialog /
/// showAppMessageDialog / showAppContentDialog / showAppActionSheet
/// （底层 showAppDialog / showAppGetDialog）。
///
/// 说明：底部 sheet（showModalBottomSheet / Get.bottomSheet）不在本守卫范围，
/// 简单动作菜单应优先使用 showAppActionSheet，富自定义 sheet 仍可自行实现。
void main() {
  test('lib/ 中不得直接使用裸弹窗 API（应走 app_dialog_style 组件族）', () {
    // 范围外/封装本体：整文件豁免（全屏来电、远程文件选择器、头像裁剪、封装本体）。
    final allowedFiles = <String>{
      'lib/shared/widgets/app_dialog_style.dart',
      'lib/modules/call/call_controller.dart',
      'lib/modules/call/call_dialogs.dart',
      'lib/shared/widgets/remote_file_picker/remote_file_picker.dart',
      'lib/modules/profile/services/avatar_cropper_service.dart',
    };
    // 行级豁免标记：需要保留裸 API 的调用（如范围外的图片预览）加此注释。
    // 格式化会把行尾注释挪到相邻行，因此标记在紧邻的上一行或下一行同样生效。
    const lineMarker = 'dialog-guard-allow';

    final forbidden = RegExp(
      r'(?<![\w$])(showDialog\s*[<(]'
      r'|showCupertinoDialog\s*\('
      r'|CupertinoAlertDialog\s*\('
      r'|Get\.dialog\s*\('
      r'|Get\.defaultDialog\s*\()',
    );

    final violations = <String>[];
    for (final entity in Directory('lib').listSync(recursive: true)) {
      if (entity is! File || !entity.path.endsWith('.dart')) continue;
      final rel = entity.path.replaceAll(r'\', '/');
      if (allowedFiles.contains(rel)) continue;
      final lines = entity.readAsLinesSync();
      final allowedLines = <int>{};
      for (var i = 0; i < lines.length; i++) {
        if (lines[i].contains(lineMarker)) {
          allowedLines.addAll(<int>[i - 1, i, i + 1]);
        }
      }
      for (var i = 0; i < lines.length; i++) {
        final line = lines[i];
        if (allowedLines.contains(i)) continue;
        if (forbidden.hasMatch(line)) {
          violations.add('$rel:${i + 1}: ${line.trim()}');
        }
      }
    }

    expect(
      violations,
      isEmpty,
      reason:
          '发现裸弹窗 API，请改用 showAppConfirmDialog / showAppInputDialog / '
          'showAppMessageDialog / showAppContentDialog / showAppActionSheet：\n'
          '${violations.join('\n')}',
    );
  });
}
