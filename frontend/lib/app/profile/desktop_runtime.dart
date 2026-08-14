import 'dart:io' show Platform;

/// 是否运行在真实桌面端运行时（仅 io 平台使用）。
///
/// flutter_tester（FLUTTER_TEST=true）里没有 path_provider 等宿主插件，
/// widget 测试的 FakeAsync 区中平台通道调用永不返回，一旦误走桌面
/// profile 存储路径整个测试会挂死，因此测试环境必须整体回落非桌面路径。
bool get isDesktopRuntime =>
    (Platform.isMacOS || Platform.isWindows || Platform.isLinux) &&
    !Platform.environment.containsKey('FLUTTER_TEST');
