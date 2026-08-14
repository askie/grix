import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/profile/profile_instance_guard.dart';

void main() {
  test('allowForegroundHandoff 在非 Windows 平台是安全的空操作', () {
    // 该方法内部在 Windows 上才会通过 dart:ffi 调 user32；其它平台必须直接返回、
    // 不加载 user32.dll、不抛异常。测试宿主(mac/linux)据此校验平台 guard 生效。
    if (Platform.isWindows) {
      return; // Windows 上的真实前台交接只能在真机验证，这里不做断言。
    }
    expect(ProfileInstanceGuard.allowForegroundHandoff, returnsNormally);
  });
}
