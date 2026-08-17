import 'dart:io' show Platform;

/// flutter_tester 通过环境变量 FLUTTER_TEST=true 标记测试环境。
bool get isFlutterTestEnv => Platform.environment.containsKey('FLUTTER_TEST');
